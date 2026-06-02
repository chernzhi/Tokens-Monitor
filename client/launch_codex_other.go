//go:build !windows

package main

// discoverCodexApp 仅 Windows 有意义；其他平台不支持 Codex 桌面版自动发现。
func discoverCodexApp() (string, bool) { return "", false }
