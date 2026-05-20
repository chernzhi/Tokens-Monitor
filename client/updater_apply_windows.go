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
setlocal
set TARGET=%~1
set NEW=%~2
set BACKUP=%~3
set LOG=%~4
set PARENTPID=%~5

REM 备份当前 exe
copy /Y "%TARGET%" "%BACKUP%" >>"%LOG%" 2>&1

REM 等待父进程退出（最多 30s）
set /a TRIES=0
:waitloop
tasklist /FI "PID eq %PARENTPID%" 2>nul | find "%PARENTPID%" >nul
if errorlevel 1 goto replace
set /a TRIES+=1
if %TRIES% GEQ 60 goto fail
ping -n 2 127.0.0.1 >nul
goto waitloop

:replace
move /Y "%NEW%" "%TARGET%" >>"%LOG%" 2>&1
if errorlevel 1 goto rollback

start "" "%TARGET%" --post-update "%BACKUP%"
exit /b 0

:rollback
copy /Y "%BACKUP%" "%TARGET%" >>"%LOG%" 2>&1
start "" "%TARGET%"
exit /b 1

:fail
echo updater: parent did not exit within 30s >>"%LOG%"
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
	currentExe, _ = filepath.Abs(currentExe)

	tmpDir := filepath.Dir(newExe)
	batPath := filepath.Join(tmpDir, "updater.bat")
	logPath := filepath.Join(tmpDir, "updater.log")
	backupPath := filepath.Join(tmpDir, fmt.Sprintf("ai-monitor-backup-%d.exe", time.Now().Unix()))

	if err := os.WriteFile(batPath, []byte(renderUpdaterBat()), 0o755); err != nil {
		return err
	}

	cmd := newDetachedCmd("cmd", "/c", batPath,
		currentExe, newExe, backupPath, logPath, fmt.Sprint(os.Getpid()))
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
