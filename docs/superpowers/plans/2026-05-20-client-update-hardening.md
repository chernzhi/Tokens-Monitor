# 客户端更新流程加固 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复「立即更新」卡住 + 多实例并存 + 升级后页面打不开。

**Architecture:** 引入真单例文件锁 (`LockFileEx`)；把 `os.Exit` 改成统一的优雅退出钩子 `RequestShutdown`（关 WebView2 + Shutdown HTTP + 还原 PAC + 释放锁）；`updater.bat` 等旧 PID 退出再替换 exe；前端 apply 后切「重启中」状态并轮询新端口；加 `--force-cleanup` 兜底清理残留。

**Tech Stack:** Go 1.x + `golang.org/x/sys/windows` (已在 go.mod) + Windows API (LockFileEx, dwmapi/user32 已用) + 现有 WebView2 集成 + cmd batch 脚本。

**Spec:** `docs/superpowers/specs/2026-05-20-client-update-hardening-design.md`

---

## File Structure

**Create:**
- `client/singleton_lock_windows.go` — LockFileEx 单例锁
- `client/singleton_lock_other.go` — 非 Windows 平台桩（保持构建可过）
- `client/singleton_lock_windows_test.go` — 并发抢锁测试
- `client/shutdown.go` — `RequestShutdown` + `shutdownCh`（跨平台）
- `client/force_cleanup_windows.go` — `--force-cleanup` 实现
- `client/force_cleanup_other.go` — 非 Windows 桩
- `client/updater_bat_render_test.go` — bat 模板渲染断言

**Modify:**
- `client/main.go` — 集成锁、shutdown channel、新 flag、--post-update 等锁
- `client/updater_apply_windows.go` — bat 模板加 PID 等待；ApplyUpdate 改用 `RequestShutdown`
- `client/updater_test.go` — 调整 `TestRenderUpdaterBat_ContainsKeyTokens` 断言（保持向后兼容则不删 token）
- `client/wizard_window_windows.go` — 暴露包级 `closeActiveWizardWindow()`
- `client/wizard_window_other.go`（如果不存在则在 main 路径加 stub）— 桩
- `client/webwizard.go` — 新增 `GET /api/wizard/instance`；前端 `applyUpdate` 切重启中状态 + 轮询；模板渲染测试断言

---

## Task 1: 跨平台单例锁接口 + 非 Windows 桩

**Files:**
- Create: `client/singleton_lock_other.go`

- [ ] **Step 1: 写非 Windows 桩**

```go
//go:build !windows

package main

import "time"

// acquireSingletonLock 在非 Windows 平台是 no-op：返回一个空 release。
// 真正的单例保护仍由现有 checkExistingInstance 提供。
func acquireSingletonLock() (release func(), ok bool, err error) {
	return func() {}, true, nil
}

func waitForSingletonLock(_ time.Duration) (release func(), err error) {
	return func() {}, nil
}

func releaseSingletonLock() {}
```

- [ ] **Step 2: 编译通过**

Run: `cd client && go build ./...`
Expected: no errors (Windows 实现下一 task 再做；当前 Windows build 会缺符号 — 暂时只在 Linux/macOS 上验证；如果在 Windows 上跑就跳到 Task 2 一起验证)。

- [ ] **Step 3: Commit**

```bash
cd client
git add singleton_lock_other.go
git commit -m "feat(client): 单例锁非 Windows 桩"
```

---

## Task 2: Windows LockFileEx 单例锁

**Files:**
- Create: `client/singleton_lock_windows.go`
- Test: `client/singleton_lock_windows_test.go`

- [ ] **Step 1: 写失败测试**

