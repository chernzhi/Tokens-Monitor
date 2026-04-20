//go:build windows

package main

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
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

	// notifyThrottleMu + notifyLastTime 实现通知节流：500ms 内多次调用只广播一次，
	// 避免 install/uninstall 连续写多条注册表时频繁广播 WM_SETTINGCHANGE，
	// 每次广播要等待所有顶层窗口响应（最长 5s），开销显著。
	notifyThrottleMu sync.Mutex
	notifyLastTime   time.Time
)

// notifyWinInetSettingsChanged 通知 WinINet 和所有顶层窗口"代理/环境变量已变"，
// 让浏览器、VS、.NET 应用及其它监听 WM_SETTINGCHANGE 的进程在不重启的情况下
// 拾取新配置。忽略所有错误（仅"最大努力"，失败不影响主流程）。
// 500ms 内重复调用会被合并（节流），避免连续注册表操作导致的广播风暴。
func notifyWinInetSettingsChanged() {
	notifyThrottleMu.Lock()
	if time.Since(notifyLastTime) < 500*time.Millisecond {
		notifyThrottleMu.Unlock()
		return
	}
	notifyLastTime = time.Now()
	notifyThrottleMu.Unlock()

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

// regSnapshotValue 三态注册表值：
//   - Present=true,  Value=X  → 键存在且值为 X
//   - Present=false, Value="" → 键不存在或读取失败
//
// 回滚时：Present=true 写回 Value；Present=false 跳过（不删除——
// 无法区分"原本不存在"和"读取失败"，误删会破坏用户配置）。
type regSnapshotValue struct {
	Present bool
	Value   string
}

// proxySnapshot captures the current WinINet proxy registry state for rollback.
type proxySnapshot struct {
	ProxyEnable   regSnapshotValue
	ProxyServer   regSnapshotValue
	ProxyOverride regSnapshotValue
	AutoConfigURL regSnapshotValue
}

// captureProxySnapshot reads the current WinINet proxy state from HKCU.
func captureProxySnapshot() proxySnapshot {
	return proxySnapshot{
		ProxyEnable:   readRegSnapshot(inetRegPath, "ProxyEnable"),
		ProxyServer:   readRegSnapshot(inetRegPath, "ProxyServer"),
		ProxyOverride: readRegSnapshot(inetRegPath, "ProxyOverride"),
		AutoConfigURL: readRegSnapshot(inetRegPath, "AutoConfigURL"),
	}
}

// readRegSnapshot queries a single registry value and returns a three-state result.
// Present=true means the key was found; Present=false means not found or reg.exe failed.
func readRegSnapshot(regPath, valueName string) regSnapshotValue {
	out, err := exec.Command("reg", "query", regPath, "/v", valueName).CombinedOutput()
	if err != nil {
		return regSnapshotValue{Present: false}
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, valueName) {
			for _, regType := range []string{"REG_SZ", "REG_DWORD"} {
				if idx := strings.Index(line, regType); idx >= 0 {
					return regSnapshotValue{Present: true, Value: strings.TrimSpace(line[idx+len(regType):])}
				}
			}
		}
	}
	return regSnapshotValue{Present: false}
}

// restoreProxySnapshot writes back a previously captured snapshot.
// Best-effort: errors are logged but not returned (used in rollback paths).
// Only restores values that were successfully captured (Present=true).
// Values not captured (Present=false) are left untouched to avoid
// destroying user configuration we couldn't read.
func restoreProxySnapshot(snap proxySnapshot) {
	if snap.ProxyEnable.Present {
		if strings.Contains(snap.ProxyEnable.Value, "0x1") || snap.ProxyEnable.Value == "1" {
			exec.Command("reg", "add", inetRegPath, "/v", "ProxyEnable", "/t", "REG_DWORD", "/d", "1", "/f").Run()
		} else {
			exec.Command("reg", "add", inetRegPath, "/v", "ProxyEnable", "/t", "REG_DWORD", "/d", "0", "/f").Run()
		}
	}
	if snap.ProxyServer.Present {
		if snap.ProxyServer.Value != "" {
			exec.Command("reg", "add", inetRegPath, "/v", "ProxyServer", "/t", "REG_SZ", "/d", snap.ProxyServer.Value, "/f").Run()
		} else {
			exec.Command("reg", "delete", inetRegPath, "/v", "ProxyServer", "/f").Run()
		}
	}
	if snap.ProxyOverride.Present {
		if snap.ProxyOverride.Value != "" {
			exec.Command("reg", "add", inetRegPath, "/v", "ProxyOverride", "/t", "REG_SZ", "/d", snap.ProxyOverride.Value, "/f").Run()
		} else {
			exec.Command("reg", "delete", inetRegPath, "/v", "ProxyOverride", "/f").Run()
		}
	}
	if snap.AutoConfigURL.Present {
		if snap.AutoConfigURL.Value != "" {
			exec.Command("reg", "add", inetRegPath, "/v", "AutoConfigURL", "/t", "REG_SZ", "/d", snap.AutoConfigURL.Value, "/f").Run()
		} else {
			exec.Command("reg", "delete", inetRegPath, "/v", "AutoConfigURL", "/f").Run()
		}
	}
	notifyWinInetSettingsChanged()
	log.Println("[proxy] snapshot restored (rollback)")
}

