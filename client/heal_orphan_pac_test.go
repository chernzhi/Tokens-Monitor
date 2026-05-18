package main

import (
	"strings"
	"testing"
)

// isAIMonitorPACURL 必须能识别 ai-monitor 自己写过的 PAC URL，
// 这样 --heal、--cleanup-network、applyTemporarySessionProxy 才能
// 把孤儿 PAC 区分于「用户的企业 PAC」。
func TestIsAIMonitorPACURLRecognizesOwn(t *testing.T) {
	// 来自常见落地点
	cases := []string{
		"file:///C:/Users/x/AppData/Roaming/ai-monitor/proxy.pac",
		"file:///c:/users/x/AppData/Roaming/AI-Monitor/proxy.pac",
		strings.ToUpper("file:///C:/Users/X/AppData/Roaming/ai-monitor/proxy.pac"),
	}
	for _, u := range cases {
		if !isAIMonitorPACURL(u) {
			t.Errorf("应识别为 ai-monitor PAC: %q", u)
		}
	}
}

// 真实的企业 PAC（来自 HTTP/HTTPS 或别的本地路径）不应被误判。
func TestIsAIMonitorPACURLAcceptsCorporatePAC(t *testing.T) {
	cases := []string{
		"http://proxy.corp.example.com/wpad.dat",
		"https://wpad.example.com/proxy.pac",
		"file:///C:/Users/x/Documents/corp.pac",
		"",
	}
	for _, u := range cases {
		if isAIMonitorPACURL(u) {
			t.Errorf("不应识别为 ai-monitor PAC: %q", u)
		}
	}
}

// isAIMonitorManagedEnvValue 用于判断「环境变量值」是否由 ai-monitor 写入。
// 既要能识别端口范围内的 self proxy，也要识别我们的 CA 路径。
func TestIsAIMonitorManagedEnvValue(t *testing.T) {
	if !isAIMonitorManagedEnvValue("http://127.0.0.1:18090") {
		t.Fatal("默认端口的自指代理未被识别为 ai-monitor 管理值")
	}
	if !isAIMonitorManagedEnvValue("http://localhost:18090/openai/v1") {
		t.Fatal("自指代理 + Base URL 未被识别")
	}
	if !isAIMonitorManagedEnvValue("C:\\Users\\x\\AppData\\Roaming\\ai-monitor\\ca.crt") {
		t.Fatal("我们的 CA 路径未被识别")
	}
	if !isAIMonitorManagedEnvValue("/Users/x/.config/ai-monitor/ca.crt") {
		t.Fatal("Unix 风格的 ai-monitor CA 路径未被识别")
	}
	// 不属于 ai-monitor 的值不应被误判
	if isAIMonitorManagedEnvValue("http://corp-proxy:8080") {
		t.Fatal("企业代理不应被识别为 ai-monitor 管理值")
	}
	if isAIMonitorManagedEnvValue("") {
		t.Fatal("空字符串不应被识别为 ai-monitor 管理值")
	}
}

// isSelfProxy 必须严格在 ai-monitor 端口范围内才返回 true。
// 这是「卸载时是否清空 HTTP_PROXY」的核心判据，误判会破坏用户的真代理。
func TestIsSelfProxyStrictRange(t *testing.T) {
	if !isSelfProxy("http://127.0.0.1:18090") {
		t.Fatal("18090 是默认 MITM 端口，应识别为 self")
	}
	if !isSelfProxy("http://localhost:18091") {
		t.Fatal("18091 仍在 MITM 顺延范围内，应识别为 self")
	}
	if isSelfProxy("http://127.0.0.1:7890") {
		t.Fatal("Clash 默认 7890 是合法上游，不应识别为 self")
	}
	if isSelfProxy("http://corp.proxy:8080") {
		t.Fatal("企业代理不应识别为 self")
	}
}
