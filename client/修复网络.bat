@echo off
chcp 65001 >nul 2>&1
cd /d "%~dp0"
title AI Token 监控 — 修复网络
echo.
echo   若上次异常退出后，系统代理仍指向无效端口，可运行本脚本恢复。
echo.
"%~dp0ai-monitor.exe" --heal
if errorlevel 1 pause
