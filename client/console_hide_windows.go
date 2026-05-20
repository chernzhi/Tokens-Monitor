//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

// 当用户直接 `go build .`（漏掉 -H=windowsgui）或从快捷方式/explorer 双击启动时，
// Windows 会为 console-subsystem 的 exe 新建一个空白 conhost 窗口，与内嵌
// WebView2 主窗口并列出现，造成「双窗口」体验。
// 这里在 main 早期检测：若控制台进程列表只有我们自己（说明这个窗口是 Windows
// 为我们新分配的、而非父 shell 共享的），就 SW_HIDE 掉它；如果是从 cmd/pwsh
// 启动（父进程共享同一个 console），进程数 >= 2，保持可见，避免把用户的终端藏掉。
func hideOwnedConsoleWindow() {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	user32 := syscall.NewLazyDLL("user32.dll")
	getConsoleWindow := kernel32.NewProc("GetConsoleWindow")
	getConsoleProcessList := kernel32.NewProc("GetConsoleProcessList")
	showWindow := user32.NewProc("ShowWindow")

	hwnd, _, _ := getConsoleWindow.Call()
	if hwnd == 0 {
		return
	}
	var pids [4]uint32
	n, _, _ := getConsoleProcessList.Call(uintptr(unsafe.Pointer(&pids[0])), uintptr(len(pids)))
	if n <= 1 {
		const swHide = 0
		_, _, _ = showWindow.Call(hwnd, swHide)
	}
}
