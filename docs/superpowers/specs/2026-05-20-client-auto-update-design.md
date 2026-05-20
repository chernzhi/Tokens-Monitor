# 客户端自动更新 — 设计文档

- 创建日期：2026-05-20
- 作者：chenzhi
- 状态：草案
- 涉及分支：`feature/client-auto-update`
- 起始基线版本：v3.2.9

---

## 1. 目标与非目标

### 目标
- 客户端能在后台静默检查 135 服务器是否有新版本，发现后通知用户。
- 用户在 webwizard 控制台点「立即更新」即可完成下载 → 校验 → 替换 exe → 重启的全流程，全程无需手动操作。
- 失败要么静默重试，要么明确给出可操作信息，不能让用户卡死或丢配置。

### 非目标
- 不做增量补丁 / bsdiff
- 不做 beta / stable 多通道
- 不做灰度发布百分比
- 不做后台上传 UI（运维通过 scp 把新版本放到服务器目录即可）
- 不替换 `.bat` / sing-box / config.json —— 只换 `ai-monitor.exe`

---

## 2. 更新粒度

**只替换 `ai-monitor.exe`**。理由：v3.2.4 → v3.2.9 全部是 Go 代码改动，分发版里的 `.bat` 与 sing-box 二进制没动过；exe 体积约 18M。

未来如果需要换整包（zip），manifest 里加 `"full_package": true` 字段，由客户端区分走另一条路径。本期只做 exe 单文件替换；`full_package` 字段保留但不实现。

---

## 3. 服务端

### 3.1 发布约定

运维把新版本 exe 与校验文件放到 135 服务器：

```
$EXTENSION_DIR/client/
├── ai-monitor-3.2.9.exe
├── ai-monitor-3.2.9.exe.sha256       # 一行 hex
├── ai-monitor-3.2.9.md               # release notes (可选)
├── ai-monitor-3.3.0.exe
├── ai-monitor-3.3.0.exe.sha256
└── ai-monitor-3.3.0.md
```

`EXTENSION_DIR` 已存在于 `backend/app/config.py`，本设计复用。子目录 `client/` 用于区分 VSIX (`extension/`) 与原始 exe。

文件名 semver 解析按 `ai-monitor-(\d+)\.(\d+)\.(\d+)\.exe` 匹配；其他文件忽略。

### 3.2 端点 — 新增 `backend/app/routers/release.py`

```
GET /api/release/client/latest?current=3.2.9&platform=win32-x64
```

响应：
```json
{
  "latest_version": "3.3.0",
  "current_version": "3.2.9",
  "has_update": true,
  "download_url": "/api/release/client/download/ai-monitor-3.3.0.exe",
  "sha256": "abc123...",
  "size_bytes": 18874368,
  "release_notes": "...",
  "mandatory": false,
  "published_at": "2026-05-20T10:00:00Z"
}
```

- `mandatory` 字段保留接口，本期始终返回 `false`。未来需要时改成读 manifest 配置。
- `platform` 字段保留扩展位（目前仅 `win32-x64`），传别的就返回 404。
- `has_update`：服务端按 semver 比较 `current` 与 `latest_version`。

```
GET /api/release/client/download/{filename}
```

- 校验文件名必须匹配 `ai-monitor-X.Y.Z.exe` 正则，防止路径穿越。
- 返回 `FileResponse`，`Content-Type: application/octet-stream`，带 `ETag` (sha256)。

### 3.3 不做的事
- 不做 `/api/release/client/upload` —— 运维 scp 上传。
- 不做后台管理 UI。
- 不在数据库存版本表 —— 完全文件系统驱动，简单且与 `extension.py` 一致。

### 3.4 测试

新增 `backend/tests/test_release_client.py`：
- `_scan_latest_client` 在空目录 / 多版本 / 半截文件三种情况下的行为。
- `/latest` 命中 / 未命中 / 客户端版本已最新。
- `/download/{filename}` 合法 / 路径穿越 / 文件不存在。

---

## 4. 客户端

### 4.1 新增文件 `client/updater.go`

