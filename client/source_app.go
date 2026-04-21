package main

import (
	"net/http"
	"strings"
)

// sourceAppPattern maps a substring (case-insensitive) to its canonical app key.
type sourceAppPattern struct {
	substr string
	app    string
}

// editorVersionPatterns checks the "editor-version" header first (more specific for Copilot traffic).
var editorVersionPatterns = []sourceAppPattern{
	{"cursor/", "cursor"},
	{"vscode/", "vscode"},
	{"windsurf/", "windsurf"},
	{"kiro/", "kiro"},
	{"trae/", "trae"},
	{"qoder/", "qoder"},
	{"qoder-", "qoder"}, // 兼容 Qoder-2.x 等带连字符的格式
}

// userAgentPatterns checks User-Agent for known IDE/tool identifiers.
// Order matters: more specific patterns first, generic "vscode" last as fallback.
var userAgentPatterns = []sourceAppPattern{
	{"cursor/", "cursor"},
	{"codium/", "vscodium"},
	{"vscodium/", "vscodium"},
	{"windsurf/", "windsurf"},
	{"kiro/", "kiro"},
	{"trae/", "trae"},
	{"qoder/", "qoder"},
	{"jetbrains", "jetbrains"},
	{"intellij", "jetbrains"},
	// Claude Code CLI: User-Agent 格式 "claude-code/X.X.X" 或 "anthropic-cli/X.X.X"
	// claude.ai 网页版 / OAuth 流也可能带 "claude/"
	{"claude-code/", "claude"},
	{"anthropic-cli/", "claude"},
	{"claude/", "claude"},
	{"codex/", "codex"},
	{"opencode/", "opencode"},
	// vscode last — many forks also contain "vscode" in their UA
	{"vscode/", "vscode"},
}

// inferSourceAppFromHeaders inspects HTTP request headers to identify which
// IDE or tool made the request. Returns "" if no match is found.
func inferSourceAppFromHeaders(h http.Header) string {
	// Priority 1: editor-version header (Copilot / GitHub API traffic)
	if ev := h.Get("Editor-Version"); ev != "" {
		evLower := strings.ToLower(ev)
		for _, p := range editorVersionPatterns {
			if strings.Contains(evLower, p.substr) {
				return p.app
			}
		}
	}

	// Priority 2: User-Agent header
	if ua := h.Get("User-Agent"); ua != "" {
		uaLower := strings.ToLower(ua)
		for _, p := range userAgentPatterns {
			if strings.Contains(uaLower, p.substr) {
				return p.app
			}
		}
	}

	return ""
}
