package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// Version is set at build time via -ldflags "-X main.Version=..."
// Fallback to "dev" when built without ldflags.
var Version = "dev"

// proxyEnvKeys lists environment variables cleared by --uninstall（含旧版曾写入的 HTTP_PROXY 等）.
var proxyEnvKeys = []string{
	"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY",
	"http_proxy", "https_proxy", "no_proxy",
	"OPENAI_BASE_URL", "OPENAI_API_BASE",
	"ANTHROPIC_BASE_URL",
	"NODE_EXTRA_CA_CERTS",
	"CODEX_CA_CERTIFICATE",
}

func selfBinaryName() string {
	if runtime.GOOS == "windows" {
		return "ai-monitor.exe"
	}
	return "ai-monitor"
}

func manualCAInstallHint(certPath string) string {
	switch runtime.GOOS {
	case "windows":
		return fmt.Sprintf(`certutil -addstore -user Root "%s"`, certPath)
	case "darwin":
		return fmt.Sprintf(`security add-trusted-cert -d -r trustRoot -k "$HOME/Library/Keychains/login.keychain-db" "%s"`, certPath)
	default:
		return fmt.Sprintf("请将 %s 手动导入系统或浏览器信任存储", certPath)
	}
}

func main() {
	install := flag.Bool("install", false, "安装: 默认仅安装 CA，不改系统代理；配合 install_system_proxy=true 或 --install-full 才写系统代理")
	installFull := flag.Bool("install-full", false, "与 --install 合用: 强制系统代理(覆盖 config 中 install_system_proxy=false)")
	installCertOnly := flag.Bool("install-cert-only", false, "与 --install 合用: 仅安装 CA，不改系统代理(与 Proxifier 共存时用)")
	installIDE := flag.Bool("install-ide", false, "与 --install 合用: 强制写入 VS Code/Cursor 的 http.proxy（默认不写，仅用系统代理）")
	globalInstall := flag.Bool("global-install", false, "全局安装: 安装 CA + 设用户级 HTTP_PROXY 环境变量 + 注册开机自启（推荐，一键覆盖所有开发工具）")
	globalUninstall := flag.Bool("global-uninstall", false, "全局卸载: 移除 CA + 恢复代理/环境变量 + 移除开机自启 + 删除安装配置")
	launch := flag.Bool("launch", false, "启动本地 MITM，并仅对子进程注入代理环境变量；不修改系统代理或用户环境变量")
	launchPreset := flag.String("launch-preset", "", "按预设启动受管应用，例如 vscode、cursor、powershell、cmd")
	listLaunchPresets := flag.Bool("list-launch-presets", false, "列出可用的受管应用启动预设")
	uninstall := flag.Bool("uninstall", false, "卸载: 移除 CA 证书，恢复代理/环境变量，并删除安装配置")
	setup := flag.Bool("setup", false, "傻瓜式配置向导：生成 config.json 并安装证书/代理")
	heal := flag.Bool("heal", false, "自愈：如果上次 ai-monitor 崩溃/被杀导致系统代理指向 dead 端口，还原原始网络配置")
	cleanupNetwork := flag.Bool("cleanup-network", false, "低侵入清理：停止后台实例，恢复系统代理/环境变量，移除 ai-monitor 写入的 IDE 代理，并清空 upstream_proxy")
	withSessionPAC := flag.Bool("with-session-pac", false, "本次会话临时写一次 AI 域名 PAC（关闭程序时自动还原）；不显式指定则按 config.auto_install_session_pac 决定，默认观察模式不动系统代理")
	defaultConfigPath := filepath.Join(appDataDir(), "config.json")
	configPath := flag.String("config", defaultConfigPath, "配置文件路径")
	showVersion := flag.Bool("version", false, "显示版本号并退出")
	postUpdate := flag.String("post-update", "", "（内部）由 updater.bat 调用，传入备份文件路径用于成功后清理")
	flag.Parse()
	if *postUpdate != "" {
		PostUpdateCleanup(*postUpdate)
	}
	defaultRunMode := !*install &&
		!*globalInstall &&
		!*globalUninstall &&
		!*launch &&
		!*listLaunchPresets &&
		strings.TrimSpace(*launchPreset) == "" &&
		!*uninstall &&
		!*setup &&
		!*heal &&
		!*cleanupNetwork

	if *showVersion {
		fmt.Println(Version)
		os.Exit(0)
	}

	// 安装 stdout/stderr 管道：所有日志、banner、log.Printf 都同时进
	// (a) 原 console（dev/带控制台构建）和 (b) 内存环形缓冲，供 WebView2
	// 内嵌窗口里的「运行日志」面板通过 /wizard/logs/stream 实时拉取。
	// 必须在第一行 fmt.Println / log.Printf 之前调用。
	initLogCapture(2000)

	fmt.Println()
	fmt.Println("  ╔══════════════════════════════════════════╗")
	fmt.Printf("  ║   AI Token 监控客户端 %-20s║\n", "v"+Version)
	fmt.Println("  ║   模式: 本地 MITM（流量须指向本代理）    ║")
	fmt.Println("  ╚══════════════════════════════════════════╝")
	fmt.Println()

	// 启动指纹：版本 + PID + 父进程 PID + 工作目录。用户排障时发回来的日志
	// 头部就能看清「是哪个 exe 在哪个 cwd 下被谁拉起来的」，免去反复问。
	if exe, eerr := os.Executable(); eerr == nil {
		log.Printf("[boot] version=%s pid=%d ppid=%d exe=%s", Version, os.Getpid(), os.Getppid(), exe)
	} else {
		log.Printf("[boot] version=%s pid=%d ppid=%d", Version, os.Getpid(), os.Getppid())
	}

	dataDir := appDataDir()
	os.MkdirAll(dataDir, 0755)

	// 始终把 log 同时写到文件（带粗滚动），这样 --global-install 通过 start /b
	// 后台启动、stdout/stderr=nil 时也能在 %APPDATA%/ai-monitor/ai-monitor.log
	// 看到 [启动]/[心跳]/[网络]/[上报] 全部诊断输出，避免「以为没在跑」的误判。
	setupFileLogging(dataDir)

	certMgr, err := NewCertManager(dataDir)
	if err != nil {
		log.Printf("[EXIT] reason=cert-init-failed pid=%d err=%v", os.Getpid(), err)
		log.Fatalf("  证书管理初始化失败: %v", err)
	}

	if *uninstall {
		doUninstall(certMgr)
		return
	}

	if *heal {
		os.Exit(runHealMode(*configPath))
	}

	if *cleanupNetwork {
		doCleanupNetwork(*configPath)
		return
	}

	if *globalUninstall {
		doGlobalUninstall(certMgr)
		return
	}

	if *globalInstall {
		cfg, err := LoadConfig(*configPath)
		if err != nil {
			log.Fatalf("  加载配置失败: %v", err)
		}
		resolveLocalUpstreamWithFallback(cfg, *configPath, nil)
		doGlobalInstall(certMgr, cfg, *configPath)
		return
	}

	if *setup {
		if err := runSetupWizard(*configPath, certMgr); err != nil {
			log.Fatalf("  %v", err)
		}
		return
	}

	// When no config exists and no explicit flags are given, launch the web wizard automatically.
	// This handles the "double-click ai-monitor.exe" scenario for first-time users.
	// 安装完成后不退出：直接 fall-through 进入正常代理启动流程，避免用户「装完了发现窗口没了」。
	if defaultRunMode {
		if _, err := os.Stat(*configPath); os.IsNotExist(err) {
			fmt.Println("  未找到 config.json，正在打开安装向导...")
			if err := runWebWizard(*configPath, certMgr); err != nil {
				log.Fatalf("  安装向导出错: %v", err)
			}
			fmt.Println("  安装完成，正在启动监控服务...")
		}
	}

	if *listLaunchPresets {
		printLaunchPresets()
		return
	}

	cfg, err := LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("  加载配置失败: %v", err)
	}
	resolveLocalUpstreamWithFallback(cfg, *configPath, nil)

	// 自动检测配置缺失并尝试恢复（配置单一化的自愈机制）
	healWarnings := ValidateAndHealConfig(cfg, *configPath)
	for _, w := range healWarnings {
		fmt.Printf("  %s\n", w)
	}
	if len(healWarnings) > 0 {
		fmt.Println()
	}

	bypass := buildProxyBypassWithConfig(cfg)
	noProxy := buildNoProxyEnvWithConfig(cfg)

	if *install {
		// Resolve actual port: reuse running instance or probe available port.
		actualPort := resolveActualPort(cfg)
		proxyAddr := fmt.Sprintf("127.0.0.1:%d", actualPort)
		full := (*installFull || cfg.EffectiveInstallSystemProxy()) && !*installCertOnly
		patchIDE := *installIDE || cfg.EffectiveInstallIDEProxy()
		doInstall(certMgr, cfg, proxyAddr, bypass, noProxy, full, patchIDE)
		return
	}

	if *launch || strings.TrimSpace(*launchPreset) != "" {
		if err := runManagedProcess(cfg, certMgr, flag.Args(), *launchPreset, *configPath); err != nil {
			log.Fatalf("  启动目标应用失败: %v", err)
		}
		return
	}

	// ── Normal run ──
	// 启动前先做一次「幽灵 session_only 状态」体检：如果上次异常退出留下
	// install_state.session_only=true，但当前注册表里实际已没有对应的
	// AutoConfigURL / ProxyServer，就清掉它——避免下次启动时把"幽灵"快照
	// 当成"用户原 PAC"还原回去（A3 修的污染源头的入口侧防护）。
	pruneGhostInstallState()

	// Singleton check: if a healthy instance already exists, don't start a second one.
	existingPort, alive := checkExistingInstance()
	if alive {
		log.Printf("[EXIT] reason=singleton-existing-instance port=%d pid=%d defaultRunMode=%v",
			existingPort, os.Getpid(), defaultRunMode)
		fmt.Printf("  已有 ai-monitor 实例运行于端口 %d，当前进程退出。\n", existingPort)
		// 第二次双击 exe 时把配置窗口拉起来，对准已运行实例的端口。
		// 内嵌窗口不可用时会自动回退到系统浏览器。
		if defaultRunMode {
			wizardURL := fmt.Sprintf("http://127.0.0.1:%d/wizard", existingPort)
			done, _ := openWizardOrBrowser(wizardURL, "AI Token 监控")
			if done != nil {
				<-done // 等用户关闭窗口再退出，体感上像 "打开了配置面板"
			}
		} else {
			fmt.Println("  如需重启，请先终止已有进程。")
		}
		os.Exit(0)
	}
	removeInstanceInfo() // clean up any stale PID file

	runtime, err := startMonitorRuntime(cfg, certMgr, "", *configPath)
	if err != nil {
		log.Printf("[EXIT] reason=monitor-runtime-start-failed pid=%d err=%v", os.Getpid(), err)
		log.Fatalf("  %v", err)
	}
	if err := writeInstanceInfo(runtime.listenPort); err != nil {
		log.Printf("[singleton] 写入 instance.json 失败: %v", err)
	}

	// 三段式接管模式（优先级从高到低）：
	//   1) 已有持久化安装（install_state.SystemProxySet 且非 SessionOnly）
	//      → applySessionManagedProxy 重应用 PAC + 用户级环境变量
	//   2) 用户显式要求本次写一次 PAC：--with-session-pac 或 config.auto_install_session_pac=true
	//      → applyTemporarySessionProxy 仅本会话写 PAC，关闭时还原
	//   3) 默认观察模式：完全不动系统设置；只有显式指向本机端口的流量被监控
	takeoverMode := "observe"
	if applySessionManagedProxy(cfg, certMgr, runtime.listenPort) {
		takeoverMode = "persistent"
	} else if *withSessionPAC || cfg.EffectiveAutoInstallSessionPAC() {
		applyTemporarySessionProxy(cfg, certMgr, runtime.listenPort)
		takeoverMode = "session"
	}
	runtime.SetTakeoverMode(takeoverMode)

	// 接管模式真的写了系统 PAC / 环境变量时（session 或 persistent），Electron 编辑器
	// 已运行的实例不会动态拾取代理变更——必须重启进程才行。这里友好询问是否帮用户
	// 处理。observe 模式不接管系统，重启反而徒劳，跳过。
	if takeoverMode == "session" || takeoverMode == "persistent" {
		runningEditors := detectRunningElectronEditors(func(image string) bool {
			ok, _ := isProcessImageRunning(image)
			return ok
		})
		if len(runningEditors) > 0 {
			interactive := stdinIsInteractive()
			if promptRestartElectronEditors(runningEditors, os.Stdin, os.Stdout, interactive) {
				killAndRelaunchEditors(runningEditors, os.Stdout)
			}
		}
	}

	startSelfWatchdog(runtime.listenPort, cfg)
	if runtime.listenPort != cfg.Port {
		log.Printf("[提示] 配置端口 %d 已被占用，已自动改用 %d（定向启动应用时请指向新端口）", cfg.Port, runtime.listenPort)
	}

	// Graceful shutdown.
	// 超时从 5s 调整到 8s：实测在 install_state 较复杂（持久 PAC + 用户级 env）
	// 时，server.Shutdown + restoreSessionManagedProxyOnShutdown 串行下还原
	// 注册表 + setx 写两套 key 偶尔会在 6s 左右才完成。给一点 buffer 避免
	// 极端情况下用户网络残留我们的写入。
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		fmt.Println("\n  正在关闭...")
		removeInstanceInfo()
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		runtime.Shutdown(ctx)
	}()

	printTakeoverBanner(takeoverMode)

	fmt.Printf("  用户: %s (%s) | 部门: %s\n", cfg.UserName, cfg.UserID, cfg.Department)
	fmt.Printf("  本地 MITM (拦截 AI 流量): %s\n", runtime.proxyAddr)
	fmt.Printf("  Token 上报服务器 (135): %s  →  POST /api/collect 与 /api/clients/heartbeat\n", cfg.ServerURL)
	if strings.TrimSpace(cfg.UpstreamProxy) != "" {
		fmt.Printf("  上游代理透传: %s\n", cfg.UpstreamProxy)
	}
	fmt.Printf("  监控域名: %d 个精确域名 + %d 条通配规则；未匹配域名走默认网络\n",
		len(effectiveMonitorHosts(cfg)), len(effectiveMonitorSuffixes(cfg)))
	fmt.Printf("  CA 证书: %s\n", certMgr.CACertPath())
	fmt.Println()
	printTakeoverHint(takeoverMode, cfg)
	printTakeoverModeQuickRef()
	fmt.Println("        经 MITM 的请求会尽量记录用量（免费额度与付费调用均尝试统计，不按计费类型过滤）；JSON 有 usage 为 [记录]，gRPC 多为 [记录·估算]。")
	fmt.Println("  扩展: config.json 可设 extra_monitor_*；report_opaque_traffic=false 可关闭体积估算。")
	if runtime.gatewayPort > 0 {
		fmt.Printf("  API Gateway (无 MITM): 127.0.0.1:%d  ← 工具可设 OPENAI_BASE_URL=http://127.0.0.1:%d/openai/v1\n", runtime.gatewayPort, runtime.gatewayPort)
	}
	fmt.Printf("  配置页面: http://127.0.0.1:%d/wizard\n", runtime.listenPort)
	fmt.Println()
	if defaultRunMode {
		openWizardOrBrowser(fmt.Sprintf("http://127.0.0.1:%d/wizard", runtime.listenPort), "AI Token 监控")
	}
	fmt.Println("  等待 AI 请求中... (Ctrl+C 退出)")
	fmt.Println("  " + strings.Repeat("─", 55))

	// Start gateway server on dedicated port (if configured)
	if runtime.gatewayServer != nil {
		go func() {
			if err := runtime.gatewayServer.Serve(runtime.gatewayLn); err != http.ErrServerClosed {
				log.Printf("[gateway] 服务启动失败: %v", err)
			}
		}()
	}

	if err := runtime.server.Serve(runtime.listener); err != http.ErrServerClosed {
		// 不使用 log.Fatalf：它会直接 os.Exit(1)，绕过 restoreSessionManagedProxyOnShutdown，
		// 导致用户级 HTTP_PROXY / 系统代理仍指向已 dead 的端口，整机网络会坏。
		log.Printf("[EXIT] reason=server-serve-returned pid=%d err=%v", os.Getpid(), err)
		log.Printf("  服务器启动失败: %v", err)
	}
	restoreSessionManagedProxyOnShutdown()
	fmt.Println("  已关闭。")
}

