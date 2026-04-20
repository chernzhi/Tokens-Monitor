package main

import (
	"net/http"
	"net/url"
	"testing"
)

func TestPatchGitHubCopilotClaudeMessages_minimalAdaptive(t *testing.T) {
	u, _ := url.Parse("https://api.individual.githubcopilot.com/v1/messages")
	r := &http.Request{URL: u, Host: u.Host}

	data := map[string]interface{}{
		"model": "claude-opus-4.7",
		"thinking": map[string]interface{}{
			"type": "enabled",
		},
	}
	if !patchGitHubCopilotClaudeMessages(r, data) {
		t.Fatal("expected patch applied")
	}
	th := data["thinking"].(map[string]interface{})
	if th["type"] != "adaptive" || len(th) != 1 {
		t.Fatalf("want only type=adaptive, got %#v", th)
	}
}

func TestPatchGitHubCopilotClaudeMessages_stripsNestedAdaptiveBudget(t *testing.T) {
	u, _ := url.Parse("https://api.individual.githubcopilot.com/v1/messages")
	r := &http.Request{URL: u, Host: u.Host}
	data := map[string]interface{}{
		"thinking": map[string]interface{}{
			"type": "adaptive",
			"adaptive": map[string]interface{}{
				"budget_tokens": float64(8000),
			},
		},
	}
	if !patchGitHubCopilotClaudeMessages(r, data) {
		t.Fatal("expected patch")
	}
	th := data["thinking"].(map[string]interface{})
	if _, bad := th["adaptive"]; bad {
		t.Fatal("nested adaptive should be removed")
	}
}

func TestPatchGitHubCopilotClaudeMessages_injectsForClearThinking(t *testing.T) {
	u, _ := url.Parse("https://api.individual.githubcopilot.com/v1/messages")
	r := &http.Request{URL: u, Host: u.Host}
	data := map[string]interface{}{
		"model": "claude-opus-4.7",
		"context_management": map[string]interface{}{
			"edits": []interface{}{
				map[string]interface{}{"keep": "all", "type": "clear_thinking_20251015"},
			},
		},
		"output_config": map[string]interface{}{"effort": "medium"},
	}
	if !patchGitHubCopilotClaudeMessages(r, data) {
		t.Fatal("expected patch")
	}
	th := data["thinking"].(map[string]interface{})
	if th["type"] != "adaptive" {
		t.Fatalf("got %#v", th)
	}
}

func TestPatchGitHubCopilotClaudeMessages_skipsOtherHosts(t *testing.T) {
	u, _ := url.Parse("https://api.anthropic.com/v1/messages")
	r := &http.Request{URL: u, Host: u.Host}
	data := map[string]interface{}{
		"thinking": map[string]interface{}{"type": "enabled"},
	}
	if patchGitHubCopilotClaudeMessages(r, data) {
		t.Fatal("must not rewrite direct Anthropic")
	}
}

func TestPatchGitHubCopilotClaudeMessages_noOpWhenIrrelevant(t *testing.T) {
	u, _ := url.Parse("https://api.individual.githubcopilot.com/v1/messages")
	r := &http.Request{URL: u, Host: u.Host}
	data := map[string]interface{}{"model": "claude-opus-4.7"}
	if patchGitHubCopilotClaudeMessages(r, data) {
		t.Fatal("expected no change")
	}
}
