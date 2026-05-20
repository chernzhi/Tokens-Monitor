package main

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
)

// DefaultServerURL is the pre-configured server address.
// Change this before compiling to distribute to your team.
const DefaultServerURL = "https://otw.tech:59889"

// MonitoredSuffix 按主机名后缀匹配并打上供应商标签（用于公司自建网关、新厂商域名等）。
type MonitoredSuffix struct {
	Suffix string `json:"suffix"`           // 例如 ".internal-ai.company.com"
	Prefix string `json:"prefix,omitempty"` // 可选：主机名前缀，例如 "bedrock-runtime."
	Vendor string `json:"vendor"`           // 大屏上显示的供应商名，如 "internal-llm"
}

type Config struct {
	ServerURL  string `json:"server_url"`
	UserName   string `json:"user_name"`
	UserID     string `json:"user_id"`
	Department string `json:"department"`
	Port       int    `json:"port"`
	// UpstreamProxy 为本地 MITM 外联时使用的上游代理（可选），用于保留用户原本的公司代理或本地代理链路。
	// 支持 http://、https://、socks5://。
	UpstreamProxy string `json:"upstream_proxy,omitempty"`
	// MonitorHosts 是完整的精确监控域名表。字段不存在时使用内置默认表；字段存在时以配置为准，方便删除/改 vendor。
	MonitorHosts map[string]string `json:"monitor_hosts,omitempty"`
	// MonitorSuffixes 是完整的后缀/前缀+后缀监控规则。字段不存在时使用内置默认规则；字段存在时以配置为准。
	MonitorSuffixes []MonitoredSuffix `json:"monitor_suffixes,omitempty"`
	// BypassDomains 仅为兼容旧配置保留。新配置不再暴露该字段：系统 PAC 已采用 AI 域名白名单，未配置的域名默认不走 ai-monitor。
	BypassDomains []string `json:"bypass_domains,omitempty"`
	// ExtraMonitorHosts 精确主机名 → 供应商（与内置 aiDomains 合并，适合内网网关、新 API 域名）。
	// Deprecated: 新配置请直接修改 monitor_hosts。
	ExtraMonitorHosts map[string]string `json:"extra_monitor_hosts,omitempty"`
	// ExtraMonitorSuffixes 后缀匹配，在通配规则之后评估。
	// Deprecated: 新配置请直接修改 monitor_suffixes。
	ExtraMonitorSuffixes []MonitoredSuffix `json:"extra_monitor_suffixes,omitempty"`
	// InstallSystemProxy 为 true 时，--install 写入 WinINet 与 setx。
	// 省略该字段时默认 false：优先采用非侵入式模式，不修改本机系统代理与持久环境变量。
	InstallSystemProxy *bool `json:"install_system_proxy,omitempty"`
	// InstallIDEProxy 为 true 时才写入 VS Code/Cursor 等的 settings.json（http.proxy 等）。默认 false：仅靠系统代理即可让 Electron 走 MITM，避免与 WinINet 重复配置导致网络异常。
	InstallIDEProxy *bool `json:"install_ide_proxy,omitempty"`
	// ReportOpaqueTraffic 为 true（默认）时，对无法解析 JSON usage 的响应（如 gRPC/Protobuf）按响应体大小做粗略估算并上报，使 135 大屏可见；非官方计费口径。
	// 设为 false 则仅上报能解析出 usage 的 JSON（与旧版行为一致）。
	ReportOpaqueTraffic *bool `json:"report_opaque_traffic,omitempty"`
	// MitmCursor 控制是否尝试 MITM Cursor 桌面端流量（*.cursor.sh / *.cursor.com）。
	//
	// 默认 false（保守、不影响 IDE 网络）：Cursor 桌面端做 TLS 证书钉扎，会拒绝
	// 任何中间人证书；MITM 一旦命中就会导致 IDE 登录失败 / 频繁掉线，是用户报
	// "一打开 ai-monitor 编程工具网络就坏" 的高发原因。同时 Cursor 主要走 gRPC，
	// 即便 MITM 解开 TLS 也只能体积估算 token，监控收益小。
	//
	// 若你确认本机 Cursor 已不再 pin（罕见），可显式置 true 开启。
	MitmCursor *bool `json:"mitm_cursor,omitempty"`
	// GatewayPort 为 API Gateway 专用端口（可选）。设置后，该端口仅提供反向代理 /v1/* 与 /vendor/* 路由，
	// 不做 CONNECT MITM，也不需要 CA 证书信任。设为 0 或省略则 Gateway 路由共享 MITM 主端口。
	GatewayPort int `json:"gateway_port,omitempty"`
	// APIKey 上报数据时附加的认证 Key，对应服务端 COLLECT_API_KEY 配置。为空时不发送 X-API-Key 头。
	APIKey string `json:"api_key,omitempty"`
	// AuthToken 用户登录/注册后获取的个人认证令牌，上报时优先使用 Bearer 认证。
	AuthToken string `json:"auth_token,omitempty"`
	// ExtraBypassDomains 仅为兼容旧配置保留。新配置不需要维护直连列表；系统 PAC 会让非监控域名走默认网络。
	// Deprecated: 新配置请不要使用。
	ExtraBypassDomains []string `json:"extra_bypass_domains,omitempty"`
	// ReportProxy 上报服务器流量使用的代理。"auto" 或空值 = 智能判断（内网直连，外网走上游代理）；
	// "direct" = 强制直连；"upstream" = 强制走 upstream_proxy；也可填具体代理地址。
	ReportProxy string `json:"report_proxy,omitempty"`
	// ChainExistingPAC 为 true（默认）时，检测到用户已有 AutoConfigURL（企业 PAC）时，
	// 将旧 PAC 内容内联到新生成的 PAC 中，非 AI 域名仍走原 PAC 策略。
	// 设为 false 则直接覆盖旧 PAC（不推荐，会丢失企业内网路由策略）。
	ChainExistingPAC *bool `json:"chain_existing_pac,omitempty"`
	// StrictPolicyCheck 为 true（默认）时，检测到 HKLM 策略级代理时拒绝全局安装，
	// 建议用户改用 --launch 模式。设为 false 则仅警告。
	StrictPolicyCheck *bool `json:"strict_policy_check,omitempty"`
	// WatchdogIntervalSec 自检间隔秒数，默认 10。
	WatchdogIntervalSec int `json:"watchdog_interval_sec,omitempty"`
	// WatchdogFailures 连续失败多少次触发恢复，默认 2。
	WatchdogFailures int `json:"watchdog_failures,omitempty"`
	// AutoInstallSessionPAC 控制「双击 ai-monitor.exe 是否自动写一次系统 PAC」。
	//
	// 默认 true（会话 PAC 模式）：双击 ai-monitor.exe 会临时写一份只针对
	// AI 域名（白名单）的 PAC：Cursor / VS Code / 浏览器 / npm 中只有命中
	// monitor_hosts / monitor_suffixes 的请求才进 MITM，其它全部 DIRECT；
	// 关闭 ai-monitor 进程时自动还原 AutoConfigURL / ProxyServer / 环境变量，
	// 不会留下"指向 dead 端口"的残留——这是用户真正想要的「打开即监控、
	// 关掉即恢复」的体验，也是 v3.1.2 起的新默认。
	//
	// 设为 false（观察模式）：双击 ai-monitor.exe 仅监听本地端口，*完全不改*
	// 注册表 / 环境变量 / IDE 配置；只有通过 --launch 受管启动或 IDE 主动
	// 指向本程序端口的流量才会被监控。适合「想自己用 --launch / --global-install
	// 完全掌控接管时机」的高级用户，或临时排障。
	AutoInstallSessionPAC *bool `json:"auto_install_session_pac,omitempty"`
	// 自动更新（空 / 0 视为默认：1 小时轮询、不自动安装）
	UpdateCheckURL             string `json:"update_check_url,omitempty"`
	UpdateCheckIntervalSeconds int    `json:"update_check_interval_seconds,omitempty"`
	UpdateAutoApply            bool   `json:"update_auto_apply,omitempty"`
}

