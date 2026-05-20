package main

import (
	"bytes"
	"html/template"
	"strings"
	"testing"
)

func TestWizardTemplateRendersForFirstTimeSetup(t *testing.T) {
	tmpl, err := template.New("wizard").Parse(webWizardHTML)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	data := struct {
		UserName     string
		ServerURL    string
		BasePath     string
		WizardToken  string
		FirstInstall bool
	}{
		UserName:     "test",
		ServerURL:    "https://example.com",
		BasePath:     "",
		WizardToken:  "",
		FirstInstall: true,
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		t.Fatalf("execute: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "var wizardToken = ") {
		t.Fatalf("missing wizardToken line")
	}
	if !strings.Contains(out, "</html>") {
		t.Fatalf("HTML truncated, no </html> close tag (len=%d)", len(out))
	}
	// FirstInstall=true must hide the stats/mode/launcher panels.
	if strings.Contains(out, "使用统计") {
		t.Fatalf("FirstInstall=true should hide 使用统计 panel")
	}
	if strings.Contains(out, "一键模式切换") {
		t.Fatalf("FirstInstall=true should hide 一键模式切换 panel")
	}
	if strings.Contains(out, "一键启动编辑器") {
		t.Fatalf("FirstInstall=true should hide launcher panel")
	}
	if !strings.Contains(out, "一键安装") {
		t.Fatalf("install button must still be visible")
	}
	t.Logf("first-install: rendered %d bytes successfully", len(out))
}

func TestWizardTemplateRendersForRuntime(t *testing.T) {
	tmpl, err := template.New("wizard").Parse(webWizardHTML)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	data := struct {
		UserName     string
		UserID       string
		ServerURL    string
		BasePath     string
		WizardToken  string
		FirstInstall bool
	}{
		UserName:     "test",
		UserID:       "u1",
		ServerURL:    "https://example.com",
		BasePath:     "/wizard",
		WizardToken:  "abc",
		FirstInstall: false,
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		t.Fatalf("execute: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "使用统计") || !strings.Contains(out, "一键模式切换") || !strings.Contains(out, "一键启动编辑器") {
		t.Fatalf("FirstInstall=false should show all runtime panels")
	}
	t.Logf("runtime: rendered %d bytes successfully", len(out))
}
