@echo off
setlocal EnableExtensions
chcp 65001 >nul 2>&1
cd /d "%~dp0"
title AI Token 监控 — 修复网络
echo.
echo   若上次异常退出后，系统代理仍指向无效端口，可运行本脚本恢复。
echo   优先：本目录或 dist 下的 ai-monitor.exe 执行 --heal（按 install_state 安全还原）；
echo   若未找到程序：可选仅将「手动代理」开关置为关（不还原你曾用的具体公司代理，也不动 PAC/环境变量）。
echo.

set "INET_REG=HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings"
set "EXE="
if exist "%~dp0ai-monitor.exe" set "EXE=%~dp0ai-monitor.exe"
if not defined EXE if exist "%~dp0dist\ai-monitor.exe" set "EXE=%~dp0dist\ai-monitor.exe"
if not defined EXE if exist "%~dp0..\dist\ai-monitor.exe" set "EXE=%~dp0..\dist\ai-monitor.exe"

set "HEAL_CONFIG="
if exist "%APPDATA%\ai-monitor\config.json" set "HEAL_CONFIG=%APPDATA%\ai-monitor\config.json"
if not defined HEAL_CONFIG if exist "%~dp0config.json" set "HEAL_CONFIG=%~dp0config.json"

if defined EXE (
  if defined HEAL_CONFIG (
    "%EXE%" --heal --config "%HEAL_CONFIG%"
  ) else (
    "%EXE%" --heal
  )
  if errorlevel 1 (
    echo.
    echo   --heal 未正常结束，请根据上方提示处理，或再试「全局卸载」。
    pause
    exit /b 1
  )
  echo.
  echo   若浏览器/终端仍走旧代理，请完全关闭后重开，或重新打开本窗口后再试网络。
  exit /b 0
)

echo   未在脚本同目录、dist\ 或 ..\dist\ 下找到 ai-monitor.exe。
echo   本步骤仅会关闭 WinINet「使用代理服务器」^（ProxyEnable=0^），
echo   不还原你之前备份的代理地址；若依赖 PAC/HTTP_PROXY，请用「全局卸载」或 ai-monitor --global-uninstall 处理。
echo.
set /p _ok=  是否继续仅置 ProxyEnable=0? [Y/n] 
if /i "%_ok%"=="n" (
  echo 已取消。
  exit /b 0
)

reg add "%INET_REG%" /v "ProxyEnable" /t REG_DWORD /d 0 /f
if errorlevel 1 (
  echo 写入当前用户 HKCU 下的代理开关失败。若「设置」里仍显示错误代理，请检查权限或手动关闭。
  pause
  exit /b 1
)

echo   已设置 ProxyEnable=0。请重开浏览器/终端。若公司网络需要 PAC/环境变量，请在本机改回你原先的代理再连内网。
pause
exit /b 0
