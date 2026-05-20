# 客户端更新流程加固 (A+B+C)

## 背景与症状

- 用户点「立即更新」后，向导页变白屏 / 转圈不消失。
- 同时系统里残留多个 `ai-monitor.exe` 进程。
- 旧 WebView2 窗口卡在空白状态（截图佐证：标题 `find ""` 的孤儿窗口）。

## 根因

1. `updater_apply_windows.go:90-93` 用 `os.Exit(0)` 硬退出整个进程，HTTP server、WebView2 宿主、PAC 还原全部跳过。前端 `setInterval` 全部 404，UI 永远停在「更新中…」。
2. `singleton.go` 的单例检测是 read-then-write，**没有文件锁**。更新瞬间 + 自启 + 用户双击三方并发可同时通过。
3. `updater.bat` 不 `taskkill` 旧 PID，仅靠 `os.Exit` 自杀；旧进程死之前新 exe 已被 `start ""` 拉起 → 多实例。
4. WebView2 user-data 锁未释放，下一个进程的内嵌窗口可能初始化失败。

## 目标

更新流程从「硬退出 + 重启」改成「优雅交接」：
- 旧进程：触发统一 shutdown 路径（关 HTTP、关窗口、还原 PAC、释放锁）。
- updater.bat：等旧 PID 真正退出 → 替换 exe → 启新进程。
- 新进程：抢真单例锁；若 `--post-update` 模式则容忍短暂等待。
- 前端：apply ACK 后切「重启中」状态，自动探测新端口并跳转。
- 额外提供 `--force-cleanup`，一键清理任何残留多实例 / 锁文件 / instance.json。

## 非目标

- 不重写更新协议、不改下载源、不动签名验证。
- 不动 Linux/macOS 路径（updater 已仅 Windows）。
- 不解决「升级失败回滚」体验（已有 backup，沿用现状）。

## 设计

### 模块 1：优雅退出钩子（A）

**新增** `client/shutdown_windows.go`：

```go
var shutdownOnce sync.Once
var shutdownCh = make(chan struct{}, 1)

// RequestShutdown 由 updater / signal handler / WebView2 关闭事件共同调用。
// 多次调用安全。返回不阻塞；真正的 shutdown 在 main goroutine 中执行。
func RequestShutdown(reason string) {
    shutdownOnce.Do(func() {
        log.Printf("[shutdown] requested: %s", reason)
        close(shutdownCh)
    })
}
```

**改 `main.go`**：
- 主循环新增 select：原 SIGINT goroutine + `<-shutdownCh` 走同一段 cleanup 代码。
- 把现有 `<-sigCh` 后的 cleanup 抽成 `gracefulShutdown(rt *MonitorRuntime)`：
  - `CloseWizardWindow()`（新暴露）
  - `runtime.Shutdown(ctx)`（8s timeout，沿用）
  - `removeInstanceInfo()`
  - `restoreSessionManagedProxyOnShutdown()`
  - `releaseSingletonLock()`
- 不调 `os.Exit` 让 `main` 自然返回。

**改 `wizard_window_windows.go`**：
- 暴露 `CloseWizardWindow()`：若 WebView2 窗口存在则 PostQuitMessage / `window.Close()`，幂等。

### 模块 2：updater 接力改造（A）

**改 `updater_apply_windows.go`**：
- 删 `os.Exit(0)`，改为 `RequestShutdown("update-apply")`。
- bat 启动后 sleep 300ms 让 cmd.Start 落盘，然后 return（不再阻塞 caller）。

**改 bat 模板**（同文件中的生成器）：
```bat
@echo off
setlocal
set OLDPID=%~1
set NEWEXE=%~2
set TARGET=%~3
set BACKUP=%~4

REM 1) 等待旧进程退出，最多 10s
for /l %%i in (1,1,20) do (
    tasklist /FI "PID eq %OLDPID%" | findstr /C:"%OLDPID%" >nul
    if errorlevel 1 goto :killed
    timeout /t 1 /nobreak >nul
)
REM 还没退？强杀（保险）
taskkill /PID %OLDPID% /T /F >nul 2>&1
timeout /t 1 /nobreak >nul

:killed
REM 2) 替换 exe（重试 5 次，每次间隔 1s）
for /l %%i in (1,1,5) do (
    move /Y "%NEWEXE%" "%TARGET%" >nul 2>&1
    if not errorlevel 1 goto :moved
    timeout /t 1 /nobreak >nul
)
echo move failed >&2
exit /b 1

:moved
REM 3) 启动新进程（带 --post-update 用于清理 backup）
start "" "%TARGET%" --post-update "%BACKUP%"
endlocal
exit /b 0
```

生成器签名加上 `oldPID int`；测试断言含 `tasklist /FI "PID eq` 与 `move /Y`。

### 模块 3：真单例锁（C）

**新增** `client/singleton_lock_windows.go`：

```go
// acquireSingletonLock 用 LockFileEx 独占锁定 %APPDATA%/ai-monitor/instance.lock。
// 成功返回 release 函数；失败返回 false（说明有其他实例持锁）。
// 进程崩溃 / 被 kill 时 OS 自动释放，不会留死锁。
func acquireSingletonLock() (release func(), ok bool, err error)

// waitForSingletonLock 给 --post-update 用：最多等 timeout 秒抢锁，
// 旧进程优雅退出窗口期内自动接力。
func waitForSingletonLock(timeout time.Duration) (release func(), err error)
```

