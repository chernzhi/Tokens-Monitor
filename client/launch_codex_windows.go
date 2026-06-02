//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// discoverCodexApp 在固定 KnownPaths / PATH 都找不到 Codex 桌面版时，
// 真正去机器上「搜」出 Codex.exe。因为不同机器安装位置不同（Squirrel/NSIS、
// per-user / per-machine、自定义目录），这里多策略联合发现，命中即返回绝对路径。
//
// 策略顺序（命中即停）：
//  1. Microsoft Store / Appx：Get-AppxPackage 的 InstallLocation\app\Codex.exe。
//  2. 注册表卸载项：HKCU/HKLM 的 Uninstall\*，DisplayName 含 codex → InstallLocation/DisplayIcon。
//  3. App Paths：HKCU/HKLM 的 App Paths\Codex.exe。
//  4. 开始菜单快捷方式 Codex.lnk 解析目标。
//  5. 常见根目录下递归浅扫 Codex.exe（Programs / Squirrel 风格 app-* 目录）。
func discoverCodexApp() (string, bool) {
	if p, ok := codexFromAppxPackage(); ok {
		return p, true
	}
	if p, ok := codexFromUninstallKeys(); ok {
		return p, true
	}
	if p, ok := codexFromAppPaths(); ok {
		return p, true
	}
	if p, ok := codexFromStartMenu(); ok {
		return p, true
	}
	if p, ok := codexFromCommonRoots(); ok {
		return p, true
	}
	return "", false
}

