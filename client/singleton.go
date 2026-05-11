package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// InstanceInfo describes a running ai-monitor instance, persisted to instance.json
// so that other processes (IDE extensions, subsequent CLI invocations) can discover it.
type InstanceInfo struct {
	PID       int    `json:"pid"`
	Port      int    `json:"port"`
	Version   string `json:"version"`
	StartedAt string `json:"started_at"`
}

func instanceInfoPath() string {
	return filepath.Join(appDataDir(), "instance.json")
}

func writeInstanceInfo(port int) error {
	info := InstanceInfo{
		PID:       os.Getpid(),
		Port:      port,
		Version:   Version,
		StartedAt: time.Now().Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}
	p := instanceInfoPath()
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return fmt.Errorf("create instance dir: %w", err)
	}
	return os.WriteFile(p, data, 0644)
}

func readInstanceInfo() (*InstanceInfo, error) {
	data, err := os.ReadFile(instanceInfoPath())
	if err != nil {
		return nil, err
	}
	var info InstanceInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

func removeInstanceInfo() {
	os.Remove(instanceInfoPath())
}

func stopExistingInstanceForUninstall() {
	info, err := readInstanceInfo()
	if err != nil || info.PID <= 0 {
		return
	}
	if info.PID == os.Getpid() {
		removeInstanceInfo()
		return
	}
	if !isProcessAlive(info.PID) {
		removeInstanceInfo()
		return
	}
	if info.Port <= 0 || !probeInstanceStatus(info.Port) {
		fmt.Printf("    ⚠ instance.json 指向 PID %d，但 /status 不可达，跳过终止以避免误杀。\n", info.PID)
		return
	}
	proc, err := os.FindProcess(info.PID)
	if err != nil {
		fmt.Printf("    ⚠ 找不到旧实例 PID %d: %v\n", info.PID, err)
		return
	}
	if err := proc.Kill(); err != nil {
		fmt.Printf("    ⚠ 停止旧实例 PID %d 失败: %v\n", info.PID, err)
		return
	}
	for i := 0; i < 20; i++ {
		if !isProcessAlive(info.PID) {
			fmt.Printf("    ✓ 已停止旧实例 PID %d\n", info.PID)
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	fmt.Printf("    ⚠ 已发送停止信号，但 PID %d 仍在运行。\n", info.PID)
}

// checkExistingInstance reads the PID file, checks if the process is alive,
// and probes the /status endpoint to verify the instance is healthy.
// Returns (port, true) if a healthy instance exists; (0, false) otherwise.
func checkExistingInstance() (int, bool) {
	info, err := readInstanceInfo()
	if err != nil {
		return 0, false
	}

	// Check if the recorded process is still alive
	if !isProcessAlive(info.PID) {
		log.Printf("[singleton] PID %d 不存在，忽略过期的 instance.json", info.PID)
		return 0, false
	}

	// Probe the HTTP /status endpoint on the recorded port
	if probeInstanceStatus(info.Port) {
		return info.Port, true
	}

	log.Printf("[singleton] PID %d 存活但端口 %d 不可达，视为异常实例", info.PID, info.Port)
	return 0, false
}

// probeInstanceStatus sends a GET /status to localhost:port with a short timeout.
func probeInstanceStatus(port int) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/status", port))
	if err != nil {
		return false
	}
	// 必须 drain body 后再 Close，否则 HTTP/1.1 keep-alive 连接无法复用，
	// watchdog 每 10s 一次会持续打开新 TCP，长期运行造成 TIME_WAIT 堆积。
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
