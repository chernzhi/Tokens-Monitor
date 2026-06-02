# AI Token 监控客户端 v3.3.14

## 修复

- **Codex 桌面版 token 漏记**：Codex 引擎只认 `HTTPS_PROXY` / `CODEX_CA_CERTIFICATE` 环境变量，不认 Windows 系统代理。GUI（VS Code/Cursor 里的 Codex 扩展、Codex 桌面版）若在启用监控**之前**就已打开，其子进程不会继承新写入的代理环境，导致 token 统计为 0（CLI 在新开终端里始终带有该环境变量，所以一直正常）。

## 新增

- **Codex 桌面版一键启动入口**：向导新增「Codex 桌面版」按钮，启动时直接注入 `HTTPS_PROXY` + CA 证书，绕过「需重启才继承环境变量」的问题。找不到 exe 时可用「自定义应用」指向实际 `Codex.exe`，效果相同。
- **运行实例自检横幅**：控制台检测到 Codex 扩展所在的编辑器 / Codex 桌面版正在运行时，提示「完全退出并重启」，避免在启用监控前打开的实例继续漏记。
