package main

import (
	"strings"
	"testing"
)

func TestPSProfileBlockSetsNoProxy(t *testing.T) {
	block := psProfileBlock("http://127.0.0.1:18090", `C:\Users\me\AppData\Roaming\ai-monitor\ca.crt`, "localhost,127.0.0.1,::1")

	for _, want := range []string{
		`$_aiMonitorNoProxy = "localhost,127.0.0.1,::1"`,
		`$env:NO_PROXY`,
		`$env:no_proxy`,
		`$env:http_proxy`,
		`$env:CODEX_CA_CERTIFICATE`,
		`Remove-Item Env:CODEX_CA_CERTIFICATE`,
		`Remove-Item Env:no_proxy`,
	} {
		if !strings.Contains(block, want) {
			t.Fatalf("PowerShell profile block missing %q in:\n%s", want, block)
		}
	}
}
