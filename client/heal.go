package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
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
		// install_state 没记录变更，也要兜底清理「孤儿 PAC URL」——上次异常
		// 退出可能没写 install_state 但已写 AutoConfigURL，留下死链让浏览器/IDE
		// 反复尝试加载不存在的 PAC，表现为「打开 ai-monitor 又关掉之后网络变差」。
		if removed := healOrphanAIMonitorRegistry(); removed {
			fmt.Println("  [heal] 已清理上次残留的 ai-monitor 注册表项（孤儿 PAC / 自指代理）。")
			return 0
		}
		fmt.Println("  [heal] install_state 未记录系统代理变更，无需恢复。")
		return 0
	}

	if instanceHealthy() {
		fmt.Println("  [heal] 检测到健康的 ai-monitor 实例，不执行恢复。")
		return 0
	}

	// 实例不可达 → 进一步确认「真没人在监听」。注意 instance.json 可能因强杀残留，
	// 因此不依赖 PID，只看 install_state 记录的端口或 instance.json 端口实际可连。
	port := healCandidatePort()
	if port > 0 && portIsListening(port) {
		// 端口在监听但 /status 不通：可能是占用同端口的别的程序，不动配置避免误伤。
		fmt.Printf("  [heal] 端口 %d 有进程在监听但非 ai-monitor，跳过恢复以避免误伤。\n", port)
		return 0
	}

	fmt.Println("  [heal] 检测到 ai-monitor 已停止，但系统代理仍指向其端口，正在恢复…")
	if runtime.GOOS == "windows" {
		restoreProxyFromState(st)
		restoreOrClearEnvVars(st)
		// 兜底：即使 install_state 没把 ai-monitor PAC URL 记到 PACFileSet 路径，
		// 也尝试扫一遍当前注册表，剔除任何指向我们自己的孤儿值。
		healOrphanAIMonitorRegistry()
		fmt.Println("  [heal] 已恢复系统代理/PAC 与用户级环境变量。")
	}

	// 残留 instance.json 一并清掉，避免下次启动时被当作活实例。
	removeInstanceInfo()

	// 如果用户已删除 config.json，认为不再使用，连 install_state 一起清掉。
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		clearInstallState()
		fmt.Println("  [heal] 未找到 config.json，已清空 install_state（不会再次自动重建代理）。")
	} else if st.SessionOnly {
		clearInstallState()
		fmt.Println("  [heal] 临时会话状态已清空。")
	} else {
		fmt.Println("  [heal] 提示：下次启动 ai-monitor 时才会临时接管代理，关闭后会恢复。")
	}
	return 0
}

// healOrphanAIMonitorRegistry 扫除 Windows 注册表里残留的 ai-monitor 配置：
//   - AutoConfigURL 是 file:///%APPDATA%/ai-monitor/proxy.pac
//   - ProxyEnable=1 且 ProxyServer=127.0.0.1:<MITM 端口范围>
//   - HKCU\Environment 里 HTTP_PROXY / HTTPS_PROXY 等指向自己
//
// 用于以下两类场景：
//  1. taskkill /F 后 install_state 不存在但注册表残留；
//  2. 用户手动改了 install_state 后又把 ai-monitor 当作普通程序运行。
//
// 返回 true 表示确实清理了某项配置；false 表示无残留。
func healOrphanAIMonitorRegistry() bool {
	if runtime.GOOS != "windows" {
		return false
	}
	cleaned := false
	if pac := ReadCurrentAutoConfigURL(); isAIMonitorPACURL(pac) {
		fmt.Printf("  [heal] 删除孤儿 AutoConfigURL: %s\n", pac)
		DisableSystemProxyPAC()
		removePACFile()
		cleaned = true
	}
	if sys := readCurrentSystemProxy(); isSelfProxy(sys) {
		fmt.Printf("  [heal] 关闭指向自身的系统代理: %s\n", sys)
		DisableSystemProxy()
		cleaned = true
	}
	var orphanKeys []string
	for _, key := range proxyEnvKeys {
		userV := strings.TrimSpace(ReadUserLevelEnv(key))
		procV := strings.TrimSpace(os.Getenv(key))
		if isAIMonitorManagedEnvValue(userV) || isAIMonitorManagedEnvValue(procV) {
			orphanKeys = append(orphanKeys, key)
		}
	}
	if len(orphanKeys) > 0 {
		fmt.Printf("  [heal] 清理指向 ai-monitor 的用户级环境变量: %v\n", orphanKeys)
		ClearEnvProxy(orphanKeys)
		cleaned = true
	}
	return cleaned
}

// instanceHealthy 探测 instance.json 中记录的端口 /status 是否 200。
func instanceHealthy() bool {
	info, err := readInstanceInfo()
	if err != nil || info.Port <= 0 {
		return false
	}
	return probeInstanceStatus(info.Port)
}

// healCandidatePort 返回最可能的 MITM 端口：优先 instance.json，否则 17590（默认）。
func healCandidatePort() int {
	if info, err := readInstanceInfo(); err == nil && info.Port > 0 {
		return info.Port
	}
	// 从 install_state 没存端口；尝试默认端口 18090（与 wizard 默认一致）。
	return 18090
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
	if runtime.GOOS != "windows" {
		return
	}
	go func() {
		interval := time.Duration(cfg.EffectiveWatchdogInterval()) * time.Second
		failureThreshold := cfg.EffectiveWatchdogFailures()
		client := &http.Client{Timeout: 15 * time.Second}
		failures := 0
		var lastReason string
		// 启动后先等 interval，避免冷启动期间偶发失败误触发。
		time.Sleep(interval)
		for {
			// 用 /healthz（纯内存）而不是 /status（要读 Windows 注册表 4 次）——
			// 后者在杀软扫描时会偶发超时，把好好的进程误杀。
			resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/healthz", port))
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					failures = 0
					lastReason = ""
					time.Sleep(interval)
					continue
				}
				lastReason = fmt.Sprintf("HTTP %d", resp.StatusCode)
			} else {
				lastReason = err.Error()
			}
			failures++
			log.Printf("[watchdog] /healthz 探测失败 %d/%d: %s", failures, failureThreshold, lastReason)
			if failures < failureThreshold {
				time.Sleep(interval)
				continue
			}
			// 退出前打一次内存快照，便于事后定位「是不是因为内存爆了被自己 kill」。
			logMemStatsSnapshot("watchdog-exit")
			log.Printf("[EXIT] reason=watchdog failures=%d last=%q port=%d pid=%d — 立即恢复系统代理并退出，避免污染本地网络。",
				failures, lastReason, port, os.Getpid())
			restoreSessionManagedProxyOnShutdown()
			removeInstanceInfo()
			os.Exit(2)
		}
	}()
}
