# AI Token 监控客户端 v3.3.12

## 修复

- **updater 黑窗闪烁**：DETACHED_PROCESS + CREATE_NO_WINDOW 组合下，cmd 派生的 ping/tasklist/taskkill 被强行分配新控制台，导致更新期间黑窗反复闪烁。改为直接走 `.bat` 分支（`start /b` + 仅 CREATE_NO_WINDOW），子进程复用隐藏控制台。
- **wizard 启动器图标空白**：
  - `cdn.simpleicons.org` 被 Cloudflare 拦截 + 微软系商标（VS Code / PowerShell / Windows Terminal）被 Simple Icons 下架；
  - 全部下线 CDN 依赖，改为 `//go:embed assets/icons` 嵌入本地 SVG/PNG。
  - 新增 `/wizard/icons/<name>` 路由（`webwizard_icons.go`）。
  - Kiro / Trae / Qoder / Windsurf / VS Code 用官方站点下载的图标。

## 新增

- **wizard 左右分栏可拖拽**：
  - 主面板 ↔ 日志面板之间加 8px 分隔条，鼠标拖动改宽度，比例限制 25%–75% 并落 `localStorage.wizardCardPct`。
  - 窗口宽度 ≤1100px 自动堆叠并隐藏分隔条。
- **按钮 4 列容器查询自适应**：
  - 一键模式切换 / 一键启动编辑器 / JetBrains 系列三栏默认 4 列；
  - `.card` 启用 `container-type: inline-size`，按卡片实际宽度（不是视口宽度）自适应到 3 / 2 / 1 列，拖动分隔条立即响应。

## 兜底

如果 3.3.12 自更新失败导致多实例残留，可用：

```
ai-monitor.exe --force-cleanup
```

强制清理同名进程 + 释放端口。
