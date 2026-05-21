//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// doForceCleanup 强制清理本机所有 ai-monitor.exe 进程 + 删除单例标记。
// 不动 PAC / 环境变量 / 注册表（那是 --cleanup-network 的职责）。
func doForceCleanup() {
	fmt.Println("  ══════════════════════════════════════════")
	fmt.Println("  强制清理 ai-monitor 残留进程与锁文件")
	fmt.Println("  ══════════════════════════════════════════")

	myPID := os.Getpid()
	pids := listAIMonitorPIDs()
	killed := 0
	for _, pid := range pids {
		if pid == myPID {
			continue
		}
		cmd := exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/T", "/F")
		if err := cmd.Run(); err != nil {
			fmt.Printf("    ⚠ taskkill PID %d 失败: %v\n", pid, err)
			continue
		}
		fmt.Printf("    ✓ 已杀进程 PID %d\n", pid)
		killed++
	}
	if killed == 0 {
		fmt.Println("    — 未发现其他 ai-monitor 进程")
	}

	for _, p := range []string{instanceInfoPath(), singletonLockPath()} {
		if err := os.Remove(p); err == nil {
			fmt.Printf("    ✓ 已删除 %s\n", p)
		} else if !os.IsNotExist(err) {
			fmt.Printf("    ⚠ 删除 %s 失败: %v\n", p, err)
		}
	}

	fmt.Println()
	fmt.Println("  ✓ 清理完成，可重新启动 ai-monitor.exe")
}

func listAIMonitorPIDs() []int {
	out, err := exec.Command("tasklist", "/FI", "IMAGENAME eq ai-monitor.exe", "/FO", "CSV", "/NH").Output()
	if err != nil {
		return nil
	}
	var pids []int
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "INFO:") {
			continue
		}
		parts := strings.Split(line, ",")
		if len(parts) < 2 {
			continue
		}
		pidStr := strings.Trim(parts[1], `" `)
		if pid, err := strconv.Atoi(pidStr); err == nil {
			pids = append(pids, pid)
		}
	}
	return pids
}
