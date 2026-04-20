#!/usr/bin/env pwsh
<#
.SYNOPSIS
    仅部署 backend/frontend 容器并跑迁移，不上传 VSIX 与 ai-monitor.exe（与「第四点·更新 API」对应）。

.EXAMPLE
    $env:SSH_PASS = '你的SSH密码'
    .\scripts\deploy-backend.ps1

.NOTES
    默认远程：SSH_HOST（默认 192.168.0.135）、SSH_USER（默认 root）。
    若服务器不是该地址：$env:SSH_HOST = 'x.x.x.x'
#>

param(
    [switch]$DryRun
)

$ErrorActionPreference = 'Stop'
$RepoRoot = Split-Path -Parent $PSScriptRoot

if (-not (Get-Command python -ErrorAction SilentlyContinue) -and -not (Get-Command py -ErrorAction SilentlyContinue)) {
    throw '未找到 python / py，请先安装 Python 并加入 PATH。'
}

if (-not $env:SSH_PASS -or $env:SSH_PASS.Trim() -eq '') {
    Write-Host '[提示] 未检测到环境变量 SSH_PASS，将在下一步安全输入服务器密码（用于 SSH）' -ForegroundColor Yellow
    $secure = Read-Host '请输入 SSH 密码 (SSH_PASS)' -AsSecureString
    $ptr = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($secure)
    try {
        $env:SSH_PASS = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($ptr)
    } finally {
        [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($ptr) | Out-Null
    }
}

$deployPy = Join-Path $RepoRoot 'scripts\deploy.py'
Push-Location $RepoRoot
try {
    if ($DryRun) {
        Write-Host '[DryRun] 将执行: python scripts/deploy.py all --no-artifacts' -ForegroundColor Cyan
        exit 0
    }
    if (Get-Command python -ErrorAction SilentlyContinue) {
        python $deployPy 'all' '--no-artifacts'
    } else {
        py -3 $deployPy 'all' '--no-artifacts'
    }
    exit $LASTEXITCODE
} finally {
    Pop-Location
}
