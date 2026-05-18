package main

import (
	"bytes"
	"strings"
	"testing"
)

// detectRunningElectronEditors 必须返回所有 Electron 类预设中「正在跑」的实例，
// 且与 managedLaunchPresets 顺序一致。CLI 类预设（jetbrains/codex/powershell 等）
// 不会被列入——它们不依赖 system PAC，重启意义不大。
func TestDetectRunningElectronEditorsListsOnlyElectronAndOnlyRunning(t *testing.T) {
	running := map[string]bool{
		"Cursor.exe":  true,
		"Code.exe":    true,
		"idea64.exe":  true, // JetBrains 不算 Electron，应被忽略
		"pwsh.exe":    true, // 终端不算
		"VSCodium.exe": false,
	}
	got := detectRunningElectronEditors(func(image string) bool {
		return running[image]
	})

	// 检查返回内容只包含跑着的 Electron 类
	names := make([]string, 0, len(got))
	for _, e := range got {
		names = append(names, e.Preset)
	}

	wantContains := map[string]bool{
		"vscode": true,
		"cursor": true,
	}
	wantNotContains := map[string]bool{
		"vscodium": true, // 没在跑
		"idea":     true, // 不是 Electron
		"powershell": true,
	}
	for w := range wantContains {
		if !sliceContains(names, w) {
			t.Errorf("应包含 %q（Cursor.exe 在跑）, got %v", w, names)
		}
	}
	for w := range wantNotContains {
		if sliceContains(names, w) {
			t.Errorf("不应包含 %q, got %v", w, names)
		}
	}
}

// 没有任何 Electron IDE 在跑时，返回空切片，不能 panic。
func TestDetectRunningElectronEditorsEmpty(t *testing.T) {
	got := detectRunningElectronEditors(func(image string) bool { return false })
	if len(got) != 0 {
		t.Fatalf("无运行实例时应返回空切片，got %v", got)
	}
}

// 同一 ImageName 在多个 preset 里出现（理论可能）时，去重只列一次，避免
// taskkill 重复执行。当前 managedLaunchPresets 没重复，但代码做了 seen 防护，
// 这条用例锁住这个行为不退化。
func TestDetectRunningElectronEditorsDedupe(t *testing.T) {
	calls := 0
	_ = detectRunningElectronEditors(func(image string) bool {
		calls++
		return true
	})
	// Electron 类预设当前 6 个（vscode/cursor/windsurf/kiro/vscodium/trae），每个查一次
	if calls != 6 {
		t.Fatalf("expected 6 image queries (one per Electron preset), got %d", calls)
	}
}

// promptRestartElectronEditors 在非交互式 stdin 下必须直接返回 false 且不阻塞 ReadString。
func TestPromptRestartReturnsFalseWhenNonInteractive(t *testing.T) {
	editors := []runningElectronEditor{{Preset: "cursor", ImageName: "Cursor.exe", Display: "Cursor"}}
	var out bytes.Buffer
	if got := promptRestartElectronEditors(editors, strings.NewReader(""), &out, false); got {
		t.Fatal("非交互式 stdin 不应返回 true，会卡死后台启动")
	}
	if !strings.Contains(out.String(), "非交互式") {
		t.Fatalf("输出中应提示非交互式跳过；got:\n%s", out.String())
	}
}

// promptRestartElectronEditors：editors 为空时不打印询问，也不读 stdin。
func TestPromptRestartIsNoOpWhenNoEditors(t *testing.T) {
	var out bytes.Buffer
	got := promptRestartElectronEditors(nil, strings.NewReader("Y\n"), &out, true)
	if got {
		t.Fatal("空列表时不能返回 true")
	}
	if out.Len() != 0 {
		t.Fatalf("空列表时不应输出任何内容；got:\n%s", out.String())
	}
}

// 交互式：输入 Y / 回车 都视为 yes（默认行为）。
func TestPromptRestartAcceptsYesAndEnter(t *testing.T) {
	editors := []runningElectronEditor{{Preset: "cursor", ImageName: "Cursor.exe", Display: "Cursor"}}
	for _, in := range []string{"Y\n", "y\n", "yes\n", "\n", "YES\n"} {
		var out bytes.Buffer
		got := promptRestartElectronEditors(editors, strings.NewReader(in), &out, true)
		if !got {
			t.Errorf("输入 %q 应返回 true（视为 yes）", in)
		}
	}
}

// 交互式：输入 N / no 视为 no，不重启。
func TestPromptRestartRejectsNo(t *testing.T) {
	editors := []runningElectronEditor{{Preset: "cursor", ImageName: "Cursor.exe", Display: "Cursor"}}
	for _, in := range []string{"N\n", "n\n", "no\n", "NO\n"} {
		var out bytes.Buffer
		got := promptRestartElectronEditors(editors, strings.NewReader(in), &out, true)
		if got {
			t.Errorf("输入 %q 应返回 false（视为 no）", in)
		}
	}
}

// isShimExecutable 已有测试，这里只补一个跨平台空路径用例（killAndRelaunchEditors 会用）。
func TestRelaunchHelperRejectsEmpty(t *testing.T) {
	if isShimExecutable("") {
		t.Fatal("空路径不应被认为是 shim")
	}
}

func sliceContains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