实现要点：
- 用 `golang.org/x/sys/windows`（项目已有依赖）的 `LockFileEx`。
- 锁文件路径独立于 `instance.json`，避免和读 instance 元数据互相干扰。
- flag = `LOCKFILE_EXCLUSIVE_LOCK | LOCKFILE_FAIL_IMMEDIATELY`（非 post-update）。
- post-update 模式用阻塞重试（每 200ms 尝试一次，最多 10s）。

**改 `main.go:230-256`** 单例分支：
- 先 `acquireSingletonLock()`：
  - 拿到锁 → 走「我是新实例」路径。
  - 拿不到锁 → 走现有 `checkExistingInstance` 流程（拉起向导窗口指向已存在端口）。
- `--post-update` 模式调 `waitForSingletonLock(10s)`，超时再退到现有 alive-check 分支。

**`singleton.go` 保留不动**：HTTP 探活仍用来「找到那个端口」，但「能否启动」交给锁。

### 模块 4：前端等待重启 UI（B）

**改 `webwizard.go`** 模板：

JS 新增状态机：
```js
let updatePhase = 'idle'; // idle | downloading | restarting

async function applyUpdate() {
    const r = await fetch('/api/wizard/update/apply', {method: 'POST'});
    if (!r.ok) { showError(...); return; }
    updatePhase = 'restarting';
    setBanner('正在重启监控服务，请稍候…');
    stopStatusPolling();
    startReconnectPolling();
}

function startReconnectPolling() {
    const candidates = [location.port, '{{.CfgPort}}']; // 实测端口 + 配置端口
    let elapsed = 0;
    const t = setInterval(async () => {
        elapsed += 1;
        for (const p of candidates) {
            try {
                const r = await fetch(`http://127.0.0.1:${p}/api/wizard/instance`, {cache:'no-store'});
                if (r.ok) {
                    const j = await r.json();
                    clearInterval(t);
                    location.href = `http://127.0.0.1:${j.port}/wizard`;
                    return;
                }
            } catch(_) {}
        }
        if (elapsed >= 30) {
            clearInterval(t);
            setBanner('重启耗时较长，请手动关闭本窗口后重新打开 ai-monitor。');
        }
    }, 1000);
}
```

**后端**新增 `GET /api/wizard/instance`：返回 `{port, pid, version}`。这是现有 `instance.json` 的 HTTP 镜像；前端不需要直接读文件。

### 模块 5：兜底清理子命令

**改 `main.go`** flag 段，新增：
```go
forceCleanup := flag.Bool("force-cleanup", false, "强制清理: 杀掉所有 ai-monitor.exe + 删除 instance.json/instance.lock，用于多实例残留")
```

实现 `doForceCleanup()`：
- `tasklist` 枚举本机所有 `ai-monitor.exe` PID，逐个 `taskkill /PID <pid> /T /F`（跳过自己）。
- 删除 `instance.json`、`instance.lock`。
- 不动 PAC / 环境变量 / 注册表（那是 `--cleanup-network` 的职责）。
- 退出码 0。

文档：在 `用户手册.md` / 启动 banner 上加一行「卡住时执行 `ai-monitor.exe --force-cleanup`」。

## 测试

- **新增** `singleton_lock_test.go`：两个 goroutine 同时调 `acquireSingletonLock`，断言一个成功一个失败；释放后另一个能抢到。
- **新增** `updater_bat_template_test.go`：渲染 bat 模板，断言含 `tasklist /FI`、`taskkill /PID %OLDPID% /T /F`、`move /Y`、`start "" "%TARGET%" --post-update`。
- **改** `updater_test.go`：把 `os.Exit` 相关断言换成「调用了 RequestShutdown」（注入 mock）。
- **改** `webwizard_render_test.go`：断言模板含 `startReconnectPolling` 与 `/api/wizard/instance`。
- **手动验收**：
  1. 构造 fake 升级包，点「立即更新」。
  2. 观察：旧进程退出 → 旧窗口关闭 → 新进程起来 → 前端自动跳转 → 仅一个 ai-monitor 进程。
  3. 反例：手动 `taskkill /F` 制造残留，运行 `--force-cleanup`，验证清空。

## 兼容性 / 回滚

- 所有新增 flag 默认关闭；老 config 无需迁移。
- 锁文件路径新增，旧版本不读不写，前向兼容。
- `instance.json` HTTP 镜像 endpoint 为新增，不替代任何旧 API。
- 若新 updater 在某客户上回滚，单跑 `--force-cleanup` 可清场，再用旧 exe 继续。

## 风险

| 风险 | 缓解 |
|---|---|
| LockFileEx 在某些受控环境（杀软）下失败 | 失败时退化为现有 read-then-write 行为，加 log 但不中断启动 |
| WebView2 关闭时机和 HTTP shutdown 竞争 | 严格顺序：先关窗口（不阻塞），再 server.Shutdown，再释放锁 |
| bat 在某些 Win10 LTSC 上 `tasklist /FI` 输出差异 | 用 `findstr /C:"%OLDPID%"` 而非依赖 errorlevel 文案 |
| 用户点更新后立刻关窗口 | RequestShutdown 已被触发，正常走完即可 |

## 提交边界

按上述 5 个模块各自一条 commit，便于回滚。建议顺序：
1. 单例锁（独立可测）
2. 优雅退出钩子
3. updater bat 改造
4. force-cleanup
5. 前端 UI