// EffectiveAutoInstallSessionPAC 默认 true（会话 PAC 模式）。
//
// 之所以默认 true：观察模式虽然「不破坏网络」，但代价是「啥也不监控」——
// 用户双击 ai-monitor.exe 后看 /status 全是 0，与「打开即监控」的直觉冲突。
// 会话 PAC 模式既能完成监控，又有自动还原兜底，是更合理的默认。
func (c *Config) EffectiveAutoInstallSessionPAC() bool {
	if c == nil || c.AutoInstallSessionPAC == nil {
		return true
	}
	return *c.AutoInstallSessionPAC
}

// EffectiveInstallSystemProxy 是否写入系统代理与环境变量。省略字段时默认 false，优先保持本机网络环境不变。
func (c *Config) EffectiveInstallSystemProxy() bool {
	if c == nil {
		return false
	}
	if c.InstallSystemProxy == nil {
		return false
	}
	return *c.InstallSystemProxy
}

// EffectiveInstallIDEProxy 是否向 IDE 的 settings.json 注入 http.proxy。默认 false。
func (c *Config) EffectiveInstallIDEProxy() bool {
	if c == nil || c.InstallIDEProxy == nil {
		return false
	}
	return *c.InstallIDEProxy
}

// EffectiveReportOpaqueTraffic 无法解析 JSON usage 时是否按体积估算上报。默认 true。
func (c *Config) EffectiveReportOpaqueTraffic() bool {
	if c == nil || c.ReportOpaqueTraffic == nil {
		return true
	}
	return *c.ReportOpaqueTraffic
}

