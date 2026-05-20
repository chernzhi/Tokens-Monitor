package main

import (
	"fmt"
	"log"
	"runtime"
	"time"
)

// logMemStatsSnapshot 打一行内存快照，含堆 / 系统 / GC / 协程数。
// 用于关键时刻（启动、watchdog 退出、定期巡检）回看资源是否异常。
func logMemStatsSnapshot(tag string) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	log.Printf("[memstats:%s] HeapAlloc=%s Sys=%s NumGC=%d Goroutines=%d NextGC=%s",
		tag,
		humanBytes(m.HeapAlloc),
		humanBytes(m.Sys),
		m.NumGC,
		runtime.NumGoroutine(),
		humanBytes(m.NextGC),
	)
}

// startMemStatsTicker 后台每 10 分钟打一次 memstats，便于长时间运行时回溯涨幅。
// 周期偏长是为了避免污染日志；watchdog 退出 / 启动时另外强制打一次。
func startMemStatsTicker() {
	go func() {
		t := time.NewTicker(10 * time.Minute)
		defer t.Stop()
		for range t.C {
			logMemStatsSnapshot("tick")
		}
	}()
}

func humanBytes(n uint64) string {
	const k = 1024.0
	switch {
	case n < 1024:
		return fmt.Sprintf("%dB", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1fKB", float64(n)/k)
	case n < 1024*1024*1024:
		return fmt.Sprintf("%.1fMB", float64(n)/(k*k))
	default:
		return fmt.Sprintf("%.1fGB", float64(n)/(k*k*k))
	}
}
