@echo off
chcp 65001 >nul 2>&1
cd /d "%~dp0"
title AI Token 监控 — 全局卸载
echo.
echo   全局卸载（证书、环境变量、系统代理与开机自启等）
echo.
"%~dp0ai-monitor.exe" --global-uninstall
if errorlevel 1 pause
