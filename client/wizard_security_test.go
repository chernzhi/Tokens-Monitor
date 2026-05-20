package main

import (
	"net/http"
	"net/http/httptest"
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

func TestConsoleLaunchRejectsCustomBinary(t *testing.T) {
	resp := validateConsoleLaunchRequest(consoleLaunchRequest{CustomBinary: "powershell.exe", CustomArgs: []string{"-Command", "whoami"}})
	if resp.Success {
		t.Fatal("custom_binary launch unexpectedly allowed")
	}
	if !strings.Contains(resp.Message, "preset") {
		t.Fatalf("message=%q", resp.Message)
	}
}
