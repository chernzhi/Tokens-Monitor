# P1 配置单一化 + 独立 Watchdog 实施计划

> **For Hermes:** 直接按 Task 顺序执行，每个 Task 都是一个独立的代码修改。

**目标：** 消除双配置源 + 创建独立 watchdog 进程，提升可靠性和可维护性。

**技术栈：** Go 1.26, Windows API, `schtasks.exe`

---

## 第 1 部分：配置单一化

**当前问题：**
- `client/config.json`（项目目录）和 `%APPDATA%/ai-monitor/config.json` 内容不同步
- 安装流程写 `identity.json` 但不写完整 config
- 没有 `SaveConfig` 函数，只有 `patchConfigUpstreamProxy` 一个字段的写入

**目标架构：**
```
client/config.example.json  ← 模板（带注释），不参与运行
%APPDATA%/ai-monitor/config.json  ← 唯一运行时配置源
```

---

### Task 1: 添加 SaveConfig 函数（补全配置写入能力）

**目标：** 让代码有能力将完整 Config 写回文件，为后续同步做准备。

**文件：** `client/config.go`

**代码：** 在文件末尾（`}` 闭合前）添加：

```go
// SaveConfig writes the full configuration back to a JSON file with
// indentation, preserving all fields including those not set by the user.
func SaveConfig(cfg *Config, path string) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	return os.WriteFile(path, data, 0600)
}
```

需要在文件头部添加 import：`"encoding/json"` 和 `"path/filepath"` 和 `"os"`（检查是否已存在）。

**验证：** `cd client && go build .`

---

### Task 2: 安装流程同步 config 到 APPDATA

**目标：** `--install` / `--global-install` / `--setup` 完成时，把完整 config 写入 APPDATA。

**修改文件：** `client/main.go`

**修改点 A：** 在 `doInstall()` 函数末尾（`✓ 安装完成!` 之前），添加：

```go
// 确保 APPDATA 中的 config.json 与当前配置完全同步
roamingConfig := filepath.Join(appDataDir(), "config.json")
if err := SaveConfig(cfg, roamingConfig); err != nil {
    log.Printf("    ⚠ 同步配置到 %s 失败: %v", roamingConfig, err)
} else {
    fmt.Printf("    ✓ 配置已同步到: %s\n", roamingConfig)
}
```

**修改点 B：** 在 `doGlobalInstall()` 函数末尾，添加相同的同步代码。

**修改点 C：** 在 `runSetupWizard()` 完成后（`client/wizard.go` 中），检查是否有保存 config 的逻辑，如果有缺失则补充。

先检查 wizard.go 的 config 保存逻辑：
```bash
grep -n "SaveConfig\|WriteFile\|config.json" client/wizard.go
```

**验证：** 执行 `--install` 后，确认 `%APPDATA%/ai-monitor/config.json` 包含完整字段。

---

### Task 3: 项目目录 config.json → config.example.json

**目标：** 把项目目录的 `config.json` 改为模板文件，避免混淆。

**步骤：**

```bash
cd client/
cp config.json config.example.json
# 在 config.example.json 中添加注释说明
git rm --cached config.json  # 保留本地文件但从 git 跟踪中移除
```

**编辑 `config.example.json`**，改成带说明的模板：

```json
{
  "server_url": "https://otw.tech:59889",
  "user_name": "姓名",
  "user_id": "your-id@company.com",
  "department": "部门",
  "port": 18090,
  "upstream_proxy": "http://127.0.0.1:8089",
  "_comment_upstream": "上游代理地址，如 sing-box/Clash 的 HTTP 端口",
  "install_system_proxy": true,
  "install_ide_proxy": false,
  "auth_token": ""
}
```

> **注意：** JSON 不支持注释，`_comment_*` 字段会被忽略（因为 Config struct 没有对应 tag）。

**修改 `build.bat`**：确保打包时包含 `config.example.json` 而非 `config.json`。

**修改 `.gitignore`**：添加 `client/config.json` 防止再次提交。

**验证：** `git status` 确认 `config.json` 被忽略，`config.example.json` 存在。

---

### Task 4: 启动时检测配置完整性和自动修复

**目标：** ai-monitor 正常运行时，如果检测到关键字段缺失，自动尝试从 identity.json / install_state 恢复，并写回 config。

**文件：** `client/config.go`（或新建 `client/config_heal.go`）

**新增函数 `ValidateAndHealConfig`：**

