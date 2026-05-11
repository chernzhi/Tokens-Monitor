# 从 sing-box config.json 中的 hysteria2 出站生成 iOS（Shadowrocket/Stash/Surge 等）可导入的 hy2 URI
# 用法: .\generate_ios_hysteria_uri.ps1 [-ConfigPath 'D:\proxifier\sing-box\config.json'] [-PrintOnly]
# 默认把链接写入配置文件同目录 iphone-hysteria2-导入链接.txt（含敏感信息，请勿上传网盘）

param(
    [string] $ConfigPath = 'D:\proxifier\sing-box\config.json',
    [switch] $PrintOnly
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

function Convert-ToHysteriaPortSpec {
    param([object] $ServerPortNode, [object] $ServerPortsNode)

    function Convert-SingleSegment([string] $s) {
        $t = $s.Trim()
        if ($t -match '^(\d+):(\d+)$') { return "$( $matches[1] )-$($matches[2])" }
        if ($t -match '^[\d\-,]+$') { return $t }
        throw "Unhandled server_ports entry: $t"
    }

    if ($ServerPortsNode -and @($ServerPortsNode).Count -gt 0) {
        $segs = @()
        foreach ($p in @($ServerPortsNode)) {
            $segs += (Convert-SingleSegment -s ([string] $p))
        }
        return ($segs -join ',')
    }
    if ($null -ne $ServerPortNode -and '' -ne "$ServerPortNode") {
        return "$ServerPortNode"
    }
    return '443'
}

if (-not (Test-Path $ConfigPath)) {
    Write-Error "配置文件不存在: $ConfigPath"
    exit 2
}

$raw = Get-Content -LiteralPath $ConfigPath -Raw -Encoding UTF8
$obj = $raw | ConvertFrom-Json

$hy = $obj.outbounds | Where-Object { $_.type -eq 'hysteria2' } | Select-Object -First 1
if (-not $hy) {
    Write-Error '未在 outbounds 中找到 type 为 hysteria2 的项。'
    exit 3
}

$server = [string]$hy.server
if ([string]::IsNullOrWhiteSpace($server)) { throw 'hysteria2.server 为空。' }

$sni = $server
if ($hy.tls -and $hy.tls.server_name) { $sni = [string]$hy.tls.server_name }

$pwd = [string]$hy.password

$singlePortProp = $hy.PSObject.Properties['server_port']
$multiPortProp = $hy.PSObject.Properties['server_ports']
$portsSpec = Convert-ToHysteriaPortSpec `
    -ServerPortsNode $( if ($multiPortProp) { $multiPortProp.Value } ) `
    -ServerPortNode $( if ($singlePortProp) { $singlePortProp.Value } )

$obfs = ''
$obfsPwd = ''
if ($hy.obfs -and $hy.obfs.type) { $obfs = [string]$hy.obfs.type }
if ($hy.obfs -and $hy.obfs.password) { $obfsPwd = [string]$hy.obfs.password }

$insecureFlag = '0'
if ($hy.tls) {
    $a1 = $hy.tls.PSObject.Properties['allow_insecure']
    $a2 = $hy.tls.PSObject.Properties['insecure']
    if ($a1 -and $a1.Value -eq $true) { $insecureFlag = '1' }
    elseif ($a2 -and $a2.Value -eq $true) { $insecureFlag = '1' }
}

$pairs = New-Object System.Collections.Generic.List[string]
$pairs.Add(("sni={0}") -f [uri]::EscapeDataString($sni))
$pairs.Add("insecure=$insecureFlag")
if ($obfs -and $obfs -ne '') {
    $pairs.Add(("obfs={0}") -f [uri]::EscapeDataString($obfs))
}
if ($obfsPwd -and $obfsPwd -ne '') {
    $pairs.Add(("obfs-password={0}") -f [uri]::EscapeDataString($obfsPwd))
}
$query = $pairs -join '&'

# userinfo 含特殊字符时需百分号编码（与 Hysteria 官方 URI 说明一致）
$authorityUser = [uri]::EscapeDataString($pwd)

$full = "hysteria2://${authorityUser}@${server}:$portsSpec/?${query}"

$shadowRocket = "shadowrocket://add/url=$([Uri]::EscapeDataString($full))"
$ts = Get-Date -Format 'yyyy-MM-dd HH:mm:ss'
$cfgResolved = Resolve-Path -LiteralPath $ConfigPath

$lines = @"
========================================
iPhone 使用方法（执行本脚本后即可按下面步骤操作）：
1）App Store / TestFlight 安装支持 Hy2 的客户端（如 Shadowrocket、Stash、Surge）。
2）复制下面「整行导入链接」（从 hysteria2:// 开始到行尾）。
3）打开客户端 → 导入或「从剪贴板导入」，也可手动添加 hysteria 节点粘贴整行 URL。
4）连接失败时请检查服务端、蜂窝/Wi‑Fi、运营商对 UDP 的限制/VPN。

注意：iPhone 无法像在电脑上那样运行 sing-box 可执行文件，必须用支持的客户端 + 下方链接导入。

[导入链接]
$full

[Shadowrocket URL Scheme（部分版本可用）]
$shadowRocket

[生成时间] $ts
[来源配置] $cfgResolved
========================================
"@

Write-Host $lines -ForegroundColor Cyan

$outTxt = Join-Path (Split-Path -Parent $ConfigPath) 'iphone-hysteria2-导入链接.txt'

if (-not $PrintOnly) {
    Set-Content -LiteralPath $outTxt -Value $lines -Encoding UTF8
    Write-Host ""
    Write-Host "已写入: $outTxt" -ForegroundColor Green
    Write-Host "请勿把该 txt 上传到公开网页或分享给他人。"
}