```go
//go:build windows

package main

import (
	"sync"
	"testing"
	"time"
)

func TestAcquireSingletonLock_ExclusiveBetweenInProcessCallers(t *testing.T) {
	// 同进程内 LockFileEx 也是独占的（用不同的 file handle）。
	t.Setenv("APPDATA", t.TempDir())

	release1, ok1, err := acquireSingletonLock()
	if err != nil || !ok1 {
		t.Fatalf("first acquire should succeed: ok=%v err=%v", ok1, err)
	}
	defer release1()

	_, ok2, err := acquireSingletonLock()
	if err != nil {
		t.Fatalf("second acquire returned error: %v", err)
	}
	if ok2 {
		t.Fatalf("second acquire should fail while first holds the lock")
	}
}

func TestAcquireSingletonLock_ReacquireAfterRelease(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())

	release1, ok1, err := acquireSingletonLock()
	if err != nil || !ok1 {
		t.Fatalf("first acquire failed: %v", err)
	}
	release1()

	release2, ok2, err := acquireSingletonLock()
	if err != nil || !ok2 {
		t.Fatalf("re-acquire after release should succeed: ok=%v err=%v", ok2, err)
	}
	release2()
}

func TestWaitForSingletonLock_TimesOutWhenHeld(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())

	release1, ok, err := acquireSingletonLock()
	if !ok || err != nil {
		t.Fatalf("setup failed: ok=%v err=%v", ok, err)
	}
	defer release1()

	start := time.Now()
	_, err = waitForSingletonLock(500 * time.Millisecond)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatalf("expected timeout, got nil")
	}
	if elapsed < 400*time.Millisecond {
		t.Fatalf("returned too early: %v", elapsed)
	}
}

func TestWaitForSingletonLock_AcquiresAfterReleased(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())

	release1, _, _ := acquireSingletonLock()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(200 * time.Millisecond)
		release1()
	}()

	release2, err := waitForSingletonLock(2 * time.Second)
	if err != nil {
		t.Fatalf("wait should succeed within timeout: %v", err)
	}
	release2()
	wg.Wait()
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd client && go test -run TestAcquireSingletonLock -tags windows -v` (实际只能在 Windows 上跑；如本机非 Windows 跳过此 step，跑 Task 3 集成后由 CI / 手工 Windows 跑)
Expected: build error — `acquireSingletonLock undefined`

- [ ] **Step 3: 写最小实现**

```go
//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	lockfileExclusive       = 0x00000002
	lockfileFailImmediately = 0x00000001
)

var (
	lockMu     sync.Mutex
	lockHandle windows.Handle
	lockFile   string
)

func singletonLockPath() string {
	return filepath.Join(appDataDir(), "instance.lock")
}

// acquireSingletonLock 非阻塞地尝试拿独占锁。返回 ok=true 表示拿到。
// 进程崩溃 / 被 kill 时 Windows 内核会自动释放，不会留死锁。
func acquireSingletonLock() (release func(), ok bool, err error) {
	lockMu.Lock()
	defer lockMu.Unlock()
	if lockHandle != 0 {
		// 同进程已持有；返回 noop release，避免误关。
		return func() {}, true, nil
	}

	p := singletonLockPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return nil, false, fmt.Errorf("mkdir lock dir: %w", err)
	}

	utf16, _ := windows.UTF16PtrFromString(p)
	h, openErr := windows.CreateFile(
		utf16,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_ALWAYS,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if openErr != nil {
		return nil, false, fmt.Errorf("open lock file: %w", openErr)
	}

	var overlapped windows.Overlapped
	lockEx := syscall.NewLazyDLL("kernel32.dll").NewProc("LockFileEx")
	r1, _, lockErr := lockEx.Call(
		uintptr(h),
		uintptr(lockfileExclusive|lockfileFailImmediately),
		0,
		uintptr(0xFFFFFFFF), uintptr(0xFFFFFFFF), // 锁整个文件
		uintptr(unsafe.Pointer(&overlapped)),
	)
	if r1 == 0 {
		windows.CloseHandle(h)
		// ERROR_LOCK_VIOLATION = 33；其他错误也按"被占用"处理但带 err 给调用者诊断。
		if errno, okCast := lockErr.(syscall.Errno); okCast && errno == 33 {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("LockFileEx: %v", lockErr)
	}

	lockHandle = h
	lockFile = p
	return func() { releaseSingletonLockLocked() }, true, nil
}

// waitForSingletonLock 阻塞重试至 timeout。给 --post-update 用，
// 旧进程优雅退出窗口期内自动接力。
func waitForSingletonLock(timeout time.Duration) (release func(), err error) {
	deadline := time.Now().Add(timeout)
	for {
		rel, ok, err := acquireSingletonLock()
		if ok {
			return rel, nil
		}
		if err != nil {
			return nil, err
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("acquire singleton lock timeout after %s", timeout)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// releaseSingletonLock 由 shutdown 路径调用。幂等。
func releaseSingletonLock() {
	lockMu.Lock()
	defer lockMu.Unlock()
	releaseSingletonLockLocked()
}

func releaseSingletonLockLocked() {
	if lockHandle == 0 {
		return
	}
	unlockEx := syscall.NewLazyDLL("kernel32.dll").NewProc("UnlockFileEx")
	var overlapped windows.Overlapped
	unlockEx.Call(
		uintptr(lockHandle),
		0,
		uintptr(0xFFFFFFFF), uintptr(0xFFFFFFFF),
		uintptr(unsafe.Pointer(&overlapped)),
	)
	windows.CloseHandle(lockHandle)
	lockHandle = 0
}
```