// resolveNodeExtraCACerts 返回本次安装应写入 NODE_EXTRA_CA_CERTS 的值。
// 该变量只支持单一路径，若用户已经设了别的值（如公司自签 CA），
// 直接覆盖会让 Node/Electron CLI 丢失原有信任链、外网请求失败。
// 处理策略：
//   - 未设置 或 已指向本程序 CA：写入本程序 CA。
//   - 已设置为其他路径：保留原值并在终端打印警告，要求客户手动合并。
func resolveNodeExtraCACerts(ourCAPath string, previousEnv map[string]string) string {
	prev := ""
	if previousEnv != nil {
		if v, ok := previousEnv["NODE_EXTRA_CA_CERTS"]; ok {
			prev = strings.TrimSpace(v)
		}
	}
	if prev == "" {
		prev = strings.TrimSpace(os.Getenv("NODE_EXTRA_CA_CERTS"))
	}
	if prev == "" || strings.EqualFold(prev, ourCAPath) {
		return ourCAPath
	}
	fmt.Printf("    ⚠ 检测到已有 NODE_EXTRA_CA_CERTS=%s（保留不变）\n", prev)
	fmt.Printf("      Node 仅支持单一路径；如需同时信任 ai-monitor CA，请手动把 %s 合并到原文件。\n", ourCAPath)
	return prev
}

