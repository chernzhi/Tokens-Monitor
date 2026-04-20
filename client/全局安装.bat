@echo off
chcp 65001 >nul 2>&1
cd /d "%~dp0"
title AI Token 监控 — 全局安装
echo.
echo   全局安装（环境变量 + 系统 PAC 等，需已有 config.json）
echo   若尚未配置，请先双击「开始使用.bat」
echo.
"%~dp0ai-monitor.exe" --global-install
if errorlevel 1 pause