主要导出：
- `type Updater struct{ ... }`
- `func NewUpdater(cfg *Config) *Updater`
- `func (u *Updater) Start(ctx context.Context)` — 后台轮询循环
- `func (u *Updater) CheckNow() (*ReleaseInfo, error)` — webwizard 用
- `func (u *Updater) ApplyUpdate(info *ReleaseInfo) error` — 用户触发安装

### 4.2 轮询时机

- 启动后 10 秒做第一次检查（避开冷启动峰值）。
- 之后每 **1 小时** 轮询一次。
- webwizard 「立即检查」按钮可触发即时检查（不影响下次定时）。

### 4.3 版本比较

用 `golang.org/x/mod/semver` 比较（已在 go.mod 间接依赖；如无直接添加）。
比较两端先去 `v` 前缀并规范化（`3.2.9` → `v3.2.9`）。

### 4.4 行为矩阵

| 场景 | 用户感知 |
|------|----------|
| 无新版 | 静默；webwizard 显示「已是最新 v3.2.9」 |
| 有新版，非强制 | webwizard 头部黄色横幅 + 「关于」卡片显示「新版本 v3.3.0 可用 · [立即更新] [稍后]」；日志一行 `[updater] 检测到新版本 v3.3.0` |
| 有新版，强制 | 同上 + 横幅红色「该版本必须更新」+ 60 秒倒计时；倒计时归零自动 `ApplyUpdate` |
| 下载/校验失败 | 日志一行 `[updater] 更新失败: <err>`；webwizard 横幅变红显示原因；下一轮重试 |
| 替换失败 | 备份还原；日志 `[updater] 替换失败已回滚` |

### 4.5 下载流程

1. 创建临时目录 `%TEMP%\ai-monitor-update\`（不存在则创建）。
2. 流式下载到 `ai-monitor-X.Y.Z.exe.part`，超时 5 分钟。
3. 边写边算 SHA256。
4. 完成后比对 manifest 给的 sha256：
   - 一致 → 改名为 `ai-monitor-X.Y.Z.exe`
   - 不一致 → 删除 part 文件，返回错误
5. 大小校验：若与 manifest `size_bytes` 不符也失败（防截断）。

### 4.6 替换 + 重启（Windows 文件占用）

写一段 `updater.bat` 到 `%TEMP%\ai-monitor-update\`：

```bat
@echo off
setlocal
set TARGET=%~1
set NEW=%~2
set BACKUP=%~3
set LOG=%~4

REM 备份当前 exe
copy /Y "%TARGET%" "%BACKUP%" >>"%LOG%" 2>&1

REM 等待父进程退出（最多 30s）
set /a TRIES=0
:waitloop
tasklist /FI "PID eq %5" 2>nul | find "%5" >nul
if errorlevel 1 goto replace
set /a TRIES+=1
if %TRIES% GEQ 60 goto fail
ping -n 2 127.0.0.1 >nul
goto waitloop

:replace
move /Y "%NEW%" "%TARGET%" >>"%LOG%" 2>&1
if errorlevel 1 goto rollback

REM 拉起新进程
start "" "%TARGET%" --post-update "%BACKUP%"
exit /b 0

:rollback
copy /Y "%BACKUP%" "%TARGET%" >>"%LOG%" 2>&1
start "" "%TARGET%"
exit /b 1

:fail
echo updater: parent did not exit within 30s >>"%LOG%"
exit /b 2
```

调用方式：
```go
exec := newDetachedCmd("cmd", "/c", batPath,
    currentExe, newExe, backupPath, logPath, fmt.Sprint(os.Getpid()))
