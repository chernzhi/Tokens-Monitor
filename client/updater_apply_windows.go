//go:build windows

package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

const updaterBatTemplate = `@echo off
setlocal EnableExtensions
set "TARGET=%~1"
set "NEW=%~2"
set "BACKUP=%~3"
set "LOG=%~4"
set "OLDPID=%~5"

> "%LOG%" echo [updater] %DATE% %TIME% start
>>"%LOG%" echo TARGET=%TARGET%
>>"%LOG%" echo NEW=%NEW%
>>"%LOG%" echo BACKUP=%BACKUP%
>>"%LOG%" echo OLDPID=%OLDPID%

REM 1) 等待旧进程优雅退出，最多 ~10s
set /a WAIT=0
:waitloop
tasklist /FI "PID eq %OLDPID%" 2>nul | findstr /C:"%OLDPID%" >nul
if errorlevel 1 goto killed
set /a WAIT+=1
if %WAIT% GEQ 20 goto forcekill
ping -n 2 127.0.0.1 >nul
goto waitloop

:forcekill
>>"%LOG%" echo old pid %OLDPID% still alive after 10s, force kill
taskkill /PID %OLDPID% /T /F >>"%LOG%" 2>&1
ping -n 2 127.0.0.1 >nul

:killed
REM 2) 备份当前 exe
copy /Y "%TARGET%" "%BACKUP%" >>"%LOG%" 2>&1
if errorlevel 1 (
  >>"%LOG%" echo backup failed
  exit /b 1
)

REM 3) 重试覆盖：旧进程理论上已死，给 10 次容错
set /a TRIES=0
:retry
move /Y "%NEW%" "%TARGET%" >>"%LOG%" 2>&1
if not errorlevel 1 goto launched
set /a TRIES+=1
if %TRIES% GEQ 10 goto rollback
ping -n 2 127.0.0.1 >nul
goto retry

:launched
>>"%LOG%" echo move ok after %TRIES% retries
start "" "%TARGET%" --post-update "%BACKUP%"
exit /b 0

:rollback
>>"%LOG%" echo move failed, rolling back
copy /Y "%BACKUP%" "%TARGET%" >>"%LOG%" 2>&1
start "" "%TARGET%"
exit /b 2
`

func renderUpdaterBat() string { return updaterBatTemplate }

// ApplyUpdate 写 bat 并 detach 启动，自身退出。
func (u *Updater) ApplyUpdate(info *ReleaseInfo) error {
	if info == nil {
		return fmt.Errorf("无可用更新")
	}
	newExe, err := u.downloadToTemp(info)
	if err != nil {
		return err
	}
	currentExe, err := os.Executable()
	if err != nil {
		return err
	}
	absExe, err := filepath.Abs(currentExe)
	if err != nil {
		return fmt.Errorf("解析当前 exe 路径失败: %w", err)
	}
	currentExe = absExe

	tmpDir := filepath.Dir(newExe)
	batPath := filepath.Join(tmpDir, "updater.bat")
	logPath := filepath.Join(tmpDir, "updater.log")
	backupPath := filepath.Join(tmpDir, fmt.Sprintf("ai-monitor-backup-%d.exe", time.Now().UnixNano()))

	if err := os.WriteFile(batPath, []byte(renderUpdaterBat()), 0o755); err != nil {
		return err
	}

	myPID := fmt.Sprintf("%d", os.Getpid())
	// 直接传 batPath 让 newDetachedCmd 走 .bat 分支（CREATE_NO_WINDOW + start /b），
	// 避免 DETACHED_PROCESS 组合下 cmd 的子进程（ping/tasklist/taskkill）被强行
	// 分配新控制台导致黑窗持续闪烁。
	cmd := newDetachedCmd(batPath,
		currentExe, newExe, backupPath, logPath, myPID)
	if err := cmd.Start(); err != nil {
		return err
	}
	log.Printf("[updater] 已派发 updater.bat (pid=%d)，本进程将优雅退出释放 exe", cmd.Process.Pid)
	go func() {
		time.Sleep(500 * time.Millisecond)
		RequestShutdown("update-apply")
	}()
	return nil
}

// PostUpdateCleanup 在 --post-update <backup> 启动时调用：
// 30 秒后若本进程仍存活，删除备份文件。
func PostUpdateCleanup(backupPath string) {
	log.Printf("[updater] ✅ 已更新到 v%s（备份: %s）", Version, backupPath)
	go func() {
		time.Sleep(30 * time.Second)
		if backupPath == "" {
			return
		}
		if err := os.Remove(backupPath); err != nil && !os.IsNotExist(err) {
			log.Printf("[updater] 备份清理失败: %v", err)
			return
		}
		log.Printf("[updater] 备份已清理")
	}()
}
