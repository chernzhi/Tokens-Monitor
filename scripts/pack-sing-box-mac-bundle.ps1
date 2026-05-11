# Pack sing-box Mac bundle to E:\sing-box-mac-bundle
$ErrorActionPreference = 'Stop'
$dest = 'E:\sing-box-mac-bundle'
if (-not (Test-Path 'E:\')) {
    Write-Error 'E: drive not found. Plug in or map the disk first.'
    exit 1
}
New-Item -ItemType Directory -Force -Path $dest | Out-Null
Copy-Item -Force 'd:\proxifier\sing-box\config.json' (Join-Path $dest 'config.json')

$rel = Invoke-RestMethod -Uri 'https://api.github.com/repos/SagerNet/sing-box/releases/latest' `
    -Headers @{ 'User-Agent' = 'sing-box-bundle-script' }

$amd64Asset = $rel.assets | Where-Object { $_.name -match '^sing-box-.+-darwin-amd64\.tar\.gz$' }
$arm64Asset = $rel.assets | Where-Object { $_.name -match '^sing-box-.+-darwin-arm64\.tar\.gz$' }
if (-not $amd64Asset -or -not $arm64Asset) {
    throw 'Could not find darwin tarballs in latest release'
}
Write-Host ('Version: ' + $rel.tag_name)
Write-Host ('AMD64: ' + $amd64Asset.name)
Write-Host ('ARM64: ' + $arm64Asset.name)

Invoke-WebRequest -Uri $amd64Asset.browser_download_url -OutFile (Join-Path $dest 'darwin-amd64.tar.gz') -UseBasicParsing
Invoke-WebRequest -Uri $arm64Asset.browser_download_url -OutFile (Join-Path $dest 'darwin-arm64.tar.gz') -UseBasicParsing

Push-Location $dest
try {
    tar -xzf 'darwin-amd64.tar.gz'
    tar -xzf 'darwin-arm64.tar.gz'
    $dAmd = Get-ChildItem -Directory | Where-Object { $_.Name -match '^sing-box-.+-darwin-amd64$' } | Select-Object -First 1
    $dArm = Get-ChildItem -Directory | Where-Object { $_.Name -match '^sing-box-.+-darwin-arm64$' } | Select-Object -First 1
    if (-not $dAmd -or -not $dArm) { throw 'Unexpected archive layout after extract' }
    Copy-Item (Join-Path $dAmd.FullName 'sing-box') (Join-Path $dest 'sing-box-amd64') -Force
    Copy-Item (Join-Path $dArm.FullName 'sing-box') (Join-Path $dest 'sing-box-arm64') -Force
    Remove-Item $dAmd.FullName -Recurse -Force
    Remove-Item $dArm.FullName -Recurse -Force
    Remove-Item (Join-Path $dest 'darwin-amd64.tar.gz') -Force
    Remove-Item (Join-Path $dest 'darwin-arm64.tar.gz') -Force
}
finally {
    Pop-Location
}

$runSh = @'
#!/bin/bash
cd "$(dirname "$0")"
ARCH=$(uname -m)
case "$ARCH" in
  arm64) BIN="./sing-box-arm64" ;;
  x86_64) BIN="./sing-box-amd64" ;;
  *)
    echo "Unsupported architecture: $ARCH"
    exit 1
    ;;
esac
exec "$BIN" run -c "$(pwd)/config.json"
'@
# UTF8 no BOM for bash on Mac
$utf8 = New-Object System.Text.UTF8Encoding $false
[System.IO.File]::WriteAllText((Join-Path $dest 'run.sh'), $runSh, $utf8)

$runCmd = @'
#!/bin/bash
cd "$(dirname "$0")"
chmod +x sing-box-amd64 sing-box-arm64 run.sh 2>/dev/null || true
ARCH=$(uname -m)
case "$ARCH" in
  arm64) BIN="./sing-box-arm64" ;;
  x86_64) BIN="./sing-box-amd64" ;;
  *)
    echo "Unsupported architecture: $ARCH"
    read -rp "Press Enter to close..."
    exit 1
    ;;
esac
"$BIN" run -c "$(pwd)/config.json"
echo
read -rp "Stopped. Press Enter to close..."
'@
[System.IO.File]::WriteAllText((Join-Path $dest '运行代理.command'), $runCmd, $utf8)

$readme = @"
sing-box Mac 便携包（与 Windows 共用同一份 config.json）
版本：$($rel.tag_name)
本机端口：mixed 监听 127.0.0.1:9099（与 Proxifier / 浏览器转发一致）

=== 在 Mac 上使用 ===

方法一（推荐）：双击「运行代理.command」
  • 首次可能提示「来自未验证开发者」：系统设置 → 隐私与安全性 → 仍要打开；
    或对终端执行：xattr -cr （将此文件夹拖到终端里得到路径）

方法二：终端进入本文件夹后执行：
  chmod +x sing-box-amd64 sing-box-arm64 run.sh
  ./run.sh

停止：在该终端窗口按 Ctrl+C。

=== 注意事项 ===

1. 本包内含 Intel (amd64) 与 Apple 硅 (arm64) 两份官方二进制，脚本会自动选用。
2. 配置里规则集从网上拉取；首次启动需可访问 GitHub RAW（或可联网）。
3. 勿将本文件夹上传至公开场合；内含与你账号相关的配置。
4. E 盘若 exFAT 在 Mac 上可能无法保留 UNIX 可执行权限，若双击无效请用方法二并在终端 chmod +x。

打包时间：$(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')
"@

[System.IO.File]::WriteAllText((Join-Path $dest '使用说明.txt'), $readme, [System.Text.Encoding]::UTF8)

Write-Host 'Done:'
Get-ChildItem $dest | Format-Table Name, Length -AutoSize
