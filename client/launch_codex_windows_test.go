//go:build windows

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindCodexUnderAppxInstallLocation(t *testing.T) {
	root := t.TempDir()
	appDir := filepath.Join(root, "app")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(appDir, "Codex.exe")
	if err := os.WriteFile(exe, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, ok := findCodexUnder(root)
	if !ok || got != exe {
		t.Fatalf("findCodexUnder()=(%q,%v), want %q,true", got, ok, exe)
	}
}
