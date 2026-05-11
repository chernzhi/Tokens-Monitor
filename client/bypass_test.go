package main

import (
	"strings"
	"testing"
)

func TestBuildNoProxyEnvWithConfigKeepsMandatoryLoopback(t *testing.T) {
	cfg := &Config{BypassDomains: []string{"example.internal"}}
	noProxy := buildNoProxyEnvWithConfig(cfg)

	for _, want := range []string{"localhost", "localhost.localdomain", "127.0.0.1", "127.*", "::1", "[::1]", "169.254.169.254", "example.internal"} {
		if !strings.Contains(noProxy, want) {
			t.Fatalf("NO_PROXY missing %q in %q", want, noProxy)
		}
	}
}

func TestPACKeepsLoopbackDirect(t *testing.T) {
	pac := generatePACContent(18090, nil, "http://upstream.example:8080", "")

	for _, want := range []string{
		`host = (host || "").toLowerCase();`,
		`host === "localhost"`,
		`host === "127.0.0.1"`,
		`host === "169.254.169.254"`,
		`shExpMatch(host, "127.*")`,
		`return "DIRECT"`,
	} {
		if !strings.Contains(pac, want) {
			t.Fatalf("PAC missing %q in:\n%s", want, pac)
		}
	}
}
