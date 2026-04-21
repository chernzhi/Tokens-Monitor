package main

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
)

// 日志文件保留策略：单文件超过 maxLogBytes 就在原地切到 .1（覆盖旧 .1），重新写入。
// 不引入第三方滚动日志依赖；够 desktop 端调试用即可。
const maxLogBytes = 4 * 1024 * 1024 // 4 MiB

var (
	logFileMu  sync.Mutex
	logFile    *os.File
	logDataDir string
)

// setupFileLogging 把 log 包默认 logger 同时输出到 stderr 和
// %APPDATA%/ai-monitor/ai-monitor.log。后台模式 stderr 为 nil 时，
// 文件输出依然有效，这是排查"上报像没在跑"的唯一可靠现场。
func setupFileLogging(dataDir string) {
	logDataDir = dataDir
	f := openLogFile(dataDir)
	if f == nil {
		return
	}
	logFile = f
	log.SetOutput(io.MultiWriter(os.Stderr, &rotatingWriter{}))
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
}

func openLogFile(dataDir string) *os.File {
	path := filepath.Join(dataDir, "ai-monitor.log")
	rotateIfTooLarge(path)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil
	}
	return f
}

func rotateIfTooLarge(path string) {
	st, err := os.Stat(path)
	if err != nil || st.Size() < maxLogBytes {
		return
	}
	old := path + ".1"
	_ = os.Remove(old)
	_ = os.Rename(path, old)
}

// rotatingWriter 在每次 Write 前检查文件大小，超过阈值就滚动并重开。
// 单进程场景下足够：log 包在 Output() 内对 io.Writer 的调用本就是串行的。
type rotatingWriter struct{}

func (rotatingWriter) Write(p []byte) (int, error) {
	logFileMu.Lock()
	defer logFileMu.Unlock()
	if logFile == nil {
		return len(p), nil
	}
	if st, err := logFile.Stat(); err == nil && st.Size()+int64(len(p)) >= maxLogBytes {
		_ = logFile.Close()
		path := filepath.Join(logDataDir, "ai-monitor.log")
		rotateIfTooLarge(path)
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err == nil {
			logFile = f
		}
	}
	return logFile.Write(p)
}
