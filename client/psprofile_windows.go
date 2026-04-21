package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// psProfileMarker 是写入 PowerShell Profile 的内容标识符，卸载时按此定位删除。
const psProfileMarker = "# ── AI Monitor 代理包装 (ai-monitor managed) ──"
const psProfileEndMarker = "# ── END AI Monitor 代理包装 ──"

// psProfileBlock 返回要写入 PowerShell Profile 的代理包装代码块。
func psProfileBlock(proxyAddr, caCertPath string) string {
	return fmt.Sprintf(`%s
# 此段由 ai-monitor --install 自动生成，卸载时自动移除。
# 让 claude / codex 等 Node.js CLI 工具走本地 MITM 代理，无需每次手动设置。
$_aiMonitorProxy  = "%s"
$_aiMonitorCACert = "%s"

function _Invoke-AIMonitorProxy {
    param([string]$Cmd, [string[]]$CmdArgs)
    $env:HTTPS_PROXY         = $_aiMonitorProxy
    $env:HTTP_PROXY          = $_aiMonitorProxy
    $env:ANTHROPIC_BASE_URL  = "$_aiMonitorProxy/anthropic"
    $env:OPENAI_BASE_URL     = "$_aiMonitorProxy/openai/v1"
    $env:OPENAI_API_BASE     = "$_aiMonitorProxy/openai/v1"
    if (Test-Path $_aiMonitorCACert) {
        $env:NODE_EXTRA_CA_CERTS = $_aiMonitorCACert
        $env:SSL_CERT_FILE       = $_aiMonitorCACert
    }
    & $Cmd @CmdArgs
    Remove-Item Env:HTTPS_PROXY        -ErrorAction SilentlyContinue
    Remove-Item Env:HTTP_PROXY         -ErrorAction SilentlyContinue
    Remove-Item Env:ANTHROPIC_BASE_URL -ErrorAction SilentlyContinue
    Remove-Item Env:OPENAI_BASE_URL    -ErrorAction SilentlyContinue
    Remove-Item Env:OPENAI_API_BASE    -ErrorAction SilentlyContinue
}

function claude { _Invoke-AIMonitorProxy "claude" $args }
function codex  { _Invoke-AIMonitorProxy "codex"  $args }
%s
`, psProfileMarker, proxyAddr, caCertPath, psProfileEndMarker)
}

// powershellProfilePath 返回当前用户的 PowerShell 5 和 PowerShell 7 Profile 路径。
func powershellProfilePaths() []string {
	docs := ""
	if out, err := exec.Command("powershell", "-NoProfile", "-Command",
		"[Environment]::GetFolderPath('MyDocuments')").Output(); err == nil {
		docs = strings.TrimSpace(string(out))
	}
	if docs == "" {
		docs = filepath.Join(os.Getenv("USERPROFILE"), "Documents")
	}

	return []string{
		filepath.Join(docs, "WindowsPowerShell", "Microsoft.PowerShell_profile.ps1"), // PS5
		filepath.Join(docs, "PowerShell", "Microsoft.PowerShell_profile.ps1"),        // PS7
	}
}

// InstallPowerShellProfile 向用户的 PowerShell Profile 注入代理包装函数。
// 幂等：已存在则跳过，不会重复写入。
func InstallPowerShellProfile(proxyAddr, caCertPath string) error {
	block := psProfileBlock(proxyAddr, caCertPath)
	var lastErr error
	installed := 0

	for _, profilePath := range powershellProfilePaths() {
		if err := os.MkdirAll(filepath.Dir(profilePath), 0755); err != nil {
			lastErr = err
			continue
		}

		existing := ""
		if data, err := os.ReadFile(profilePath); err == nil {
			existing = string(data)
		}

		// 已经有标记，跳过
		if strings.Contains(existing, psProfileMarker) {
			log.Printf("[psprofile] 已存在，跳过: %s", profilePath)
			installed++
			continue
		}

		newContent := existing
		if len(newContent) > 0 && !strings.HasSuffix(newContent, "\n") {
			newContent += "\n"
		}
		newContent += "\n" + block

		if err := os.WriteFile(profilePath, []byte(newContent), 0644); err != nil {
			lastErr = err
			log.Printf("[psprofile] 写入失败 %s: %v", profilePath, err)
			continue
		}
		log.Printf("[psprofile] 已写入: %s", profilePath)
		installed++
	}

	if installed == 0 && lastErr != nil {
		return lastErr
	}
	return nil
}

// RemovePowerShellProfile 从 PowerShell Profile 里移除代理包装代码块。
func RemovePowerShellProfile() {
	for _, profilePath := range powershellProfilePaths() {
		data, err := os.ReadFile(profilePath)
		if err != nil {
			continue
		}
		content := string(data)
		if !strings.Contains(content, psProfileMarker) {
			continue
		}

		start := strings.Index(content, psProfileMarker)
		end := strings.Index(content, psProfileEndMarker)
		if start == -1 || end == -1 || end < start {
			continue
		}
		end += len(psProfileEndMarker)
		// 也吃掉尾部换行
		for end < len(content) && (content[end] == '\n' || content[end] == '\r') {
			end++
		}

		newContent := content[:start] + content[end:]
		newContent = strings.TrimRight(newContent, "\r\n") + "\n"

		if err := os.WriteFile(profilePath, []byte(newContent), 0644); err != nil {
			log.Printf("[psprofile] 移除失败 %s: %v", profilePath, err)
			continue
		}
		log.Printf("[psprofile] 已移除: %s", profilePath)
	}
}