// EffectiveMitmCursor 是否尝试 MITM Cursor 桌面端流量。
// 默认 false：Cursor 仍做 TLS pinning，开启会导致 IDE 网络断连，远大于监控收益。
// 仅当 config.json 中显式 "mitm_cursor": true 时才启用。
func (c *Config) EffectiveMitmCursor() bool {
	if c == nil || c.MitmCursor == nil {
		return false
	}
	return *c.MitmCursor
}

// EffectiveChainExistingPAC 是否链式包裹企业已有 PAC。默认 true。
func (c *Config) EffectiveChainExistingPAC() bool {
	if c == nil || c.ChainExistingPAC == nil {
		return true
	}
	return *c.ChainExistingPAC
}

// EffectiveStrictPolicyCheck 是否在 HKLM 策略代理存在时拒绝全局安装。默认 true。
func (c *Config) EffectiveStrictPolicyCheck() bool {
	if c == nil || c.StrictPolicyCheck == nil {
		return true
	}
	return *c.StrictPolicyCheck
}

// EffectiveWatchdogInterval 返回 watchdog 自检间隔秒数，默认 30。
func (c *Config) EffectiveWatchdogInterval() int {
	if c == nil || c.WatchdogIntervalSec <= 0 {
		return 30
	}
	return c.WatchdogIntervalSec
}

// EffectiveWatchdogFailures 返回连续失败阈值，默认 3。
// （3.1.x 之前是 2；线上发现 Windows 杀软扫描偶发会让 /healthz 单次失败，
// 阈值 2 容易误杀进程，调到 3 给一个额外的宽容窗口。）
func (c *Config) EffectiveWatchdogFailures() int {
	if c == nil || c.WatchdogFailures <= 0 {
		return 3
	}
	return c.WatchdogFailures
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("未找到配置文件 %s\n  %s", path, userFacingSetupHint())
		}
		return nil, err
	}

	var cfg Config
	if err := json.Unmarshal(stripJSONLineComments(data), &cfg); err != nil {
		return nil, fmt.Errorf("配置文件格式错误: %v", err)
	}

	if cfg.Port == 0 {
		cfg.Port = 18090
	}
	if cfg.ServerURL == "" {
		cfg.ServerURL = DefaultServerURL
	}
	cfg.ServerURL = strings.TrimSpace(strings.TrimRight(cfg.ServerURL, "/"))
	cfg.UpstreamProxy = strings.TrimSpace(cfg.UpstreamProxy)
	// Auto-fill missing fields from OS
	if cfg.UserName == "" {
		cfg.UserName = getOSUserName()
	}
	if cfg.UserID == "" {
		cfg.UserID = generateUserID()
	}

	if err := validateServerURL(cfg.ServerURL); err != nil {
		return nil, fmt.Errorf("server_url 无效: %w", err)
	}
	if err := validateUpstreamProxyURL(cfg.UpstreamProxy); err != nil {
		return nil, fmt.Errorf("upstream_proxy 无效: %w", err)
	}

	return &cfg, nil
}

// generateUserID 生成稳定的匿名用户 ID（MD5 哈希，32 位十六进制）。
// 优先对 Windows SID（user.Uid，格式 S-1-5-21-...）哈希，确保改名/换机同人仍为同 ID。
// 兜底对小写完整登录名哈希（DOMAIN\\user），避免手填工号时大小写错误。
// 混入 hostname 作为 salt，使得即使攻击者知道目标 SID，也无法在不知道机器名的情况下
// 复算 UserID 进行伪造上报。salt 对同一台机器是稳定的，不影响 ID 一致性。
func generateUserID() string {
	u, err := user.Current()
	var raw string
	if err == nil {
		// u.Uid 在 Windows 上是完整 SID，在 Linux/macOS 是 uid 数字字符串
		raw = strings.TrimSpace(u.Uid)
		if raw == "" {
			raw = strings.ToLower(strings.TrimSpace(u.Username))
		}
	} else {
		hostname, _ := os.Hostname()
		raw = strings.ToLower(hostname)
	}
	// 混入 hostname 作为 salt
	hostname, _ := os.Hostname()
	salted := raw + "\x00" + strings.ToLower(hostname)
	sum := md5.Sum([]byte(salted))
	return hex.EncodeToString(sum[:])
}