func doInstall(certMgr *CertManager, cfg *Config, proxyAddr, bypass, noProxy string, fullSystemProxy, patchIDE bool) {
	httpProxy := "http://" + proxyAddr

	fmt.Println("  [1/4] 安装 CA 证书到用户信任存储...")
	caInstalled := true
	if err := certMgr.InstallCA(); err != nil {
		caInstalled = false
		log.Printf("    ✗ CA 证书安装失败: %v", err)
		fmt.Printf("    手动安装: %s\n", manualCAInstallHint(certMgr.CACertPath()))
	} else {
		fmt.Printf("    ✓ CA 证书已安装: %s\n", certMgr.CACertPath())
	}

	// 安全门：全量代理模式要求 CA 必须安装成功，否则浏览器 / IDE 对 AI 域名
	// 都会出现 SSL 错误，属于典型"把整机网络搞坏"的场景。此时自动降级为仅安装 CA。
	if fullSystemProxy && !caInstalled {
		fmt.Println()
		fmt.Println("  ⚠ CA 证书未成功安装到用户信任存储，已自动降级为『仅 CA』模式：")
		fmt.Println("    — 未修改系统代理 / 环境变量 / IDE 设置，本机原有网络不受影响。")
		fmt.Printf("    — 请先按上面的提示手动安装 CA，再重新执行 %s --install-full。\n", selfBinaryName())
		fmt.Println("    — 临时试用可用: ai-monitor.exe --launch <你的程序> 仅注入子进程环境变量。")
		fmt.Println()
		return
	}

	if !fullSystemProxy {
		fmt.Println("  [2/4] 跳过系统代理 / 环境变量 / IDE（默认，不破坏本机原有代理）")
		fmt.Println("    — 未修改系统代理或持久环境变量，可与本机原有网络配置共存。")
		fmt.Printf("    — 推荐用法: %s --launch <你的程序>，仅对子进程注入 HTTP(S)_PROXY 与 Base URL。\n", selfBinaryName())
		fmt.Println("    — 若目标应用已有固定公司代理，可在 config.json 中设置 upstream_proxy 让本地 MITM 外联继续走原代理。")
		if runtime.GOOS == "windows" {
			fmt.Println("    — 若确需整机代理导流，可重新安装并启用 install_system_proxy=true 或执行 --install-full。")
		} else {
			fmt.Println("    — 非 Windows 暂未实现自动整机代理与持久环境变量配置，请优先使用 --launch 模式。")
		}
		fmt.Println()
		fmt.Println("  ══════════════════════════════════════════")
		fmt.Println("  ✓ 安装完成 (仅 CA)")
		fmt.Printf("  运行 %s 启动监控，或用 --launch 定向启动目标应用。\n", selfBinaryName())
		fmt.Printf("  卸载: %s --uninstall\n", selfBinaryName())
		fmt.Println("  ══════════════════════════════════════════")
		return
	}

	configuredUpstream := strings.TrimSpace(cfg.UpstreamProxy)
	previousProxy := readCurrentSystemProxy()
	previousAutoConfigURL := ReadCurrentAutoConfigURL()
	previousEnvVars := snapshotProxyEnvVars()

	if configuredUpstream != "" {
		fmt.Printf("    ℹ 使用显式上游代理: %s\n", configuredUpstream)
	}

	// 解析实际监听端口，PAC 文件需要它
	actualPort := resolveActualPort(cfg)

	saveInstallState(&InstallState{
		SystemProxySet:        true,
		PreviousProxyAddr:     previousProxy,
		PreviousProxyEnabled:  previousProxy != "" && !isSelfProxy(previousProxy),
		IDESettingsPatched:    patchIDE,
		PreviousUpstreamProxy: configuredUpstream,
		PreviousEnvVars:       previousEnvVars,
		PACFileSet:            true,
		PACFilePath:           pacFilePath(),
		PreviousAutoConfigURL: previousAutoConfigURL,
	})

	fmt.Println("  [2/4] 设置系统代理 (PAC 白名单模式)...")
	if previousProxy != "" && !isSelfProxy(previousProxy) {
		fmt.Printf("    ℹ 检测到现有系统代理: %s（已备份，卸载时将恢复）\n", previousProxy)
	}
	if previousAutoConfigURL != "" {
		fmt.Printf("    ℹ 检测到现有 PAC: %s（已备份，卸载时将恢复）\n", previousAutoConfigURL)
	}
	pacURL, err := writePACFile(actualPort, cfg, "")
	if err != nil {
		log.Printf("    ✗ PAC 文件生成失败: %v", err)
	} else if err := EnableSystemProxyPAC(pacURL); err != nil {
		log.Printf("    ✗ 系统代理 (PAC) 设置失败: %v", err)
	} else {
		fmt.Printf("    ✓ PAC 文件: %s\n", pacFilePath())
		fmt.Printf("    ✓ 系统代理: %s\n", pacURL)
		fmt.Println("      → 仅 AI 域名走 MITM；浏览器/内网/其他软件全部 DIRECT")
		fmt.Println("      → MITM 异常时 PAC 自动回退 DIRECT，不影响整机网络")
	}

	fmt.Println("  [3/4] 设置环境变量（HTTP(S)_PROXY 指向本程序 + NO_PROXY 与系统代理例外一致）...")
	fmt.Println("    — 未列入 NO_PROXY 的域名（如各 AI API）经 Node/CLI 也会走本机 MITM；GitHub/VS Code/CDN 走直连。")
	envVars := map[string]string{
		"HTTP_PROXY":           httpProxy,
		"HTTPS_PROXY":          httpProxy,
		"NO_PROXY":             noProxy,
		"http_proxy":           httpProxy,
		"https_proxy":          httpProxy,
		"no_proxy":             noProxy,
		"OPENAI_BASE_URL":      httpProxy + "/openai/v1",
		"OPENAI_API_BASE":      httpProxy + "/openai/v1",
		"ANTHROPIC_BASE_URL":   httpProxy + "/anthropic",
		"NODE_EXTRA_CA_CERTS":  resolveNodeExtraCACerts(certMgr.CACertPath(), previousEnvVars),
		"CODEX_CA_CERTIFICATE": certMgr.CACertPath(),
	}
	if err := SetEnvProxy(envVars); err != nil {
		log.Printf("    ✗ 环境变量设置失败: %v", err)
	} else {
		fmt.Println("    ✓ 已设置:")
		envOrder := []string{"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "OPENAI_BASE_URL", "OPENAI_API_BASE", "ANTHROPIC_BASE_URL", "NODE_EXTRA_CA_CERTS", "CODEX_CA_CERTIFICATE"}
		for _, k := range envOrder {
			v := envVars[k]
			if k == "NO_PROXY" && len(v) > 120 {
				fmt.Printf("      %s=%s…\n", k, v[:120])
				continue
			}
			fmt.Printf("      %s=%s\n", k, v)
		}
	}

	fmt.Println("  [4/4] IDE 内嵌代理 (VS Code / Cursor)...")
	if patchIDE {
		ideCount := configureIDEProxy(httpProxy, certMgr.CACertPath())
		if ideCount > 0 {
			fmt.Printf("    ✓ 已写入 %d 个 IDE 的 settings.json（config install_ide_proxy=true）\n", ideCount)
		} else {
			fmt.Println("    — 未发现已安装的 IDE")
		}
	} else {
		if runtime.GOOS == "windows" {
			fmt.Println("    — 已跳过（默认）。Electron/VS Code 将使用 Windows 系统代理走 MITM，避免与 IDE 内 http.proxy 重复导致网络异常。")
			fmt.Println("      若某扩展仍不走系统代理，可在 config.json 设 \"install_ide_proxy\": true 后重新执行 --install")
		} else {
			fmt.Println("    — 已跳过。若需要让 VS Code / Cursor 明确走本地 MITM，可在 config.json 设 \"install_ide_proxy\": true 后重新执行 --install")
		}
	}

	fmt.Println()
	fmt.Println("  ══════════════════════════════════════════")
	fmt.Println("  ✓ 安装完成!")
	fmt.Println()
	// 确保 %APPDATA% 中的 config.json 与当前配置完全同步（配置单一化）
	roamingConfig := filepath.Join(appDataDir(), "config.json")
	if err := SaveConfig(cfg, roamingConfig); err != nil {
		log.Printf("    ⚠ 同步配置到 %s 失败: %v", roamingConfig, err)
	} else {
		fmt.Printf("    ✓ 配置已同步到: %s\n", roamingConfig)
	}
	fmt.Println()
	fmt.Println("  注意: 需重新打开终端窗口，环境变量才生效。")
	fmt.Printf("  运行 %s 即可启动监控。\n", selfBinaryName())
	fmt.Println("  Token 记录发往 config.json 中的 server_url（默认 otw.tech:59889）。")
	fmt.Printf("  卸载: %s --uninstall\n", selfBinaryName())
	fmt.Println("  ══════════════════════════════════════════")
}

func doUninstall(certMgr *CertManager) {
	state := loadInstallState()

	fmt.Println("  [1/6] 停止后台实例...")
	stopExistingInstanceForUninstall()
	fmt.Println("    ✓ done")

	fmt.Println("  [2/6] 移除 CA 证书...")
	certMgr.UninstallCA()
	fmt.Println("    ✓ done")

	fmt.Println("  [3/6] 恢复系统代理...")
	restoreProxyFromState(state)
	fmt.Println("    ✓ done")

	fmt.Println("  [4/6] 恢复环境变量...")
	restoreOrClearEnvVars(state)
	fmt.Println("    ✓ done")

	fmt.Println("  [5/6] 清除 IDE 代理配置...")
	removeIDEProxy()
	fmt.Println("    ✓ done")

	fmt.Println("  [6/6] 移除 PowerShell Profile 代理包装并清理安装配置...")
	RemovePowerShellProfile()
	if err := cleanupInstallDataDir(); err != nil {
		log.Printf("    ⚠ 清理安装目录失败: %v", err)
	} else {
		fmt.Printf("    ✓ 已删除安装配置目录: %s\n", appDataDir())
	}

	fmt.Println()
	fmt.Println("  ✓ 卸载完成! 重新打开终端窗口和 IDE 使更改生效。")
}

func doGlobalInstall(certMgr *CertManager, cfg *Config, configPath string) {
	fmt.Println("  ══════════════════════════════════════════")
	fmt.Println("  全局安装模式")
	fmt.Println("  效果: 所有新启动的开发工具自动走 ai-monitor 监控")
	fmt.Println("  ══════════════════════════════════════════")
	fmt.Println()

	actualPort := resolveActualPort(cfg)
	// 全局安装下坚持使用 127.0.0.1，避免 localhost 在 IPv6 优先解析场景下
	// 把 HTTP_PROXY / PAC 指到 ::1 上（本程序仅监听 IPv4 回环）。
	proxyAddr := fmt.Sprintf("127.0.0.1:%d", actualPort)
	httpProxy := "http://" + proxyAddr

	configuredUpstream := strings.TrimSpace(cfg.UpstreamProxy)
	previousSysProxy := readCurrentSystemProxy()
	previousEnvVars := snapshotProxyEnvVars()

	if configuredUpstream != "" {
		fmt.Printf("  ℹ 使用显式上游代理: %s\n", configuredUpstream)
		fmt.Println()
	}

	// Step 1: Install CA certificate
	fmt.Println("  [1/4] 安装 CA 证书到用户信任存储...")
	caInstalled := true
	if err := certMgr.InstallCA(); err != nil {
		caInstalled = false
		log.Printf("    ✗ CA 证书安装失败: %v", err)
		fmt.Printf("    手动安装: %s\n", manualCAInstallHint(certMgr.CACertPath()))
	} else {
		fmt.Printf("    ✓ CA 证书已安装: %s\n", certMgr.CACertPath())
	}

	// 安全门：CA 未装入用户信任存储时，不能写系统代理 / PAC / 环境变量，
	// 否则所有浏览器 / IDE / CLI 访问 AI 域名都会 SSL 握手失败，影响面大。
	// 此时只保留 CA 文件生成 + 开机自启；其余步骤跳过，等待用户手动信任后再跑一次。
	if !caInstalled {
		fmt.Println()
		fmt.Println("  ⚠ CA 证书未成功安装到用户信任存储，全局安装已自动暂停：")
		fmt.Println("    — 未修改系统代理 / 环境变量 / PAC，本机原有网络不受影响。")
		fmt.Printf("    — 按上面的提示手动信任 CA 后，再执行一次 %s --global-install 即可完成整机接管。\n", selfBinaryName())
		fmt.Println("    — 临时试用: ai-monitor.exe --launch <程序> 不依赖全局代理。")
		fmt.Println()
		return
	}

	// Step 2: Set user-level environment variables
	fmt.Println("  [2/4] 设置用户级环境变量...")
	noProxy := buildNoProxyEnvWithConfig(cfg)
	envVars := map[string]string{
		"HTTP_PROXY":           httpProxy,
		"HTTPS_PROXY":          httpProxy,
		"NO_PROXY":             noProxy,
		"http_proxy":           httpProxy,
		"https_proxy":          httpProxy,
		"no_proxy":             noProxy,
		"OPENAI_BASE_URL":      httpProxy + "/openai/v1",
		"OPENAI_API_BASE":      httpProxy + "/openai/v1",
		"ANTHROPIC_BASE_URL":   httpProxy + "/anthropic",
		"NODE_EXTRA_CA_CERTS":  resolveNodeExtraCACerts(certMgr.CACertPath(), previousEnvVars),
		"CODEX_CA_CERTIFICATE": certMgr.CACertPath(),
	}
	if err := SetEnvProxy(envVars); err != nil {
		log.Printf("    ✗ 环境变量设置失败: %v", err)
	} else {
		fmt.Println("    ✓ 已设置 HTTP_PROXY / HTTPS_PROXY / ANTHROPIC_BASE_URL / OPENAI_BASE_URL / NODE_EXTRA_CA_CERTS / CODEX_CA_CERTIFICATE")
		fmt.Println("      → VS Code/Cursor/JetBrains/Claude Code/Aider/Codex 等 CLI 工具自动走监控")
	}

	// Step 3: Set Windows system proxy via PAC (with DIRECT fallback for crash safety)
	fmt.Println("  [3/4] 设置 Windows 系统代理（PAC + DIRECT 回退）...")
	previousAutoConfigURL := ReadCurrentAutoConfigURL()
	if previousSysProxy != "" && !isSelfProxy(previousSysProxy) {
		fmt.Printf("    ℹ 检测到现有系统代理: %s（已备份，卸载时将恢复）\n", previousSysProxy)
	}
	if previousAutoConfigURL != "" {
		fmt.Printf("    ℹ 检测到现有 PAC: %s（已备份，卸载时将恢复）\n", previousAutoConfigURL)
	}
	pacURL, err := writePACFile(actualPort, cfg, "")
	if err != nil {
		log.Printf("    ✗ PAC 文件生成失败: %v", err)
	} else {
		fmt.Printf("    ✓ PAC 文件: %s\n", pacFilePath())
	}
	saveInstallState(&InstallState{
		SystemProxySet:        true,
		PreviousProxyAddr:     previousSysProxy,
		PreviousProxyEnabled:  previousSysProxy != "" && !isSelfProxy(previousSysProxy),
		IDESettingsPatched:    cfg.EffectiveInstallIDEProxy(),
		PreviousUpstreamProxy: configuredUpstream,
		PreviousEnvVars:       previousEnvVars,
		PACFileSet:            true,
		PACFilePath:           pacFilePath(),
		PreviousAutoConfigURL: previousAutoConfigURL,
	})
	if err := EnableSystemProxyPAC(pacURL); err != nil {
		log.Printf("    ✗ 系统代理 (PAC) 设置失败: %v", err)
		fmt.Println("      Visual Studio 中的 GitHub Copilot 可能无法被监控")
	} else {
		fmt.Printf("    ✓ 系统代理 (PAC): %s\n", pacURL)
		fmt.Println("      → 浏览器 / Visual Studio / .NET 应用自动走监控")
		fmt.Println("      → MITM 异常时自动回退直连，不影响上网（无需看门狗）")
	}

	// Step 4: Optionally patch IDE proxy settings. This is off by default because
	// writing VS Code/Cursor settings is more intrusive than a temporary PAC. Users
	// can enable it explicitly when an IDE has a hardcoded proxy that bypasses PAC.
	fmt.Println("  [4/6] 写入 IDE 内嵌代理 (VS Code / Cursor / Kiro)...")
	if cfg.EffectiveInstallIDEProxy() {
		if ideCount := configureIDEProxy(httpProxy, certMgr.CACertPath()); ideCount > 0 {
			fmt.Printf("    ✓ 已写入 %d 个 IDE 的 settings.json\n", ideCount)
		} else {
			fmt.Println("    — 未发现已安装的 IDE")
		}
	} else {
		fmt.Println("    — 已跳过（默认）。如某 IDE 已硬编码 http.proxy，可在 config.json 设 install_ide_proxy=true 后重装")
	}

	// Step 5: Register auto-start
	fmt.Println("  [5/6] 注册开机自启...")
	if err := installAutoStart(configPath); err != nil {
		log.Printf("    ✗ 注册失败: %v", err)
		fmt.Println("    可手动将 ai-monitor.exe 快捷方式放入「启动」文件夹")
	} else {
		fmt.Println("    ✓ 已注册: 每次登录自动在后台启动 ai-monitor")
	}

	// Step 6: Inject PowerShell Profile wrapper for claude / codex CLI tools
	fmt.Println("  [6/6] 写入 PowerShell Profile（claude / codex 命令自动带代理）...")
	if err := InstallPowerShellProfile(httpProxy, certMgr.CACertPath(), noProxy); err != nil {
		log.Printf("    ✗ PowerShell Profile 写入失败: %v", err)
	} else {
		fmt.Println("    ✓ 已写入: 重新打开 PowerShell 后 claude / codex 直接使用即可")
	}

	// Start background instance now if not already running
	if _, alive := checkExistingInstance(); !alive {
		fmt.Println()
		fmt.Println("  正在启动后台服务...")
		if err := startBackgroundInstance(configPath); err != nil {
			log.Printf("    ✗ 后台启动失败: %v", err)
			fmt.Println("    请手动运行 ai-monitor.exe")
		} else {
			fmt.Println("    ✓ ai-monitor 已在后台运行")
		}
	} else {
		fmt.Println()
		fmt.Println("  ✓ ai-monitor 已在运行中")
	}

	// PAC 已带 DIRECT 回退，不再注册每分钟运行的外部 watchdog；
	// 同时清理旧版本遗留任务，避免控制台窗口定时闪现。
	if err := uninstallWatchdogTask(); err != nil {
		log.Printf("    ⚠ 清理旧 watchdog 计划任务失败: %v", err)
	}

	fmt.Println()
	fmt.Println("  ══════════════════════════════════════════")
	fmt.Println("  ✓ 全局安装完成!")
	fmt.Println()
	// 确保 %APPDATA% 中的 config.json 与当前配置完全同步（配置单一化）
	roamingConfig := filepath.Join(appDataDir(), "config.json")
	if err := SaveConfig(cfg, roamingConfig); err != nil {
		log.Printf("    ⚠ 同步配置到 %s 失败: %v", roamingConfig, err)
	} else {
		fmt.Printf("    ✓ 配置已同步到: %s\n", roamingConfig)
	}
	fmt.Println()
	fmt.Println("  覆盖范围:")
	fmt.Println("    ✓ VS Code / Cursor / Windsurf / Kiro / Trae  (系统 PAC / 环境变量；IDE settings 默认不改)")
	fmt.Println("    ✓ JetBrains IDEA / WebStorm / PyCharm / GoLand  (环境变量)")
	fmt.Println("    ✓ Visual Studio 2022 + GitHub Copilot  (系统代理)")
	fmt.Println("    ✓ Claude Code / Codex / Aider / OpenCode 等 CLI  (环境变量)")
	if configuredUpstream != "" {
		fmt.Println()
		fmt.Printf("  代理兼容: 已显式配置上游代理 %s\n", configuredUpstream)
	}
	fmt.Println()
	fmt.Println("  重要: 需重新打开终端窗口和 IDE，环境变量才对新进程生效。")
	fmt.Println("  已打开的程序不受影响，关闭后再打开即可。")
	fmt.Printf("  卸载: %s --global-uninstall\n", selfBinaryName())
	fmt.Println("  ══════════════════════════════════════════")
}

func doGlobalUninstall(certMgr *CertManager) {
	fmt.Println("  ══════════════════════════════════════════")
	fmt.Println("  全局卸载")
	fmt.Println("  ══════════════════════════════════════════")
	fmt.Println()

	state := loadInstallState()

	fmt.Println("  [1/7] 停止后台实例...")
	stopExistingInstanceForUninstall()
	fmt.Println("    ✓ done")

	fmt.Println("  [2/7] 移除 CA 证书...")
	certMgr.UninstallCA()
	fmt.Println("    ✓ done")

	fmt.Println("  [3/7] 恢复用户级环境变量...")
	restoreOrClearEnvVars(state)
	fmt.Println("    ✓ done")

	fmt.Println("  [4/7] 恢复系统代理...")
	restoreProxyFromState(state)
	fmt.Println("    ✓ done")

	fmt.Println("  [5/7] 移除计划任务（开机自启 + 旧看门狗清理）...")
	if err := uninstallAutoStart(); err != nil {
		log.Printf("    ⚠ 开机自启: %v", err)
	} else {
		fmt.Println("    ✓ 已移除开机自启")
	}
	// Clean up watchdog task from previous installs (before PAC migration)
	if err := uninstallWatchdogTask(); err != nil {
		log.Printf("    ⚠ 看门狗: %v", err)
	} else {
		fmt.Println("    ✓ 已移除看门狗（如有）")
	}

	fmt.Println("  [6/7] 清除 IDE 代理配置...")
	removeIDEProxy()
	fmt.Println("    ✓ done")

	fmt.Println("  [7/7] 移除 PowerShell Profile 代理包装并清理安装配置...")
	RemovePowerShellProfile()
	if err := cleanupInstallDataDir(); err != nil {
		log.Printf("    ⚠ 清理安装目录失败: %v", err)
	} else {
		fmt.Printf("    ✓ 已删除安装配置目录: %s\n", appDataDir())
	}

	fmt.Println()
	fmt.Println("  ✓ 全局卸载完成! 重新打开终端窗口和 IDE 使更改生效。")
}

func doCleanupNetwork(configPath string) {
	fmt.Println("  ══════════════════════════════════════════")
	fmt.Println("  低侵入网络清理")
	fmt.Println("  ══════════════════════════════════════════")
	fmt.Println()

	state := loadInstallState()

	fmt.Println("  [1/6] 停止正在运行的 ai-monitor 实例...")
	stopExistingInstanceForUninstall()
	fmt.Println("    ✓ done")

	fmt.Println("  [2/6] 恢复系统代理/PAC 与用户级环境变量...")
	restoreProxyFromState(state)
	clearAIMonitorPACIfCurrent()
	restoreOrClearEnvVars(state)
	fmt.Println("    ✓ done")

	fmt.Println("  [3/6] 移除 IDE 中指向 ai-monitor 的 http.proxy...")
	if count := removeAIMonitorIDEProxy(); count > 0 {
		fmt.Printf("    ✓ 已清理 %d 个 IDE 配置\n", count)
	} else {
		fmt.Println("    — 未发现指向 ai-monitor 的 IDE 代理配置")
	}

	fmt.Println("  [4/6] 清理 PowerShell Profile 与开机自启...")
	RemovePowerShellProfile()
	if err := uninstallAutoStart(); err != nil {
		log.Printf("    ⚠ 开机自启清理失败: %v", err)
	}
	if err := uninstallWatchdogTask(); err != nil {
		log.Printf("    ⚠ 旧 watchdog 清理失败: %v", err)
	}
	fmt.Println("    ✓ done")

	fmt.Println("  [5/6] 清空 upstream_proxy 残留...")
	clearedConfig := clearConfiguredUpstreamProxy(configPath)
	clearedState := clearSavedUpstreamProxy()
	if clearedConfig || clearedState {
		fmt.Println("    ✓ upstream_proxy 已清空；后续默认按本机网络直连，不再依赖上游地址")
	} else {
		fmt.Println("    — 未发现 upstream_proxy 残留")
	}

	fmt.Println("  [6/6] 清理旧安装状态...")
	clearInstallState()
	fmt.Println("    ✓ done")

	fmt.Println()
	fmt.Println("  ✓ 清理完成。后续打开 ai-monitor 才会临时监控，关闭后恢复本机网络。")
	fmt.Println("  建议重新打开 VS Code / Cursor / PowerShell，让新配置生效。")
}

func clearConfiguredUpstreamProxy(configPath string) bool {
	cfg, err := LoadConfig(configPath)
	if err != nil {
		log.Printf("    ⚠ 读取配置失败，跳过 config.json: %v", err)
		return false
	}
	if strings.TrimSpace(cfg.UpstreamProxy) == "" {
		return false
	}
	cfg.UpstreamProxy = ""
	if err := SaveConfig(cfg, configPath); err != nil {
		log.Printf("    ⚠ 写入 config.json 失败: %v", err)
		return false
	}
	return true
}

func clearSavedUpstreamProxy() bool {
	state := loadInstallState()
	if state == nil || strings.TrimSpace(state.PreviousUpstreamProxy) == "" {
		return false
	}
	state.PreviousUpstreamProxy = ""
	if err := saveInstallState(state); err != nil {
		log.Printf("    ⚠ 更新 install_state 失败: %v", err)
		return false
	}
	return true
}

func clearAIMonitorPACIfCurrent() {
	if runtime.GOOS != "windows" {
		return
	}
	currentPAC := ReadCurrentAutoConfigURL()
	if !isAIMonitorPACURL(currentPAC) {
		return
	}
	DisableSystemProxyPAC()
	removePACFile()
}

func isAIMonitorPACURL(raw string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(raw), "\\", "/"))
	if normalized == "" {
		return false
	}
	localPAC := strings.ToLower(strings.ReplaceAll(pacFilePath(), "\\", "/"))
	return strings.Contains(normalized, "ai-monitor/proxy.pac") || strings.Contains(normalized, localPAC)
}

