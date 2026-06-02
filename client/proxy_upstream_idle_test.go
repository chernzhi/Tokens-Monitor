package main

import (
	"net/http"
	"net/url"
	"testing"
)

func TestUpstreamBodyIdleForReq_CodexResponsesDisablesIdle(t *testing.T) {
	req := &http.Request{
		URL: &url.URL{Path: "/backend-api/codex/responses/compact"},
	}
	if got := upstreamBodyIdleForReq(req); got != 0 {
		t.Fatalf("compact: want idle 0, got %v", got)
	}
	if got := upstreamBodyIdleForReq(nil); got != upstreamBodyIdleTimeout {
		t.Fatalf("nil req: want default %v, got %v", upstreamBodyIdleTimeout, got)
	}
	normal := &http.Request{URL: &url.URL{Path: "/backend-api/codex/responses"}}
	if got := upstreamBodyIdleForReq(normal); got != 0 {
		t.Fatalf("responses: want idle 0, got %v", got)
	}
	otherModel := &http.Request{URL: &url.URL{Path: "/v1/chat/completions"}}
	if got := upstreamBodyIdleForReq(otherModel); got != upstreamBodyIdleTimeout {
		t.Fatalf("other API: want default %v, got %v", upstreamBodyIdleTimeout, got)
	}
}

func TestUpstreamBodyIdle_StreamingDisablesIdle(t *testing.T) {
	// 任意厂商的流式响应（SSE / chunked / 无 Content-Length）都应禁用 idle timer，
	// 避免模型长推理时 5 分钟无字节被 cancel，导致 stream disconnected。
	chat := &http.Request{URL: &url.URL{Path: "/v1/chat/completions"}}

	sse := http.Header{"Content-Type": []string{"text/event-stream"}}
	if got := upstreamBodyIdle(chat, sse); got != 0 {
		t.Fatalf("SSE chat/completions: want idle 0, got %v", got)
	}

	chunked := http.Header{
		"Content-Type":      []string{"application/json"},
		"Transfer-Encoding": []string{"chunked"},
	}
	messages := &http.Request{URL: &url.URL{Path: "/v1/messages"}}
	if got := upstreamBodyIdle(messages, chunked); got != 0 {
		t.Fatalf("chunked /v1/messages: want idle 0, got %v", got)
	}

	noLen := http.Header{"Content-Type": []string{"application/json"}}
	if got := upstreamBodyIdle(chat, noLen); got != 0 {
		t.Fatalf("no Content-Length: want idle 0, got %v", got)
	}

	// 非流式（带 Content-Length 的整块 JSON）保留默认 idle，用于检测卡死连接。
	whole := http.Header{
		"Content-Type":   []string{"application/json"},
		"Content-Length": []string{"128"},
	}
	if got := upstreamBodyIdle(chat, whole); got != upstreamBodyIdleTimeout {
		t.Fatalf("non-stream chat/completions: want default %v, got %v", upstreamBodyIdleTimeout, got)
	}

	// Codex 路径即便是非流式整块响应，也始终禁用 idle（沿用 path 特例）。
	codex := &http.Request{URL: &url.URL{Path: "/backend-api/codex/responses"}}
	if got := upstreamBodyIdle(codex, whole); got != 0 {
		t.Fatalf("codex non-stream: want idle 0, got %v", got)
	}
}
