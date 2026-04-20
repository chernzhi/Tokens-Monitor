//go:build windows

package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// readCurrentSystemProxy reads the WinINet system proxy from the Windows registry.
// Returns the proxy address (e.g. "http://proxy.corp:8080") if enabled, or "" otherwise.
func readCurrentSystemProxy() string {
	// Check if proxy is enabled (ProxyEnable == 0x1)
	enableOut, err := exec.Command("reg", "query",
		`HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`,
		"/v", "ProxyEnable",
	).Output()
	if err != nil {
		return ""
	}
	enableStr := strings.TrimSpace(string(enableOut))
	// Registry output contains "0x1" for enabled
	if !strings.Contains(enableStr, "0x1") {
		return ""
	}

	// Read ProxyServer value
	serverOut, err := exec.Command("reg", "query",
		`HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`,
		"/v", "ProxyServer",
	).Output()
	if err != nil {
		return ""
	}

	// Parse the REG_SZ value from output like:
	//     ProxyServer    REG_SZ    proxy.corp:8080
	lines := strings.Split(string(serverOut), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ProxyServer") {
			parts := strings.SplitN(line, "REG_SZ", 2)
			if len(parts) == 2 {
				addr := strings.TrimSpace(parts[1])
				if addr != "" {
					// WinINet ProxyServer may or may not have a scheme; normalize.
					if !strings.Contains(addr, "://") {
						addr = fmt.Sprintf("http://%s", addr)
					}
					return addr
				}
			}
		}
	}

	return ""
}

// readCurrentProxyOverride reads the WinINet ProxyOverride (bypass list) from HKCU.
func readCurrentProxyOverride() string {
	out, err := exec.Command("reg", "query",
		`HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`,
		"/v", "ProxyOverride",
	).Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ProxyOverride") {
			parts := strings.SplitN(line, "REG_SZ", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
}

// readCurrentAutoDetect reads the WinINet AutoDetect (WPAD) flag from HKCU.
// Returns (value, present). present=false when the key doesn't exist or can't be read.
func readCurrentAutoDetect() (uint32, bool) {
	out, err := exec.Command("reg", "query",
		`HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`,
		"/v", "AutoDetect",
	).Output()
	if err != nil {
		return 0, false
	}
	s := strings.TrimSpace(string(out))
	if strings.Contains(s, "0x1") {
		return 1, true
	}
	// Key exists with value 0x0
	if strings.Contains(s, "AutoDetect") {
		return 0, true
	}
	return 0, false
}

// readMachinePolicyProxy checks HKLM for machine-level proxy policy (Group Policy / MDM).
// Returns true if a proxy policy is configured at machine scope that would override
// or nullify HKCU proxy settings.
func readMachinePolicyProxy() (found bool, detail string) {
	// Check ProxySettingsPerUser first: if set to 0, WinINet ignores HKCU entirely
	// and only reads proxy config from HKLM. Our HKCU writes would be silently ignored.
	perUserPath := `HKLM\SOFTWARE\Policies\Microsoft\Windows\CurrentVersion\Internet Settings`
	perUserOut, err := exec.Command("reg", "query", perUserPath, "/v", "ProxySettingsPerUser").Output()
	if err == nil && strings.Contains(string(perUserOut), "0x0") {
		return true, "ProxySettingsPerUser=0 (HKCU 代理设置被策略禁用，仅 HKLM 生效)"
	}

	// Check HKLM policy path (set by Group Policy / MDM)
	policyPath := perUserPath

	// Check ProxyEnable
	enableOut, err := exec.Command("reg", "query", policyPath, "/v", "ProxyEnable").Output()
	if err == nil && strings.Contains(string(enableOut), "0x1") {
		// ProxyServer
		serverOut, _ := exec.Command("reg", "query", policyPath, "/v", "ProxyServer").Output()
		for _, line := range strings.Split(string(serverOut), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "ProxyServer") {
				parts := strings.SplitN(line, "REG_SZ", 2)
				if len(parts) == 2 {
					return true, fmt.Sprintf("HKLM 策略代理: %s", strings.TrimSpace(parts[1]))
				}
			}
		}
		return true, "HKLM ProxyEnable=1"
	}

	// Check AutoConfigURL in policy
	pacOut, err := exec.Command("reg", "query", policyPath, "/v", "AutoConfigURL").Output()
	if err == nil {
		for _, line := range strings.Split(string(pacOut), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "AutoConfigURL") {
				parts := strings.SplitN(line, "REG_SZ", 2)
				if len(parts) == 2 && strings.TrimSpace(parts[1]) != "" {
					return true, fmt.Sprintf("HKLM 策略 PAC: %s", strings.TrimSpace(parts[1]))
				}
			}
		}
	}

	return false, ""
}

// fetchPACBody downloads the content of a PAC file from a file:// or http(s):// URL.
// Returns the body text or an error. Max 256KB, 5s timeout.
func fetchPACBody(pacURL string) (string, error) {
	pacURL = strings.TrimSpace(pacURL)
	if pacURL == "" {
		return "", fmt.Errorf("empty PAC URL")
	}

	// Handle file:// URLs
	if strings.HasPrefix(strings.ToLower(pacURL), "file://") {
		u, err := url.Parse(pacURL)
		if err != nil {
			return "", fmt.Errorf("parse PAC URL %s: %w", pacURL, err)
		}

		var path string
		if u.Host != "" {
			// UNC path: file://server/share/x.pac → \\server\share\x.pac
			// 仅对 Path 做 PathUnescape，Host 部分保持原样（避免 IPv6 zone-id 中的 % 被误解码）。
			unescapedPath, err := url.PathUnescape(u.Path)
			if err != nil {
				return "", fmt.Errorf("unescape UNC PAC path %s: %w", u.Path, err)
			}
			path = `\\` + u.Host + filepath.FromSlash(unescapedPath)
		} else {
			// Local path: file:///C:/path → Host="", Path="/C:/path"
			// On Windows, strip leading "/" from "/C:/path" to get "C:/path"
			path = u.Path
			if len(path) > 2 && path[0] == '/' && path[2] == ':' {
				path = path[1:]
			}
			path, err = url.PathUnescape(path)
			if err != nil {
				return "", fmt.Errorf("unescape PAC path %s: %w", path, err)
			}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read PAC file %s: %w", path, err)
		}
		if len(data) > 256*1024 {
			return "", fmt.Errorf("PAC file too large: %d bytes", len(data))
		}
		return string(data), nil
	}

	// HTTP(S) fetch — use Transport with Proxy:nil to bypass any stale
	// HTTP_PROXY env var that might point at our own dead MITM port.
	client := &http.Client{
		Transport: &http.Transport{Proxy: nil},
		Timeout:   5 * time.Second,
	}
	resp, err := client.Get(pacURL)
	if err != nil {
		return "", fmt.Errorf("fetch PAC %s: %w", pacURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("PAC URL %s returned status %d", pacURL, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024+1))
	if err != nil {
		return "", fmt.Errorf("read PAC body: %w", err)
	}
	if len(body) > 256*1024 {
		return "", fmt.Errorf("PAC body too large: >256KB")
	}

	return string(body), nil
}