// restoreOrClearEnvVars restores previously saved environment variables, or
// clears ai-monitor's env vars if no previous state was saved.
func restoreOrClearEnvVars(state *InstallState) {
	keysToManage := append([]string(nil), proxyEnvKeys...)

	if state != nil && len(state.PreviousEnvVars) > 0 {
		// Restore previous values for keys that had values before install
		restored := make(map[string]string)
		for _, key := range keysToManage {
			if prev, ok := state.PreviousEnvVars[key]; ok && prev != "" && !isAIMonitorManagedEnvValue(prev) {
				restored[key] = prev
			}
		}
		// Also check lowercase variants
		for _, key := range []string{"http_proxy", "https_proxy", "no_proxy"} {
			if prev, ok := state.PreviousEnvVars[key]; ok && prev != "" && !isAIMonitorManagedEnvValue(prev) {
				restored[key] = prev
			}
		}

		if len(restored) > 0 {
			fmt.Println("    ℹ 恢复之前的环境变量:")
			if err := SetEnvProxy(restored); err != nil {
				log.Printf("    ⚠ 恢复环境变量失败: %v", err)
			} else {
				for k, v := range restored {
					fmt.Printf("      %s=%s\n", k, v)
				}
			}
		}

		// Clear keys that had no previous value (were set by ai-monitor for the first time)
		var toClear []string
		for _, key := range keysToManage {
			if _, ok := state.PreviousEnvVars[key]; !ok {
				toClear = append(toClear, key)
			}
		}
		if len(toClear) > 0 {
			ClearEnvProxy(toClear)
		}
	} else {
		// 无安装快照时仅清理指向 ai-monitor 的残留，避免误删用户自己的代理环境变量。
		clearSelfProxyEnvVars()
	}
}

