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

// appendUpdaterLog 向 updater 日志追加一行（带时间戳），失败静默。
// ApplyUpdate / RunApplySwap 全程用它写诊断，确保即使派发失败也能在
// %TEMP%\ai-monitor-update\updater.log 留下证据。
func appendUpdaterLog(logPath, line string) {
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\r\n", time.Now().Format("2006-01-02 15:04:05.000"), line)
}

// ApplyUpdate 下载新版本后，让「新 exe」自己完成替换：
// 通过 newDetachedCmd 的 .exe 分支（DETACHED_PROCESS）拉起 newExe --apply-swap ...，
// 由 RunApplySwap 在新进程里等待旧进程释放文件锁、备份、覆盖目标、重启。
// 不再经过 cmd /c start /b updater.bat —— 既避免长时间运行后 cmd+conhost
// 在桌面堆耗尽时 CreateProcess 失败导致更新「悄悄不生效」，也彻底消除
// ping/tasklist/taskkill 子控制台的黑窗闪烁。
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
	logPath := filepath.Join(tmpDir, "updater.log")
	backupPath := filepath.Join(tmpDir, fmt.Sprintf("ai-monitor-backup-%d.exe", time.Now().UnixNano()))
	myPID := fmt.Sprintf("%d", os.Getpid())

	appendUpdaterLog(logPath, fmt.Sprintf("[ApplyUpdate] start target=%s new=%s backup=%s oldpid=%s", currentExe, newExe, backupPath, myPID))

	// 走 .exe DETACHED 分支：直接派发新 exe 自身完成替换，无 cmd/.bat 中间层。
	cmd := newDetachedCmd(newExe,
		"--apply-swap", currentExe,
		"--apply-backup", backupPath,
		"--apply-oldpid", myPID,
		"--apply-log", logPath)
	if err := cmd.Start(); err != nil {
		appendUpdaterLog(logPath, fmt.Sprintf("[ApplyUpdate] 派发新 exe 失败: %v", err))
		return fmt.Errorf("派发更新进程失败: %w", err)
	}
	appendUpdaterLog(logPath, fmt.Sprintf("[ApplyUpdate] 已派发 swap 进程 pid=%d，本进程将优雅退出", cmd.Process.Pid))
	log.Printf("[updater] 已派发 swap 进程 (pid=%d)，本进程将优雅退出释放 exe", cmd.Process.Pid)
	go func() {
		time.Sleep(500 * time.Millisecond)
		RequestShutdown("update-apply")
	}()
	return nil
}

// RunApplySwap 在「新 exe」进程里执行实际替换。由 main.go 解析 --apply-swap 调用，
// 完成后调用 os.Exit。所有步骤写入 logPath，便于排障。
//
//	target  : 要被覆盖的已安装 exe 路径
//	backup  : 备份路径（替换前先把 target 复制到这里，便于回滚 / --post-update 清理）
//	oldPID  : 旧进程 PID（仅用于日志；实际靠「文件锁是否释放」判断旧进程是否退出）
func RunApplySwap(target, backup, oldPID, logPath string) {
	self, err := os.Executable()
	if err != nil {
		appendUpdaterLog(logPath, fmt.Sprintf("[swap] os.Executable 失败: %v", err))
		os.Exit(1)
	}
	if abs, aerr := filepath.Abs(self); aerr == nil {
		self = abs
	}
	appendUpdaterLog(logPath, fmt.Sprintf("[swap] start self=%s target=%s backup=%s oldpid=%s", self, target, backup, oldPID))

	// 1) 备份当前已安装 exe（读取旧 exe 即使其仍在运行也允许）。
	if err := copyFile(target, backup); err != nil {
		appendUpdaterLog(logPath, fmt.Sprintf("[swap] 备份失败（继续尝试覆盖）: %v", err))
	} else {
		appendUpdaterLog(logPath, "[swap] 备份完成")
	}

	// 2) 重试覆盖：旧进程退出前 target 仍被文件锁占用，copyFile 会失败；
	//    最多重试约 20 次（每次 500ms，合计 ~10s）等待旧进程退出释放锁。
	const maxTries = 20
	var lastErr error
	for i := 1; i <= maxTries; i++ {
		if lastErr = copyFile(self, target); lastErr == nil {
			appendUpdaterLog(logPath, fmt.Sprintf("[swap] 覆盖成功（第 %d 次尝试）", i))
			break
		}
		appendUpdaterLog(logPath, fmt.Sprintf("[swap] 覆盖第 %d/%d 次失败: %v", i, maxTries, lastErr))
		time.Sleep(500 * time.Millisecond)
	}

	if lastErr != nil {
		// 覆盖始终失败：尝试回滚（target 可能已被部分写坏）并拉起原版本。
		appendUpdaterLog(logPath, "[swap] 覆盖最终失败，尝试回滚")
		if _, statErr := os.Stat(backup); statErr == nil {
			if rbErr := copyFile(backup, target); rbErr != nil {
				appendUpdaterLog(logPath, fmt.Sprintf("[swap] 回滚失败: %v", rbErr))
			} else {
				appendUpdaterLog(logPath, "[swap] 已回滚到备份版本")
			}
		}
		relaunch(target, logPath)
		os.Exit(2)
	}

	// 3) 覆盖成功，拉起新版本并传入备份路径用于成功后清理。
	//    用 newDetachedGuiCmd（不带 SW_HIDE），否则新进程的 WebView2 主窗口
	//    会因继承 STARTUPINFO 的 SW_HIDE 而初始隐藏，表现为「更新后没起来」。
	cmd := newDetachedGuiCmd(target, "--post-update", backup)
	if err := cmd.Start(); err != nil {
		appendUpdaterLog(logPath, fmt.Sprintf("[swap] 重启新版本失败: %v", err))
		os.Exit(3)
	}
	appendUpdaterLog(logPath, fmt.Sprintf("[swap] 已重启新版本 pid=%d，更新完成", cmd.Process.Pid))
	os.Exit(0)
}

// relaunch 兜底拉起 target（不带 --post-update），失败仅记日志。
func relaunch(target, logPath string) {
	cmd := newDetachedGuiCmd(target)
	if err := cmd.Start(); err != nil {
		appendUpdaterLog(logPath, fmt.Sprintf("[swap] 兜底重启失败: %v", err))
		return
	}
	appendUpdaterLog(logPath, fmt.Sprintf("[swap] 兜底已重启 pid=%d", cmd.Process.Pid))
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
