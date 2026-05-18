@echo off
chcp 65001 >nul 2>&1
cd /d "%~dp0"
title AI Token 监控 — 低侵入模式
echo.
echo   低侵入监控模式
echo   打开本窗口时仅临时启用 AI 域名 PAC；关闭后恢复系统代理。
echo   不写用户级 HTTP_PROXY/HTTPS_PROXY，减少对 sing-box / Proxifier 的叠加影响。
echo   如需拉起指定工具，请使用「启动-VSCode监控.bat」或「启动-Cursor监控.bat」。
echo.
"%~dp0ai-monitor.exe"
if errorlevel 1 pause