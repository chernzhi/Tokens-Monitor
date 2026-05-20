//go:build !windows

package main

import "os/exec"

// newHiddenCmd 在非 Windows 平台与 exec.Command 等价；HideWindow 只对 Windows 有意义。
func newHiddenCmd(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}

// newDetachedCmd 在非 Windows 平台与 exec.Command 等价；
// Windows 上才有"绕过 cmd 包装、走 DETACHED_PROCESS"的差异。
func newDetachedCmd(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}

