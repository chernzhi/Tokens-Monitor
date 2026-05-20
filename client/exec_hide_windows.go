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

