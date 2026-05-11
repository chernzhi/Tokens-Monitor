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
