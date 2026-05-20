package main

import (
	"bufio"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

var percentEnvPattern = regexp.MustCompile(`%([^%]+)%`)

type launchPreset struct {
	Name        string
	Description string
	Candidates  []string
	Args        []string
	KnownPaths  []string
}

var managedLaunchPresets = []launchPreset{
	{
		Name:        "vscode",
		Description: "启动 VS Code（仅当前进程走本地 MITM）",
		Candidates:  []string{"code.cmd", "code.exe", "code"},
		KnownPaths: []string{
			"%LOCALAPPDATA%\\Programs\\Microsoft VS Code\\Code.exe",
			"%PROGRAMFILES%\\Microsoft VS Code\\Code.exe",
			"%PROGRAMFILES(X86)%\\Microsoft VS Code\\Code.exe",
		},
	},
	{
		Name:        "cursor",
		Description: "启动 Cursor（仅当前进程走本地 MITM）",
		Candidates:  []string{"cursor.exe", "cursor.cmd", "cursor"},
		KnownPaths: []string{
			"%LOCALAPPDATA%\\Programs\\Cursor\\Cursor.exe",
			"%PROGRAMFILES%\\Cursor\\Cursor.exe",
			"%PROGRAMFILES(X86)%\\Cursor\\Cursor.exe",
		},
	},
	{
		Name:        "windsurf",
		Description: "启动 Windsurf",
		Candidates:  []string{"windsurf.exe", "windsurf.cmd", "windsurf"},
		KnownPaths: []string{
			"%LOCALAPPDATA%\\Programs\\Windsurf\\Windsurf.exe",
		},
	},
	{
		Name:        "kiro",
		Description: "启动 Kiro",
		Candidates:  []string{"kiro.exe", "kiro.cmd", "kiro"},
		KnownPaths: []string{
			"%LOCALAPPDATA%\\Programs\\Kiro\\Kiro.exe",
		},
	},
	{
		Name:        "vscodium",
		Description: "启动 VS Codium",
		Candidates:  []string{"codium.cmd", "codium.exe", "codium"},
		KnownPaths: []string{
			"%LOCALAPPDATA%\\Programs\\VSCodium\\VSCodium.exe",
			"%PROGRAMFILES%\\VSCodium\\VSCodium.exe",
		},
	},
	{
		Name:        "trae",
		Description: "启动 Trae",
		Candidates:  []string{"trae.exe", "trae.cmd", "trae"},
		KnownPaths: []string{
			"%LOCALAPPDATA%\\Programs\\Trae\\Trae.exe",
		},
	},
	{
		Name:        "zed",
		Description: "启动 Zed 编辑器",
		Candidates:  []string{"zed.exe", "zed"},
		KnownPaths: []string{
			"%LOCALAPPDATA%\\Programs\\Zed\\zed.exe",
		},
	},
	{
		Name:        "codex",
		Description: "启动 Codex CLI（仅当前进程走本地 MITM）",
		Candidates:  []string{"codex.cmd", "codex.exe", "codex"},
		KnownPaths: []string{
			"%APPDATA%\\npm\\codex.cmd",
			"/usr/local/bin/codex",
			"/opt/homebrew/bin/codex",
		},
	},
	{
		Name:        "idea",
		Description: "启动 IntelliJ IDEA",
		Candidates:  []string{"idea64.exe", "idea.cmd", "idea"},
		KnownPaths: []string{
			"%PROGRAMFILES%\\JetBrains\\IntelliJ IDEA *\\bin\\idea64.exe",
		},
	},
	{
		Name:        "webstorm",
		Description: "启动 WebStorm",
		Candidates:  []string{"webstorm64.exe", "webstorm.cmd", "webstorm"},
		KnownPaths: []string{
			"%PROGRAMFILES%\\JetBrains\\WebStorm *\\bin\\webstorm64.exe",
		},
	},
	{
		Name:        "pycharm",
		Description: "启动 PyCharm",
		Candidates:  []string{"pycharm64.exe", "pycharm.cmd", "pycharm"},
		KnownPaths: []string{
			"%PROGRAMFILES%\\JetBrains\\PyCharm *\\bin\\pycharm64.exe",
		},
	},
	{
		Name:        "goland",
		Description: "启动 GoLand",
		Candidates:  []string{"goland64.exe", "goland.cmd", "goland"},
		KnownPaths: []string{
			"%PROGRAMFILES%\\JetBrains\\GoLand *\\bin\\goland64.exe",
		},
	},
	{
		Name:        "powershell",
		Description: "启动 PowerShell 终端（适合再在里面运行 CLI 工具）",
		Candidates:  []string{"pwsh.exe", "powershell.exe"},
		KnownPaths: []string{
			"%PROGRAMFILES%\\PowerShell\\7\\pwsh.exe",
			"%PROGRAMFILES(X86)%\\PowerShell\\7\\pwsh.exe",
			"%SYSTEMROOT%\\System32\\WindowsPowerShell\\v1.0\\powershell.exe",
		},
	},
	{
		Name:        "cmd",
		Description: "启动 CMD 终端（适合再在里面运行 CLI 工具）",
		Candidates:  []string{"cmd.exe"},
		KnownPaths:  []string{"%SYSTEMROOT%\\System32\\cmd.exe"},
	},
	{
		Name:        "claude-code",
		Description: "启动 Claude Code CLI（Node.js，需注入 HTTPS_PROXY 才能被代理统计）",
		Candidates:  []string{"claude.cmd", "claude.exe", "claude"},
		KnownPaths: []string{
			"%APPDATA%\\npm\\claude.cmd",
			"%APPDATA%\\npm\\claude.ps1",
		},
	},
	{
		Name:        "qoder",
		Description: "启动 Qoder IDE",
		Candidates:  []string{"qoder.exe", "qoder.cmd", "qoder"},
		KnownPaths: []string{
			"%LOCALAPPDATA%\\Programs\\Qoder\\Qoder.exe",
			"%PROGRAMFILES%\\Qoder\\Qoder.exe",
		},
	},
}

type monitorRuntime struct {
	reporter       *Reporter
	reporterCancel context.CancelFunc
	server         *http.Server
	listener       net.Listener
	proxy          *ProxyServer // 持有引用以便注入接管模式 / 运行期更新
	proxyAddr      string
	listenPort     int
	compatLn       []net.Listener
	gatewayServer  *http.Server // nil if gateway_port not configured
	gatewayLn      net.Listener // nil if gateway_port not configured
	gatewayPort    int
}

// SetTakeoverMode 透传到内部 ProxyServer，让 /status 能上报真实模式。
// 在 main.go 决定 takeoverMode 后调用一次即可。
func (r *monitorRuntime) SetTakeoverMode(mode string) {
	if r == nil || r.proxy == nil {
		return
	}
	r.proxy.SetTakeoverMode(mode)
}

func startMonitorRuntime(cfg *Config, certMgr *CertManager, sourceApp string, configPath string) (*monitorRuntime, error) {
	reporter := NewReporter(cfg)
	reporter.sourceApp = sourceApp
	reporterCtx, reporterCancel := context.WithCancel(context.Background())
	go reporter.Start(reporterCtx)
	// 后台同步 Cursor 官方精确用量；失败/未登录会静默降级到 MITM 估算。
	StartCursorOfficialSync(reporterCtx, reporter)
	// 启动时打一次内存快照 + 每 10 分钟一次，便于回溯长跑后的内存涨幅。
	logMemStatsSnapshot("start")
	startMemStatsTicker()

	ctxPing, cancelPing := context.WithTimeout(context.Background(), 6*time.Second)
	go func() {
		defer cancelPing()
		if err := reporter.PingServer(ctxPing); err != nil {
			log.Printf("[启动] 探测上报服务器 /health 失败: %v（心跳与上报将自动重试）", err)
		} else {
			log.Printf("[启动] 上报服务器 %s 可达", cfg.ServerURL)
		}
	}()

	proxy := NewProxyServer(cfg, reporter, certMgr, configPath)
	ln, listenPort, err := tryListenMitmPort(cfg.Port)
	if err != nil {
		reporterCancel() // 失败路径：取消 reporter ctx，避免 goroutine + context 泄漏
		return nil, err
	}
	proxy.listenPort = listenPort

	// 认证失败时引导用户到登录向导。
	// 仅在完全没有 token 时（首次使用）自动打开浏览器；
	// token 已存在但失效时只打日志，避免在 Mac/Linux 上反复弹出浏览器窗口。
	reporter.OnAuthFailed = func() {
		wizardURL := fmt.Sprintf("http://127.0.0.1:%d/wizard", listenPort)
		if cfg.AuthToken != "" || cfg.APIKey != "" {
			log.Printf("[认证] 令牌已失效，请访问 %s 重新登录", wizardURL)
			return
		}
		// 稍等 MITM 端口监听起来，避免抢跑
		time.Sleep(500 * time.Millisecond)
		log.Printf("[认证] 已打开登录向导 %s", wizardURL)
		openWizardOrBrowser(wizardURL, "AI Token 监控")
	}

	rt := &monitorRuntime{
		reporter:       reporter,
		reporterCancel: reporterCancel,
		server: &http.Server{
			Handler:           proxy,
			ReadHeaderTimeout: 30 * time.Second,
			ReadTimeout:       0,
			WriteTimeout:      0,
			IdleTimeout:       120 * time.Second,
		},
		listener:   ln,
		proxy:      proxy,
		proxyAddr:  fmt.Sprintf("127.0.0.1:%d", listenPort),
		listenPort: listenPort,
	}
	startCompatProxyListeners(rt, cfg)

	// Start dedicated gateway port if configured (no MITM, no cert needed).
	if cfg.GatewayPort > 0 && cfg.GatewayPort != cfg.Port {
		gwHandler := newGatewayOnlyHandler(proxy)
		gwLn, gwPort, err := tryListenMitmPort(cfg.GatewayPort)
		if err != nil {
			log.Printf("[gateway] 无法监听端口 %d: %v（Gateway 路由在主端口 %d 仍可用）", cfg.GatewayPort, err, listenPort)
		} else {
			rt.gatewayServer = &http.Server{
				Handler:           gwHandler,
				ReadHeaderTimeout: 30 * time.Second,
				IdleTimeout:       120 * time.Second,
			}
			rt.gatewayLn = gwLn
			rt.gatewayPort = gwPort
			log.Printf("[gateway] API Gateway 监听端口 %d（无 MITM）", gwPort)
		}
	}

	return rt, nil
}

func (m *monitorRuntime) Shutdown(ctx context.Context) error {
	m.reporterCancel() // signal reporter goroutine to exit (it will do a final Flush)
	if m.gatewayServer != nil {
		m.gatewayServer.Shutdown(ctx)
	}
	return m.server.Shutdown(ctx)
}

func startCompatProxyListeners(rt *monitorRuntime, cfg *Config) {
	for _, port := range compatProxyPorts(cfg, rt.listenPort) {
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			continue
		}
		rt.compatLn = append(rt.compatLn, ln)
		log.Printf("[proxy] 兼容监听旧代理端口 127.0.0.1:%d → 当前代理处理器", port)
		go func(port int, ln net.Listener) {
			if err := rt.server.Serve(ln); err != nil && err != http.ErrServerClosed {
				log.Printf("[proxy] 兼容端口 %d 退出: %v", port, err)
			}
		}(port, ln)
	}
}

