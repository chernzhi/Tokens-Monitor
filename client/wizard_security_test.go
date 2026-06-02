package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWizardActionRequiresTokenForRuntimePost(t *testing.T) {
	s := &ProxyServer{wizardToken: "secret-token"}

	missing := httptest.NewRequest(http.MethodPost, "/wizard/api/console/mode", nil)
	if s.authorizeWizardAction(missing) {
		t.Fatal("missing wizard token authorized")
	}

	wrong := httptest.NewRequest(http.MethodPost, "/wizard/api/console/mode", nil)
	wrong.Header.Set("X-AI-Monitor-Wizard-Token", "wrong-token")
	if s.authorizeWizardAction(wrong) {
		t.Fatal("wrong wizard token authorized")
	}

	ok := httptest.NewRequest(http.MethodPost, "/wizard/api/console/mode", nil)
	ok.Header.Set("X-AI-Monitor-Wizard-Token", "secret-token")
	if !s.authorizeWizardAction(ok) {
		t.Fatal("valid wizard token rejected")
	}
}

func TestWizardPageInjectsRuntimeToken(t *testing.T) {
	s := &ProxyServer{cfg: &Config{}, wizardToken: "secret-token"}
	r := httptest.NewRequest(http.MethodGet, "/wizard", nil)
	w := httptest.NewRecorder()

	s.serveWizard(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "secret-token") || !strings.Contains(body, "wizardToken") {
		t.Fatalf("wizard page does not include runtime token: %s", body[:min(len(body), 200)])
	}
}

func TestConsoleLaunchAllowsCustomBinary(t *testing.T) {
	resp := validateConsoleLaunchRequest(consoleLaunchRequest{CustomBinary: `C:\Tools\App\app.exe`, CustomArgs: []string{"--profile", "dev"}})
	if !resp.Success {
		t.Fatalf("custom_binary launch rejected: %q", resp.Message)
	}
}

func TestConsoleLaunchRejectsMixedPresetAndCustomBinary(t *testing.T) {
	resp := validateConsoleLaunchRequest(consoleLaunchRequest{Preset: "vscode", CustomBinary: `C:\Tools\App\app.exe`})
	if resp.Success {
		t.Fatal("mixed preset/custom launch unexpectedly allowed")
	}
	if !strings.Contains(resp.Message, "不能同时") {
		t.Fatalf("message=%q", resp.Message)
	}
}

func TestConsoleLaunchRejectsInvalidCustomBinary(t *testing.T) {
	resp := validateConsoleLaunchRequest(consoleLaunchRequest{CustomBinary: "app.exe\n--bad"})
	if resp.Success {
		t.Fatal("invalid custom_binary unexpectedly allowed")
	}
}

func TestResolveCustomLaunchBinary(t *testing.T) {
	lookPath := func(name string) (string, error) {
		if name == "code.cmd" {
			return `C:\Tools\code.cmd`, nil
		}
		return "", fmt.Errorf("not found")
	}

	// 裸命令名：从 PATH 解析为绝对路径。
	got, err := resolveCustomLaunchBinary("code.cmd", lookPath)
	if err != nil || got != `C:\Tools\code.cmd` {
		t.Fatalf("bare command resolve failed: got=%q err=%v", got, err)
	}

	// 裸命令名不存在：报 PATH 找不到。
	if _, err := resolveCustomLaunchBinary("nope-binary", lookPath); err == nil ||
		!strings.Contains(err.Error(), "PATH") {
		t.Fatalf("expected PATH not-found error, got %v", err)
	}

	// 路径不存在：报找不到应用文件，且不应调用 lookPath。
	noLookPath := func(string) (string, error) {
		t.Fatal("lookPath should not be called for an explicit path")
		return "", nil
	}
	if _, err := resolveCustomLaunchBinary(`C:\__nope__\App\App.exe`, noLookPath); err == nil ||
		!strings.Contains(err.Error(), "找不到应用文件") {
		t.Fatalf("expected missing-file error, got %v", err)
	}

	// 真实存在的绝对路径：原样返回。
	realFile := filepath.Join(t.TempDir(), "app.exe")
	if err := os.WriteFile(realFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := resolveCustomLaunchBinary(realFile, noLookPath); err != nil || got != realFile {
		t.Fatalf("existing path resolve failed: got=%q err=%v", got, err)
	}
}

