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

> "%LOG%" echo [updater] %DATE% %TIME% start
>>"%LOG%" echo TARGET=%TARGET%
>>"%LOG%" echo NEW=%NEW%
>>"%LOG%" echo BACKUP=%BACKUP%

REM 备份当前 exe（即使父进程未退出 copy 仍可读）
copy /Y "%TARGET%" "%BACKUP%" >>"%LOG%" 2>&1
if errorlevel 1 (
  >>"%LOG%" echo backup failed
  exit /b 1
)

REM 重试覆盖：父进程未退出时 move 会失败，循环至多 ~60s
set /a TRIES=0
:retry
move /Y "%NEW%" "%TARGET%" >>"%LOG%" 2>&1
if not errorlevel 1 goto launched
set /a TRIES+=1
if %TRIES% GEQ 60 goto rollback
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

	cmd := newDetachedCmd("cmd", "/c", batPath,
		currentExe, newExe, backupPath, logPath)
	if err := cmd.Start(); err != nil {
		return err
	}
	log.Printf("[updater] 已派发 updater.bat (pid=%d)，本进程将退出以释放 exe", cmd.Process.Pid)
	go func() {
		time.Sleep(500 * time.Millisecond)
		os.Exit(0)
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
