package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"time"
)

// runHealMode 是离线/外部恢复入口：当 ai-monitor 进程异常退出（崩溃、被杀、断电）
// 而系统代理 / 用户级 HTTP_PROXY 还指向已 dead 的 MITM 端口时，恢复用户原始网络配置。
//
// 安全准则：
//   - 不动任何「健康」状态：只在 install_state 标记 SystemProxySet=true 且实例不可达时执行。
//   - 不删 install_state，避免下次启动 ai-monitor 后无法 applySessionManagedProxy。
//     仅当确实没有 config.json 时才清掉 install_state（说明用户已不打算再用本程序）。
//
// 退出码：
//
//	0  成功（无事可做或修复完成）
//	1  发现需要恢复但执行失败
func runHealMode(configPath string) int {
	st := loadInstallState()
	if st == nil || !st.SystemProxySet {
		fmt.Println("  [heal] install_state 未记录系统代理变更，无需恢复。")
		return 0
	}

	// 非 Windows 平台无系统代理恢复实现：直接早返回，避免打印误导性"正在恢复…"日志。
	// 当前所有注册表/WinINet 操作都在 sysproxy_windows.go 内 (//go:build windows)，
	// 在 Linux/macOS 下不存在恢复路径，install_state 也不会被写入。
	if runtime.GOOS != "windows" {
		fmt.Println("  [heal] 当前平台不支持系统代理恢复，跳过。")
		return 0
	}

	if instanceHealthy() {
		fmt.Println("  [heal] 检测到健康的 ai-monitor 实例，不执行恢复。")
		return 0
	}

	// 实例不可达 → 进一步确认「真没人在监听」。注意 instance.json 可能因强杀残留，
	// 因此不依赖 PID，只看 install_state 记录的端口或 instance.json 端口实际可连。
	port := healCandidatePort(st)
	if port > 0 && portIsListening(port) {
		// 端口在监听但 /status 不通：可能是占用同端口的别的程序，不动配置避免误伤。
		fmt.Printf("  [heal] 端口 %d 有进程在监听但非 ai-monitor，跳过恢复以避免误伤。\n", port)
		return 0
	}

	fmt.Println("  [heal] 检测到 ai-monitor 已停止，但系统代理仍指向其端口，正在恢复…")
	{
		if st.PACFileSet {
			// PAC 模式：恢复原始 AutoConfigURL 或清除
			if st.PreviousAutoConfigURL != "" {
				fmt.Printf("  [heal] 恢复原始 PAC: %s\n", st.PreviousAutoConfigURL)
				EnableSystemProxyPAC(st.PreviousAutoConfigURL)
			} else {
				// 没有原始 PAC，清除我们的
				DisableSystemProxyPAC()
			}
			removePACFile()
			restoreOrClearEnvVars(st)
			RestoreAutoDetect(st.PreviousAutoDetect, st.PreviousAutoDetectPresent)
			fmt.Println("  [heal] 已恢复系统代理与用户级环境变量。")
		} else {
			restoreWinInetProxyFromState(st)
			restoreOrClearEnvVars(st)
			RestoreAutoDetect(st.PreviousAutoDetect, st.PreviousAutoDetectPresent)
			fmt.Println("  [heal] 已恢复系统代理与用户级环境变量。")
		}
	}

	// 残留 instance.json 一并清掉，避免下次启动时被当作活实例。
	removeInstanceInfo()

	// 如果用户已删除 config.json，认为不再使用，连 install_state 一起清掉。
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		clearInstallState()
		fmt.Println("  [heal] 未找到 config.json，已清空 install_state（不会再次自动重建代理）。")
	} else {
		fmt.Println("  [heal] 提示：下次启动 ai-monitor 将自动重新接管代理。")
	}
	return 0
}

// instanceHealthy 探测 instance.json 中记录的端口 /status 是否 200。
func instanceHealthy() bool {
	info, err := readInstanceInfo()
	if err != nil || info.Port <= 0 {
		return false
	}
	return probeInstanceStatus(info.Port)
}

// healCandidatePort 返回最可能的 MITM 端口。优先级：
//  1. install_state.PortAtInstall（精确记录安装时端口）
//  2. instance.json 中记录的端口
//  3. 扫描整个 18090..18090+mitmPortMaxFallback 端口范围
//  4. 默认 18090
func healCandidatePort(st *InstallState) int {
	// 1. install_state 记录的端口
	if st != nil && st.PortAtInstall > 0 {
		return st.PortAtInstall
	}

	// 2. instance.json 端口
	if info, err := readInstanceInfo(); err == nil && info.Port > 0 {
		return info.Port
	}

	// 3. 扫描端口范围，找到第一个"在监听但 /status 不通"的端口
	basePort := 18090
	for i := 0; i < mitmPortMaxFallback; i++ {
		port := basePort + i
		if portIsListening(port) && !probeInstanceStatus(port) {
			return port
		}
	}

	return basePort
}

// portIsListening 判断本机指定端口是否被任何进程监听。
// 用 Dial 而非 Listen：Listen 会因端口实际空闲而成功，无法区分「无人监听」和「端口被占」。
func portIsListening(port int) bool {
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", addr, 800*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// startSelfWatchdog 在主进程内部定期探测自身 /status，检测 HTTP 服务是否仍能响应。
// 若连续多次失败，说明 listener 实际已 dead（被杀、accept 退出、防火墙阻断），
// 主动触发 restoreSessionManagedProxyOnShutdown 并退出，让用户网络立即恢复。
//
// 不替代 OS 的 graceful shutdown — 只兜底「listener 死了但进程还在」的边缘场景。
func startSelfWatchdog(port int, cfg *Config) {
	if runtime.GOOS != "windows" || cfg == nil {
		return
	}
	go func() {
		interval := time.Duration(cfg.EffectiveWatchdogInterval()) * time.Second
		failureThreshold := cfg.EffectiveWatchdogFailures()
		if failureThreshold < 1 {
			failureThreshold = 2
		}

		client := &http.Client{Timeout: 3 * time.Second}
		failures := 0
		// 启动后先等 15s，避免冷启动期间偶发失败误触发。
		time.Sleep(15 * time.Second)
		for {
			resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/status", port))
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					failures = 0
					time.Sleep(interval)
					continue
				}
			}
			failures++
			if failures < failureThreshold {
				time.Sleep(interval)
				continue
			}
			log.Printf("[watchdog] 连续 %d 次自检失败，立即恢复系统代理并退出，避免污染本地网络。", failures)
			restoreSessionManagedProxyOnShutdown()
			removeInstanceInfo()
			os.Exit(2)
		}
	}()
}
