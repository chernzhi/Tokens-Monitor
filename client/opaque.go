package main

import (
	"encoding/json"
	"strings"
)

// 仅对“看起来像真实模型推理”的 opaque 流量做估算，尽量排除 Cursor 内部服务、CDN、实验配置等噪音。
var opaqueEndpointDenylist = []string{
	"ReportAgentSnapshot",
	"ReportClientNumericMetrics",
	"OnlineMetricsService",
	"analytics",
	"telemetry",
	"extensions-control",
	"/tev1/",
	"rgstr",
	"feature",
	"experiment",
	"statsig",
	"blame",
	"permission",
	"optimizer",
	"usageevents",
	"teammemberusage",
	"spend",
	"cdn",
	"download",
	// Cursor gRPC 配置/元数据接口（非 LLM 推理，不计 token）：
	"availablemodels", // AiService/AvailableModels：模型列表查询
	"getdefaultmodel", // AiService/GetDefaultModelNudgeData 等配置接口
	"nudgedata",       // AiService/GetDefaultModelNudgeData 后缀
	"nudgeconfig",     // 类似配置接口
	"getmodels",       // 各类模型枚举端点
	"modelinfo",       // 模型元数据
	"listmodels",      // 模型列表
}

var opaqueEndpointAllowlist = []string{
	"/chat",
	"streamchat",
	"unifiedchat",
	"/messages",
	"/responses",
	"/completions",
	"/generate",
	"/invoke",
	"/backend-api/conversation",
	"/backend-api/codex",
	"aiservice",
	"cmdkservice",
	"composerservice",
}

var opaqueModelHintDenyKeywords = []string{
	"permission",
	"permissions",
	"optimizer",
	"blame",
	"llamaindex",
	"cursor",
	"cdn",
	"telemetry",
	"analytics",
	"statsig",
	"experiment",
	"feature",
	"judge",
	"summarizer",
	"orientation",
	"feasibility",
	"planmodel",
	"classifier",
	"rerank",
	"embedding",
}

const (
	opaqueSourceEstimate = "client-mitm-estimate"
	opaqueModelSuffix    = "·opaque(估算)"
)

// shouldOpaqueEstimate 是否对无法解析的响应做体积估算上报。
// 规则：非 JSON + 命中推理接口白名单 + 命中真实模型名 + 不命中内部服务黑名单。
func shouldOpaqueEstimate(endpoint, modelHint string, body []byte) bool {
	if len(body) < 16 {
		return false
	}
	// 合法 JSON 已由 ExtractUsage 处理；未取得 usage 时不再用体积估算，避免与「无 usage 字段」的 JSON 双计。
	if json.Valid(body) {
		return false
	}
	ep := strings.ToLower(endpoint)
	for _, s := range opaqueEndpointDenylist {
		if strings.Contains(ep, strings.ToLower(s)) {
			return false
		}
	}
	allowed := false
	for _, s := range opaqueEndpointAllowlist {
		if strings.Contains(ep, strings.ToLower(s)) {
			allowed = true
			break
		}
	}
	if !allowed {
		return false
	}
	return looksLikeBillableOpaqueModelHint(modelHint)
}

func shouldOpaqueEstimateForVendor(vendor, endpoint, modelHint string, body []byte) bool {
	if shouldOpaqueEstimate(endpoint, modelHint, body) {
		return true
	}
	if isChatGPTLikeVendor(vendor) {
		return shouldEstimateChatGPTWeb(endpoint, body)
	}
	// Cursor Agent 任务（Composer/Cmdk）不在请求体里显式带模型名（服务端选模型），
	// 导致 modelHint 为空而跳过估算。对已知推理端点直接按体积估算。
	if vendor == "cursor" {
		return shouldEstimateCursorAgent(endpoint, body)
	}
	return false
}

// shouldEstimateCursorAgent 对 Cursor gRPC 推理端点（AiService/ComposerService/CmdkService）
// 做体积估算。不要求 model hint，因为 Cursor Agent 任务通常由服务端决定用哪个模型。
func shouldEstimateCursorAgent(endpoint string, body []byte) bool {
	if len(body) < 32 {
		return false
	}
	ep := strings.ToLower(endpoint)
	for _, s := range opaqueEndpointDenylist {
		if strings.Contains(ep, strings.ToLower(s)) {
			return false
		}
	}
	return strings.Contains(ep, "aiservice") ||
		strings.Contains(ep, "composerservice") ||
		strings.Contains(ep, "cmdkservice") ||
		strings.Contains(ep, "streamchat")
}