func isAIMonitorManagedEnvValue(value string) bool {
	v := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), "\\", "/"))
	if v == "" {
		return false
	}
	if isSelfProxy(v) || strings.Contains(v, "127.0.0.1:18090/") || strings.Contains(v, "localhost:18090/") {
		return true
	}
	return strings.Contains(v, "/ai-monitor/ca.crt")
}

// restoreWinInetProxyFromState restores the user's previous WinINet proxy or disables it.
func restoreWinInetProxyFromState(state *InstallState) {
	if state != nil && state.PreviousProxyEnabled && state.PreviousProxyAddr != "" {
		fmt.Printf("    ℹ 恢复之前的系统代理: %s\n", state.PreviousProxyAddr)
		if err := EnableSystemProxy(state.PreviousProxyAddr, ""); err != nil {
			log.Printf("    ⚠ 恢复系统代理失败: %v", err)
		}
	} else {
		DisableSystemProxy()
	}
}

// applySessionManagedProxy re-applies system proxy and user env vars when install_state
// records a global / full install (SystemProxySet). Called each time the MITM process starts
// so that after a prior graceful shutdown cleared env vars, the next run configures them again.
// For PAC-based installs, regenerates the PAC file with the current listen port.
func applySessionManagedProxy(cfg *Config, certMgr *CertManager, listenPort int) bool {
	if runtime.GOOS != "windows" {
		return false
	}
	st := loadInstallState()
	if st == nil || !st.SystemProxySet {
		return false
	}
	if st.SessionOnly {
		return false
	}
	proxyAddr := fmt.Sprintf("127.0.0.1:%d", listenPort)
	httpProxy := "http://" + proxyAddr
	noProxy := buildNoProxyEnvWithConfig(cfg)
	// 会话重维时不知道用户原始环境（install_state 里已快照），
	// 优先用快照里的 NODE_EXTRA_CA_CERTS；否则才带上本程序 CA。
	var prevEnv map[string]string
	prevEnv = st.PreviousEnvVars
	envVars := map[string]string{
		"HTTP_PROXY":           httpProxy,
		"HTTPS_PROXY":          httpProxy,
		"NO_PROXY":             noProxy,
		"http_proxy":           httpProxy,
		"https_proxy":          httpProxy,
		"no_proxy":             noProxy,
		"NODE_EXTRA_CA_CERTS":  resolveNodeExtraCACerts(certMgr.CACertPath(), prevEnv),
		"CODEX_CA_CERTIFICATE": certMgr.CACertPath(),
	}

	if st.PACFileSet {
		// PAC mode: regenerate PAC file with current port (may have changed)
		pacURL, err := writePACFile(listenPort, cfg, "")
		if err != nil {
			log.Printf("[session] PAC 文件重新生成失败: %v", err)
		} else {
			// Re-set AutoConfigURL to prompt WinINet to re-read the PAC file
			if err := EnableSystemProxyPAC(pacURL); err != nil {
				log.Printf("[session] 启用 PAC 代理失败: %v", err)
			} else {
				fmt.Printf("  [会话] 已更新 PAC 代理 (端口 %d)\n", listenPort)
			}
		}
	} else {
		// Legacy (pre-PAC) install: use hardcoded ProxyServer
		bypass := buildProxyBypassWithConfig(cfg)
		if err := EnableSystemProxy(proxyAddr, bypass); err != nil {
			log.Printf("[session] 启用系统代理失败: %v", err)
		} else {
			fmt.Printf("  [会话] 已启用系统代理 %s\n", proxyAddr)
		}
	}

	if err := SetEnvProxy(envVars); err != nil {
		log.Printf("[session] 设置用户环境变量失败: %v", err)
	} else {
		fmt.Println("  [会话] 已同步用户级 HTTP(S)_PROXY")
	}
	return true
}