func compatProxyPorts(cfg *Config, listenPort int) []int {
	seen := map[int]struct{}{listenPort: {}}
	var ports []int
	add := func(port int) {
		if !isAIMonitorPort(port) {
			return
		}
		if _, ok := seen[port]; ok {
			return
		}
		seen[port] = struct{}{}
		ports = append(ports, port)
	}
	if cfg != nil {
		add(cfg.Port)
		add(cfg.Port + 1)
	}
	if st := loadInstallState(); st != nil {
		add(st.PortAtInstall)
	}
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy"} {
		_, port, ok := proxyHostPort(os.Getenv(key))
		if ok {
			add(port)
		}
	}
	return ports
}

func isAIMonitorPort(port int) bool {
	return port >= 18090 && port <= 18090+mitmPortMaxFallback-1
}

func runManagedProcess(cfg *Config, certMgr *CertManager, args []string, presetName string, configPath string) error {
	commandArgs, preset, err := resolveLaunchCommand(args, presetName, exec.LookPath)
	if err != nil {
		return err
	}
	if err := ensureManagedPresetProcessNotRunning(preset); err != nil {
		return err
	}

	// Singleton check for launch mode: if a healthy instance is already running, reuse it
	// instead of starting another one. We still launch the child process but skip starting a new proxy.
	existingPort, alive := checkExistingInstance()
	if alive {
		log.Printf("[launch] 检测到已运行的 ai-monitor 实例 (端口 %d)，复用已有实例", existingPort)
		return launchChildWithExistingProxy(cfg, certMgr, commandArgs, preset, existingPort)
	}
	removeInstanceInfo()

	if err := certMgr.InstallCA(); err != nil {
		log.Printf("[launch] 安装 CA 失败，请手动信任 %s: %v", certMgr.CACertPath(), err)
	} else {
		log.Printf("[launch] 已确保 CA 证书安装到当前用户信任存储")
	}

	sourceApp := inferSourceApp(commandArgs, preset)
	runtime, err := startMonitorRuntime(cfg, certMgr, sourceApp, configPath)
	if err != nil {
		return err
	}
	if err := writeInstanceInfo(runtime.listenPort); err != nil {
		log.Printf("[launch] 写入 instance.json 失败: %v", err)
	}
	applySessionManagedProxy(cfg, certMgr, runtime.listenPort)
	go func() {
		if err := runtime.server.Serve(runtime.listener); err != nil && err != http.ErrServerClosed {
			log.Printf("[launch] 本地 MITM 退出: %v", err)
		}
	}()

	httpProxy := "http://" + runtime.proxyAddr
	noProxy := buildNoProxyEnvWithConfig(cfg)
	envVars := map[string]string{
		"HTTP_PROXY":             httpProxy,
		"HTTPS_PROXY":            httpProxy,
		"NO_PROXY":               noProxy,
		"http_proxy":             httpProxy,
		"https_proxy":            httpProxy,
		"no_proxy":               noProxy,
		"OPENAI_BASE_URL":        httpProxy + "/openai/v1",
		"OPENAI_API_BASE":        httpProxy + "/openai/v1",
		"ANTHROPIC_BASE_URL":     httpProxy + "/anthropic",
		"AI_MONITOR_LAUNCH_MODE": "managed-process",
		"AI_MONITOR_SOURCE_APP":  sourceApp,
		"NODE_EXTRA_CA_CERTS":    certMgr.CACertPath(),
		"SSL_CERT_FILE":          certMgr.CACertPath(),
		"CODEX_CA_CERTIFICATE":   certMgr.CACertPath(),
	}
	if preset != nil {
		envVars["AI_MONITOR_LAUNCH_PRESET"] = preset.Name
	}

	cmd := exec.Command(commandArgs[0], commandArgs[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = mergeEnv(os.Environ(), envVars)

	if preset != nil {
		log.Printf("[launch] 使用预设 %q 启动受管应用: %s", preset.Name, strings.Join(commandArgs, " "))
	} else {
		log.Printf("[launch] 仅对目标进程注入代理环境变量: %s", strings.Join(commandArgs, " "))
	}
	err = cmd.Run()
	removeInstanceInfo()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	shutdownErr := runtime.Shutdown(ctx)
	restoreSessionManagedProxyOnShutdown()
	if err != nil {
		return err
	}
	return shutdownErr
}

// launchChildWithExistingProxy launches the target process pointing at an already-running
// ai-monitor instance. No new proxy is started.
func launchChildWithExistingProxy(cfg *Config, certMgr *CertManager, commandArgs []string, preset *launchPreset, port int) error {
	sourceApp := inferSourceApp(commandArgs, preset)
	envVars := buildManagedLaunchEnv(cfg, certMgr, sourceApp, port, preset)

	cmd := exec.Command(commandArgs[0], commandArgs[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = mergeEnv(os.Environ(), envVars)

	log.Printf("[launch] 复用已有代理 (127.0.0.1:%d)，启动: %s", port, strings.Join(commandArgs, " "))
	return cmd.Run()
}

func buildManagedLaunchEnv(cfg *Config, certMgr *CertManager, sourceApp string, port int, preset *launchPreset) map[string]string {
	httpProxy := fmt.Sprintf("http://127.0.0.1:%d", port)
	noProxy := buildNoProxyEnvWithConfig(cfg)
	envVars := map[string]string{
		"HTTP_PROXY":             httpProxy,
		"HTTPS_PROXY":            httpProxy,
		"NO_PROXY":               noProxy,
		"http_proxy":             httpProxy,
		"https_proxy":            httpProxy,
		"no_proxy":               noProxy,
		"OPENAI_BASE_URL":        httpProxy + "/openai/v1",
		"OPENAI_API_BASE":        httpProxy + "/openai/v1",
		"ANTHROPIC_BASE_URL":     httpProxy + "/anthropic",
		"AI_MONITOR_LAUNCH_MODE": "managed-process",
		"AI_MONITOR_SOURCE_APP":  sourceApp,
		"NODE_EXTRA_CA_CERTS":    certMgr.CACertPath(),
		"SSL_CERT_FILE":          certMgr.CACertPath(),
		"CODEX_CA_CERTIFICATE":   certMgr.CACertPath(),
	}
	if preset != nil {
		envVars["AI_MONITOR_LAUNCH_PRESET"] = preset.Name
	}
	return envVars
}

// launchChildWithExistingProxyDetached launches a managed child process without waiting for exit.
// Used by the web console one-click launcher to avoid blocking HTTP requests.
func launchChildWithExistingProxyDetached(cfg *Config, certMgr *CertManager, commandArgs []string, preset *launchPreset, port int) error {
	if len(commandArgs) == 0 {
		return fmt.Errorf("empty launch command")
	}
	sourceApp := inferSourceApp(commandArgs, preset)
	envVars := buildManagedLaunchEnv(cfg, certMgr, sourceApp, port, preset)
	if runtime.GOOS == "windows" {
		// 优先用 newDetachedCmd 直接 spawn 目标，避开 cmd /c start "" 包装。
		// 少一层 cmd+conhost 能显著减少 Default Desktop 堆占用，
		// 缓解长时间运行后偶发的 "Not enough memory resources" CreateProcess 失败。
		cmd := newDetachedCmd(commandArgs[0], commandArgs[1:]...)
		cmd.Env = mergeEnv(os.Environ(), envVars)
		return cmd.Start()
	}
	cmd := exec.Command(commandArgs[0], commandArgs[1:]...)
	cmd.Env = mergeEnv(os.Environ(), envVars)
	return cmd.Start()
}

func inferSourceApp(commandArgs []string, preset *launchPreset) string {
	if preset != nil && strings.TrimSpace(preset.Name) != "" {
		return preset.Name
	}
	if len(commandArgs) == 0 {
		return ""
	}
	base := strings.ToLower(commandBaseName(commandArgs[0]))
	for _, ext := range []string{".exe", ".cmd", ".bat"} {
		base = strings.TrimSuffix(base, ext)
	}
	switch base {
	case "code", "vscode":
		return "vscode"
	case "cursor":
		return "cursor"
	case "windsurf":
		return "windsurf"
	case "kiro":
		return "kiro"
	case "codium", "vscodium":
		return "vscodium"
	case "trae":
		return "trae"
	case "zed":
		return "zed"
	case "codex":
		return "codex"
	case "idea", "idea64":
		return "jetbrains"
	case "webstorm", "webstorm64":
		return "jetbrains"
	case "pycharm", "pycharm64":
		return "jetbrains"
	case "goland", "goland64":
		return "jetbrains"
	case "pwsh", "powershell":
		return "powershell"
	case "cmd":
		return "cmd"
	default:
		return base
	}
}

func commandBaseName(path string) string {
	base := filepath.Base(path)
	if strings.Contains(base, `\`) {
		parts := strings.Split(base, `\`)
		base = parts[len(parts)-1]
	}
	return base
}

func resolveLaunchCommand(args []string, presetName string, lookPath func(string) (string, error)) ([]string, *launchPreset, error) {
	presetName = strings.TrimSpace(strings.ToLower(presetName))
	if presetName == "" {
		if len(args) == 0 {
			return nil, nil, fmt.Errorf("--launch 后需要提供目标程序，例如: ai-monitor.exe --launch code.cmd；或使用 --launch-preset vscode")
		}
		return args, nil, nil
	}

	preset := findLaunchPreset(presetName)
	if preset == nil {
		return nil, nil, fmt.Errorf("未知 launch 预设 %q，可先执行 --list-launch-presets 查看", presetName)
	}

	resolved, candidate, err := resolvePresetBinary(*preset, lookPath, fileExists)
	if err != nil {
		return nil, nil, fmt.Errorf("launch 预设 %q 未找到可执行文件（尝试过: %s）", preset.Name, strings.Join(candidate, ", "))
	}

	// 解析到 .cmd / .bat shim 而预设属于 Electron GUI 时（VS Code / Cursor / Kiro 等），
	// 多数 shim 会把真实 GUI 进程 detach（如 C:\Program Files\cursor\resources\app\bin\cursor.cmd
	// 启动 Cursor.exe 后立刻退出），导致 ai-monitor 跟着退出，监控失效。
	// 此处只能告警；真正的修复在 resolvePresetBinary —— 对 GUI 预设优先扫 KnownPaths。
	if isGUIPreset(preset) && isShimExecutable(resolved) {
		log.Printf("[launch] ⚠ 预设 %q 命中了脱钩 shim %s。建议把 GUI 主程序加入 KnownPaths 或修复 PATH，使 ai-monitor 能等待 GUI 进程退出。",
			preset.Name, resolved)
	}

	command := []string{resolved}
	command = append(command, preset.Args...)
	command = append(command, args...)
	return command, preset, nil
}

// isGUIPreset 判断预设是否启动 Electron 类 GUI 进程。
// 复用 managedPresetProcessImage —— 凡能给出 ImageName 的都是会被 ai-monitor
// 严格 singleton 检查的 Electron GUI（VS Code / Cursor / Windsurf / Kiro / VSCodium / Trae）。
// 这些应用的 .cmd shim 几乎都会脱钩主 GUI 进程，需要特殊解析顺序。
func isGUIPreset(preset *launchPreset) bool {
	img, _ := managedPresetProcessImage(preset)
	return img != ""
}

// isShimExecutable 判断解析出的可执行文件是否是 *.cmd / *.bat shim —— 这类入口
// 通常立即 spawn GUI 并退出，不适合作为 cmd.Run() 同步等待的目标。
func isShimExecutable(path string) bool {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(path)))
	return ext == ".cmd" || ext == ".bat" || ext == ".ps1"
}

// runningElectronEditor 表示一个正在跑且会被 PAC 接管影响的 Electron 编辑器实例。
// 仅用于「ai-monitor 启动时提示用户重启」的展示与重启决策，不持久化。
type runningElectronEditor struct {
	Preset    string // 对应 launchPreset.Name，例如 "cursor" / "vscode"
	ImageName string // tasklist 的 IMAGENAME，例如 "Cursor.exe"
	Display   string // 给用户看的展示名，例如 "Cursor"
}

// detectRunningElectronEditors 扫描当前主机上正在跑的、且依赖系统 PAC / 环境变量
// 才能被 ai-monitor 接管的 Electron 编辑器。返回顺序与 managedLaunchPresets 一致，
// 方便测试断言。
//
// 之所以单列 Electron 类：这类应用启动后**不会动态拾取 PAC 变更**，必须
// 重启进程才能让新写的会话 PAC 生效；JetBrains 系不走 system PAC（走自己
// 的 settings + JVM proxy），不在此列。
func detectRunningElectronEditors(presetRunning func(string) bool) []runningElectronEditor {
	var out []runningElectronEditor
	seen := map[string]struct{}{}
	for _, p := range managedLaunchPresets {
		img, display := managedPresetProcessImage(&p)
		if img == "" {
			continue
		}
		key := strings.ToLower(img)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if presetRunning(img) {
			out = append(out, runningElectronEditor{
				Preset:    p.Name,
				ImageName: img,
				Display:   display,
			})
		}
	}
	return out
}

// promptRestartElectronEditors 在 stdin 是交互式终端时，把 editors 列出来
// 询问用户「是否帮你重启它们让监控立即生效？[Y/n]」。
//
// 非交互式 stdin（例如开机自启的后台进程、被 nohup / start /b 启动）跳过提示，
// 仅打印一段"提示用户重启"的告警日志，避免阻塞启动。
//
// 用户输入：
//   - Y / 回车   → 调用 killAndRelaunchEditors 一一处理
//   - N         → 仅打印警告
//   - 其它      → 视为 N
func promptRestartElectronEditors(editors []runningElectronEditor, in io.Reader, out io.Writer, isInteractive bool) bool {
	if len(editors) == 0 {
		return false
	}
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "  ┌────────────────────────────────────────────────────────────┐")
	fmt.Fprintln(out, "  │ 检测到以下 IDE 已在运行，*不会* 自动捕获新写入的会话 PAC： │")
	for _, e := range editors {
		fmt.Fprintf(out, "  │    · %-50s  │\n", e.Display+" ("+e.ImageName+")")
	}
	fmt.Fprintln(out, "  │ 你需要重启这些 IDE 才能让监控立即生效。                    │")
	fmt.Fprintln(out, "  └────────────────────────────────────────────────────────────┘")
	if !isInteractive {
		fmt.Fprintln(out, "  ⓘ 当前为非交互式启动，跳过 Y/N 询问；请手动关闭并重开这些 IDE。")
		return false
	}
	fmt.Fprint(out, "  是否帮你立刻 *关闭* 它们并重新启动？[Y/n] ")
	reader := bufio.NewReader(in)
	line, _ := reader.ReadString('\n')
	answer := strings.ToLower(strings.TrimSpace(line))
	if answer == "n" || answer == "no" {
		fmt.Fprintln(out, "  → 已跳过。请你方便时手动重启这些 IDE，监控才会生效。")
		return false
	}
	return true
}

// stdinIsInteractive 判断当前进程的 stdin 是不是一个终端 / 控制台。
// 用于避免在 start /b 后台启动 / Windows 计划任务下尝试 ReadString 阻塞主流程。
func stdinIsInteractive() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	mode := info.Mode()
	// 终端（控制台、tty）会带 ModeCharDevice；管道/重定向不会。
	return mode&os.ModeCharDevice != 0
}

// killAndRelaunchEditors 终止指定 Electron IDE 进程，等其端口/锁释放后用对应预设
// 的 KnownPaths（v3.1.1+ 已避开 .cmd shim）detached 重新启动一次。
//
// 失败仅打印 warning，**不阻断 ai-monitor 主流程**——监控本身已经在跑了，
// 用户手动重开 IDE 也可以让 PAC 生效。
func killAndRelaunchEditors(editors []runningElectronEditor, out io.Writer) {
	for _, e := range editors {
		fmt.Fprintf(out, "  [%s] 终止进程 %s ...\n", e.Display, e.ImageName)
		if err := newHiddenCmd("taskkill", "/F", "/IM", e.ImageName).Run(); err != nil {
			fmt.Fprintf(out, "    ⚠ taskkill /F /IM %s 失败: %v\n", e.ImageName, err)
			continue
		}
	}
	// 给文件锁与端口腾出释放时间，否则 Electron 立刻重启容易报 single-instance lock。
	time.Sleep(1500 * time.Millisecond)

	for _, e := range editors {
		preset := findLaunchPreset(e.Preset)
		if preset == nil {
			fmt.Fprintf(out, "  [%s] 未找到对应 launchPreset，跳过自动重启。\n", e.Display)
			continue
		}
		resolved, _, err := resolvePresetBinary(*preset, exec.LookPath, fileExists)
		if err != nil {
			fmt.Fprintf(out, "  [%s] 未在 KnownPaths/PATH 找到主程序，请手动重开。\n", e.Display)
			continue
		}
		if isShimExecutable(resolved) {
			fmt.Fprintf(out, "  [%s] 命中 shim %s（KnownPaths 全部未命中），自动重启可能脱钩；请手动重开。\n",
				e.Display, resolved)
			continue
		}
		fmt.Fprintf(out, "  [%s] 启动 %s ...\n", e.Display, resolved)
		if err := relaunchDetached(resolved); err != nil {
			fmt.Fprintf(out, "    ⚠ 启动失败: %v；请手动重开 %s。\n", err, e.Display)
			continue
		}
		fmt.Fprintf(out, "    ✓ 已请求启动 %s（GUI 在后台拉起，可能需要 1-3 秒）\n", e.Display)
	}
}

// relaunchDetached 在 Windows 用 `cmd /c start "" path` 完成完全脱钩——
// ai-monitor 之后退出时不会牵连 GUI；GUI 也不会继承 ai-monitor 的 stdin/stdout。
// 非 Windows 平台直接 exec.Command(path).Start()，对 GUI Editor 已经够用。
func relaunchDetached(path string) error {
	if runtime.GOOS == "windows" {
		// 直接 spawn 目标 .exe，跳过 cmd /c start "" 包装。
		// 见 newDetachedCmd 注释：少一层 cmd+conhost 可缓解桌面堆耗尽问题。
		return newDetachedCmd(path).Start()
	}
	cmd := exec.Command(path)
	return cmd.Start()
}

func findLaunchPreset(name string) *launchPreset {
	for idx := range managedLaunchPresets {
		preset := &managedLaunchPresets[idx]
		if preset.Name == name {
			return preset
		}
	}
	return nil
}

func ensureManagedPresetProcessNotRunning(preset *launchPreset) error {
	imageName, displayName := managedPresetProcessImage(preset)
	if imageName == "" {
		return nil
	}
	running, err := isProcessImageRunning(imageName)
	if err != nil {
		log.Printf("[launch] 检查现有 %s 进程失败，跳过预检查: %v", displayName, err)
		return nil
	}
	if running {
		return fmt.Errorf("检测到已运行的 %s 进程。请先彻底退出现有 %s（包括后台残留窗口），再使用启动器重新打开，否则会复用旧实例，导致监控不生效", displayName, displayName)
	}
	return nil
}

func managedPresetProcessImage(preset *launchPreset) (imageName, displayName string) {
	if preset == nil {
		return "", ""
	}
	switch strings.ToLower(strings.TrimSpace(preset.Name)) {
	case "vscode":
		return "Code.exe", "VS Code"
	case "cursor":
		return "Cursor.exe", "Cursor"
	case "windsurf":
		return "Windsurf.exe", "Windsurf"
	case "kiro":
		return "Kiro.exe", "Kiro"
	case "vscodium":
		return "VSCodium.exe", "VS Codium"
	case "trae":
		return "Trae.exe", "Trae"
	default:
		return "", ""
	}
}

func isProcessImageRunning(imageName string) (bool, error) {
	if strings.TrimSpace(imageName) == "" {
		return false, nil
	}
	if runtime.GOOS != "windows" {
		return false, nil
	}
	out, err := newHiddenCmd("tasklist", "/FI", fmt.Sprintf("IMAGENAME eq %s", imageName), "/FO", "CSV", "/NH").Output()
	if err != nil {
		return false, err
	}
	text := strings.TrimSpace(string(out))
	if text == "" || strings.HasPrefix(strings.ToUpper(text), "INFO:") {
		return false, nil
	}
	reader := csv.NewReader(strings.NewReader(text))
	for {
		record, err := reader.Read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return false, err
		}
		if len(record) > 0 && strings.EqualFold(strings.TrimSpace(record[0]), imageName) {
			return true, nil
		}
	}
	return false, nil
}

// resolvePresetBinary 解析预设可执行文件路径。
//
// 关键修复（v3.1.0）：
//   - 对 GUI 类预设（VS Code / Cursor / Windsurf / Kiro / VSCodium / Trae）
//     先扫 KnownPaths 找真实 .exe，命中即用。这样可避免 PATH 里的 .cmd shim
//     把 GUI 主进程 detach 后立刻退出 → ai-monitor 跟着退出 → 监控失效。
//   - 仅当 KnownPaths 全部不存在时才回退到 lookPath（候选名）。CLI 类预设
//     （cmd / powershell / claude-code / codex 等）保留原优先级，因为它们的
//     入口通常就是 PATH 中的可执行命令。
//
// tried 仍按实际尝试顺序追加，便于错误信息复现失败路径。
func resolvePresetBinary(preset launchPreset, lookPath func(string) (string, error), exists func(string) bool) (string, []string, error) {
	tried := make([]string, 0, len(preset.Candidates)+len(preset.KnownPaths))

	tryKnownPaths := func() (string, bool) {
		for _, rawPath := range preset.KnownPaths {
			resolved := expandEnvPath(rawPath)
			if strings.TrimSpace(resolved) == "" {
				continue
			}
			tried = append(tried, resolved)
			if exists(resolved) {
				return resolved, true
			}
		}
		return "", false
	}
	tryLookPath := func() (string, bool) {
		for _, candidate := range preset.Candidates {
			tried = append(tried, candidate)
			resolved, err := lookPath(candidate)
			if err == nil {
				return resolved, true
			}
		}
		return "", false
	}

	if isGUIPreset(&preset) {
		if got, ok := tryKnownPaths(); ok {
			return got, tried, nil
		}
		if got, ok := tryLookPath(); ok {
			return got, tried, nil
		}
	} else {
		if got, ok := tryLookPath(); ok {
			return got, tried, nil
		}
		if got, ok := tryKnownPaths(); ok {
			return got, tried, nil
		}
	}

	return "", tried, errors.New("not found")
}

func expandEnvPath(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	return percentEnvPattern.ReplaceAllStringFunc(raw, func(match string) string {
		key := strings.Trim(match, "%")
		if key == "" {
			return match
		}
		if value := os.Getenv(key); value != "" {
			return value
		}
		return match
	})
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func printLaunchPresets() {
	fmt.Println("可用 launch 预设:")
	for _, preset := range managedLaunchPresets {
		fmt.Printf("  - %s: %s\n", preset.Name, preset.Description)
		fmt.Printf("    候选命令: %s\n", strings.Join(preset.Candidates, ", "))
		if len(preset.KnownPaths) > 0 {
			fmt.Printf("    常见安装目录: %s\n", strings.Join(preset.KnownPaths, ", "))
		}
	}
	fmt.Println()
	fmt.Println("示例:")
	fmt.Printf("  %s --launch-preset vscode\n", selfBinaryName())
	fmt.Printf("  %s --launch-preset cursor\n", selfBinaryName())
	fmt.Printf("  %s --launch-preset powershell\n", selfBinaryName())
	fmt.Printf("  %s --launch code --reuse-window\n", selfBinaryName())
}

func mergeEnv(existing []string, overrides map[string]string) []string {
	merged := make(map[string]string, len(existing)+len(overrides))
	for _, item := range existing {
		parts := strings.SplitN(item, "=", 2)
		if len(parts) != 2 {
			continue
		}
		merged[strings.ToUpper(parts[0])] = parts[1]
	}
	for key, value := range overrides {
		merged[strings.ToUpper(key)] = value
	}
	result := make([]string, 0, len(merged))
	for key, value := range merged {
		result = append(result, key+"="+value)
	}
	return result
}
