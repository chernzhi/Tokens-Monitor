@echo off
chcp 65001 >nul 2>&1
cd /d "%~dp0"

set "OUT=%~dp0..\dist\ai-monitor-分发版"
echo.
echo   正在编译并生成分发目录...
echo   输出: %OUT%
echo.

if not exist "%~dp0..\dist" mkdir "%~dp0..\dist"
if exist "%OUT%" rmdir /s /q "%OUT%"
if exist "%OUT%" (
    echo   [错误] 无法清理旧分发目录：%OUT%
    echo   请先关闭正在从该目录运行的 ai-monitor.exe，再重新打包。
    pause
    exit /b 1
)
mkdir "%OUT%" 2>nul
if errorlevel 1 (
    echo   [错误] 无法创建分发目录：%OUT%
    pause
    exit /b 1
)

where go >nul 2>&1
if errorlevel 1 (
    echo   [错误] 未找到 Go，无法编译。请将已编好的 ai-monitor.exe 复制到 %OUT% 后，手动拷贝下列文件。
    pause
    exit /b 1
)

set "VERSION=dev"
if exist "%~dp0VERSION" (
    set /p VERSION=<"%~dp0VERSION"
)
set "LDFLAGS=-s -w -X main.Version=%VERSION%"

go build -ldflags="%LDFLAGS%" -o "%OUT%\ai-monitor.exe" .
if errorlevel 1 (
    echo   [错误] 编译失败
    pause
    exit /b 1
)
if not exist "%OUT%\ai-monitor.exe" (
    echo   [错误] 未生成 ai-monitor.exe
    pause
    exit /b 1
)

copy /Y "%~dp0卸载.bat" "%OUT%\" >nul
copy /Y "%~dp0修复网络.bat" "%OUT%\" >nul
copy /Y "%~dp0config.example.json" "%OUT%\" >nul
copy /Y "%~dp0使用说明.md" "%OUT%\" >nul

echo   ✓ 完成
echo   分发包只包含: ai-monitor.exe、卸载.bat、修复网络.bat、config.example.json、使用说明.md
echo   请将文件夹「ai-monitor-分发版」打成 zip 发给同事。
echo   提醒：不要附带仓库里的 config.json（真实配置）。
echo.
pause