- [ ] **Step 4: 跑测试确认通过**

Run (Windows): `cd client && go test -run TestAcquireSingletonLock -v && go test -run TestWaitForSingletonLock -v`
Expected: 4 个 case 全 PASS。

如本机非 Windows，至少跑 `GOOS=windows go vet ./...` 保证编译通过。

- [ ] **Step 5: Commit**

```bash
cd client
git add singleton_lock_windows.go singleton_lock_windows_test.go
git commit -m "feat(client): 真单例锁 LockFileEx + 并发测试"
```

---

## Task 3: 跨平台 shutdown channel

**Files:**
- Create: `client/shutdown.go`

- [ ] **Step 1: 写实现**

```go
package main

import (
	"log"
	"sync"
)

// shutdownCh 在 RequestShutdown 第一次被调用时关闭。
// main 循环监听它来触发优雅退出，与 SIGINT 路径共享同一段清理代码。
var (
	shutdownOnce sync.Once
	shutdownCh   = make(chan struct{})
)

// RequestShutdown 由 updater / WebView2 关闭事件 / signal handler 调用。
// 多次调用安全。返回不阻塞；真正的 shutdown 在 main 中执行。
func RequestShutdown(reason string) {
	shutdownOnce.Do(func() {
		log.Printf("[shutdown] requested: %s", reason)
		close(shutdownCh)
	})
}
```

- [ ] **Step 2: 编译通过**

Run: `cd client && go build ./...`
Expected: success

- [ ] **Step 3: Commit**

```bash
cd client
git add shutdown.go
git commit -m "feat(client): RequestShutdown 优雅退出钩子"
```

---

## Task 4: WebView2 关闭包级访问

**Files:**
- Modify: `client/wizard_window_windows.go`
- Create: `client/wizard_window_other.go` (如不存在)

- [ ] **Step 1: 在 wizard_window_windows.go 顶部加包级 closer 注册**

在文件 import 后插入：

```go
// activeWizardCloser 是最近一次 openWizardWindow 注册的 close 回调。
// shutdown 路径调用 closeActiveWizardWindow 一次性关闭 WebView2 宿主窗口，
// 避免「卡死无响应的旧窗口」（截图中标题 find "" 的孤儿窗口）。
var (
	activeWizardMu     sync.Mutex
	activeWizardCloser func()
)

func setActiveWizardCloser(fn func()) {
	activeWizardMu.Lock()
	activeWizardCloser = fn
	activeWizardMu.Unlock()
}

func closeActiveWizardWindow() {
	activeWizardMu.Lock()
	fn := activeWizardCloser
	activeWizardCloser = nil
	activeWizardMu.Unlock()
	if fn != nil {
		fn()
	}
}
```

- [ ] **Step 2: 在 openWizardWindow 内 closeOnRequest 赋值处同步注册到包级**

定位 `wizard_window_windows.go:218` 附近的 `*closeOnRequest = func() {...}`，在赋值后加：

```go
			*closeOnRequest = func() { /* existing body */ }
			setActiveWizardCloser(*closeOnRequest)  // <-- 新增
```

如果调用方传 nil（main.go 那两处确实传 nil），改成：构造一个本地 closer 直接 setActiveWizardCloser：

```go
		// 即便调用方未要求 closeOnRequest，也注册包级 closer 给 shutdown 路径用。
		localCloser := func() {
			if hwnd != 0 { postWMClose(hwnd) }
			w.Dispatch(func() { w.Terminate() })
		}
		setActiveWizardCloser(localCloser)
		if closeOnRequest != nil {
			*closeOnRequest = localCloser
		}
```

