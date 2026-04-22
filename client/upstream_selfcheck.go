package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	upstreamCheckTimeout = 2 * time.Second
)

// loopbackUpstreamsEqual compares two proxy URL strings (trimmed) for de-duplication.
func loopbackUpstreamsEqual(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

func defaultTCPDial(addr string) error {
	c, err := net.DialTimeout("tcp", addr, upstreamCheckTimeout)
	if err != nil {
		return err
	}
	_ = c.Close()
	return nil
}

// proxyURLToDialAddress returns host:port for a tcp dial to the HTTP/SOCKS proxy,
// and whether the host is a loopback address (so we can self-check before use).
func proxyURLToDialAddress(raw string) (addr string, loopback bool, err error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", false, nil
	}
	if !strings.Contains(s, "://") {
		s = "http://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return "", false, err
	}
	h := u.Hostname()
	if h == "" {
		return "", false, fmt.Errorf("missing host in upstream proxy URL")
	}
	loopback = isLoopbackHost(h)
	if u.Port() != "" {
		return net.JoinHostPort(h, u.Port()), loopback, nil
	}
	switch u.Scheme {
	case "http", "socks5", "socks5h":
		return net.JoinHostPort(h, "80"), loopback, nil
	case "https":
		return net.JoinHostPort(h, "443"), loopback, nil
	default:
		return net.JoinHostPort(h, "80"), loopback, nil
	}
}

func isLoopbackHost(h string) bool {
	h = strings.ToLower(strings.TrimSpace(h))
	if h == "localhost" {
		return true
	}
	if ip := net.ParseIP(h); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// selfCheckContext 为测试注入回退配置路径与 TCP 拨号行为，避免与并行测试竞态 t.Setenv / 全局 dial。
// 生产环境传 nil。
type selfCheckContext struct {
	roamingConfigPath string
	dial              func(string) error
}

func (s *selfCheckContext) checkTCP(addr string) bool {
	d := defaultTCPDial
	if s != nil && s.dial != nil {
		d = s.dial
	}
	return d(addr) == nil
}

func readUpstreamField(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var raw struct {
		UpstreamProxy string `json:"upstream_proxy"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", err
	}
	return strings.TrimSpace(raw.UpstreamProxy), nil
}

func wellKnownAppDataConfigPath() string {
	ad := os.Getenv("APPDATA")
	if ad == "" {
		return ""
	}
	return filepath.Join(ad, "ai-monitor", "config.json")
}

// resolveLocalUpstreamWithFallback: 本机上的 upstream 若端口未监听，尝试从
// %APPDATA%\ai-monitor\config.json 与 install_state 恢复，并回写当前 config 文件，避免
// 多份 config（如便携目录与 Roaming 各一份）内容不一致时 MITM 外联全部失败。
func resolveLocalUpstreamWithFallback(cfg *Config, configPath string, sctx *selfCheckContext) {
	if cfg == nil || strings.TrimSpace(cfg.UpstreamProxy) == "" {
		return
	}
	addr, loopback, err := proxyURLToDialAddress(cfg.UpstreamProxy)
	if err != nil {
		log.Printf("[config] 解析 upstream_proxy 失败，跳过自检: %v", err)
		return
	}
	if !loopback {
		return
	}
	if sctx.checkTCP(addr) {
		return
	}
	log.Printf("[config] 警告: 本机上游代理不可达: %s（已解析为 TCP %s），将尝试从 Roaming 配置 / install_state 回退。",
		cfg.UpstreamProxy, addr)
	hintMismatchedGatewayPort(addr)

	absConfig, _ := filepath.Abs(configPath)
	candidates := make([]string, 0, 4)
	seen := make(map[string]struct{})

	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" || isSelfProxy(s) {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		candidates = append(candidates, s)
	}
	roaming := ""
	if sctx != nil && sctx.roamingConfigPath != "" {
		roaming = sctx.roamingConfigPath
	} else {
		roaming = wellKnownAppDataConfigPath()
	}
	addFromFile := func(p string) {
		if p == "" {
			return
		}
		ap, err := filepath.Abs(p)
		if err != nil {
			return
		}
		if ap == absConfig {
			return
		}
		u, err := readUpstreamField(ap)
		if err != nil || u == "" {
			return
		}
		add(u)
	}
	addFromFile(roaming)
	if sctx == nil || sctx.roamingConfigPath == "" {
		if st := loadInstallState(); st != nil {
			add(st.PreviousUpstreamProxy)
		}
	}

	for _, c := range candidates {
		if loopbackUpstreamsEqual(c, cfg.UpstreamProxy) {
			continue
		}
		dial, lb, err := proxyURLToDialAddress(c)
		if err != nil || !lb {
			continue
		}
		if !sctx.checkTCP(dial) {
			continue
		}
		log.Printf("[config] 已使用回退上游: %s (TCP 可达 %s)，将同步到当前配置文件", c, dial)
		cfg.UpstreamProxy = c
		if err := patchConfigUpstreamProxy(configPath, c); err != nil {
			log.Printf("[config] 回写 config.json 失败 (进程仍使用回退值): %v", err)
		}
		return
	}
	log.Println("[config] 警告: 无可用本机回退; 已清空 upstream_proxy，出网将尝试直连。若需公司/本地代理，请填正确且已监听的地址。")
	cfg.UpstreamProxy = ""
	if err := patchConfigUpstreamProxy(configPath, ""); err != nil {
		log.Printf("[config] 无法清空 config.json 中的 upstream_proxy: %v", err)
	}
}

// hintMismatchedGatewayPort 在常见误填 gateway 端口(18091 等)时给出一行说明。
func hintMismatchedGatewayPort(dialAddr string) {
	_, p, err := net.SplitHostPort(dialAddr)
	if err != nil {
		return
	}
	// 18091 常被误认为「上游」; 实为本程序可选 gateway_port 或随机端口
	if p == "18091" {
		log.Printf("[config] 提示: 若本意是使用「API gateway_port」的 HTTP 反代，请另设监听; upstream_proxy 应填 Clash/公司代理等「出口」的 HTTP/SOCKS 地址。")
	}
}
