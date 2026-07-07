# Start backend in development mode.
# Port: 8091 (differs from prod 8090).
# Config: bin/config.test-main.json.
# Database / uploads / logs / backups all live under bin/.
$p = Get-NetTCPConnection -LocalPort 8091 -State Listen -ErrorAction SilentlyContinue
if ($p) { Stop-Process -Id $p.OwningProcess -Force -ErrorAction SilentlyContinue; Write-Host "killed old 8091" }
Set-Location $PSScriptRoot
$env:CONFIG_FILE = "$PSScriptRoot\bin\config.test-main.json"
go run ./main.go