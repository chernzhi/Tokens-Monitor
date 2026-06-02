//go:build windows

package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// newHiddenCmd 返回一个不会弹出 conhost 黑窗的 exec.Cmd。
// 用于替换所有调用外部命令（reg/certutil/powershell/schtasks/taskkill 等）的场景，
// 避免运行期周期性任务（reporter detectUpstreamProxy 等）在屏幕上闪烁黑色控制台。
func newHiddenCmd(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
	return cmd
}

// newDetachedCmd 直接 spawn 目标进程并完全脱钩父进程，不经过 cmd.exe / start。
// 比 `cmd /c start "" target` 少一层 cmd+conhost，能显著缓解长时间运行后
// 桌面堆 (Desktop Heap) 耗尽导致的 "Not enough memory resources" CreateProcess 失败。
//
// 仅适用于 .exe 目标。若 name 是 .bat/.cmd/.ps1 shim，自动回退到 newHiddenCmd("cmd","/c","start",...)
// 形式，因为非 PE 文件必须由 cmd 解释。
//
// 调用方仍需自行 .Start()；不要 .Run() / .Wait()，否则就不是 detach 了。
func newDetachedCmd(name string, args ...string) *exec.Cmd {
	ext := strings.ToLower(filepath.Ext(name))
	if ext == ".bat" || ext == ".cmd" || ext == ".ps1" {
		// shim：仍需 cmd 包装，但用 start /b 避免新建可见窗口
		parts := append([]string{"/c", "start", "/b", ""}, append([]string{name}, args...)...)
		return newHiddenCmd("cmd", parts...)
	}
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow: true,
		// DETACHED_PROCESS (0x8) | CREATE_NEW_PROCESS_GROUP (0x200) | CREATE_NO_WINDOW (0x08000000)
		// 子进程不会继承 stdio，也不会附着到任何控制台，相当于 `start /b` 的效果但少一层 cmd。
		CreationFlags: 0x00000008 | 0x00000200 | 0x08000000,
	}
	return cmd
}

// newDetachedGuiCmd 与 newDetachedCmd 一样脱钩父进程的控制台，但**绝不设置
// HideWindow**。专用于「重新拉起本程序（GUI 子系统 / WebView2 窗口）」的场景。
//
// 关键原因（Windows 坑）：HideWindow=true 会让 Go 在 STARTUPINFO 里置位
// STARTF_USESHOWWINDOW 且 wShowWindow=SW_HIDE。被拉起进程「第一次调用
// ShowWindow」时，按 MSDN 规则会忽略自己传入的 nCmdShow，改用 STARTUPINFO 里的
// SW_HIDE —— 于是 WebView2 主窗口初始即隐藏，进程在跑却「没有窗口弹出」，
// 表现为更新后自动重启「没有起来」。这里去掉 HideWindow，让窗口正常按
// SW_SHOW 显示。
//
// 目标是 -H windowsgui 构建的无控制台 GUI 二进制，因此不设 HideWindow 也不会
// 闪出黑色控制台；仍保留 DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP 以与
// 派发它的旧/swap 进程彻底脱钩。
func newDetachedGuiCmd(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		// 不要 HideWindow / 不要 CREATE_NO_WINDOW —— 它们会强制 SW_HIDE，隐藏 GUI 主窗口。
		// DETACHED_PROCESS (0x8) | CREATE_NEW_PROCESS_GROUP (0x200)
		CreationFlags: 0x00000008 | 0x00000200,
	}
	return cmd
}

// newConsoleCmd 为目标进程**新建一个可见的控制台窗口**（CREATE_NEW_CONSOLE）。
// 专用于从仪表盘一键启动「终端类 CLI 工具」（Codex CLI / Claude Code CLI 等）：
// 这类 TUI 需要附着到一个真实控制台才能交互，若用 newDetachedCmd（DETACHED +
// CREATE_NO_WINDOW）启动会没有任何窗口、用户看不到也没法输入，等于没起来。
//
// 不设 HideWindow —— 我们就是要让这个新控制台显示出来。CREATE_NEW_PROCESS_GROUP
// 让它与 ai-monitor 的 Ctrl 信号组隔离，关闭 CLI 不会波及监控进程。
func newConsoleCmd(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		// CREATE_NEW_CONSOLE (0x10) | CREATE_NEW_PROCESS_GROUP (0x200)
		CreationFlags: 0x00000010 | 0x00000200,
	}
	return cmd
}