func isCodexExe(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	base := strings.ToLower(filepath.Base(path))
	if base != "codex.exe" && base != "codex-app.exe" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func codexFromAppxPackage() (string, bool) {
	cmd := newHiddenCmd("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", "Get-AppxPackage -Name *Codex* | Where-Object { $_.Name -like 'OpenAI.Codex*' -or $_.PackageFamilyName -like 'OpenAI.Codex*' } | Sort-Object Version -Descending | Select-Object -ExpandProperty InstallLocation")
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(string(out), "\n") {
		if p, ok := findCodexUnder(line); ok {
			return p, true
		}
	}
	return "", false
}

// codexFromUninstallKeys 扫卸载项，DisplayName 含 "codex" 时优先用 DisplayIcon
// （常直接指向主 exe），否则在 InstallLocation 下浅找 Codex.exe。
func codexFromUninstallKeys() (string, bool) {
	roots := []struct {
		key  registry.Key
		path string
	}{
		{registry.CURRENT_USER, `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`},
		{registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`},
		{registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`},
	}
	for _, r := range roots {
		base, err := registry.OpenKey(r.key, r.path, registry.READ)
		if err != nil {
			continue
		}
		subs, err := base.ReadSubKeyNames(-1)
		if err != nil {
			base.Close()
			continue
		}
		for _, sub := range subs {
			k, err := registry.OpenKey(r.key, r.path+`\`+sub, registry.QUERY_VALUE)
			if err != nil {
				continue
			}
			name, _, _ := k.GetStringValue("DisplayName")
			if !strings.Contains(strings.ToLower(name), "codex") {
				k.Close()
				continue
			}
			icon, _, _ := k.GetStringValue("DisplayIcon")
			loc, _, _ := k.GetStringValue("InstallLocation")
			k.Close()
			// DisplayIcon 形如 "C:\...\Codex.exe,0"，去掉逗号索引。
			if icon != "" {
				if comma := strings.LastIndex(icon, ","); comma > 1 {
					icon = icon[:comma]
				}
				icon = strings.Trim(strings.TrimSpace(icon), `"`)
				if isCodexExe(icon) {
					base.Close()
					return icon, true
				}
			}
			if loc != "" {
				if p, ok := findCodexUnder(loc); ok {
					base.Close()
					return p, true
				}
			}
		}
		base.Close()
	}
	return "", false
}

func codexFromAppPaths() (string, bool) {
	const sub = `SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\Codex.exe`
	for _, root := range []registry.Key{registry.CURRENT_USER, registry.LOCAL_MACHINE} {
		k, err := registry.OpenKey(root, sub, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		val, _, _ := k.GetStringValue("")
		k.Close()
		val = strings.Trim(strings.TrimSpace(val), `"`)
		if isCodexExe(val) {
			return val, true
		}
	}
	return "", false
}

// codexFromStartMenu 在开始菜单目录里找 Codex*.lnk 并解析其目标 exe。
func codexFromStartMenu() (string, bool) {
	dirs := []string{
		filepath.Join(os.Getenv("APPDATA"), `Microsoft\Windows\Start Menu\Programs`),
		filepath.Join(os.Getenv("ProgramData"), `Microsoft\Windows\Start Menu\Programs`),
	}
	var found string
	for _, d := range dirs {
		if strings.TrimSpace(d) == "" {
			continue
		}
		_ = filepath.WalkDir(d, func(path string, de os.DirEntry, err error) error {
			if err != nil || de.IsDir() {
				return nil
			}
			base := strings.ToLower(de.Name())
			if !strings.HasSuffix(base, ".lnk") || !strings.Contains(base, "codex") {
				return nil
			}
			if target, ok := resolveShortcutTarget(path); ok && isCodexExe(target) {
				found = target
				return filepath.SkipAll
			}
			return nil
		})
		if found != "" {
			return found, true
		}
	}
	return "", false
}

// codexFromCommonRoots 在最常见的安装根目录下浅层 glob Codex.exe。
func codexFromCommonRoots() (string, bool) {
	patterns := []string{
		// Microsoft Store / Appx 版：C:\Program Files\WindowsApps\OpenAI.Codex_*\app\Codex.exe。
		filepath.Join(os.Getenv("ProgramFiles"), `WindowsApps`, `OpenAI.Codex_*`, `app`, `Codex.exe`),
		// 新版原生 Codex CLI：%LOCALAPPDATA%\OpenAI\Codex\bin\codex.exe（小写、bin 子目录）。
		filepath.Join(os.Getenv("LOCALAPPDATA"), `OpenAI`, `Codex`, `bin`, `codex.exe`),
		filepath.Join(os.Getenv("ProgramFiles"), `OpenAI`, `Codex`, `bin`, `codex.exe`),
		filepath.Join(os.Getenv("LOCALAPPDATA"), `*odex*`, `bin`, `codex.exe`),
		filepath.Join(os.Getenv("LOCALAPPDATA"), `Programs`, `*odex*`, `Codex.exe`),
		filepath.Join(os.Getenv("LOCALAPPDATA"), `*odex*`, `Codex.exe`),
		filepath.Join(os.Getenv("LOCALAPPDATA"), `*odex*`, `app-*`, `Codex.exe`),
		filepath.Join(os.Getenv("ProgramFiles"), `*odex*`, `Codex.exe`),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), `*odex*`, `Codex.exe`),
	}
	for _, pat := range patterns {
		if strings.TrimSpace(pat) == "" {
			continue
		}
		matches, _ := filepath.Glob(pat)
		// Squirrel 的 app-* 多版本并存，取字典序最大（通常即最新版本）。
		var best string
		for _, m := range matches {
			if isCodexExe(m) && m > best {
				best = m
			}
		}
		if best != "" {
			return best, true
		}
	}
	return "", false
}

// findCodexUnder 在目录 dir 下浅层（自身 + app-* 子目录）找 Codex.exe。
func findCodexUnder(dir string) (string, bool) {
	dir = strings.Trim(strings.TrimSpace(dir), `"`)
	if dir == "" {
		return "", false
	}
	direct := filepath.Join(dir, "Codex.exe")
	if isCodexExe(direct) {
		return direct, true
	}
	// Microsoft Store / Appx 版的 InstallLocation 下实际入口为 app\Codex.exe。
	if appExe := filepath.Join(dir, "app", "Codex.exe"); isCodexExe(appExe) {
		return appExe, true
	}
	// 新版原生 CLI 把 exe 放在 bin\codex.exe（InstallLocation 常指向 Codex\ 根目录）。
	if binExe := filepath.Join(dir, "bin", "codex.exe"); isCodexExe(binExe) {
		return binExe, true
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "app-*", "Codex.exe"))
	var best string
	for _, m := range matches {
		if isCodexExe(m) && m > best {
			best = m
		}
	}
	if best != "" {
		return best, true
	}
	return "", false
}
