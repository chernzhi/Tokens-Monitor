package main

import (
	"net/http"
	"net/url"
	"testing"
)

func TestShouldDialUpstreamTargetDirect(t *testing.T) {
	cases := []struct {
		rawURL string
		want   bool
	}{
		{"http://localhost:62142/responses", true},
		{"http://127.0.0.1:62142/responses", true},
		{"http://[::1]:62142/responses", true},
		{"http://169.254.169.254/latest/meta-data", true},
		{"https://api.openai.com/v1/chat/completions", false},
		{"https://chatgpt.com/backend-api/codex/responses", false},
	}
	for _, tc := range cases {
		u, err := url.Parse(tc.rawURL)
		if err != nil {
			t.Fatalf("parse %q: %v", tc.rawURL, err)
		}
		req := &http.Request{URL: u}
		if got := shouldDialUpstreamTargetDirect(req); got != tc.want {
			t.Fatalf("%s: got %v want %v", tc.rawURL, got, tc.want)
		}
	}
}

func TestUpstreamProxyPreserveLoopback(t *testing.T) {
	dummy := &url.URL{Scheme: "http", Host: "10.253.253.253:8118"}
	upstreamAlways := func(*http.Request) (*url.URL, error) { return dummy, nil }

	wrapped := upstreamProxyPreserveLoopback(upstreamAlways)
	local, _ := http.NewRequest(http.MethodPost, "http://localhost:62142/responses", nil)
	u, err := wrapped(local)
	if err != nil || u != nil {
		t.Fatalf("localhost: want direct (nil,nil), got %v err=%v", u, err)
	}
	public, _ := http.NewRequest(http.MethodGet, "https://example.com/foo", nil)
	u2, err := wrapped(public)
	if err != nil {
		t.Fatalf("public: err=%v", err)
	}
	if u2 == nil || u2.String() != dummy.String() {
		t.Fatalf("public: want upstream proxy %s, got %v", dummy, u2)
	}

	wrappedNil := upstreamProxyPreserveLoopback(nil)
	u3, _ := wrappedNil(public)
	if u3 != nil {
		t.Fatalf("nil base: want no proxy")
	}
}