完整替换的代码段（覆盖 215-229 行）：

```go
		localCloser := func() {
			if hwnd != 0 {
				postWMClose(hwnd)
			}
			localW := w
			localW.Dispatch(func() { localW.Terminate() })
		}
		setActiveWizardCloser(localCloser)
		if closeOnRequest != nil {
			*closeOnRequest = localCloser
		}
```

- [ ] **Step 3: 在 goroutine 退出（窗口被关闭）时清理包级 closer**

在 `defer close(done)` 之后追加 `defer setActiveWizardCloser(nil)`。

- [ ] **Step 4: 创建非 Windows 桩**

Create `client/wizard_window_other.go`:

```go
//go:build !windows

package main

func closeActiveWizardWindow() {}
```

(注意如果项目已有同名 stub，跳过此 step；先 `ls client/wizard_window_*.go` 确认。)

- [ ] **Step 5: 编译通过**

Run: `cd client && go build ./... && GOOS=linux go build ./... 2>/dev/null || true`
Expected: Windows 编译通过；Linux 路径如果失败说明缺 stub，补 stub 文件。

- [ ] **Step 6: Commit**

```bash
cd client
git add wizard_window_windows.go wizard_window_other.go
git commit -m "feat(client): WebView2 窗口暴露包级 close 给 shutdown 路径"
```

---

## Task 5: main.go 接入单例锁与 shutdown channel

**Files:**
- Modify: `client/main.go`

- [ ] **Step 1: 在 main.go 单例分支前抢锁**

定位 `client/main.go:230`（`existingPort, alive := checkExistingInstance()` 前），插入：

```go
	// 真单例锁：进程崩溃 OS 自动释放。--post-update 走阻塞重试，给旧进程交接时间。
	var lockRelease func()
	if *postUpdate != "" {
		rel, err := waitForSingletonLock(10 * time.Second)
		if err != nil {
			log.Printf("[singleton] --post-update 抢锁超时: %v；降级走旧 HTTP 探活", err)
		} else {
			lockRelease = rel
		}
	} else {
		rel, ok, err := acquireSingletonLock()
		if err != nil {
			log.Printf("[singleton] 锁文件错误: %v；降级走旧 HTTP 探活", err)
		} else if !ok {
			// 锁被占 → 必然有另一实例，沿用旧分支拉起向导对准已运行端口。
			existingPort, alive := checkExistingInstance()
			if alive {
				log.Printf("[EXIT] reason=singleton-lock-held port=%d pid=%d", existingPort, os.Getpid())
				fmt.Printf("  已有 ai-monitor 实例运行于端口 %d，当前进程退出。\n", existingPort)
				if defaultRunMode {
					wizardURL := fmt.Sprintf("http://127.0.0.1:%d/wizard", existingPort)
					done, _ := openWizardOrBrowser(wizardURL, "AI Token 监控")
					if done != nil {
						<-done
					}
				} else {
					fmt.Println("  如需重启，请先终止已有进程。")
				}
				os.Exit(0)
			}
			// 锁被占但 HTTP 探不到 → 异常残留：提示用户跑 --force-cleanup
			fmt.Println("  ⚠ 检测到 instance.lock 被占但服务端口不可达，可能有残留进程。")
			fmt.Println("    请执行: ai-monitor.exe --force-cleanup")
			log.Printf("[EXIT] reason=singleton-lock-held-but-unreachable pid=%d", os.Getpid())
			os.Exit(1)
		} else {
			lockRelease = rel
		}
	}
	_ = lockRelease // 将在 graceful shutdown 中通过 releaseSingletonLock 释放
```

把原来的单例块（230-247 行）整体替换或紧随其后；保留 `removeInstanceInfo() // clean up any stale PID file`。

- [ ] **Step 2: 在 signal handler 处加 shutdownCh 监听**

定位 `main.go:300-309` 的 `go func() { sigCh ... <-sigCh ... }`，改成：

