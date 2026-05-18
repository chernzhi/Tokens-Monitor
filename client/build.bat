@echo off
echo.
echo  Building AI Token Monitor Client...
echo.

:: ── Resolve version ──
if exist "%~dp0VERSION" (
    set /p VERSION=<"%~dp0VERSION"
) else (
    set VERSION=dev
)
echo  Version: %VERSION%
:: -H=windowsgui 隐藏 conhost 控制台窗口：日志通过内嵌 WebView2 里的"运行日志"
:: 面板显示，整个程序看起来是单一窗口应用。
set LDFLAGS=-s -w -H=windowsgui -X main.Version=%VERSION%

:: Build for Windows amd64
set GOOS=windows
set GOARCH=amd64
go build -ldflags="%LDFLAGS%" -o ai-monitor.exe .

if %ERRORLEVEL% NEQ 0 (
    echo  Build FAILED!
    pause
    exit /b 1
)

echo  Build SUCCESS: ai-monitor.exe
echo.

:: Show file size
for %%A in (ai-monitor.exe) do echo  Size: %%~zA bytes
echo.
pause
