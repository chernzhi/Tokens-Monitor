@echo off
setlocal EnableExtensions
chcp 65001 >nul 2>&1
cd /d "%~dp0"
title AI Token 监控 — 修复网络
echo.
echo   一键修复因 ai-monitor 残留导致的网络异常。处理顺序：
echo     1) 优先调用 ai-monitor --cleanup-network 全量清理（PAC/代理/环境变量/IDE/开机自启）；
echo     2) 兜底调用 ai-monitor --heal（按 install_state 安全还原 + 扫除孤儿注册表项）；
echo     3) 仍无 ai-monitor.exe 时，最后才可选「仅置 ProxyEnable=0」紧急关代理。
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
  echo   [1/2] 执行 --cleanup-network（停后台实例 / 清 PAC / 还原环境变量 / 清 IDE 代理）...
  if defined HEAL_CONFIG (
    "%EXE%" --cleanup-network --config "%HEAL_CONFIG%"
  ) else (
    "%EXE%" --cleanup-network
  )
  if errorlevel 1 (
    echo   --cleanup-network 失败，继续尝试 --heal 兜底...
  )

  echo.
  echo   [2/2] 执行 --heal（扫除孤儿 AutoConfigURL / HTTP_PROXY 等残留）...
  if defined HEAL_CONFIG (
    "%EXE%" --heal --config "%HEAL_CONFIG%"
  ) else (
    "%EXE%" --heal
  )
  if errorlevel 1 (
    echo.
    echo   --heal 未正常结束，请根据上方提示处理，或再试「卸载.bat」。
    pause
    exit /b 1
  )
  echo.
  echo   完成。若浏览器/终端仍走旧代理，请完全关闭后重开，让 WinINet 重新读取注册表。
  pause
  exit /b 0
)

echo   未在脚本同目录、dist\ 或 ..\dist\ 下找到 ai-monitor.exe。
echo   本步骤仅会关闭 WinINet「使用代理服务器」^（ProxyEnable=0^），
echo   不还原你之前备份的代理地址；若依赖 PAC/HTTP_PROXY，请用「卸载.bat」或 ai-monitor --global-uninstall 处理。
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