exec.Start()
os.Exit(0)
```

`newDetachedCmd` 已在 v3.2.9 引入，不会留 conhost。

### 4.7 启动钩子 `--post-update <backup>`

新进程启动后：
1. 写日志 `[updater] ✅ 已更新到 v3.3.0`。
2. 启动 30 秒后若进程仍存活（无 panic、无 `os.Exit(非 0)`），删除 `<backup>`。判定方式：post-update 启动时记下 PID，30 秒后由同一进程内的定时器执行删除。
3. webwizard 控制台收到 `LastUpdateApplied` 状态，展示成功提示。

如果新进程在 30 秒内 panic：
- 自动重启会被 schtasks `AIMonitorAutoStart` 拉起；
- 但因为 backup 还在，下次启动若仍崩，bat 已经退出，无法自动回滚 → **本期不做自动回滚**，只保留 backup 文件 24h，方便用户手动改名找回。
- 文档里写清楚：失败时手动操作步骤。

### 4.8 测试

新增 `client/updater_test.go`：
- semver 比较（含相同、新、旧、含/不含 v 前缀、非法值）。
- ReleaseInfo JSON 解析。
- SHA256 校验函数（含正确、不一致、空文件）。
- 模拟 HTTP 服务的端到端 check 流程（不走真正 exec）。
- updater.bat 生成内容快照测试。

---

## 5. webwizard UI

### 5.1 控制台头部
在 `client/webwizard.go` 渲染头部时增加一个状态条：

- 无新版：不显示。
- 有新版：黄色横幅「🆕 v3.3.0 可用 · [立即更新] [稍后]」；点「稍后」存到 session，1 小时内不再提示。
- 强制更新：红色横幅 + 倒计时。
- 下载中：进度条（百分比来自 updater 暴露的 atomic 字段）。
- 安装中：「正在重启，请稍候…」。

### 5.2 「关于」卡片（新增）
在控制台底部加一张小卡片：

```
关于
当前版本：v3.2.9
最新版本：v3.3.0  · 2026-05-20 发布
[ 检查更新 ]   [ 立即更新 ]

更新说明
- xxx
- yyy
```

### 5.3 后端 API
- `GET /api/wizard/update/status` → 当前 ReleaseInfo + 下载进度
- `POST /api/wizard/update/check` → 立即触发一次 check
- `POST /api/wizard/update/apply` → 触发 ApplyUpdate

走 webwizard 现有的 mux，加 3 个 handler。

---

## 6. 配置

`config.json` 新增字段（可选，缺省即默认值）：

```json
{
  "update_check_url": "",          // 留空则用 ServerURL
  "update_check_interval_seconds": 3600,
  "update_auto_apply": false        // 留 false；强制更新无视此开关
}
```

---

## 7. 安全考虑

- **HTTPS / 内网**：当前 ServerURL 多数是 `http://内网 IP`，依赖 sha256 防篡改。
- **SHA256 必校**：服务端响应中没有 sha256 直接拒绝。
- **路径穿越**：download 端点用正则白名单。
- **签名**：本期不引入 Authenticode 签名（用户机器都关了 SmartScreen 不会强制），未来如果想接 PKI，加一个 `signature` 字段配合公钥校验，不影响现有协议。

---

## 8. 失败 / 回滚

| 失败点 | 行为 |
|--------|------|
| 网络超时 | 静默，下轮 1h 后重试 |
| 404 / 5xx | 静默 + 日志 |
| 下载中断 | 删 part 文件 + 重试 |
| SHA256 不符 | 删 part 文件 + 日志 + 横幅红色 |
| bat 启动失败 | 日志，本进程继续运行（不退出） |
| 替换失败 | bat 内 rollback：copy backup → 拉起原 exe |
| 新进程启动失败 | schtasks 拉起原 exe（已配置自启）；backup 保留 24h，手动恢复 |

---

## 9. 实施顺序

1. **后端**：`release.py` + 路由注册 + 测试，发布 v3.2.9.exe 作为基线
2. **客户端核心**：`updater.go` check + download + sha 校验 + 单测
3. **客户端 apply**：`updater.bat` 生成 + 替换 + 重启 + post-update 钩子
4. **UI**：webwizard 横幅 + 关于卡片 + 3 个 API
5. **发布 v3.3.0**：第一个真正能通过自更新到达的版本

每步独立 commit，方便 review。

---

## 10. 未来工作（不在本期）

- 整包 zip 更新（`full_package: true` 分支）
- 自动健康检查 + 自动回滚（30 秒无 panic 则确认成功）
- beta / stable 通道
- Authenticode 签名校验
- 后台发布上传 UI
