package main

import "testing"

// v3.1.2 起：auto_install_session_pac 默认 true（双击就启用会话 PAC，关闭自动还原）。
// 这是「双击即监控、关掉即恢复」最贴近用户预期的默认；观察模式只是高级用户的 opt-out。
func TestEffectiveAutoInstallSessionPACDefaultTrue(t *testing.T) {
	var nilCfg *Config
	if !nilCfg.EffectiveAutoInstallSessionPAC() {
		t.Fatal("nil cfg 时默认必须为 true（与 v3.1.2 行为约定一致）")
	}
	empty := &Config{}
	if !empty.EffectiveAutoInstallSessionPAC() {
		t.Fatal("空 Config 默认必须为 true，否则双击 ai-monitor.exe 后 stats 永远是 0")
	}
}

// 显式 true：保持启用。
func TestEffectiveAutoInstallSessionPACExplicitTrue(t *testing.T) {
	on := true
	cfg := &Config{AutoInstallSessionPAC: &on}
	if !cfg.EffectiveAutoInstallSessionPAC() {
		t.Fatal("显式 true 时 EffectiveAutoInstallSessionPAC 应返回 true")
	}
}

// 显式 false（v3.1.0/v3.1.1 老 config / 高级用户主动选择）：依然 opt-out。
func TestEffectiveAutoInstallSessionPACExplicitFalse(t *testing.T) {
	off := false
	cfg := &Config{AutoInstallSessionPAC: &off}
	if cfg.EffectiveAutoInstallSessionPAC() {
		t.Fatal("显式 false 时 EffectiveAutoInstallSessionPAC 应返回 false，不能覆盖用户显式选择")
	}
}

// 默认 MitmCursor 必须为 false（这是配套的核心保护）。
func TestEffectiveMitmCursorDefaultFalseAfterFix(t *testing.T) {
	var nilCfg *Config
	if nilCfg.EffectiveMitmCursor() {
		t.Fatal("nil cfg 时 EffectiveMitmCursor 必须为 false")
	}
	empty := &Config{}
	if empty.EffectiveMitmCursor() {
		t.Fatal("空 Config 默认 EffectiveMitmCursor 必须为 false，否则会把 Cursor 网络弄坏")
	}
}
