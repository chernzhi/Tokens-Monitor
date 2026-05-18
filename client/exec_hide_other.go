//go:build !windows

package main

import "os/exec"

// newHiddenCmd 在非 Windows 平台与 exec.Command 等价；HideWindow 只对 Windows 有意义。
func newHiddenCmd(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}
