package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestResolveLaunchCommand_ExplicitCommand(t *testing.T) {
	args, preset, err := resolveLaunchCommand([]string{"code.cmd", "--reuse-window"}, "", func(cmd string) (string, error) {
		return "", fmt.Errorf("should not call lookPath for explicit command: %s", cmd)
	})
	if err != nil {
		t.Fatal(err)
	}
	if preset != nil {
		t.Fatal("expected nil preset for explicit command")
	}
	if len(args) != 2 || args[0] != "code.cmd" || args[1] != "--reuse-window" {
		t.Fatalf("unexpected args: %#v", args)
	}
}

// 验证 resolveLaunchCommand 在「KnownPaths 全部不存在」时正确回退到 lookPath，
// 同时附加 args 与 preset.Args 的拼接逻辑。
// 用 t.Setenv 把 KnownPaths 里出现的 env vars 都指向不存在的目录，
// 避免依赖本机是否真的装了 VS Code（v3.1.0 起 GUI 预设优先扫 KnownPaths）。
func TestResolveLaunchCommand_Preset(t *testing.T) {
	t.Setenv("LOCALAPPDATA", `C:\__nope__\LOCALAPPDATA`)
	t.Setenv("PROGRAMFILES", `C:\__nope__\PROGRAMFILES`)
	t.Setenv("PROGRAMFILES(X86)", `C:\__nope__\PROGRAMFILES_X86`)

	args, preset, err := resolveLaunchCommand([]string{"--new-window"}, "vscode", func(cmd string) (string, error) {
		if cmd == "code.cmd" {
			return `C:\Tools\code.cmd`, nil
		}
		return "", fmt.Errorf("not found")
	})
	if err != nil {
		t.Fatal(err)
	}
	if preset == nil || preset.Name != "vscode" {
		t.Fatalf("unexpected preset: %#v", preset)
	}
	if len(args) != 2 || args[0] != `C:\Tools\code.cmd` || args[1] != "--new-window" {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestResolveLaunchCommand_UnknownPreset(t *testing.T) {
	_, _, err := resolveLaunchCommand(nil, "unknown-app", func(cmd string) (string, error) {
		return "", fmt.Errorf("not found")
	})
	if err == nil || !strings.Contains(err.Error(), "未知 launch 预设") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolvePresetBinary_MissingBinary(t *testing.T) {
	preset := launchPreset{
		Name:       "cursor",
		Candidates: []string{"cursor.exe", "cursor.cmd"},
		KnownPaths: []string{`C:\Users\tester\AppData\Local\Programs\Cursor\Cursor.exe`},
	}
	_, tried, err := resolvePresetBinary(preset, func(cmd string) (string, error) {
		return "", fmt.Errorf("not found: %s", cmd)
	}, func(path string) bool {
		return false
	})
	if err == nil {
		t.Fatal("expected error for missing preset binary")
	}
	if len(tried) != 3 {
		t.Fatalf("expected all candidates to be tracked, got %#v", tried)
	}
}

// GUI 预设：KnownPath 存在时 *必须先* 命中 KnownPath，不能去走 PATH 里的 .cmd shim。
// 这是 3.1.0 修复的核心——cursor.cmd 这类 shim 启动 GUI 后立即返回 0，
// ai-monitor 会跟着退出，监控失效。
func TestResolvePresetBinary_GUIPresetPrefersKnownPathOverShim(t *testing.T) {
	preset := launchPreset{
		Name:       "cursor",
		Candidates: []string{"cursor.exe", "cursor.cmd", "cursor"},
		KnownPaths: []string{`C:\Program Files\Cursor\Cursor.exe`},
	}
	got, tried, err := resolvePresetBinary(preset, func(cmd string) (string, error) {
		// 模拟用户机器：cursor.exe 不在 PATH，cursor.cmd 在 PATH（shim）
		if cmd == "cursor.cmd" {
			return `C:\Program Files\cursor\resources\app\bin\cursor.cmd`, nil
		}
		return "", fmt.Errorf("not found")
	}, func(path string) bool {
		return path == `C:\Program Files\Cursor\Cursor.exe`
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != `C:\Program Files\Cursor\Cursor.exe` {
		t.Fatalf("GUI 预设必须优先用 KnownPath 的 .exe，避开 .cmd shim；got %q", got)
	}
	// 解析顺序：先 KnownPath 命中即返回，不应再去走 lookPath。
	if len(tried) != 1 {
		t.Fatalf("expected 1 tried entry (KnownPath hit first), got %#v", tried)
	}
}

// GUI 预设：KnownPath 都不存在时才回退到 PATH（可能拿到 .cmd shim）。
// 这条路径会触发 resolveLaunchCommand 中的告警日志，但仍能让用户跑起来。
func TestResolvePresetBinary_GUIPresetFallsBackToPathWhenKnownMissing(t *testing.T) {
	preset := launchPreset{
		Name:       "cursor",
		Candidates: []string{"cursor.exe", "cursor.cmd"},
		KnownPaths: []string{`C:\NonExistent\Cursor\Cursor.exe`},
	}
	got, tried, err := resolvePresetBinary(preset, func(cmd string) (string, error) {
		if cmd == "cursor.cmd" {
			return `C:\Program Files\cursor\resources\app\bin\cursor.cmd`, nil
		}
		return "", fmt.Errorf("not found")
	}, func(path string) bool {
		return false
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != `C:\Program Files\cursor\resources\app\bin\cursor.cmd` {
		t.Fatalf("KnownPath 不存在时应回退到 lookPath；got %q", got)
	}
	// 解析顺序：先尝试 KnownPath（1 entry），再尝试 lookPath（2 entries：cursor.exe + cursor.cmd）。
	if len(tried) != 3 {
		t.Fatalf("expected 3 tried entries (1 KnownPath + 2 Candidates), got %#v", tried)
	}
}

// CLI 预设（powershell / cmd / codex / claude-code）保留旧顺序：PATH 优先。
// 这些工具的入口本来就是命令名，KnownPaths 只是兜底。
func TestResolvePresetBinary_CLIPresetKeepsPathFirst(t *testing.T) {
	preset := launchPreset{
		Name:       "powershell",
		Candidates: []string{"pwsh.exe", "powershell.exe"},
		KnownPaths: []string{`C:\Program Files\PowerShell\7\pwsh.exe`},
	}
	got, _, err := resolvePresetBinary(preset, func(cmd string) (string, error) {
		if cmd == "pwsh.exe" {
			return `C:\Program Files\PowerShell\7\pwsh.exe`, nil
		}
		return "", fmt.Errorf("not found")
	}, func(path string) bool {
		// 故意让 KnownPaths 也存在，验证 CLI 预设*不会*被它顶上来
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != `C:\Program Files\PowerShell\7\pwsh.exe` {
		t.Fatalf("CLI 预设应保留 lookPath 优先；got %q", got)
	}
}

func TestIsShimExecutable(t *testing.T) {
	cases := map[string]bool{
		`C:\Program Files\cursor\resources\app\bin\cursor.cmd`: true,
		`C:\Tools\code.cmd`:                  true,
		`C:\Tools\foo.bat`:                   true,
		`C:\Tools\bar.ps1`:                   true,
		`C:\Program Files\cursor\Cursor.exe`: false,
		`/usr/local/bin/cursor`:              false,
		``:                                   false,
	}
	for path, want := range cases {
		if got := isShimExecutable(path); got != want {
			t.Errorf("isShimExecutable(%q)=%v want %v", path, got, want)
		}
	}
}

func TestCodexLaunchModeDetection(t *testing.T) {
	nativeCLI := `C:/Users/test/AppData/Local/OpenAI/Codex/bin/codex.exe`
	storeGUI := `C:/Program Files/WindowsApps/OpenAI.Codex_26.527.7698.0_x64__2p2nqsd0c76g0/app/Codex.exe`

	if !launchNeedsTerminal(nil, nativeCLI) {
		t.Fatal("custom native Codex CLI should open in a visible terminal")
	}
	if launchNeedsVisibleGUI(nil, nativeCLI) {
		t.Fatal("native Codex CLI should not use GUI detached launch")
	}

	codexApp := &launchPreset{Name: "codex-app"}
	if launchNeedsTerminal(codexApp, storeGUI) {
		t.Fatal("Store/Appx Codex GUI should not open in a terminal")
	}
	if !launchNeedsVisibleGUI(codexApp, storeGUI) {
		t.Fatal("Store/Appx Codex GUI should avoid HideWindow detached launch")
	}
	if !launchNeedsVisibleGUI(nil, storeGUI) {
		t.Fatal("custom Codex GUI path should avoid HideWindow detached launch")
	}
}

func TestIsGUIPreset(t *testing.T) {
	gui := []string{"vscode", "cursor", "windsurf", "kiro", "vscodium", "trae"}
	for _, n := range gui {
		if !isGUIPreset(&launchPreset{Name: n}) {
			t.Errorf("%s 应被识别为 GUI 预设", n)
		}
	}
	cli := []string{"powershell", "cmd", "claude-code", "codex", "idea", "webstorm", "pycharm", "goland", "zed", "qoder"}
	for _, n := range cli {
		if isGUIPreset(&launchPreset{Name: n}) {
			t.Errorf("%s 不应被识别为 GUI 预设（不会触发 KnownPaths 优先）", n)
		}
	}
	if isGUIPreset(nil) {
		t.Error("nil preset 不应被识别为 GUI")
	}
}

func TestExpandEnvPath(t *testing.T) {
	t.Setenv("LOCALAPPDATA", `C:\Users\tester\AppData\Local`)
	got := expandEnvPath(`%LOCALAPPDATA%\Programs\Cursor\Cursor.exe`)
	want := `C:\Users\tester\AppData\Local\Programs\Cursor\Cursor.exe`
	if got != want {
		t.Fatalf("expandEnvPath()=%q want %q", got, want)
	}
}

func TestInferSourceApp(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		preset  *launchPreset
		wantApp string
	}{
		{name: "preset wins", args: []string{"C:\\Tools\\cursor.exe"}, preset: &launchPreset{Name: "cursor"}, wantApp: "cursor"},
		{name: "vscode command", args: []string{"code.cmd"}, wantApp: "vscode"},
		{name: "codex command", args: []string{"/usr/local/bin/codex"}, wantApp: "codex"},
		{name: "powershell exe", args: []string{"C:\\Windows\\System32\\WindowsPowerShell\\v1.0\\powershell.exe"}, wantApp: "powershell"},
		{name: "custom command", args: []string{"python.exe"}, wantApp: "python"},
		{name: "empty args", args: nil, wantApp: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := inferSourceApp(tc.args, tc.preset); got != tc.wantApp {
				t.Fatalf("inferSourceApp()=%q want %q", got, tc.wantApp)
			}
		})
	}
}

func TestCompatProxyPortsIncludesStaleSelfProxyPorts(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:18092")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:7890")

	got := compatProxyPorts(&Config{Port: 18090}, 18090)
	if !intSliceContains(got, 18091) || !intSliceContains(got, 18092) {
		t.Fatalf("compatProxyPorts()=%v, want stale ai-monitor ports 18091 and 18092", got)
	}
	if intSliceContains(got, 18090) || intSliceContains(got, 7890) {
		t.Fatalf("compatProxyPorts()=%v, should exclude current port and non-ai-monitor proxy port", got)
	}
}

func intSliceContains(xs []int, want int) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

func TestManagedPresetProcessImage(t *testing.T) {
	tests := []struct {
		name        string
		preset      *launchPreset
		wantImage   string
		wantDisplay string
	}{
		{name: "nil", preset: nil},
		{name: "vscode", preset: &launchPreset{Name: "vscode"}, wantImage: "Code.exe", wantDisplay: "VS Code"},
		{name: "cursor", preset: &launchPreset{Name: "cursor"}, wantImage: "Cursor.exe", wantDisplay: "Cursor"},
		{name: "powershell", preset: &launchPreset{Name: "powershell"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotImage, gotDisplay := managedPresetProcessImage(tc.preset)
			if gotImage != tc.wantImage || gotDisplay != tc.wantDisplay {
				t.Fatalf("managedPresetProcessImage()=(%q,%q) want (%q,%q)", gotImage, gotDisplay, tc.wantImage, tc.wantDisplay)
			}
		})
	}
}