func applyTemporarySessionProxy(cfg *Config, certMgr *CertManager, listenPort int) {
	if runtime.GOOS != "windows" {
		return
	}
	previousSysProxy := readCurrentSystemProxy()
	previousAutoConfigURL := ReadCurrentAutoConfigURL()
	// 自污染防护：如果当前注册表里残留的 AutoConfigURL 本来就是 ai-monitor 自己
	// 上次写入的 PAC URL（例如上次 taskkill /F 后 install_state 没清干净），就不能
	// 把它当作「用户原本的 PAC」快照下来——否则关闭 ai-monitor 时还原回这个
	// 已删除/即将删除的 PAC URL，用户网络会永远绑在我们的孤儿配置上。
	if isAIMonitorPACURL(previousAutoConfigURL) {
		log.Printf("[session] 检测到上次残留的 ai-monitor PAC URL: %s，视为无原 PAC。", previousAutoConfigURL)
		previousAutoConfigURL = ""
	}
	if isSelfProxy(previousSysProxy) {
		log.Printf("[session] 检测到系统手动代理仍指向 ai-monitor 自身: %s，视为无原代理。", previousSysProxy)
		previousSysProxy = ""
	}
	previousEnvVars := snapshotProxyEnvVars()
	previousProxyOverride := readCurrentProxyOverride()
	previousAutoDetect, previousAutoDetectPresent := readCurrentAutoDetect()
	previousPACBody := ""
	if previousAutoConfigURL != "" && cfg.EffectiveChainExistingPAC() {
		if body, err := fetchPACBody(previousAutoConfigURL); err == nil && !isAIMonitorPACBody(body) {
			previousPACBody = body
		}
	}
	state := &InstallState{
		SystemProxySet:            true,
		PreviousProxyAddr:         previousSysProxy,
		PreviousProxyEnabled:      previousSysProxy != "" && !isSelfProxy(previousSysProxy),
		PreviousUpstreamProxy:     strings.TrimSpace(cfg.UpstreamProxy),
		PreviousEnvVars:           previousEnvVars,
		PACFileSet:                true,
		PACFilePath:               pacFilePath(),
		PreviousAutoConfigURL:     previousAutoConfigURL,
		PreviousAutoConfigURLBody: previousPACBody,
		PreviousProxyOverride:     previousProxyOverride,
		PreviousAutoDetect:        previousAutoDetect,
		PreviousAutoDetectPresent: previousAutoDetectPresent,
		PortAtInstall:             listenPort,
		Version:                   3,
		SessionOnly:               true,
	}
	if err := saveInstallState(state); err != nil {
		log.Printf("[session] 保存临时恢复状态失败: %v", err)
	}

	pacURL, err := writePACFile(listenPort, cfg, previousPACBody)
	if err != nil {
		log.Printf("[session] PAC 文件生成失败: %v", err)
	} else if err := EnableSystemProxyPAC(pacURL); err != nil {
		log.Printf("[session] 启用临时 PAC 失败: %v", err)
	} else {
		fmt.Printf("  [会话] 已临时启用 AI 域名 PAC (端口 %d)\n", listenPort)
	}
	clearSelfProxyEnvVars()
	fmt.Println("  [会话] 已确保用户级 HTTP(S)_PROXY 不指向 ai-monitor，减少全局网络影响。")
}

