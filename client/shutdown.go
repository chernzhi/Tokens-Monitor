package main

import (
	"log"
	"sync"
)

// shutdownCh 在 RequestShutdown 第一次被调用时关闭。
// main 循环监听它来触发优雅退出，与 SIGINT 路径共享同一段清理代码。
var (
	shutdownOnce sync.Once
	shutdownCh   = make(chan struct{})
)

// RequestShutdown 由 updater / WebView2 关闭事件 / signal handler 调用。
// 多次调用安全。返回不阻塞；真正的 shutdown 在 main 中执行。
func RequestShutdown(reason string) {
	shutdownOnce.Do(func() {
		log.Printf("[shutdown] requested: %s", reason)
		close(shutdownCh)
	})
}