```go
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		select {
		case <-sigCh:
			log.Println("[shutdown] signal received")
		case <-shutdownCh:
			log.Println("[shutdown] internal request received")
		}
		fmt.Println("\n  正在关闭...")
		closeActiveWizardWindow()
		removeInstanceInfo()
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		runtime.Shutdown(ctx)
		releaseSingletonLock()
	}()
```

- [ ] **Step 3: 编译通过**

Run: `cd client && go build ./...`
Expected: success（如果报 `runtime` 名冲突，main.go 顶部已 `import "runtime"`，注意变量名 `runtime` 在 main 中是 MonitorRuntime；此处用的是变量 not package — 沿用 252 行 `runtime, err := startMonitorRuntime(...)`，无冲突）。

- [ ] **Step 4: Commit**

```bash
cd client
git add main.go
git commit -m "feat(client): main 接入单例锁 + shutdownCh"
```

---

## Task 6: ApplyUpdate 改用 RequestShutdown + bat 等待旧 PID

**Files:**
- Modify: `client/updater_apply_windows.go`
- Create: `client/updater_bat_render_test.go`

- [ ] **Step 1: 写失败测试（bat 模板）**

```go
//go:build windows

package main

import (
	"strings"
	"testing"
)

func TestRenderUpdaterBat_WaitsForOldPID(t *testing.T) {
	got := renderUpdaterBat()
	must := []string{
		`tasklist /FI "PID eq %OLDPID%"`,
		"taskkill /PID %OLDPID% /T /F",
		`start "" "%TARGET%" --post-update "%BACKUP%"`,
		"move /Y",
	}
	for _, s := range must {
		if !strings.Contains(got, s) {
			t.Errorf("bat missing %q\n--- got ---\n%s", s, got)
		}
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run (Windows): `cd client && go test -run TestRenderUpdaterBat_WaitsForOldPID -v`
Expected: FAIL — 模板里没有 `tasklist /FI` 和 `OLDPID`。

- [ ] **Step 3: 改 bat 模板加 PID 等待 + taskkill 兜底**

替换 `client/updater_apply_windows.go:13-52` 整个 `updaterBatTemplate`：

```go
const updaterBatTemplate = `@echo off
setlocal EnableExtensions
set "TARGET=%~1"
set "NEW=%~2"
set "BACKUP=%~3"
set "LOG=%~4"
set "OLDPID=%~5"

> "%LOG%" echo [updater] %DATE% %TIME% start
>>"%LOG%" echo TARGET=%TARGET%
>>"%LOG%" echo NEW=%NEW%
>>"%LOG%" echo BACKUP=%BACKUP%
>>"%LOG%" echo OLDPID=%OLDPID%

REM 1) 等待旧进程退出，最多 ~10s
set /a WAIT=0
:waitloop
tasklist /FI "PID eq %OLDPID%" 2>nul | findstr /C:"%OLDPID%" >nul
if errorlevel 1 goto killed
set /a WAIT+=1
if %WAIT% GEQ 20 goto forcekill
ping -n 2 127.0.0.1 >nul
goto waitloop

:forcekill
>>"%LOG%" echo old pid %OLDPID% still alive after 10s, force kill
taskkill /PID %OLDPID% /T /F >>"%LOG%" 2>&1
ping -n 2 127.0.0.1 >nul

:killed
REM 2) 备份当前 exe
copy /Y "%TARGET%" "%BACKUP%" >>"%LOG%" 2>&1
if errorlevel 1 (
  >>"%LOG%" echo backup failed
  exit /b 1
)

REM 3) 重试覆盖：理论上旧进程已死，5 次足够
set /a TRIES=0
:retry
move /Y "%NEW%" "%TARGET%" >>"%LOG%" 2>&1
if not errorlevel 1 goto launched
set /a TRIES+=1
if %TRIES% GEQ 10 goto rollback
ping -n 2 127.0.0.1 >nul
goto retry

:launched
>>"%LOG%" echo move ok after %TRIES% retries
start "" "%TARGET%" --post-update "%BACKUP%"
exit /b 0

:rollback
>>"%LOG%" echo move failed, rolling back
copy /Y "%BACKUP%" "%TARGET%" >>"%LOG%" 2>&1
start "" "%TARGET%"
exit /b 2
`
```

- [ ] **Step 4: 改 ApplyUpdate 传 PID + 调 RequestShutdown**

替换 `client/updater_apply_windows.go:80-94`：

```go
	if err := os.WriteFile(batPath, []byte(renderUpdaterBat()), 0o755); err != nil {
		return err
	}

	myPID := fmt.Sprintf("%d", os.Getpid())
	cmd := newDetachedCmd("cmd", "/c", batPath,
		currentExe, newExe, backupPath, logPath, myPID)
	if err := cmd.Start(); err != nil {
		return err
	}
	log.Printf("[updater] 已派发 updater.bat (pid=%d)，本进程将优雅退出释放 exe", cmd.Process.Pid)
	go func() {
		time.Sleep(500 * time.Millisecond)
		RequestShutdown("update-apply")
	}()
	return nil
}
```

- [ ] **Step 5: 跑测试确认通过**

Run (Windows): `cd client && go test -run TestRenderUpdaterBat -v`
Expected: 两个 case (旧的 ContainsKeyTokens + 新 WaitsForOldPID) 全 PASS。

- [ ] **Step 6: Commit**

```bash
cd client
git add updater_apply_windows.go updater_bat_render_test.go
git commit -m "feat(client): updater.bat 等旧 PID 退出 + ApplyUpdate 走 RequestShutdown"
```

---

## Task 7: /api/wizard/instance HTTP 端点

**Files:**
- Modify: `client/webwizard.go`

- [ ] **Step 1: 写失败测试（模板渲染）**

加到 `client/webwizard_render_test.go` 末尾：

```go
func TestWebWizardTemplate_ContainsInstanceEndpointAndReconnect(t *testing.T) {
	got := webWizardHTML
	for _, s := range []string{
		"/api/wizard/instance",
		"startReconnectPolling",
	} {
		if !strings.Contains(got, s) {
			t.Errorf("template missing %q", s)
		}
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd client && go test -run TestWebWizardTemplate_ContainsInstanceEndpointAndReconnect -v`
Expected: FAIL

- [ ] **Step 3: 加 handler**

定位 `webwizard.go:1540` 前（`/api/wizard/update/status` 上面），插入：

```go
	if subPath == "/api/wizard/instance" && r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		info, _ := readInstanceInfo()
		if info == nil {
			info = &InstanceInfo{
				PID:     os.Getpid(),
				Port:    s.listenPort, // s 字段确认：见下方注释
				Version: Version,
			}
		}
		_ = json.NewEncoder(w).Encode(info)
		return
	}
```

⚠ `s.listenPort` 字段名以实际 struct 为准；若不存在，用 `info` 仅读 instance.json，缺则返回 503 `{"error":"no instance"}`。先 `grep -n "type.*wizardServer\|listenPort" webwizard.go` 确认字段后修正。

- [ ] **Step 4: 编译通过**

Run: `cd client && go build ./...`
Expected: success

- [ ] **Step 5: Commit**

```bash
cd client
git add webwizard.go
git commit -m "feat(client): /api/wizard/instance 暴露当前实例端口"
```

---

## Task 8: 前端 applyUpdate 切重启中 + 自动重连

**Files:**
- Modify: `client/webwizard.go`

- [ ] **Step 1: 替换 applyUpdate JS**

定位 `client/webwizard.go:1102-1127`，整段替换：

```javascript
function applyUpdate() {
  if (!confirm('确认立即更新到 v' + updateState.latest + '？应用会自动重启。')) return;
  var btn = document.getElementById('updateBtn');
  updateState.busy = true;
  btn.disabled = true;
  btn.textContent = '更新中…';
  setUpdateMsg('正在下载新版本…');
  fetch(basePath + '/api/wizard/update/apply', {method:'POST', headers: wizardHeaders()})
    .then(function(r){ return r.json().then(function(d){ return {ok:r.ok, data:d}; }); })
    .then(function(res){
      if (!res.ok) {
        setUpdateMsg('✗ ' + (res.data && res.data.error || 'HTTP 错误'), '#f87171');
        updateState.busy = false;
        btn.disabled = false;
        btn.textContent = '立即更新';
        return;
      }
      setUpdateMsg('已派发更新，等待新版本启动…', '#34d399');
      btn.textContent = '重启中…';
      startReconnectPolling();
    })
    .catch(function(e){
      setUpdateMsg('✗ ' + e.message, '#f87171');
      updateState.busy = false;
      btn.disabled = false;
      btn.textContent = '立即更新';
    });
}

function startReconnectPolling() {
  var elapsed = 0;
  var currentPort = location.port || '80';
  var t = setInterval(function() {
    elapsed += 1;
    // 优先探当前端口（多数情况下新进程端口不变），失败再读 instance.json 镜像
    fetch('/api/wizard/instance', {cache:'no-store'})
      .then(function(r){ return r.ok ? r.json() : null; })
      .then(function(info){
        if (!info || !info.port) return;
        if (String(info.port) !== currentPort) {
          clearInterval(t);
          location.href = location.protocol + '//127.0.0.1:' + info.port + '/wizard';
          return;
        }
        // 同端口但能响应 → 新进程已起来，刷一下
        clearInterval(t);
        location.reload();
      })
      .catch(function(){ /* 仍在重启窗口期 */ });
    if (elapsed >= 30) {
      clearInterval(t);
      setUpdateMsg('重启耗时较长，请手动关闭本窗口后重新打开 ai-monitor。', '#f87171');
    }
  }, 1000);
}
```

- [ ] **Step 2: 跑模板测试确认通过**

Run: `cd client && go test -run TestWebWizardTemplate_ContainsInstanceEndpointAndReconnect -v`
Expected: PASS

- [ ] **Step 3: 编译通过**

Run: `cd client && go build ./...`
Expected: success

- [ ] **Step 4: Commit**

```bash
cd client
git add webwizard.go
git commit -m "feat(client): 前端 apply 后切重启中状态 + 自动重连新端口"
```

---

## Task 9: --force-cleanup 子命令

**Files:**
- Create: `client/force_cleanup_windows.go`
- Create: `client/force_cleanup_other.go`
- Modify: `client/main.go`

- [ ] **Step 1: 写 Windows 实现**

```go
//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// doForceCleanup 强制清理本机所有 ai-monitor.exe 进程 + 删除单例标记。
// 不动 PAC / 环境变量 / 注册表（那是 --cleanup-network 的职责）。
func doForceCleanup() {
	fmt.Println("  ══════════════════════════════════════════")
	fmt.Println("  强制清理 ai-monitor 残留进程与锁文件")
	fmt.Println("  ══════════════════════════════════════════")

	myPID := os.Getpid()
	pids := listAIMonitorPIDs()
	killed := 0
	for _, pid := range pids {
		if pid == myPID {
			continue
		}
		cmd := exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/T", "/F")
		if err := cmd.Run(); err != nil {
			fmt.Printf("    ⚠ taskkill PID %d 失败: %v\n", pid, err)
			continue
		}
		fmt.Printf("    ✓ 已杀进程 PID %d\n", pid)
		killed++
	}
	if killed == 0 {
		fmt.Println("    — 未发现其他 ai-monitor 进程")
	}

	for _, p := range []string{instanceInfoPath(), singletonLockPath()} {
		if err := os.Remove(p); err == nil {
			fmt.Printf("    ✓ 已删除 %s\n", p)
		} else if !os.IsNotExist(err) {
			fmt.Printf("    ⚠ 删除 %s 失败: %v\n", p, err)
		}
	}

	fmt.Println()
	fmt.Println("  ✓ 清理完成，可重新启动 ai-monitor.exe")
}

func listAIMonitorPIDs() []int {
	out, err := exec.Command("tasklist", "/FI", "IMAGENAME eq ai-monitor.exe", "/FO", "CSV", "/NH").Output()
	if err != nil {
		return nil
	}
	var pids []int
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "INFO:") {
			continue
		}
		// CSV: "ai-monitor.exe","1234","Console","1","12,345 K"
		parts := strings.Split(line, ",")
		if len(parts) < 2 {
			continue
		}
		pidStr := strings.Trim(parts[1], `" `)
		if pid, err := strconv.Atoi(pidStr); err == nil {
			pids = append(pids, pid)
		}
	}
	return pids
}
```

- [ ] **Step 2: 写非 Windows 桩**

`client/force_cleanup_other.go`:

```go
//go:build !windows