// EnableSystemProxy sets the WinINet system proxy for the current user.
// On partial failure, rolls back to the pre-call state to avoid leaving
// the proxy in a half-configured state (e.g. ProxyEnable=1 but no ProxyServer).
func EnableSystemProxy(proxyAddr, bypass string) error {
	snapshot := captureProxySnapshot()

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
			log.Printf("[proxy] %s failed: %v — rolling back", c.desc, err)
			restoreProxySnapshot(snapshot)
			return fmt.Errorf("%s: %w", c.desc, err)
		}
	}
	// 清除残留的 PAC 配置——WinINet 优先使用 AutoConfigURL，残留的 PAC 会覆盖
	// 刚写入的手工代理，导致代理"设了没生效"。与 EnableSystemProxyPAC 清除
	// ProxyServer/ProxyOverride 对称。
	if err := exec.Command("reg", "delete", inetRegPath, "/v", "AutoConfigURL", "/f").Run(); err != nil {
		// AutoConfigURL 本就不存在时 reg delete 会报错，属正常情况，不告警。
		// 只在键存在但删除失败时才值得关注——此时检查 snapshot 判断原来是否有值。
		if snapshot.AutoConfigURL.Present {
			log.Printf("[proxy] ⚠ 清除残留 AutoConfigURL 失败: %v（PAC 可能覆盖手工代理）", err)
		}
	}
	notifyWinInetSettingsChanged()
	log.Printf("[proxy] system proxy set: %s", proxyAddr)
	return nil
}

// DisableSystemProxy removes the WinINet system proxy setting.
// 注意：本函数仅置 ProxyEnable=0，不还原 AutoConfigURL / ProxyServer 等其他注册表项。
// 完整恢复请使用 restoreWinInetProxyFromState（依赖 install_state 快照）。
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
// On partial failure, rolls back to the pre-call state.
func EnableSystemProxyPAC(pacURL string) error {
	snapshot := captureProxySnapshot()

	cmds := []struct {
		args []string
		desc string
	}{
		{[]string{"reg", "add", inetRegPath, "/v", "AutoConfigURL", "/t", "REG_SZ", "/d", pacURL, "/f"}, "set AutoConfigURL"},
		{[]string{"reg", "add", inetRegPath, "/v", "ProxyEnable", "/t", "REG_DWORD", "/d", "0", "/f"}, "disable manual proxy"},
	}
	for _, c := range cmds {
		if err := exec.Command(c.args[0], c.args[1:]...).Run(); err != nil {
			log.Printf("[proxy] %s failed: %v — rolling back", c.desc, err)
			restoreProxySnapshot(snapshot)
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

// RestoreAutoDetect writes back the WPAD AutoDetect flag.
// present=false 表示安装时未成功捕获原值，跳过还原以免覆盖用户配置。
// present=true 时写回 value（含 0——即"用户原来禁用了 WPAD"）。
func RestoreAutoDetect(value uint32, present bool) {
	if !present {
		return
	}
	exec.Command("reg", "add", inetRegPath, "/v", "AutoDetect", "/t", "REG_DWORD",
		"/d", fmt.Sprintf("%d", value), "/f").Run()
	notifyWinInetSettingsChanged()
	log.Printf("[proxy] AutoDetect restored to %d", value)
}