func shouldEstimateChatGPTWeb(endpoint string, body []byte) bool {
	if len(body) < 32 {
		return false
	}
	ep := strings.ToLower(endpoint)
	if !(strings.Contains(ep, "/backend-api/conversation") ||
		strings.Contains(ep, "/backend-api/codex") ||
		strings.Contains(ep, "/responses") ||
		strings.Contains(ep, "/chat")) {
		return false
	}
	text := strings.ToLower(string(body))
	return strings.Contains(text, "data:") ||
		strings.Contains(text, `"message"`) ||
		strings.Contains(text, `"conversation"`) ||
		strings.Contains(text, `"assistant"`) ||
		strings.Contains(text, `"codex"`)
}

// opaqueTokenSplit 估算 opaque 二进制响应（gRPC/protobuf 等）的 token 数。
//
// completion tokens：优先从 gRPC 帧中提取实际文本字节数（÷4），
// 比 body_bytes/4 精确得多（排除帧头和 proto 字段开销，误差从 ±50% 降至 ±20%）。
//
// prompt tokens：优先使用调用方从请求体提取的文本字节数（promptTextBytes÷4）；
// 若未提供则按端点类型用比例推算（以 completion 为基准）。
func opaqueTokenSplit(body []byte, endpoint string, promptTextBytes int) (prompt, completion, total int) {
	if len(body) < 16 {
		return 0, 0, 0
	}

	const maxTok = 500_000

	// —— completion tokens ——
	// 尝试从 gRPC binary 提取可打印文本字节，比 body_bytes/4 精确得多。
	textBytes := extractGRPCTextBytes(body)
	completionBase := textBytes
	if completionBase == 0 {
		// 非 gRPC binary（如 WebSocket 纯文本帧）：退回 body 字节数
		completionBase = len(body)
	}
	completion = completionBase / 4
	if completion < 1 {
		completion = 1
	}
	if completion > maxTok {
		completion = maxTok
	}

	// —— prompt tokens ——
	if promptTextBytes > 0 {
		// 来自请求体的实际对话文本字节数（由 processRequestBody 提供）
		prompt = promptTextBytes / 4
		if prompt < 1 {
			prompt = 1
		}
		if prompt > maxTok {
			prompt = maxTok
		}
	} else {
		// 无请求体信息时，按端点类型估算比例
		// - chat/completion：生成内容为主，prompt 约占 30%
		// - edit/composer：输入更重，prompt 约占 45%
		ep := strings.ToLower(endpoint)
		promptPct := 30
		if strings.Contains(ep, "edit") || strings.Contains(ep, "composer") || strings.Contains(ep, "apply") {
			promptPct = 45
		}
		// 以 completion 为基准反推 prompt（避免 total = completion/(1-pct) 的浮点误差）
		prompt = completion * promptPct / (100 - promptPct)
		if prompt < 1 {
			prompt = 1
		}
	}

	total = prompt + completion
	if total > maxTok {
		// 按比例缩放，保持 prompt/completion 相对关系
		prompt = maxTok * prompt / total
		completion = maxTok - prompt
		total = maxTok
	}
	return prompt, completion, total
}

func opaqueModelLabel(vendor string) string {
	v := strings.TrimSpace(vendor)
	if v == "" {
		v = "unknown"
	}
	return v + opaqueModelSuffix
}

func opaqueModelLabelWithHint(vendor, modelHint string) string {
	m := strings.TrimSpace(modelHint)
	if m != "" {
		if strings.Contains(m, opaqueModelSuffix) {
			return m
		}
		return m + opaqueModelSuffix
	}
	return opaqueModelLabel(vendor)
}

func looksLikeBillableOpaqueModelHint(model string) bool {
	model = strings.ToLower(normalizeModelHint(model))
	if model == "" {
		return false
	}
	for _, keyword := range opaqueModelHintDenyKeywords {
		if strings.Contains(model, keyword) {
			return false
		}
	}
	if strings.Contains(model, ".com") || strings.Contains(model, ".ai") || strings.Contains(model, ".cn") || strings.Contains(model, ".sh") {
		return false
	}
	for _, prefix := range knownModelPrefixes {
		if strings.HasPrefix(model, prefix) {
			return true
		}
	}
	return false
}