package main

import "fmt"

func doForceCleanup() {
	fmt.Println("--force-cleanup 仅 Windows 实现")
}
```

- [ ] **Step 3: 在 main.go 加 flag + 分支**

定位 `client/main.go:68` 附近，与其他 flag 并列加：

```go
	forceCleanup := flag.Bool("force-cleanup", false, "强制清理: 杀掉所有 ai-monitor.exe + 删除 instance.json/instance.lock，用于多实例残留")
```

在 `flag.Parse()` 后，紧随 `if *postUpdate != "" {...}` 之后插入：

```go
	if *forceCleanup {
		doForceCleanup()
		return
	}
```

同时把 `forceCleanup` 加入 `defaultRunMode` 计算（取反 AND）：

```go
	defaultRunMode := !*install &&
		!*globalInstall &&
		!*globalUninstall &&
		!*launch &&
		!*listLaunchPresets &&
		strings.TrimSpace(*launchPreset) == "" &&
		!*uninstall &&
		!*setup &&
		!*heal &&
		!*cleanupNetwork &&
		!*forceCleanup
```

- [ ] **Step 4: 编译通过**

Run: `cd client && go build ./...`
Expected: success

- [ ] **Step 5: 手动验收**

Windows 上：
1. 启动 ai-monitor.exe，看到「正在监听」横幅。
2. 另开 PowerShell：`taskkill /IM ai-monitor.exe /F`（模拟卡死残留）。
3. 跑 `.\ai-monitor.exe --force-cleanup`。
4. 验证：输出「已杀进程」/「已删除 instance.lock」，无报错。

- [ ] **Step 6: Commit**

```bash
cd client
git add force_cleanup_windows.go force_cleanup_other.go main.go
git commit -m "feat(client): --force-cleanup 强制清理残留实例"
```

---

## Task 10: 端到端手动验收 + 文档更新

**Files:**
- Modify: `client/main.go`（启动 banner 加一行提示）

- [ ] **Step 1: banner 加提示**

定位 `client/main.go:335-336` 附近的 `fmt.Println("  等待 AI 请求中... (Ctrl+C 退出)")`，前面插入：

```go
	fmt.Println("  卡住/多实例时可执行: ai-monitor.exe --force-cleanup")