// clearSelfProxyEnvVars 扫描当前进程环境 *和* 用户级注册表（HKCU\Environment），
// 把指向 ai-monitor 自己的旧值清除：
//   - http://127.0.0.1:<MITM 端口范围> 等 isSelfProxy 命中的代理
//   - 已知由 ai-monitor 写入的 NODE_EXTRA_CA_CERTS / CODEX_CA_CERTIFICATE 路径
//
// 历史 Bug：之前 v 与 userV 都调用 os.Getenv，注册表残留永远清不掉，导致
// 旧 --global-install 留下的 HTTP_PROXY 在卸载后仍指向已 dead 的 18090 端口，
// npm / pip / curl 全线超时。
func clearSelfProxyEnvVars() {
	seen := make(map[string]struct{}, len(proxyEnvKeys))
	var toClear []string
	add := func(key string) {
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		toClear = append(toClear, key)
	}

	for _, key := range proxyEnvKeys {
		procV := strings.TrimSpace(os.Getenv(key))
		userV := strings.TrimSpace(ReadUserLevelEnv(key))
		if isAIMonitorManagedEnvValue(procV) || isAIMonitorManagedEnvValue(userV) {
			add(key)
		}
	}
	if len(toClear) > 0 {
		ClearEnvProxy(toClear)
	}
}

// pruneGhostInstallState 在主流程开始前清理「会话残留」的 install_state。
//
// 触发场景：上一次 ai-monitor 以「会话 PAC 模式」启动并写了 install_state.json，
// 但进程被 taskkill /F、断电、Windows 直接关机等方式强制结束，没有跑到优雅
// 退出的 restoreSessionManagedProxyOnShutdown。此时下次启动会读到一份
//
//	{ "system_proxy_set": true, "session_only": true, ... }
//
// 而注册表里实际上要么已经被「修复网络.bat」清干净，要么从未真正写入。
// 如果不剪掉，applyTemporarySessionProxy 仍会把当前注册表内容（甚至 ai-monitor
// 自己的旧 PAC URL）当作"用户原 PAC"再次快照下来，形成自污染。
//
// 安全准则：仅在 install_state 标记为 SessionOnly 且当前注册表里没有任何
// AutoConfigURL / 自指 ProxyServer 时才清空；不会动持久化安装的 install_state。
func pruneGhostInstallState() {
	if runtime.GOOS != "windows" {
		return
	}
	st := loadInstallState()
	if st == nil || !st.SessionOnly {
		return
	}
	currentPAC := ReadCurrentAutoConfigURL()
	currentSys := readCurrentSystemProxy()
	hasOwnTakeover := isAIMonitorPACURL(currentPAC) || isSelfProxy(currentSys)
	hasUserPAC := currentPAC != "" && !isAIMonitorPACURL(currentPAC)
	hasUserSys := currentSys != "" && !isSelfProxy(currentSys)
	if !hasOwnTakeover && !hasUserPAC && !hasUserSys {
		// 既没我们的接管痕迹，也没用户原配置——这份 SessionOnly 状态完全失效。
		log.Println("[session] 检测到上一次会话异常退出残留的 install_state，已清理（注册表已无对应接管）。")
		clearInstallState()
	}
}

