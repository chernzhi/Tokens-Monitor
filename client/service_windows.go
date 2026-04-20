//go:build windows

package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const taskName = "AIMonitorAutoStart"
const healTaskName = "AIMonitorHeal"

// startupShortcutPath returns the path to the startup folder shortcut.
func startupShortcutPath() string {
	appData := os.Getenv("APPDATA")
	return filepath.Join(appData, `Microsoft\Windows\Start Menu\Programs\Startup`, "ai-monitor.lnk")
}

// installAutoStart registers ai-monitor to run at user logon.
// Strategy: try schtasks first (works on most systems); if "Access is denied",
// fall back to creating a shortcut in the user's Startup folder (no admin needed).
func installAutoStart(configPath string) error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取可执行文件路径失败: %w", err)
	}
	exePath, _ = filepath.Abs(exePath)
	absConfig, _ := filepath.Abs(configPath)

	args := fmt.Sprintf(`--config "%s"`, absConfig)

	// Try schtasks first
	cmd := exec.Command("schtasks", "/Create",
		"/TN", taskName,
		"/TR", fmt.Sprintf(`"%s" %s`, exePath, args),
		"/SC", "ONLOGON",
		"/RL", "LIMITED",
		"/F",
	)
	output, err := cmd.CombinedOutput()
	if err == nil {
		log.Printf("[service] 已注册开机自启任务 %q (schtasks)", taskName)
		removeStartupShortcut() // clean up shortcut if exists from previous fallback
		// 同时注册自愈任务：主任务起不来时也能恢复代理。失败不动探主流程。
		if herr := installHealTask(exePath, absConfig); herr != nil {
			log.Printf("[service] 注册自愈任务失败（不影响开机自启）: %v", herr)
		}
		return nil
	}

	outStr := strings.TrimSpace(string(output))
	if !strings.Contains(strings.ToLower(outStr), "access") &&
		!strings.Contains(outStr, "拒绝") {
		return fmt.Errorf("创建计划任务失败: %w\n%s", err, outStr)
	}

	// Fallback: create shortcut in Startup folder (no admin needed)
	log.Printf("[service] schtasks 权限不足，改用启动文件夹快捷方式")
	return createStartupShortcut(exePath, absConfig)
}

// installHealTask 注册 "AIMonitorHeal" 计划任务：每次登录运行 `ai-monitor.exe --heal`。
// 该任务是主进程之外的安全网：如果 ai-monitor 被强杀/崩溃后未重启，下一次
// 登录会主动清理指向 dead 端口的代理设置，避免污染 VS Code/Cursor 等应用。
func installHealTask(exePath, configPath string) error {
	cmd := exec.Command("schtasks", "/Create",
		"/TN", healTaskName,
		"/TR", fmt.Sprintf(`"%s" --heal --config "%s"`, exePath, configPath),
		"/SC", "ONLOGON",
		"/RL", "LIMITED",
		"/F",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	log.Printf("[service] 已注册自愈任务 %q", healTaskName)
	return nil
}

// uninstallHealTask 移除自愈计划任务。任务不存在不报错。
func uninstallHealTask() error {
	cmd := exec.Command("schtasks", "/Delete", "/TN", healTaskName, "/F")
	output, err := cmd.CombinedOutput()
	if err != nil {
		outStr := strings.TrimSpace(string(output))
		if strings.Contains(outStr, "ERROR: The system cannot find") ||
			strings.Contains(outStr, "错误: 系统找不到") {
			return nil
		}
		return fmt.Errorf("删除自愈计划任务失败: %w\n%s", err, outStr)
	}
	log.Printf("[service] 已移除自愈任务 %q", healTaskName)
	return nil
}

// createStartupShortcut creates a .lnk shortcut in the user's Startup folder
// using a VBScript one-liner (no external dependencies).
func createStartupShortcut(exePath, configPath string) error {
	lnkPath := startupShortcutPath()
	workDir := filepath.Dir(exePath)

	// Use PowerShell to create the shortcut — more reliable than VBScript
	script := fmt.Sprintf(
		`$ws = New-Object -ComObject WScript.Shell; `+
			`$s = $ws.CreateShortcut('%s'); `+
			`$s.TargetPath = '%s'; `+
			`$s.Arguments = '--config "%s"'; `+
			`$s.WorkingDirectory = '%s'; `+
			`$s.WindowStyle = 7; `+
			`$s.Save()`,
		lnkPath, exePath, configPath, workDir,
	)

	cmd := exec.Command("powershell", "-NoProfile", "-Command", script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("创建启动快捷方式失败: %w\n%s", err, string(output))
	}
	log.Printf("[service] 已创建启动快捷方式: %s", lnkPath)
	return nil
}

// removeStartupShortcut removes the Startup folder shortcut if it exists.
func removeStartupShortcut() {
	lnkPath := startupShortcutPath()
	if _, err := os.Stat(lnkPath); err == nil {
		os.Remove(lnkPath)
		log.Printf("[service] 已移除启动快捷方式: %s", lnkPath)
	}
}

// uninstallWatchdogTask removes the watchdog scheduled task left by pre-PAC installs.
func uninstallWatchdogTask() error {
	const watchdogTaskName = "AIMonitorWatchdog"
	cmd := exec.Command("schtasks", "/Delete", "/TN", watchdogTaskName, "/F")
	output, err := cmd.CombinedOutput()
	if err != nil {
		outStr := strings.TrimSpace(string(output))
		if strings.Contains(outStr, "ERROR: The system cannot find") ||
			strings.Contains(outStr, "错误: 系统找不到") {
			return nil
		}
		return fmt.Errorf("删除看门狗计划任务失败: %w\n%s", err, outStr)
	}
	log.Printf("[service] 已移除网络看门狗任务 %q", watchdogTaskName)
	return nil
}

// uninstallAutoStart removes both the scheduled task and Startup shortcut.
func uninstallAutoStart() error {
	// Remove scheduled task
	cmd := exec.Command("schtasks", "/Delete", "/TN", taskName, "/F")
	output, err := cmd.CombinedOutput()
	if err != nil {
		outStr := strings.TrimSpace(string(output))
		if !strings.Contains(outStr, "ERROR: The system cannot find") &&
			!strings.Contains(outStr, "错误: 系统找不到") {
			log.Printf("[service] 删除计划任务失败: %s", outStr)
		}
	} else {
		log.Printf("[service] 已移除开机自启任务 %q", taskName)
	}

	// Remove Startup shortcut
	removeStartupShortcut()

	// Remove heal task (不影响主进程，但与该函数语义成对)
	if err := uninstallHealTask(); err != nil {
		log.Printf("[service] 移除自愈任务失败: %v", err)
	}

	return nil
}

// isAutoStartInstalled checks if auto-start is configured (either schtasks or shortcut).
func isAutoStartInstalled() bool {
	cmd := exec.Command("schtasks", "/Query", "/TN", taskName)
	if cmd.Run() == nil {
		return true
	}
	if _, err := os.Stat(startupShortcutPath()); err == nil {
		return true
	}
	return false
}

// startBackgroundInstance starts ai-monitor detached from the current console.
func startBackgroundInstance(configPath string) error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取可执行文件路径失败: %w", err)
	}
	exePath, _ = filepath.Abs(exePath)
	absConfig, _ := filepath.Abs(configPath)

	cmd := exec.Command("cmd", "/C", "start", "/b", "",
		exePath, "--config", absConfig)
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	return cmd.Start()
}