```go
// ValidateAndHealConfig checks for missing critical fields and attempts
// to restore them from identity.json or install_state.
// Returns a list of warnings for the user.
func ValidateAndHealConfig(cfg *Config, configPath string) []string {
    var warnings []string
    
    if strings.TrimSpace(cfg.UpstreamProxy) == "" {
        // Try to recover from install_state
        if st := loadInstallState(); st != nil && st.PreviousUpstreamProxy != "" {
            cfg.UpstreamProxy = st.PreviousUpstreamProxy
            SaveConfig(cfg, configPath)
            warnings = append(warnings, 
                "upstream_proxy 已从安装记录恢复，已同步到配置文件")
        } else {
            warnings = append(warnings,
                "⚠ upstream_proxy 未设置，国际 AI API（Copilot/OpenAI 等）可能无法访问。"+
                "请用 ai-monitor.exe --setup 配置，或手动编辑 config.json 添加 upstream_proxy 字段。")
        }
    }
    
    return warnings
}
```

**修改 `main.go`**：在 `LoadConfig` 之后、`startMonitorRuntime` 之前调用：

```go
cfg, err := LoadConfig(*configPath)
// ...existing error handling...

// 自动检测并修复配置
healWarnings := ValidateAndHealConfig(cfg, *configPath)
for _, w := range healWarnings {
    fmt.Printf("  %s\n", w)
}
```

**验证：** 启动 ai-monitor，确认日志中显示配置检测结果。

---

## 第 2 部分：独立 Watchdog 进程

**当前问题：**
- Watchdog 在主进程内运行，进程崩溃后无法自愈
- 仅 Windows 支持
- 硬编码 30s / 3 次失败

**目标架构：**
```
watchdog.exe（独立进程，由计划任务触发）
  → GET http://127.0.0.1:18090/status
  → 连续失败 N 次 → 执行 ai-monitor.exe --heal
  → 可选：自动重启 ai-monitor.exe
```

---

### Task 5: 创建 watchdog 入口

**目标：** 创建极简的独立 watchdog 程序。

**文件：** `client/cmd/watchdog/main.go`

```go
package main

import (
    "fmt"
    "net/http"
    "os"
    "os/exec"
    "path/filepath"
    "time"
)

func main() {
    port := "18090"
    if len(os.Args) > 1 {
        port = os.Args[1]
    }
    statusURL := fmt.Sprintf("http://127.0.0.1:%s/status", port)
    
    // Load state file to track consecutive failures
    stateFile := filepath.Join(appDataDir(), "watchdog_state.json")
    failures := readFailureCount(stateFile)
    
    client := &http.Client{Timeout: 5 * time.Second}
    resp, err := client.Get(statusURL)
    if err == nil && resp.StatusCode == 200 {
        resp.Body.Close()
        // ai-monitor is healthy, reset failures
        if failures > 0 {
            writeFailureCount(stateFile, 0)
        }
        os.Exit(0)
    }
    if resp != nil {
        resp.Body.Close()
    }
    
    failures++
    writeFailureCount(stateFile, failures)
    
    if failures >= 3 {
        // Run heal
        exePath := findAIMonitor()
        if exePath != "" {
            cmd := exec.Command(exePath, "--heal")
            cmd.Run()
        }
        writeFailureCount(stateFile, 0)
        os.Exit(0)
    }
    
    fmt.Printf("ai-monitor not responding (%d/3 failures)\n", failures)
    os.Exit(1)
}

func appDataDir() string {
    return filepath.Join(os.Getenv("APPDATA"), "ai-monitor")
}

type watchdogState struct {
    Failures int `json:"failures"`
}

func readFailureCount(path string) int {
    data, err := os.ReadFile(path)
    if err != nil {
        return 0
    }
    var s watchdogState
    if err := json.Unmarshal(data, &s); err != nil {
        return 0
    }
    return s.Failures
}

func writeFailureCount(path string, count int) {
    data, _ := json.Marshal(watchdogState{Failures: count})
    os.WriteFile(path, data, 0600)
}

func findAIMonitor() string {
    // 优先检查 APPDATA 下的路径
    // 其次检查程序自身同目录
    // 最后搜索 PATH
    candidates := []string{
        filepath.Join(appDataDir(), "ai-monitor.exe"),
        filepath.Join(filepath.Dir(os.Args[0]), "ai-monitor.exe"),
    }
    for _, p := range candidates {
        if _, err := os.Stat(p); err == nil {
            return p
        }
    }
    return "ai-monitor.exe" // fallback to PATH
}
```

需要在头部 import `"encoding/json"`。

**验证：** `cd client/cmd/watchdog && go build -o watchdog.exe .`

---

### Task 6: 编译 watchdog + 集成到构建流程

**目标：** 在 `build.bat` 中加入 watchdog 编译。

**文件：** `client/build.bat`

在第 18 行（`go build ... ai-monitor.exe .`）之后添加：

```batch
:: Build watchdog
echo  Building Watchdog...
go build -ldflags="%LDFLAGS%" -o watchdog.exe ./cmd/watchdog/
if %ERRORLEVEL% NEQ 0 (
    echo  Watchdog build FAILED!
    pause
    exit /b 1
)
echo  Watchdog build SUCCESS: watchdog.exe
```

