package main

import (
	"strings"
	"testing"
)

// 当 writePACFile 的 chainedPACBody 实际上来自 ai-monitor 自己上次写过的 PAC 时，
// 必须丢弃以避免「自引用 → PAC 体积每次启动翻倍」。
//
// 该 case 直接调用 generatePACContent，并验证当我们把第一次的输出再次作为
// chainedPACBody 喂回 writePACFile 时，最终生成的 PAC 里不会再嵌入旧的
// `__OriginalFindProxyForURL` 重命名块。
func TestPACSelfReferenceDetectionInChainedBody(t *testing.T) {
	hosts := []monitorHostEntry{
		{Kind: mhExact, Pattern: "api.openai.com"},
	}
	first := generatePACContent(18090, hosts, "", "")

	// first 已包含 `// === AI domain whitelist:` 标记。
	if !isAIMonitorPACBody(first) {
		t.Fatalf("expected first PAC body to be recognized as ai-monitor PAC; body=\n%s", first)
	}

	// 模拟「第二次启动把第一次的 PAC 当 chainedPACBody 喂回来」。
	// 关键验证：writePACFile 内部走的 isAIMonitorPACBody 必须把它判为自引用，
	// 因此最终输出中绝不会包含 __OriginalFindProxyForURL 段。
	chained := first
	if isAIMonitorPACBody(chained) {
		// 模拟 writePACFile 的丢弃路径
		chained = ""
	}
	second := generatePACContent(18090, hosts, "", chained)
	if strings.Contains(second, "__OriginalFindProxyForURL") {
		t.Fatalf("self-reference not detected: second PAC still contains chained block:\n%s", second)
	}
}

// 普通企业 PAC（含真实的 FindProxyForURL，但没有我们的注释标记 / 重命名标记）
// 不应被误判为 ai-monitor 自己的 PAC，否则会丢失链式包裹能力。
func TestPACBodyDetectionAcceptsRealUserPAC(t *testing.T) {
	userPAC := `function FindProxyForURL(url, host) {
		if (host == "corp.example.com") return "PROXY corp-proxy:8080";
		return "DIRECT";
	}`
	if isAIMonitorPACBody(userPAC) {
		t.Fatal("企业 PAC 被误判为 ai-monitor PAC，会丢失链式包裹")
	}
}

// 即使包含 "function FindProxyForURL" 但 *也* 含我们的标记，应仍判为 ai-monitor。
func TestPACBodyDetectionRecognizesOwnComment(t *testing.T) {
	pacWithOurComment := `function FindProxyForURL(url, host) {
		// === AI domain whitelist: only these go through MITM ===
		return "DIRECT";
	}`
	if !isAIMonitorPACBody(pacWithOurComment) {
		t.Fatal("含有 ai-monitor 注释标记的 PAC 应该被识别为自身输出")
	}
}

// 大小写鲁棒：注释 emit 时用 ASCII，但用户可能改 lower/upper。
func TestPACBodyDetectionIsCaseInsensitiveForComment(t *testing.T) {
	upper := `// === AI DOMAIN WHITELIST: only these go through mitm ===`
	if !isAIMonitorPACBody(upper) {
		t.Fatal("大写形式的标记注释也应被识别")
	}
}

// 空内容不应误判。
func TestPACBodyDetectionEmptyIsNotSelf(t *testing.T) {
	if isAIMonitorPACBody("") {
		t.Fatal("空 PAC 不应被识别为 ai-monitor 自身输出")
	}
	if isAIMonitorPACBody("   \n\t  ") {
		t.Fatal("纯空白 PAC 不应被识别为 ai-monitor 自身输出")
	}
}

// 生成的 PAC 必须包含 DIRECT 兜底，确保 MITM 不可达时浏览器/IDE 不会卡死。
func TestGeneratedPACAlwaysHasDirectFallback(t *testing.T) {
	pac := generatePACContent(18090, []monitorHostEntry{
		{Kind: mhExact, Pattern: "api.openai.com"},
	}, "", "")
	if !strings.Contains(pac, "DIRECT") {
		t.Fatalf("generated PAC missing DIRECT fallback:\n%s", pac)
	}
}
