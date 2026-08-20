# K.5：config.json 与 config.test-main.json 顶层 key 一致性校验。
#
# Phase 4b 把 bin/config.json jwt.secret 清空，但 test-main 仍保留 secret。
# 结构 diff 未测试——下次改动可能两边漂移。
# 本脚本只校验"key 集合"一致（顶层 + 嵌套一层），值差异属正常。
#
# 使用：pwsh backend/scripts/diff_configs.ps1
# 退出码：0 = 一致；1 = 不一致

$ErrorActionPreference = "Stop"

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$backendDir = Split-Path -Parent $scriptDir

$prodConfig = Join-Path $backendDir "bin\config.json"
$devConfig  = Join-Path $backendDir "bin\config.test-main.json"

if (-not (Test-Path $prodConfig)) {
    Write-Host "❌ 缺少 $prodConfig" -ForegroundColor Red
    exit 1
}
if (-not (Test-Path $devConfig)) {
    Write-Host "❌ 缺少 $devConfig" -ForegroundColor Red
    exit 1
}

# 提取顶层 key（递归一层）
function Get-Keys {
    param([string]$Path)

    $json = Get-Content $Path -Raw | ConvertFrom-Json
    $keys = @()
    foreach ($prop in $json.PSObject.Properties) {
        $keys += $prop.Name
        # 如果值是嵌套对象，追加 child keys
        if ($prop.Value -is [PSCustomObject]) {
            foreach ($child in $prop.Value.PSObject.Properties) {
                $keys += "$($prop.Name).$($child.Name)"
            }
        }
    }
    return $keys | Sort-Object
}

$prodKeys = Get-Keys $prodConfig
$devKeys  = Get-Keys $devConfig

# 顶层 key
$prodTop  = $prodKeys | Where-Object { -not $_.Contains('.') }
$devTop   = $devKeys  | Where-Object { -not $_.Contains('.') }
$missingInDev = $prodTop | Where-Object { $_ -notin $devTop }
$missingInProd = $devTop | Where-Object { $_ -notin $prodTop }

if ($missingInDev.Count -eq 0 -and $missingInProd.Count -eq 0) {
    Write-Host "✅ K.5 通过：config.json 与 config.test-main.json 顶层 key 一致" -ForegroundColor Green
    exit 0
}

Write-Host "❌ K.5 不一致：" -ForegroundColor Red
if ($missingInDev.Count -gt 0) {
    Write-Host "  prod 有但 dev 缺：" -ForegroundColor Yellow
    foreach ($k in $missingInDev) { Write-Host "    - $k" }
}
if ($missingInProd.Count -gt 0) {
    Write-Host "  dev 有但 prod 缺：" -ForegroundColor Yellow
    foreach ($k in $missingInProd) { Write-Host "    - $k" }
}
Write-Host ""
Write-Host "差异通常是故意（生产环境省略 dev-only 字段），如确认无问题请同步两侧。"
exit 1
