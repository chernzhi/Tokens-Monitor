package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEffectiveInstallSystemProxy(t *testing.T) {
	truePtr := func(b bool) *bool { return &b }
	cases := []struct {
		name string
		cfg  *Config
		want bool
	}{
		{"nil config", nil, false},
		{"omit field defaults non-invasive", &Config{}, false},
		{"explicit true", &Config{InstallSystemProxy: truePtr(true)}, true},
		{"explicit false", &Config{InstallSystemProxy: truePtr(false)}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var c *Config
			if tc.cfg != nil {
				c = tc.cfg
			}
			if got := c.EffectiveInstallSystemProxy(); got != tc.want {
				t.Fatalf("EffectiveInstallSystemProxy() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestEffectiveInstallIDEProxy(t *testing.T) {
	truePtr := func(b bool) *bool { return &b }
	cases := []struct {
		name string
		cfg  *Config
		want bool
	}{
		{"nil config", nil, false},
		{"omit field", &Config{}, false},
		{"explicit false", &Config{InstallIDEProxy: truePtr(false)}, false},
		{"explicit true", &Config{InstallIDEProxy: truePtr(true)}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var c *Config
			if tc.cfg != nil {
				c = tc.cfg
			}
			if got := c.EffectiveInstallIDEProxy(); got != tc.want {
				t.Fatalf("EffectiveInstallIDEProxy() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLoadConfig_JSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	raw := `{
  "server_url": "https://otw.tech:59889",
  "port": 18090,
  "install_system_proxy": true
}`
	if err := os.WriteFile(path, []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ServerURL != "https://otw.tech:59889" {
		t.Fatalf("ServerURL = %q", cfg.ServerURL)
	}
	if cfg.Port != 18090 {
		t.Fatalf("Port = %d", cfg.Port)
	}
	if cfg.InstallSystemProxy == nil || !*cfg.InstallSystemProxy {
		t.Fatal("expected install_system_proxy true")
	}
	if !cfg.EffectiveInstallSystemProxy() {
		t.Fatal("EffectiveInstallSystemProxy should be true")
	}
}

func TestLoadConfig_JSONCLineComments(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	raw := `{
  // server_url 的 https:// 不应被注释剥离逻辑误删
  "server_url": "https://otw.tech:59889", // 上报服务
  // 本机监听端口
  "port": 18090,
  "upstream_proxy": "http://127.0.0.1:7890" // 上游代理 URL 也包含 //
}`
	if err := os.WriteFile(path, []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ServerURL != "https://otw.tech:59889" {
		t.Fatalf("ServerURL = %q", cfg.ServerURL)
	}
	if cfg.UpstreamProxy != "http://127.0.0.1:7890" {
		t.Fatalf("UpstreamProxy = %q", cfg.UpstreamProxy)
	}
}

func TestEffectiveReportOpaqueTraffic(t *testing.T) {
	b := func(v bool) *bool { return &v }
	if !(&Config{}).EffectiveReportOpaqueTraffic() {
		t.Fatal("default should be true")
	}
	if (&Config{ReportOpaqueTraffic: b(false)}).EffectiveReportOpaqueTraffic() {
		t.Fatal("explicit false")
	}
	if !(&Config{ReportOpaqueTraffic: b(true)}).EffectiveReportOpaqueTraffic() {
		t.Fatal("explicit true")
	}
}

func TestLoadConfig_MissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nope.json")
	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadConfig_OmitInstallSystemProxy_DefaultsFalse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	raw := `{"server_url":"https://otw.tech:59889","port":18090}`
	if err := os.WriteFile(path, []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.InstallSystemProxy != nil {
		t.Fatalf("expected omitted field as nil, got %v", cfg.InstallSystemProxy)
	}
	if cfg.EffectiveInstallSystemProxy() {
		t.Fatal("omitted install_system_proxy should default to false (非侵入式安装)")
	}
}

func TestLoadConfig_ValidatesOptionalUpstreamProxy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	raw := `{"server_url":"https://otw.tech:59889","port":18090,"upstream_proxy":"socks5://127.0.0.1:7890"}`
	if err := os.WriteFile(path, []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UpstreamProxy != "socks5://127.0.0.1:7890" {
		t.Fatalf("UpstreamProxy = %q", cfg.UpstreamProxy)
	}
}

func TestSaveConfigWritesAnnotatedSingleConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	cfg := &Config{
		ServerURL:  "https://otw.tech:59889",
		UserName:   "Alice",
		UserID:     "alice@example.com",
		Department: "Dev",
		Port:       18090,
		AuthToken:  "token-123",
	}
	if err := SaveConfig(cfg, path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		`// server_url: Token 上报服务地址`,
		`// auth_token: 用户登录/注册后得到的个人令牌`,
		`// monitor_hosts: 完整精确监控域名表`,
		`"api.openai.com": "openai"`,
		`"suffix": ".openai.azure.com"`,
		`"report_opaque_traffic": true`,
		`"install_system_proxy": false`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("saved config missing %s:\n%s", want, text)
		}
	}
	if strings.Contains(text, `"_说明_`) {
		t.Fatalf("saved config should use // comments, got legacy _说明 fields:\n%s", text)
	}
	if strings.Contains(text, `"bypass_domains"`) {
		t.Fatalf("saved config should not expose bypass_domains:\n%s", text)
	}
	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.AuthToken != "token-123" || loaded.UserID != "alice@example.com" {
		t.Fatalf("loaded annotated config incorrectly: %+v", loaded)
	}
}

func TestConfigDomainListsOverrideBuiltins(t *testing.T) {
	cfg := &Config{
		MonitorHosts: map[string]string{
			"custom.api.example.com": "my-vendor",
		},
		MonitorSuffixes: []MonitoredSuffix{
			{Suffix: ".corp.llm", Vendor: "corp-llm"},
		},
		BypassDomains: []string{"*.corp.local"},
	}
	if _, ok := effectiveMonitorHosts(cfg)["api.openai.com"]; ok {
		t.Fatal("explicit monitor_hosts should override built-in exact host list")
	}
	if got := effectiveMonitorHosts(cfg)["custom.api.example.com"]; got != "my-vendor" {
		t.Fatalf("custom host vendor = %q", got)
	}
	if len(effectiveMonitorSuffixes(cfg)) != 1 || effectiveMonitorSuffixes(cfg)[0].Vendor != "corp-llm" {
		t.Fatalf("explicit monitor_suffixes should override built-in suffix list: %+v", effectiveMonitorSuffixes(cfg))
	}
	bypass := effectiveBypassDomains(cfg)
	for _, want := range []string{"localhost", "127.0.0.1", "127.*", "::1", "*.corp.local"} {
		if !containsString(bypass, want) {
			t.Fatalf("effective bypass_domains missing %q: %+v", want, bypass)
		}
	}
	if containsString(bypass, "github.com") {
		t.Fatalf("explicit bypass_domains should override non-mandatory built-in bypass list: %+v", bypass)
	}

	server := NewProxyServer(cfg, nil, nil, "")
	if _, ok := server.matchAIDomain("api.openai.com"); ok {
		t.Fatal("api.openai.com should not match after removing it from monitor_hosts")
	}
	if vendor, ok := server.matchAIDomain("svc.corp.llm"); !ok || vendor != "corp-llm" {
		t.Fatalf("suffix config did not match: vendor=%q ok=%v", vendor, ok)
	}
}

func TestConfigExampleListsBuiltinDomains(t *testing.T) {
	cfg, err := LoadConfig("config.example.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.MonitorHosts) < len(aiDomains) {
		t.Fatalf("config.example monitor_hosts = %d, want at least %d", len(cfg.MonitorHosts), len(aiDomains))
	}
	if cfg.MonitorHosts["api.openai.com"] != "openai" {
		t.Fatalf("config.example missing api.openai.com: %+v", cfg.MonitorHosts["api.openai.com"])
	}
	if len(cfg.MonitorSuffixes) < len(aiWildcardDomains) {
		t.Fatalf("config.example monitor_suffixes = %d, want at least %d", len(cfg.MonitorSuffixes), len(aiWildcardDomains))
	}
	if cfg.BypassDomains != nil {
		t.Fatalf("config.example should not expose bypass_domains: %+v", cfg.BypassDomains)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestIsSelfProxyOnlyTreatsAIMonitorPortsAsSelf(t *testing.T) {
	for _, proxy := range []string{
		"http://127.0.0.1:18090",
		"http://localhost:18100",
		"http://[::1]:18153",
	} {
		if !isSelfProxy(proxy) {
			t.Fatalf("%s should be treated as ai-monitor self proxy", proxy)
		}
	}
	for _, proxy := range []string{
		"socks5://localhost:7890",
		"http://127.0.0.1:8899",
		"http://proxy.example:8080",
	} {
		if isSelfProxy(proxy) {
			t.Fatalf("%s should not be treated as ai-monitor self proxy", proxy)
		}
	}
	if isSelfProxy("http://proxy.example:8080") {
		t.Fatal("remote proxy should not be treated as self")
	}
}
