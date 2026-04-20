//go:build windows

package main

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"
)

const inetRegPath = `HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`
const envRegPath = `HKCU\Environment`

// WinINet 通知常量。改完 HKCU 注册表后必须广播，否则 Chrome/Edge/IE/VS/.NET 等
// 都会继续用进程内缓存的旧代理，用户往往以为"改了没生效"。
const (
	internetOptionSettingsChanged = 39
	internetOptionRefresh         = 37
	hwndBroadcast                 = 0xFFFF
	wmSettingChange               = 0x001A
	smtoAbortIfHung               = 0x0002
)

var (
	wininetDLL             = syscall.NewLazyDLL("wininet.dll")
	procInternetSetOptionW = wininetDLL.NewProc("InternetSetOptionW")
	user32DLL              = syscall.NewLazyDLL("user32.dll")
	procSendMessageTimeout = user32DLL.NewProc("SendMessageTimeoutW")
)

// notifyWinInetSettingsChanged 通知 WinINet 和所有顶层窗口"代理/环境变量已变"，
// 让浏览器、VS、.NET 应用及其它监听 WM_SETTINGCHANGE 的进程在不重启的情况下
// 拾取新配置。忽略所有错误（仅"最大努力"，失败不影响主流程）。
func notifyWinInetSettingsChanged() {
	// WinINet: 先 SETTINGS_CHANGED 再 REFRESH
	_, _, _ = procInternetSetOptionW.Call(0, uintptr(internetOptionSettingsChanged), 0, 0)
	_, _, _ = procInternetSetOptionW.Call(0, uintptr(internetOptionRefresh), 0, 0)

	// 广播 WM_SETTINGCHANGE("Environment")，让新开的 cmd/PowerShell/IDE
	// 重新读 HKCU\Environment（否则要重启 explorer.exe 才生效）。
	envPtr, err := syscall.UTF16PtrFromString("Environment")
	if err != nil {
		return
	}
	var ret uintptr
	_, _, _ = procSendMessageTimeout.Call(
		uintptr(hwndBroadcast),
		uintptr(wmSettingChange),
		0,
		uintptr(unsafe.Pointer(envPtr)),
		uintptr(smtoAbortIfHung),
		5000, // 5s 超时，避免卡在挂死的外部进程窗口
		uintptr(unsafe.Pointer(&ret)),
	)
}

// EnableSystemProxy sets the WinINet system proxy for the current user.
func EnableSystemProxy(proxyAddr, bypass string) error {
	cmds := []struct {
		args []string
		desc string
	}{
		{[]string{"reg", "add", inetRegPath, "/v", "ProxyEnable", "/t", "REG_DWORD", "/d", "1", "/f"}, "enable proxy"},
		{[]string{"reg", "add", inetRegPath, "/v", "ProxyServer", "/t", "REG_SZ", "/d", proxyAddr, "/f"}, "set proxy server"},
		{[]string{"reg", "add", inetRegPath, "/v", "ProxyOverride", "/t", "REG_SZ", "/d", bypass, "/f"}, "set bypass list"},
	}
	for _, c := range cmds {
		if err := exec.Command(c.args[0], c.args[1:]...).Run(); err != nil {
			return fmt.Errorf("%s: %w", c.desc, err)
		}
	}
	notifyWinInetSettingsChanged()
	log.Printf("[proxy] system proxy set: %s", proxyAddr)
	return nil
}

// DisableSystemProxy removes the WinINet system proxy setting.
func DisableSystemProxy() {
	exec.Command("reg", "add", inetRegPath, "/v", "ProxyEnable", "/t", "REG_DWORD", "/d", "0", "/f").Run()
	notifyWinInetSettingsChanged()
	log.Println("[proxy] system proxy disabled")
}

// SetEnvProxy sets HTTP_PROXY, HTTPS_PROXY, and AI SDK base URL env vars persistently.
func SetEnvProxy(vars map[string]string) error {
	for k, v := range vars {
		if err := exec.Command("setx", k, v).Run(); err != nil {
			return fmt.Errorf("setx %s: %w", k, err)
		}
	}
	notifyWinInetSettingsChanged()
	log.Println("[proxy] environment variables set")
	return nil
}

// ClearEnvProxy removes all proxy-related environment variables.
func ClearEnvProxy(keys []string) {
	for _, k := range keys {
		exec.Command("reg", "delete", envRegPath, "/v", k, "/f").Run()
	}
	notifyWinInetSettingsChanged()
	log.Println("[proxy] environment variables cleared")
}

// ReadCurrentAutoConfigURL reads the current AutoConfigURL from the WinINet registry.
// Returns "" if not set or on error.
func ReadCurrentAutoConfigURL() string {
	out, err := exec.Command("reg", "query", inetRegPath, "/v", "AutoConfigURL").CombinedOutput()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "AutoConfigURL") {
			parts := strings.SplitN(line, "REG_SZ", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
}

// EnableSystemProxyPAC sets the AutoConfigURL registry value so WinINet resolves
// proxies via a PAC file. Manual proxy (ProxyEnable / ProxyServer) is disabled.
func EnableSystemProxyPAC(pacURL string) error {
	cmds := []struct {
		args []string
		desc string
	}{
		{[]string{"reg", "add", inetRegPath, "/v", "AutoConfigURL", "/t", "REG_SZ", "/d", pacURL, "/f"}, "set AutoConfigURL"},
		{[]string{"reg", "add", inetRegPath, "/v", "ProxyEnable", "/t", "REG_DWORD", "/d", "0", "/f"}, "disable manual proxy"},
	}
	for _, c := range cmds {
		if err := exec.Command(c.args[0], c.args[1:]...).Run(); err != nil {
			return fmt.Errorf("%s: %w", c.desc, err)
		}
	}
	// Clean up stale manual proxy keys (ignore errors if they don't exist)
	exec.Command("reg", "delete", inetRegPath, "/v", "ProxyServer", "/f").Run()
	exec.Command("reg", "delete", inetRegPath, "/v", "ProxyOverride", "/f").Run()
	notifyWinInetSettingsChanged()
	log.Printf("[proxy] PAC proxy set: %s", pacURL)
	return nil
}

// DisableSystemProxyPAC removes the AutoConfigURL registry value.
func DisableSystemProxyPAC() {
	exec.Command("reg", "delete", inetRegPath, "/v", "AutoConfigURL", "/f").Run()
	notifyWinInetSettingsChanged()
	log.Println("[proxy] PAC proxy cleared")
}