// printTakeoverBanner 在启动信息上方打印一段「当前接管模式」横幅，
// 让用户一眼能区分 ai-monitor 现在是否真的动了系统设置。
func printTakeoverBanner(mode string) {
	switch mode {
	case "persistent":
		fmt.Println("  ┌────────────────────────────────────────────────────────────┐")
		fmt.Println("  │ 接管模式: [持久安装] 系统 PAC + 用户级环境变量已重应用     │")
		fmt.Println("  │ 关闭程序后仅 dead 端口风险由 watchdog/--heal 兜底          │")
		fmt.Println("  │ 使用: 新开终端/多数软件易自动走监控；停用请 --global-uninstall  │")
		fmt.Println("  └────────────────────────────────────────────────────────────┘")
	case "session":
		fmt.Println("  ┌────────────────────────────────────────────────────────────┐")
		fmt.Println("  │ 接管模式: [本会话 PAC] AI 域名走 MITM，其他流量保持原状    │")
		fmt.Println("  │ 关闭本程序时自动还原系统代理/环境变量                      │")
		fmt.Println("  │ 使用: 推荐日常；Electron IDE 改 PAC 后建议重启或用向导启动   │")
		fmt.Println("  └────────────────────────────────────────────────────────────┘")
	default:
		fmt.Println("  ┌────────────────────────────────────────────────────────────┐")
		fmt.Println("  │ 接管模式: [观察]  未修改系统代理 / 环境变量 / IDE 配置     │")
		fmt.Println("  │ 仅监听本地端口；只有 --launch 子进程或显式指向本端口的     │")
		fmt.Println("  │ 流量会被监控。Cursor/VS Code/浏览器 等不受影响。           │")
		fmt.Println("  │ 使用: 不改系统；浏览器/IDE 默认路径一般不经过监控          │")
		fmt.Println("  └────────────────────────────────────────────────────────────┘")
	}
}

// printTakeoverModeQuickRef 在本进程控制台打印四种运行/清理操作的简要对照，
// 与向导网页「一键模式切换」同名按钮语义一致（说明集中在 exe 窗口，便于边看边操作）。
func printTakeoverModeQuickRef() {
	fmt.Println("  ┌────────────────────────────────────────────────────────────┐")
	fmt.Println("  │ 模式速览（向导网页「一键模式切换」同名）　　　　　　　　　 │")
	fmt.Println("  │ 观察　　　不改系统代理/PAC；指向本机 MITM 端口的流量才监控 │")
	fmt.Println("  │ 会话PAC　 仅 AI 域名经 PAC；关闭本程序自动还原　　　　　 │")
	fmt.Println("  │ 全局　　　PAC+用户环境变量持久；向导一键恢复或卸载参数停用 │")
	fmt.Println("  │ 一键恢复　清理 PAC/代理残留与 config 上游等　　　　　　　 │")
	fmt.Println("  └────────────────────────────────────────────────────────────┘")
	fmt.Println()
}

// printTakeoverHint 在「监控域名 / CA 路径」之后打印一段补充说明，
// 内容随接管模式而变，避免「日志说一套，实际做一套」的认知错位。
//
// observe 模式下额外读取 cfg：若用户在 config.json 里显式 auto_install_session_pac=false，
// 给出一行 PowerShell 命令一键升级（针对 v3.1.0/v3.1.1 的老 config 文件，新默认 true 不会
// 自动覆盖已写入的 false）。
func printTakeoverHint(mode string, cfg *Config) {
	switch mode {
	case "persistent":
		fmt.Println("  说明: 已重应用持久安装：系统 PAC + 用户级 HTTP_PROXY 指向本程序。")
		fmt.Println("        如需停掉持久接管，请使用 --global-uninstall（或运行『卸载.bat』）。")
	case "session":
		fmt.Println("  说明: 本窗口打开期间临时启用 AI 域名白名单 PAC；关闭后自动恢复本机网络。")
		fmt.Println("        若希望本机不接管：在 config.json 设 \"auto_install_session_pac\": false。")
	default:
		fmt.Println("  说明: 当前 *未* 修改系统代理/环境变量/IDE 配置（观察模式 = 不监控）。")
		if cfg != nil && cfg.AutoInstallSessionPAC != nil && !*cfg.AutoInstallSessionPAC {
			fmt.Println("        原因: config.json 里显式设置了 \"auto_install_session_pac\": false。")
			fmt.Println("        一行恢复（先关闭本进程再跑）：")
			fmt.Println(`          powershell -NoProfile -Command "(Get-Content $env:APPDATA\ai-monitor\config.json) -replace '\"auto_install_session_pac\"\s*:\s*false','\"auto_install_session_pac\": true' | Set-Content $env:APPDATA\ai-monitor\config.json -Encoding UTF8"`)
		}
		fmt.Println("        其它接管方式：")
		fmt.Println("          · 本次会话临时接管：重新运行并加 --with-session-pac；")
		fmt.Println("          · 永久接管（开机自启 + 写 PAC/环境变量）：--global-install；")
		fmt.Println("          · 仅当前进程：ai-monitor --launch <程序> 启动目标。")
	}
}

// restoreSessionManagedProxyOnShutdown runs after the MITM server stops. When install_state
// records SystemProxySet, restores system proxy/PAC and environment variables so closing
// ai-monitor leaves the machine's network configuration as it was before monitoring.
func restoreSessionManagedProxyOnShutdown() {
	if runtime.GOOS != "windows" {
		return
	}
	st := loadInstallState()
	if st == nil || !st.SystemProxySet {
		return
	}

	fmt.Println("\n  [会话] 正在恢复系统代理/PAC 与用户环境变量…")
	restoreProxyFromState(st)
	restoreOrClearEnvVars(st)
	if st.SessionOnly {
		clearInstallState()
	}
	fmt.Println("  [会话] 已恢复。关闭 ai-monitor 后不再接管本机网络。")
}

// restoreProxyFromState undoes what doGlobalInstall set up: removes PAC file,
// clears AutoConfigURL, and restores the user's previous proxy configuration.
func restoreProxyFromState(state *InstallState) {
	if state == nil {
		// 无快照时只清理 ai-monitor 自己留下的接管痕迹；不碰用户原有代理。
		currentPAC := ReadCurrentAutoConfigURL()
		if isAIMonitorPACURL(currentPAC) {
			removePACFile()
			DisableSystemProxyPAC()
		}
		currentProxy := readCurrentSystemProxy()
		if isSelfProxy(currentProxy) {
			DisableSystemProxy()
		}
		return
	}
	if state != nil && state.PACFileSet {
		// PAC-based install: clean up PAC file and registry
		removePACFile()
		DisableSystemProxyPAC()
		// Restore previous AutoConfigURL if user had one before our install
		if state.PreviousAutoConfigURL != "" {
			fmt.Printf("    ℹ 恢复之前的 PAC: %s\n", state.PreviousAutoConfigURL)
			if err := EnableSystemProxyPAC(state.PreviousAutoConfigURL); err != nil {
				log.Printf("    ⚠ 恢复 PAC 失败: %v", err)
			}
		} else if state.PreviousProxyEnabled && state.PreviousProxyAddr != "" {
			// User had a manual proxy before our install
			fmt.Printf("    ℹ 恢复之前的系统代理: %s\n", state.PreviousProxyAddr)
			if err := EnableSystemProxy(state.PreviousProxyAddr, ""); err != nil {
				log.Printf("    ⚠ 恢复系统代理失败: %v", err)
			}
		}
	} else {
		// Legacy (pre-PAC) install: use old restore logic
		restoreWinInetProxyFromState(state)
	}
}

// resolveActualPort determines the port that IDE settings should point to.
// If a running instance exists, use its port. Otherwise probe to find the
// port that would actually be bound.
func resolveActualPort(cfg *Config) int {
	// Check if an existing instance is running
	if port, alive := checkExistingInstance(); alive {
		log.Printf("[install] 检测到已运行的 ai-monitor 实例，使用端口 %d", port)
		return port
	}

	// No running instance — probe which port would be bound
	ln, port, err := tryListenMitmPort(cfg.Port)
	if err != nil {
		log.Printf("[install] 端口探测失败: %v，使用配置端口 %d", err, cfg.Port)
		return cfg.Port
	}
	ln.Close()
	return port
}
