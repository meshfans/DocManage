# J.8 检查：业务代码不得直接调用 utils.IncBusinessEvent。
# 应改用 services.PublishEvent，事件总线经 metricsSubscriber 同步到该指标。
#
# 使用：pwsh backend/scripts/check_no_legacy_eventbus.ps1
# 退出码：0 = 通过；1 = 发现违规

$ErrorActionPreference = "Stop"

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$backendDir = Split-Path -Parent $scriptDir
Set-Location $backendDir

# 白名单：services/eventbus.go 是内置兼容桥接，允许
$excludedFile = Join-Path $backendDir "services/eventbus.go"

# 扫描所有 .go 文件
$goFiles = Get-ChildItem -Path $backendDir -Recurse -Filter "*.go"
$violations = @()

foreach ($file in $goFiles) {
    if ($file.FullName -eq $excludedFile) { continue }

    $content = Get-Content $file.FullName -ErrorAction SilentlyContinue
    for ($i = 0; $i -lt $content.Count; $i++) {
        if ($content[$i] -match "utils\.IncBusinessEvent") {
            $violations += [PSCustomObject]@{
                File = $file.FullName
                Line = $i + 1
                Text = $content[$i].Trim()
            }
        }
    }
}

if ($violations.Count -gt 0) {
    Write-Host "❌ J.8 发现 utils.IncBusinessEvent 业务代码直接调用：" -ForegroundColor Red
    foreach ($v in $violations) {
        $relPath = $v.File.Substring($backendDir.Length + 1)
        Write-Host "  $relPath`:$($v.Line)  $($v.Text)" -ForegroundColor Yellow
    }
    Write-Host ""
    Write-Host "请改用 services.PublishEvent（事件总线会自动同步到 business_events_total 指标）"
    exit 1
}

Write-Host "✅ J.8 通过：业务代码 0 命中 utils.IncBusinessEvent" -ForegroundColor Green
exit 0
