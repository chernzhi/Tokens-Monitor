@echo off
chcp 65001 >nul
setlocal EnableDelayedExpansion
cd /d "%~dp0"

echo.
echo  ╔══════════════════════════════════════════════════════════╗
echo  ║         AI Token 监控 - 一键发布脚本                    ║
echo  ║  后端 + 前端 + VSCode 扩展 + 客户端  →  192.168.0.135  ║
echo  ╚══════════════════════════════════════════════════════════╝
echo.

REM ── 读取版本 ──
set "VERSION=dev"
if exist "%~dp0client\VERSION" (
    set /p VERSION=<"%~dp0client\VERSION"
)
echo  版本: v!VERSION!
echo  时间: %date% %time%
echo.

REM ── 检查 SSH 密码 ──
if "%SSH_PASS%"=="" (
    echo  请输入 192.168.0.135 的 SSH 密码（输入后回车）：
    set /p SSH_PASS="  密码: "
    if "!SSH_PASS!"=="" (
        echo  [错误] 密码不能为空，已退出。
        pause
        exit /b 1
    )
    echo.
)

REM ── 检查依赖 ──
echo  检查工具依赖...
where go >nul 2>&1 || (echo  [错误] 未找到 Go，请安装后重试。 & pause & exit /b 1)
where node >nul 2>&1 || (echo  [错误] 未找到 Node.js，请安装后重试。 & pause & exit /b 1)
where python >nul 2>&1 || (echo  [错误] 未找到 Python，请安装后重试。 & pause & exit /b 1)
where npx >nul 2>&1 || (echo  [错误] 未找到 npx，请安装 Node.js 后重试。 & pause & exit /b 1)
echo  依赖检查通过 (Go / Node / Python / npx)
echo.

REM ── 选择发布模式 ──
if "%1"=="backend" goto :deploy_backend_only
if "%1"=="artifacts" goto :deploy_artifacts_only
if "%1"=="skip-build" goto :deploy_skip_build

REM ─────────────────────────────────────────────────────────────
echo  ┌─ [1/4] 编译 Windows 客户端 ai-monitor.exe ─────────────
powershell -NoProfile -ExecutionPolicy Bypass -Command ^
    "& '%~dp0client\build.ps1' -Platform win; exit $LASTEXITCODE"
if errorlevel 1 (
    echo  [错误] 客户端编译失败，请查看上方日志。
    pause
    exit /b 1
)
echo  客户端编译完成
echo.

REM ─────────────────────────────────────────────────────────────
echo  ┌─ [2/4] 打包 VS Code 扩展 VSIX ─────────────────────────
powershell -NoProfile -ExecutionPolicy Bypass -Command ^
    "& '%~dp0vscode-extension\build.ps1' -Platform win; exit $LASTEXITCODE"
if errorlevel 1 (
    echo  [错误] VS Code 扩展打包失败，请查看上方日志。
    pause
    exit /b 1
)
echo  VS Code 扩展打包完成
echo.

REM ─────────────────────────────────────────────────────────────
echo  ┌─ [3/4] 检查服务器环境 (192.168.0.135) ─────────────────
python "%~dp0scripts\deploy.py" check
if errorlevel 1 (
    echo  [警告] 服务器检查报告了问题，将继续尝试部署。
)
echo.

REM ─────────────────────────────────────────────────────────────
echo  ┌─ [4/4] 部署 后端 + 前端 + 上传分发物 ─────────────────
python "%~dp0scripts\deploy.py" all
if errorlevel 1 (
    echo.
    echo  [错误] 部署失败，请查看上方日志。
    pause
    exit /b 1
)
goto :done

REM ── 仅更新后端/前端（不上传 exe/vsix）──────────────────────
:deploy_backend_only
echo  模式: 仅更新后端 + 前端（不重新编译客户端和扩展）
echo.
echo  ┌─ [1/2] 检查服务器环境 ─────────────────────────────────
python "%~dp0scripts\deploy.py" check
echo.
echo  ┌─ [2/2] 部署 后端 + 前端 ───────────────────────────────
python "%~dp0scripts\deploy.py" all --no-artifacts
if errorlevel 1 (
    echo.
    echo  [错误] 部署失败，请查看上方日志。
    pause
    exit /b 1
)
goto :done

REM ── 仅上传 exe + vsix（后端已是最新）────────────────────────
:deploy_artifacts_only
echo  模式: 仅上传客户端 exe 和 VSIX 扩展
echo.
echo  ┌─ [1/3] 编译 Windows 客户端 ────────────────────────────
powershell -NoProfile -ExecutionPolicy Bypass -Command ^
    "& '%~dp0client\build.ps1' -Platform win; exit $LASTEXITCODE"
if errorlevel 1 ( echo  [错误] 客户端编译失败。 & pause & exit /b 1 )
echo.
echo  ┌─ [2/3] 打包 VS Code 扩展 ──────────────────────────────
powershell -NoProfile -ExecutionPolicy Bypass -Command ^
    "& '%~dp0vscode-extension\build.ps1' -Platform win; exit $LASTEXITCODE"
if errorlevel 1 ( echo  [错误] 扩展打包失败。 & pause & exit /b 1 )
echo.
echo  ┌─ [3/3] 上传 分发物 ────────────────────────────────────
python "%~dp0scripts\deploy.py" artifacts
if errorlevel 1 (
    echo  [错误] 上传失败。
    pause
    exit /b 1
)
goto :done

REM ── 跳过编译，直接部署（已有 exe/vsix）─────────────────────
:deploy_skip_build
echo  模式: 跳过编译，直接全量部署
echo.
python "%~dp0scripts\deploy.py" all
if errorlevel 1 (
    echo  [错误] 部署失败。
    pause
    exit /b 1
)

REM ─────────────────────────────────────────────────────────────
:done
echo.
echo  ╔══════════════════════════════════════════════════════════╗
echo  ║  发布完成  v!VERSION!
echo  ║                                                         ║
echo  ║  前端访问:  http://192.168.0.135:3080                   ║
echo  ║  后端 API:  http://192.168.0.135:8000                   ║
echo  ║  扩展下载:  http://192.168.0.135:8000/api/extension/latest
echo  ║  客户端:    http://192.168.0.135:8000/api/extension/client
echo  ╚══════════════════════════════════════════════════════════╝
echo.
echo  查看服务器状态请执行: python scripts\deploy.py status
echo.
pause
exit /b 0