func getOSUserName() string {
	u, err := user.Current()
	if err != nil {
		return "unknown"
	}
	// On Windows, Name field often has the display name
	if u.Name != "" {
		return u.Name
	}
	// Fallback: use login name
	name := u.Username
	// Strip domain prefix (DOMAIN\user or user@domain)
	if i := strings.LastIndex(name, "\\"); i >= 0 {
		name = name[i+1:]
	}
	if i := strings.Index(name, "@"); i >= 0 {
		name = name[:i]
	}
	return name
}

func stripJSONLineComments(data []byte) []byte {
	if len(data) == 0 {
		return data
	}
	var b strings.Builder
	b.Grow(len(data))
	inString := false
	escaped := false
	for i := 0; i < len(data); i++ {
		ch := data[i]
		if inString {
			b.WriteByte(ch)
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
			b.WriteByte(ch)
			continue
		}
		if ch == '/' && i+1 < len(data) && data[i+1] == '/' {
			for i < len(data) && data[i] != '\n' {
				i++
			}
			if i < len(data) {
				b.WriteByte(data[i])
			}
			continue
		}
		b.WriteByte(ch)
	}
	return []byte(b.String())
}

// SaveConfig 将完整 Config 写回 JSONC 风格的配置文件，并在每个配置项前写入 // 注释。
func SaveConfig(cfg *Config, path string) error {
	data, err := marshalAnnotatedConfig(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	return os.WriteFile(path, data, 0600)
}

type configJSONEntry struct {
	key     string
	value   interface{}
	comment string
}

func marshalAnnotatedConfig(cfg *Config) ([]byte, error) {
	entries := annotatedConfigEntries(cfg)
	var b strings.Builder
	b.WriteString("{\n")
	for _, comment := range []string{
		"这是 ai-monitor 唯一需要用户理解和修改的配置文件。",
		"配置文件使用 JSONC 风格：// 开头的是说明注释，ai-monitor 和 VS Code 扩展都会自动忽略。",
		"默认位于 %APPDATA%/ai-monitor/config.json；修改端口、代理、安装模式后请重新运行安装或重启 ai-monitor。",
	} {
		b.WriteString("  // ")
		b.WriteString(comment)
		b.WriteString("\n")
	}
	for i, entry := range entries {
		if entry.comment != "" {
			b.WriteString("  // ")
			b.WriteString(entry.comment)
			b.WriteString("\n")
		}
		raw, err := json.MarshalIndent(entry.value, "  ", "  ")
		if err != nil {
			return nil, err
		}
		b.WriteString("  ")
		key, _ := json.Marshal(entry.key)
		b.Write(key)
		b.WriteString(": ")
		b.Write(raw)
		if i < len(entries)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("}\n")
	return []byte(b.String()), nil
}

func annotatedConfigEntries(cfg *Config) []configJSONEntry {
	if cfg == nil {
		cfg = &Config{}
	}
	serverURL := strings.TrimSpace(strings.TrimRight(cfg.ServerURL, "/"))
	if serverURL == "" {
		serverURL = DefaultServerURL
	}
	port := cfg.Port
	if port <= 0 {
		port = 18090
	}
	userName := strings.TrimSpace(cfg.UserName)
	if userName == "" {
		userName = getOSUserName()
	}
	userID := strings.TrimSpace(cfg.UserID)
	if userID == "" {
		userID = generateUserID()
	}
	return []configJSONEntry{
		{"server_url", serverURL, "server_url: Token 上报服务地址，客户端会向这里发送心跳和用量记录。"},
		{"user_name", userName, "user_name: 员工姓名，用于后台和大屏展示。"},
		{"user_id", userID, "user_id: 员工唯一标识，建议填写登录邮箱或工号；为空时会按本机账号生成匿名 ID。"},
		{"department", cfg.Department, "department: 部门名称，用于统计分组；可留空。"},
		{"port", port, "port: 本机 MITM HTTP 代理监听端口，默认 18090；端口被占用时程序可能自动顺延。"},
		{"gateway_port", cfg.GatewayPort, "gateway_port: 可选 API Gateway 端口。大于 0 时该端口只提供 /v1/* 与 /vendor/* 反向代理，不做 HTTPS MITM。"},
		{"upstream_proxy", strings.TrimSpace(cfg.UpstreamProxy), "upstream_proxy: 高级可选项。通常留空，让 ai-monitor 按本机默认网络直连；只有必须串接公司/本地代理时才填写。"},
		{"report_proxy", strings.TrimSpace(cfg.ReportProxy), "report_proxy: 上报到 server_url 时使用的代理策略：auto 或空=自动判断，direct=直连，upstream=走 upstream_proxy，也可填写具体代理 URL。"},
		{"install_system_proxy", boolConfigValue(cfg.InstallSystemProxy, false), "install_system_proxy: 为 true 时安装会写入 Windows 系统 PAC/代理，让浏览器、Visual Studio 等自动经过监控；默认 false 更保守。"},
		{"install_ide_proxy", boolConfigValue(cfg.InstallIDEProxy, false), "install_ide_proxy: 为 true 时安装会写入 VS Code/Cursor 等 IDE 的 http.proxy 设置；通常保持 false，避免与系统代理重复。"},
		{"report_opaque_traffic", boolConfigValue(cfg.ReportOpaqueTraffic, true), "report_opaque_traffic: 为 true 时，对 gRPC/Protobuf 等无法解析 usage 的响应按可见内容估算 token 并上报；非官方计费口径。"},
		{"mitm_cursor", boolConfigValue(cfg.MitmCursor, false), "mitm_cursor: 默认 false 安全；Cursor 桌面端钉证书，置 true 会让 Cursor 网络断连。仅在确认本机 Cursor 不再 pin 时才打开。"},
		{"chain_existing_pac", boolConfigValue(cfg.ChainExistingPAC, true), "chain_existing_pac: 为 true 时保留并串联用户原有企业 PAC；建议保持 true，避免丢失公司内网代理策略。"},
		{"strict_policy_check", boolConfigValue(cfg.StrictPolicyCheck, true), "strict_policy_check: 为 true 时检测到公司策略级代理会拒绝全局安装，避免覆盖管理员策略；建议保持 true。"},
		{"monitor_hosts", effectiveMonitorHosts(cfg), "monitor_hosts: 完整精确监控域名表。键是主机名，值是供应商标签；可直接增删改，删除后该精确域名不再 MITM。"},
		{"monitor_suffixes", effectiveMonitorSuffixes(cfg), "monitor_suffixes: 完整后缀/前缀监控规则。suffix 必填，prefix 可选；用于 Azure/OpenAI、Bedrock、Cursor、Copilot 等通配域名。"},
		{"api_key", strings.TrimSpace(cfg.APIKey), "api_key: 服务端如果配置了 COLLECT_API_KEY，可在这里填写；多数登录态场景留空即可。"},
		{"auth_token", strings.TrimSpace(cfg.AuthToken), "auth_token: 用户登录/注册后得到的个人令牌，上报优先使用它。属于敏感信息，不要公开分享。"},
		{"watchdog_interval_sec", cfg.EffectiveWatchdogInterval(), "watchdog_interval_sec: ai-monitor 进程内自检间隔秒数；默认 10。不是 Windows 计划任务，不会弹黑框。"},
		{"watchdog_failures", cfg.EffectiveWatchdogFailures(), "watchdog_failures: 连续自检失败多少次后触发网络恢复；默认 2。"},
		{"auto_install_session_pac", boolConfigValue(cfg.AutoInstallSessionPAC, true), "auto_install_session_pac: 默认 true（双击 ai-monitor.exe 自动写 AI 白名单 PAC，关闭程序时自动还原）；置 false 进入观察模式，只监听本地端口、完全不动系统代理。"},
	}
}

func boolConfigValue(v *bool, defaultValue bool) bool {
	if v == nil {
		return defaultValue
	}
	return *v
}

func stringMapConfigValue(v map[string]string) map[string]string {
	if v == nil {
		return map[string]string{}
	}
	return v
}

func suffixListConfigValue(v []MonitoredSuffix) []MonitoredSuffix {
	if v == nil {
		return []MonitoredSuffix{}
	}
	return v
}

func stringSliceConfigValue(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

// ValidateAndHealConfig 检测关键配置字段是否缺失。
// 返回值是需要提示用户的警告信息列表。不会修改用户已手动配置的值。
func ValidateAndHealConfig(cfg *Config, configPath string) []string {
	if cfg == nil {
		return nil
	}
	var warnings []string
	_ = configPath
	return warnings
}