**验证：** 执行 `build.bat`，确认同时产出 `ai-monitor.exe` 和 `watchdog.exe`。

---

### Task 7: 安装/卸载时注册 Windows 计划任务

**目标：** `--global-install` 时注册计划任务（每 30s 运行 watchdog），`--global-uninstall` 时移除。

**新增文件：** `client/watchdog_schtasks_windows.go`

```go
//go:build windows

package main

import (
    "fmt"
    "os/exec"
    "path/filepath"
)

func registerWatchdogTask(watchdogPath, aiMonitorPath string) error {
    taskName := "AI Monitor Watchdog"
    cmd := exec.Command("schtasks", "/Create",
        "/TN", taskName,
        "/TR", fmt.Sprintf(`"%s" 18090`, watchdogPath),
        "/SC", "MINUTE",
        "/MO", "1",                   // 每分钟触发一次
        "/F",                          // 强制覆盖
        "/RL", "LIMITED",             // 不需要管理员权限
    )
    if out, err := cmd.CombinedOutput(); err != nil {
        return fmt.Errorf("schtasks create: %w\n%s", err, out)
    }
    return nil
}

func unregisterWatchdogTask() error {
    taskName := "AI Monitor Watchdog"
    cmd := exec.Command("schtasks", "/Delete",
        "/TN", taskName,
        "/F",
    )
    if out, err := cmd.CombinedOutput(); err != nil {
        return fmt.Errorf("schtasks delete: %w\n%s", err, out)
    }
    return nil
}
```

**文件：** `client/watchdog_schtasks_other.go`

```go
//go:build !windows

package main

func registerWatchdogTask(_, _ string) error { return nil }
func unregisterWatchdogTask() error           { return nil }
```

**修改 `client/main.go`**：在 `doGlobalInstall()` 末尾添加注册，在 `doGlobalUninstall()` 中添加注销：

```go
// 在 doGlobalInstall 末尾添加：
watchdogPath := filepath.Join(filepath.Dir(os.Args[0]), "watchdog.exe")
if _, err := os.Stat(watchdogPath); err == nil {
    if err := registerWatchdogTask(watchdogPath, os.Args[0]); err != nil {
        log.Printf("    ⚠ 注册 watchdog 计划任务失败: %v", err)
    } else {
        fmt.Println("    ✓ watchdog 计划任务已注册（每分钟检测 ai-monitor 健康状态）")
    }
} else {
    fmt.Println("    ⚠ 未找到 watchdog.exe，跳过计划任务注册")
}
```

```go
// 在 doGlobalUninstall 开头添加：
if err := unregisterWatchdogTask(); err != nil {
    log.Printf("    ⚠ 移除 watchdog 计划任务失败: %v", err)
} else {
    fmt.Println("    ✓ watchdog 计划任务已移除")
}
```

需要添加 import `"os"` 和 `"path/filepath"`（如果缺失）。

**验证：** 执行 `--global-install` 后，检查 `taskschd.msc` 看到 "AI Monitor Watchdog" 任务；执行 `--global-uninstall` 后任务消失。

---

### Task 8: 全量编译 + 集成测试

**步骤：**

```bash
cd client/
# 编译 ai-monitor
go build -ldflags="-s -w -X main.Version=$(cat VERSION)" -o ai-monitor.exe .
# 编译 watchdog  
go build -ldflags="-s -w -X main.Version=$(cat VERSION)" -o watchdog.exe ./cmd/watchdog/
# 运行全部测试
go test ./... -v -timeout 60s
```

**手动验证清单：**
- [ ] `ai-monitor.exe --global-install` → 计划任务已创建
- [ ] `%APPDATA%/ai-monitor/config.json` 包含 `upstream_proxy`
- [ ] `watchdog.exe` 单独运行 → 返回 0（ai-monitor 健康时）
- [ ] 停掉 ai-monitor → 3 次 watchdog 运行后自动执行 `--heal`
- [ ] `ai-monitor.exe --global-uninstall` → 计划任务已删除

---

## 总结

| Task | 内容 | 新文件 | 修改文件 |
|------|------|--------|---------|
| 1 | SaveConfig 函数 | - | config.go |
| 2 | 安装流同步配置 | - | main.go, wizard.go |
| 3 | config.json → config.example.json | config.example.json, .gitignore | build.bat |
| 4 | 启动时配置自检修复 | - | config.go, main.go |
| 5 | watchdog 入口 | cmd/watchdog/main.go | - |
| 6 | 构建流程集成 | - | build.bat |
| 7 | Windows 计划任务注册 | watchdog_schtasks_windows.go, watchdog_schtasks_other.go | main.go |
| 8 | 编译测试 | - | - |
