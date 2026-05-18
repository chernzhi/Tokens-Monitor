package main

import (
	"log"
	"net"
	"net/url"
	"os"
	"strings"
	"time"
)

// detectUpstreamProxy discovers the user's existing proxy before ai-monitor sets itself up.
// Priority: explicit config > Windows system proxy registry > environment variables.
// Returns "" if no upstream proxy is found or all candidates are self-referential.
func detectUpstreamProxy(cfg *Config) string {
	// 1. Explicit config takes highest priority (already validated by LoadConfig)
	if cfg != nil && strings.TrimSpace(cfg.UpstreamProxy) != "" {
		return cfg.UpstreamProxy
	}

	// 2. OS-specific system proxy read (Windows registry; no-op on other platforms)
	if sysProxy := readCurrentSystemProxy(); sysProxy != "" {
		if isSelfProxy(sysProxy) {
			log.Printf("[upstream] ignoring system proxy %s (points to self)", sysProxy)
		} else if !isUsableDetectedProxy(sysProxy) {
			log.Printf("[upstream] ignoring system proxy %s (loopback proxy is not reachable)", sysProxy)
		} else {
			log.Printf("[upstream] auto-detected system proxy: %s", sysProxy)
			return sysProxy
		}
	}

	// 3. Standard proxy environment variables
	for _, key := range []string{
		"HTTPS_PROXY", "https_proxy",
		"HTTP_PROXY", "http_proxy",
		"ALL_PROXY", "all_proxy",
	} {
		v := strings.TrimSpace(os.Getenv(key))
		if v == "" {
			continue
		}
		if isSelfProxy(v) {
			log.Printf("[upstream] ignoring env %s=%s (points to self)", key, v)
			continue
		}
		if !isUsableDetectedProxy(v) {
			log.Printf("[upstream] ignoring env %s=%s (loopback proxy is not reachable)", key, v)
			continue
		}
		log.Printf("[upstream] auto-detected env %s: %s", key, v)
		return v
	}

	// 4. Fallback: detect commonly used local proxy ports.
	// 在企业网络里，很多机器会常驻 sing-box/clash（127.0.0.1:8089/7890 等），
	// 但不会写到系统代理或环境变量。若 upstream_proxy 为空且直连外网被拦截，
	// ai-monitor 会出现「本地服务正常但 AI 外联超时」。
	// 这里做保守兜底：仅探测 loopback 常见端口且端口可连通时才采用。
	if local := detectCommonLoopbackUpstreamProxy(); local != "" {
		log.Printf("[upstream] auto-detected local loopback proxy fallback: %s", local)
		return local
	}

	return ""
}

var commonLoopbackUpstreamPorts = []int{
	8089,  // 用户现场常见：sing-box
	7890,  // Clash 默认
	7897,  // Clash Meta 常见
	10809, // v2ray/sing-box 常见 HTTP 入站
	20171, // 某些企业代理客户端常见本地端口
}

func detectCommonLoopbackUpstreamProxy() string {
	for _, port := range commonLoopbackUpstreamPorts {
		candidate := "http://127.0.0.1:" + intToString(port)
		if isSelfProxy(candidate) {
			continue
		}
		if isUsableDetectedProxy(candidate) {
			return candidate
		}
	}
	return ""
}

// snapshotProxyEnvVars captures the current proxy-related environment variables
// BEFORE installation overwrites them. Used for restoration on uninstall.
func snapshotProxyEnvVars() map[string]string {
	snapshot := make(map[string]string)
	keys := append([]string(nil), proxyEnvKeys...)
	keys = append(keys,
		"http_proxy", "https_proxy", "no_proxy",
		"ALL_PROXY", "all_proxy",
	)
	for _, key := range keys {
		if v := os.Getenv(key); v != "" && !isSelfProxy(v) {
			snapshot[key] = v
		}
	}
	return snapshot
}

// patchConfigUpstreamProxy reads config.json, sets upstream_proxy, and writes it back.
func patchConfigUpstreamProxy(configPath, upstream string) error {
	cfg, err := LoadConfig(configPath)
	if err != nil {
		return err
	}
	cfg.UpstreamProxy = upstream
	return SaveConfig(cfg, configPath)
}

// isSelfProxy returns true if the proxy URL points to ai-monitor's own listening range.
// Other loopback proxies such as Clash/V2Ray on 127.0.0.1:7890 are valid upstreams
// and must not be treated as self, otherwise full-proxy installs break internet access.
func isSelfProxy(proxy string) bool {
	host, port, ok := proxyHostPort(proxy)
	if !ok {
		return false
	}
	host = strings.ToLower(host)
	if host != "localhost" && host != "127.0.0.1" && host != "::1" {
		return false
	}
	return port >= 18090 && port <= 18090+mitmPortMaxFallback-1
}

func isUsableDetectedProxy(proxy string) bool {
	host, port, ok := proxyHostPort(proxy)
	if !ok {
		return true
	}
	host = strings.ToLower(host)
	if host != "localhost" && host != "127.0.0.1" && host != "::1" {
		return true
	}
	addr := net.JoinHostPort(host, intToString(port))
	conn, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func proxyHostPort(proxy string) (string, int, bool) {
	raw := strings.TrimSpace(proxy)
	if raw == "" {
		return "", 0, false
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", 0, false
	}
	host := u.Hostname()
	portStr := u.Port()
	if portStr == "" {
		return "", 0, false
	}
	port := 0
	for _, c := range portStr {
		if c < '0' || c > '9' {
			return "", 0, false
		}
		port = port*10 + int(c-'0')
	}
	return host, port, true
}

func intToString(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
