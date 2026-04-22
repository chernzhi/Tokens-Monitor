package main

import "testing"

func TestMatchAIDomainNormalizesHost(t *testing.T) {
	s := &ProxyServer{}
	vendor, ok := s.matchAIDomain("ChatGPT.COM.")
	if !ok || vendor != "openai-codex" {
		t.Fatalf("got vendor=%q ok=%v", vendor, ok)
	}
}
