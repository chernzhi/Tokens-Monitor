package main

import "testing"

func TestMatchAIDomainBuiltinDevTools(t *testing.T) {
	s := NewProxyServer(&Config{}, nil, nil, "")
	cases := []struct {
		host, wantVendor string
	}{
		{"api.openrouter.ai", "openrouter"},
		{"api.tabnine.com", "tabnine"},
		{"server.codeium.com", "codeium"},
		{"api.jetbrains.ai", "jetbrains-ai"},
		{"cody-gateway.sourcegraph.com", "sourcegraph-cody"},
		{"codewhisperer.us-east-1.amazonaws.com", "aws-codewhisperer"},
		{"q.us-west-2.amazonaws.com", "aws-q"},
		{"q-fips.us-gov-west-1.amazonaws.com", "aws-q"},
		{"runtime.us-east-1.kiro.dev", "kiro"},
		{"runtime.eu-central-1.kiro.dev", "kiro"},
		{"my-model.inference.azure.com", "azure-inference"},
		// Cursor / Copilot / Claude Code 相关
		{"metrics.cursor.sh", "cursor"},
		{"new.api.cursor.sh", "cursor"},
		{"api.enterprise.githubcopilot.com", "github-copilot"},
		{"foo.githubcopilot.com", "github-copilot"},
		// Codex CLI 走 ChatGPT 登录态：必须走 chatgpt.com，否则永远不进 MITM
		{"chatgpt.com", "openai-codex"},
		{"www.chatgpt.com", "openai-codex"},
		// 阿里 qwen-code OAuth 模式默认上游
		{"portal.qwen.ai", "qwen"},
		{"chat.qwen.ai", "qwen"},
		{"oauth.qwen.ai", "qwen"}, // 通配 *.qwen.ai
		{"dashscope-intl.aliyuncs.com", "qwen"},
	}
	for _, tc := range cases {
		v, ok := s.matchAIDomain(tc.host)
		if !ok || v != tc.wantVendor {
			t.Fatalf("%s: got %q ok=%v want %q", tc.host, v, ok, tc.wantVendor)
		}
	}
}

func TestMatchAIDomainExtraConfig(t *testing.T) {
	cfg := &Config{
		ExtraMonitorHosts: map[string]string{
			"custom.api.example.com": "my-vendor",
		},
		ExtraMonitorSuffixes: []MonitoredSuffix{
			{Suffix: ".corp.llm", Vendor: "corp-llm"},
		},
	}
	s := NewProxyServer(cfg, nil, nil, "")

	v, ok := s.matchAIDomain("custom.api.example.com")
	if !ok || v != "my-vendor" {
		t.Fatalf("exact extra: got %q %v", v, ok)
	}
	v, ok = s.matchAIDomain("svc-east.corp.llm")
	if !ok || v != "corp-llm" {
		t.Fatalf("suffix extra: got %q %v", v, ok)
	}
	_, ok = s.matchAIDomain("unknown.example.com")
	if ok {
		t.Fatal("expected no match")
	}
}

// 默认情况下（MitmCursor 默认 true）cursor 主域应豁免 pinning 进入 MITM 流程；
// nil cfg（极少数代码路径未传配置）保持保守，仍走 pinning。
func TestPinnedTLSHostCursorDefaultAllowsMITM(t *testing.T) {
	if !isPinnedTLSHost("api2.cursor.sh", nil) {
		t.Fatal("nil cfg 时 api2.cursor.sh 应仍在 pinned 名单内")
	}
	if isPinnedTLSHost("api.cursor.com", &Config{}) {
		t.Fatal("默认配置下 MitmCursor 应为 true，api.cursor.com 不应被 pinning 拦截")
	}
}

// 显式关闭 mitm_cursor 后 cursor 主域应回到 pinning 透传（紧急回退路径）。
func TestPinnedTLSHostCursorOptOutPins(t *testing.T) {
	disabled := false
	cfg := &Config{MitmCursor: &disabled}
	if !isPinnedTLSHost("api2.cursor.sh", cfg) {
		t.Fatal("显式关闭 MitmCursor 后 api2.cursor.sh 应回到 pinned 名单")
	}
	if !isPinnedTLSHost("api.cursor.com", cfg) {
		t.Fatal("显式关闭 MitmCursor 后 api.cursor.com 应回到 pinned 名单")
	}
}

// 开启 mitm_cursor 后 cursor 主域应该豁免 pinning，从而进入正常 MITM 流程。
func TestPinnedTLSHostCursorOptInUnpins(t *testing.T) {
	enabled := true
	cfg := &Config{MitmCursor: &enabled}
	if isPinnedTLSHost("api2.cursor.sh", cfg) {
		t.Fatal("启用 MitmCursor 后 api2.cursor.sh 不应再被 pinning 拦截")
	}
	if isPinnedTLSHost("api.cursor.com", cfg) {
		t.Fatal("启用 MitmCursor 后 api.cursor.com 不应再被 pinning 拦截")
	}
	// 其他 pinned 主机不受影响（目前列表里只有 cursor 系，加一个反例守护未来添加）
	if !isPinnedTLSHost("foo.cursor.sh", nil) {
		t.Fatal("nil cfg 时 cursor 仍应被 pinning")
	}
}
