# Start backend in development mode.
# Port: 8090 (与生产一致).
# Config: bin/config.test-main.json.
# Database / uploads / logs / backups all live under bin/.
$p = Get-NetTCPConnection -LocalPort 8090 -State Listen -ErrorAction SilentlyContinue
if ($p) { Stop-Process -Id $p.OwningProcess -Force -ErrorAction SilentlyContinue; Write-Host "killed old 8090" }
Set-Location $PSScriptRoot
$env:CONFIG_FILE = "$PSScriptRoot\bin\config.test-main.json"

# Issue M-5：JWT_SECRET 兜底仅用于本地开发。
# 生产误用此脚本会签发可预测的 JWT —— 所有 token 形同公开。
# 检测到兜底命中时打印醒目的红字横幅，避免静默启用。
$usingFallback = $false
if (-not $env:JWT_SECRET) {
    $env:JWT_SECRET = "dev_only_local_secret_at_least_32_chars"
    $usingFallback = $true
}
if ($usingFallback) {
    Write-Host ""
    Write-Host "================================================================" -ForegroundColor Red
    Write-Host "  [WARN] 使用硬编码 dev JWT_SECRET" -ForegroundColor Red
    Write-Host "         生产部署必须设置环境变量 \$env:JWT_SECRET 为高强度随机串" -ForegroundColor Red
    Write-Host "         当前密钥已知（来自 backend/run.ps1）—— 不可用于任何可信环境" -ForegroundColor Red
    Write-Host "================================================================" -ForegroundColor Red
    Write-Host ""
}

go run ./main.go