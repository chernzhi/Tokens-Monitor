package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProxyURLToDialAddress(t *testing.T) {
	t.Parallel()
	cases := []struct {
		raw     string
		want    string
		wantLB  bool
		wantErr bool
	}{
		{"", "", false, false},
		{"http://127.0.0.1:8089", "127.0.0.1:8089", true, false},
		{"http://[::1]:1234", "[::1]:1234", true, false},
		{"socks5://127.0.0.1:1080", "127.0.0.1:1080", true, false},
		{"http://198.51.100.1:80", "198.51.100.1:80", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			t.Parallel()
			addr, loopback, err := proxyURLToDialAddress(tc.raw)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err: got %v wantErr=%v", err, tc.wantErr)
			}
			if addr != tc.want {
				t.Fatalf("addr: got %q want %q", addr, tc.want)
			}
			if loopback != tc.wantLB {
				t.Fatalf("loopback: got %v want %v", loopback, tc.wantLB)
			}
		})
	}
}

func TestIsLoopbackHost(t *testing.T) {
	t.Parallel()
	if !isLoopbackHost("127.0.0.1") {
		t.Fatal("127.0.0.1")
	}
	if !isLoopbackHost("localhost") {
		t.Fatal("localhost")
	}
	if isLoopbackHost("198.51.100.1") {
		t.Fatal("public")
	}
}

func TestResolveLocalUpstreamWithFallback_usesAppDataWhenLocalDead(t *testing.T) {
	tmp := t.TempDir()
	local := filepath.Join(tmp, "config.json")
	roam := filepath.Join(tmp, "roam", "ai-monitor")
	if err := os.MkdirAll(roam, 0755); err != nil {
		t.Fatal(err)
	}
	appdata := filepath.Join(roam, "config.json")
	if err := os.WriteFile(local, []byte(`{
  "server_url": "https://example.com",
  "port": 18090,
  "upstream_proxy": "http://127.0.0.1:18091"
}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(appdata, []byte(`{
  "server_url": "https://example.com",
  "port": 18090,
  "upstream_proxy": "http://127.0.0.1:18088"
}`), 0600); err != nil {
		t.Fatal(err)
	}
	sctx := &selfCheckContext{
		roamingConfigPath: appdata,
		dial: func(addr string) error {
			// 勿用 18090–18153 段：isSelfProxy 会误判为本 MITM 端口
			if addr == "127.0.0.1:18088" {
				return nil
			}
			return fmt.Errorf("nope")
		},
	}
	cfg, err := LoadConfig(local)
	if err != nil {
		t.Fatal(err)
	}
	resolveLocalUpstreamWithFallback(cfg, local, sctx)
	if cfg.UpstreamProxy != "http://127.0.0.1:18088" {
		t.Fatalf("got upstream %q", cfg.UpstreamProxy)
	}
	b, err := os.ReadFile(local)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "18088") {
		t.Fatalf("local file not patched: %s", string(b))
	}
}