```

- [ ] **Step 2: 跑全量测试**

Run: `cd client && go test ./...`
Expected: 全 PASS

Run (Windows): `cd client && GOOS=windows go vet ./...`
Expected: clean

- [ ] **Step 3: 端到端验收**

在 Windows 环境：

1. **正常升级路径：**
   - 构造一个版本号比本地高的 release，启动旧 exe。
   - 在向导页点「立即更新」。
   - 观察：旧进程 5-10s 内退出 → bat 替换 exe → 新进程起来 → 前端页面自动跳转到新端口（或刷新）。
   - 任务管理器中**只有一个** ai-monitor.exe。

2. **并发启动单例：**
   - 同时双击 exe 两次。
   - 期望：只有一个进程拿到锁；另一个打开向导窗口指向已运行端口。

3. **--force-cleanup 兜底：**
   - 故意 `taskkill /IM ai-monitor.exe /F` 制造残留 instance.lock。
   - 跑 `--force-cleanup`，验证清空后能正常重启。

4. **优雅关闭 PAC 还原：**
   - 启用 session-pac 模式，更新一次。
   - 验证：升级过程中系统代理临时还原（无残留 PAC URL），新进程起来后重新写入。

- [ ] **Step 4: Commit**

```bash
cd client
git add main.go
git commit -m "docs(client): banner 提示 --force-cleanup 兜底命令"
```

---

## Self-Review Notes

- **Spec coverage：** 5 个模块（A 优雅退出 / A updater bat / C 单例锁 / B 前端重连 / 兜底 --force-cleanup）全部覆盖 Task 1-9，Task 10 验收。
- **Type 一致性：** `acquireSingletonLock` / `waitForSingletonLock` / `releaseSingletonLock` / `closeActiveWizardWindow` / `RequestShutdown` / `doForceCleanup` 在所有 Task 中签名一致。
- **关键风险：** Task 7 的 `s.listenPort` 字段名需在 Step 3 之前由 grep 确认（已在 step 注释中标注）；如字段不同，按 grep 结果替换。
