//go:build windows

package main

import (
	"os/exec"
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
