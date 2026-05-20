@echo off
chcp 65001 >nul
setlocal EnableDelayedExpansion
cd /d "%~dp0"

echo.
echo  ============================================================
echo    AI Token Monitor - Client Auto-Update Publisher
echo    build -^> sha256 -^> scp -^> 192.168.0.135
echo  ============================================================
echo.

if not exist "%~dp0client\VERSION" (
    echo  [ERROR] client\VERSION not found.
    pause
    exit /b 1
)
set /p CUR_VERSION=<"%~dp0client\VERSION"
echo  Current version: v!CUR_VERSION!

REM ── Auto bump patch (a.b.c -> a.b.(c+1)); allow user to override ──
for /f "tokens=1,2,3 delims=." %%a in ("!CUR_VERSION!") do (
    set MAJOR=%%a
    set MINOR=%%b
    set PATCH=%%c
)
set /a NEXT_PATCH=!PATCH!+1
set "SUGGEST=!MAJOR!.!MINOR!.!NEXT_PATCH!"
set /p VERSION="  New version [default !SUGGEST!]: "
if "!VERSION!"=="" set "VERSION=!SUGGEST!"
REM Strip whitespace user may have pasted
set "VERSION=!VERSION: =!"

REM Sanity check: must look like a.b.c (no trailing space before pipe!)
echo(!VERSION!|findstr /R "^[0-9][0-9]*\.[0-9][0-9]*\.[0-9][0-9]*$" >nul
if errorlevel 1 (
    echo  [ERROR] Invalid version format: !VERSION!  expected a.b.c
    pause
    exit /b 1
)

if /I not "!VERSION!"=="!CUR_VERSION!" (
    > "%~dp0client\VERSION" echo !VERSION!
    echo  Updated client\VERSION: v!CUR_VERSION! -^> v!VERSION!
) else (
    echo  Reusing existing version v!VERSION!
)
echo.

if "%SSH_PASS%"=="" (
    set /p SSH_PASS="  SSH password for 192.168.0.135: "
    if "!SSH_PASS!"=="" (
        echo  [ERROR] Password empty, aborted.
        pause
        exit /b 1
    )
    echo.
)

where go >nul 2>&1 || (echo  [ERROR] Go not found. & pause & exit /b 1)
where python >nul 2>&1 || (echo  [ERROR] Python not found. & pause & exit /b 1)
where powershell >nul 2>&1 || (echo  [ERROR] PowerShell not found. & pause & exit /b 1)
echo  Tool check OK (Go / Python / PowerShell)
echo.

echo  [1/2] Building Windows client v!VERSION! ...
powershell -NoProfile -ExecutionPolicy Bypass -Command "& '%~dp0client\build.ps1' -Platform win; exit $LASTEXITCODE"
if errorlevel 1 (
    echo  [ERROR] Client build failed.
    pause
    exit /b 1
)
echo  Build done.
echo.

echo  [2/2] Uploading ai-monitor-!VERSION!.exe + sha256 + release notes ...
python "%~dp0scripts\deploy.py" publish-update
if errorlevel 1 (
    echo  [ERROR] Upload failed.
    pause
    exit /b 1
)

echo.
echo  ============================================================
echo    DONE  v!VERSION!
echo  ============================================================
echo    Old v3.2.9 clients will auto-detect within 1 hour, or
echo    click "Check Updates" in the console About card.
echo.
pause
exit /b 0
