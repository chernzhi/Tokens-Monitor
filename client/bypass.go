package main

import (
	"net/url"
	"strings"
)

// bypassDomains 是系统代理 / NO_PROXY 共用的直连域名列表。
// 保留内网直连的同时，让 VS Code 扩展市场、CDN、更新等走直连，
// 避免未启动 MITM 或 MITM 仅处理 AI 域名时编辑器无法联网。
var bypassDomains = []string{
	"localhost",
	"127.0.0.1",
	"::1",
	// RFC 1918 私有地址完整覆盖
	"10.*",
	"192.168.*",
	// 172.16.0.0/12 = 172.16.* ~ 172.31.*
	"172.16.*", "172.17.*", "172.18.*", "172.19.*",
	"172.20.*", "172.21.*", "172.22.*", "172.23.*",
	"172.24.*", "172.25.*", "172.26.*", "172.27.*",
	"172.28.*", "172.29.*", "172.30.*", "172.31.*",
	// 链路本地
	"169.254.*",
	"*.local",
	// VS Code / Marketplace / 更新（见官方网络文档常见域名）
	"*.vscode-cdn.net",
	"*.gallery.vsassets.io",
	"marketplace.visualstudio.com",
	"*.vsassets.io",
	"vscodeexperiments.azureedge.net",
	"az764295.vo.msecnd.net",
	"*.vo.msecnd.net",
	"*.vscode-unpkg.net",
	"vscode.blob.core.windows.net",
	"default.exp-tas.com",
	// Microsoft 账户 / 部分 Azure 边缘节点（登录与实验配置）
	"login.microsoftonline.com",
	"*.microsoftonline.com",
	// GitHub 网站本身走直连，但 Copilot API 域名（*.githubcopilot.com / copilot-proxy.githubusercontent.com）不绕过，经 MITM 监控
	"github.com",
	"*.github.com",
}

// mergeBypassDomains returns bypassDomains + cfg.ExtraBypassDomains + existing system
// ProxyOverride entries (from install_state), all deduplicated.
// This ensures the user's existing bypass entries (e.g. corporate intranet domains)
// are preserved even after we overwrite ProxyOverride.
//
// NOTE: This function reads install_state.PreviousProxyOverride at runtime.
// It is safe to call during install (the Previous* fields are the user's
// original values) and during normal operation. However, if install_state
// were ever written with OUR bypass list instead of the user's original,
// this function would self-pollute on subsequent calls. The re-install
// guard in doInstall/doGlobalInstall/webwizard prevents this by reusing
// existingSt.Previous* fields instead of re-reading from registry.
//
// ⚠ MAINTENANCE WARNING: if you refactor doInstall / doGlobalInstall /
// webwizard install paths, you MUST preserve the existingSt guard pattern
// (reuse Previous* from existing install_state instead of re-reading registry).
// Without that guard, a re-install would snapshot our own bypass list as the
// "user's original", causing unbounded growth on every subsequent re-install.
func mergeBypassDomains(cfg *Config) []string {
	seen := make(map[string]struct{}, len(bypassDomains)+32)
	merged := make([]string, 0, len(bypassDomains)+32)

	addUnique := func(d string) {
		d = strings.TrimSpace(d)
		if d == "" || d == "<local>" {
			return
		}
		lower := strings.ToLower(d)
		if _, ok := seen[lower]; !ok {
			seen[lower] = struct{}{}
			merged = append(merged, d)
		}
	}

	// 1. Built-in bypass
	for _, d := range bypassDomains {
		addUnique(d)
	}

	// 2. Config extra bypass
	if cfg != nil {
		for _, d := range cfg.ExtraBypassDomains {
			addUnique(d)
		}
	}

	// 3. User's existing ProxyOverride entries (from install_state snapshot)
	if st := loadInstallState(); st != nil && st.PreviousProxyOverride != "" {
		for _, d := range strings.Split(st.PreviousProxyOverride, ";") {
			addUnique(d)
		}
	}

	return merged
}

// buildProxyBypass 返回 Windows「Internet 代理」的例外列表 (ProxyOverride，分号分隔)。
func buildProxyBypass() string {
	parts := make([]string, 0, len(bypassDomains)+1)
	parts = append(parts, bypassDomains...)
	parts = append(parts, "<local>")
	return strings.Join(parts, ";")
}

// buildProxyBypassWithConfig 使用合并后的 bypass 列表。
func buildProxyBypassWithConfig(cfg *Config) string {
	domains := mergeBypassDomains(cfg)
	parts := make([]string, 0, len(domains)+1)
	parts = append(parts, domains...)
	parts = append(parts, "<local>")
	return strings.Join(parts, ";")
}

// buildNoProxyEnv 返回 NO_PROXY（逗号分隔），须与 buildProxyBypass 的「直连」域名一致。
func buildNoProxyEnv() string {
	return strings.Join(bypassDomains, ",")
}

// buildNoProxyEnvWithConfig includes extra bypass domains from config
// and the reporting server hostname to prevent loops.
func buildNoProxyEnvWithConfig(cfg *Config) string {
	domains := mergeBypassDomains(cfg)
	if cfg != nil && cfg.ServerURL != "" {
		if u, err := url.Parse(cfg.ServerURL); err == nil && u.Hostname() != "" {
			domains = append(domains, u.Hostname())
		}
	}
	return strings.Join(domains, ",")
}
