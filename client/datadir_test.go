package main

import (
	"log"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCleanupInstallDataDirRemovesGeneratedFiles(t *testing.T) {
	dataDir := testAppDataDir(t)
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatalf("create data dir: %v", err)
	}
	for _, name := range []string{"config.json", "identity.json", "install_state.json", "instance.json", "proxy.pac", "ca.crt", "ca.key", "ai-monitor.log", "ai-monitor.log.1"} {
		if err := os.WriteFile(filepath.Join(dataDir, name), []byte("test"), 0600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	setupFileLogging(dataDir)
	log.Print("cleanup test")

	if err := cleanupInstallDataDir(); err != nil {
		t.Fatalf("cleanupInstallDataDir() error = %v", err)
	}
	if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
		t.Fatalf("data dir still exists after cleanup: %v", err)
	}
}

func testAppDataDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	switch runtime.GOOS {
	case "windows":
		t.Setenv("APPDATA", root)
		return filepath.Join(root, "ai-monitor")
	case "darwin":
		t.Setenv("HOME", root)
		return filepath.Join(root, ".config", "ai-monitor")
	default:
		t.Setenv("XDG_DATA_HOME", root)
		return filepath.Join(root, "ai-monitor")
	}
}
