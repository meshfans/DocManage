# DocManageTrail one-click release script
# Pipeline:
#   1. frontend\pnpm install + pnpm build        -> frontend\dist\
#   2. copy frontend\dist -> backend\web\dist
#   3. backend\go build (-ldflags version)        -> backend\bin\doc-server.exe
#   4. copy doc-server.exe -> dist\DocServer-<ver>-<date>.exe + .sha256
#
# Note: post round7 simplification, no Tauri client / rust-loader wrapper.
#
# Usage (from repo root):
#   .\package.ps1                       # default (git describe auto-resolves version)
#   .\package.ps1 -Version 1.2.0        # specify version
#   .\package.ps1 -Clean                # clean bin\doc-server.exe + web\dist first
#   .\package.ps1 -SkipInstall          # skip pnpm install (deps already installed)
#
# Output (in dist\):
#   DocServer-<version>-<yyyymmdd>.exe         # backend single exe (frontend embedded via embed.FS)
#   DocServer-<version>-<yyyymmdd>.exe.sha256  # per-file checksum
#   SHA256SUMS.txt                             # aggregated sha256 list

[CmdletBinding()]
param(
    [string]$Version = "",
    [switch]$Clean,
    [switch]$SkipInstall
)

$ErrorActionPreference = "Stop"
$ProgressPreference = "Continue"

$RootDir = $PSScriptRoot
Set-Location $RootDir

# ==================== Banner ====================
Write-Host ""
Write-Host "######################################################" -ForegroundColor Magenta
Write-Host "#  DocManageTrail Release                                " -ForegroundColor Magenta
Write-Host "#  Pipeline: frontend build -> go build -> single exe   " -ForegroundColor Magenta
Write-Host "######################################################" -ForegroundColor Magenta
Write-Host ""

# ==================== Step 0: Clean (optional) ====================
if ($Clean) {
    Write-Host "[0/4] Cleaning build artifacts..." -ForegroundColor Yellow
    $pathsToClean = @(
        (Join-Path $RootDir "backend\bin\doc-server.exe"),
        (Join-Path $RootDir "backend\web\dist"),
        (Join-Path $RootDir "frontend\dist")
    )
    foreach ($p in $pathsToClean) {
        if (Test-Path $p) {
            Remove-Item -Recurse -Force $p
            Write-Host "    Removed: $p" -ForegroundColor Gray
        }
    }
    Write-Host ""
}

# ==================== Step 1: Frontend Build ====================
Write-Host "[1/4] Building frontend (pnpm build)..." -ForegroundColor Cyan
Write-Host "------------------------------------------------------" -ForegroundColor DarkGray

$frontDir = Join-Path $RootDir "frontend"
if (-not (Test-Path (Join-Path $frontDir "package.json"))) {
    throw "frontend\package.json not found: $frontDir"
}

Push-Location $frontDir
try {
    if (-not $SkipInstall) {
        Write-Host "    pnpm install..." -ForegroundColor Gray
        pnpm install --frozen-lockfile 2>&1 | Out-Null
        if ($LASTEXITCODE -ne 0) {
            Write-Host "    --frozen-lockfile failed, retrying without..." -ForegroundColor DarkYellow
            pnpm install 2>&1 | Out-Null
            if ($LASTEXITCODE -ne 0) { throw "pnpm install failed" }
        }
    }
    Write-Host "    pnpm build..." -ForegroundColor Gray
    pnpm build 2>&1 | Out-Null
    if ($LASTEXITCODE -ne 0) { throw "pnpm build failed" }
} finally {
    Pop-Location
}
$frontDistSize = (Get-ChildItem -Path (Join-Path $frontDir "dist") -Recurse -File -ErrorAction SilentlyContinue | Measure-Object -Property Length -Sum).Sum / 1MB
Write-Host ("    frontend dist built: " + [math]::Round($frontDistSize, 2) + " MB") -ForegroundColor Gray
Write-Host ""

# ==================== Step 2: Copy frontend dist -> backend\web\dist ====================
Write-Host "[2/4] Copying frontend dist -> backend\web\dist..." -ForegroundColor Cyan
Write-Host "------------------------------------------------------" -ForegroundColor DarkGray

$webDistDir = Join-Path $RootDir "backend\web\dist"
$frontDistDir = Join-Path $frontDir "dist"
if (-not (Test-Path $frontDistDir)) {
    throw "frontend dist not found: $frontDistDir"
}
if (Test-Path $webDistDir) {
    Remove-Item -Recurse -Force $webDistDir
}
New-Item -ItemType Directory -Path $webDistDir -Force | Out-Null
Copy-Item -Path "$frontDistDir\*" -Destination $webDistDir -Recurse -Force
Write-Host "    Copied frontend assets" -ForegroundColor Gray
Write-Host ""

# ==================== Step 3: Backend Go Build ====================
Write-Host "[3/4] Building backend (Go build with version injection)..." -ForegroundColor Cyan
Write-Host "------------------------------------------------------" -ForegroundColor DarkGray

$backendDir = Join-Path $RootDir "backend"
if (-not (Test-Path (Join-Path $backendDir "go.mod"))) {
    throw "backend\go.mod not found: $backendDir"
}

# Resolve version info (no git calls; git was removed per ops request):
#   - If user passes -Version "1.2.0", use it.
#   - Otherwise fall back to "dev".
# GitCommit is always "unknown" (no git rev-parse).
$GitCommit = "unknown"
if ([string]::IsNullOrEmpty($Version)) {
    $Version = "dev"
}
$BuildTime = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")

$VersionSafe  = $Version     -replace '[^A-Za-z0-9._-]', '_'
$GitCommitSafe = $GitCommit   -replace '[^A-Za-z0-9]',         '_'
$BuildTimeSafe = $BuildTime

$env:CGO_ENABLED = "1"
$ldflags = @(
    "-s",
    "-w",
    "-X main.Version=$VersionSafe",
    "-X main.GitCommit=$GitCommitSafe",
    "-X main.BuildTime=$BuildTimeSafe"
) -join " "

$binDir = Join-Path $backendDir "bin"
if (-not (Test-Path $binDir)) {
    New-Item -ItemType Directory -Path $binDir -Force | Out-Null
}
$exePath = Join-Path $binDir "doc-server.exe"

Push-Location $backendDir
try {
    go build -trimpath -ldflags $ldflags -o $exePath main.go
    if ($LASTEXITCODE -ne 0) { throw "go build failed" }
} finally {
    Pop-Location
}

$fileInfo = Get-Item $exePath
$sizeMB = [math]::Round($fileInfo.Length / 1MB, 2)
$sha256 = (Get-FileHash -Path $exePath -Algorithm SHA256).Hash.ToLower()
Write-Host ("    Built: " + $fileInfo.Name + " (" + $sizeMB + " MB)") -ForegroundColor Gray
Write-Host ("    Version: " + $Version + "  Commit: " + $GitCommit + "  Built: " + $BuildTime) -ForegroundColor Gray

# Persist build-info.json (for downstream consumers)
$buildInfo = @{
    version    = $Version
    git_commit = $GitCommit
    build_time = $BuildTime
    go_exe     = $exePath
    go_size_mb = $sizeMB
} | ConvertTo-Json
$buildInfo | Out-File -FilePath (Join-Path $binDir "build-info.json") -Encoding UTF8
Write-Host ""

# ==================== Step 4: Copy -> dist\ ====================
Write-Host "[4/4] Copying to dist\ ..." -ForegroundColor Cyan
Write-Host "------------------------------------------------------" -ForegroundColor DarkGray

$distDir = Join-Path $RootDir "dist"
if (-not (Test-Path $distDir)) {
    New-Item -ItemType Directory -Path $distDir -Force | Out-Null
}
# Default: clean dist before writing (use -NoClean switch if you want to append)
if (Get-ChildItem $distDir -Force -ErrorAction SilentlyContinue) {
    Get-ChildItem $distDir -Force | Remove-Item -Recurse -Force -ErrorAction SilentlyContinue
}

$date = (Get-Date).ToString("yyyyMMdd")
$distName = "DocServer-" + $VersionSafe + "-" + $date + ".exe"
$distExe = Join-Path $distDir $distName

Copy-Item -Path $exePath -Destination $distExe -Force
$distSha = (Get-FileHash -Path $distExe -Algorithm SHA256).Hash.ToLower()
($distSha + "  " + $distName) | Out-File -FilePath ($distExe + ".sha256") -Encoding UTF8

# Master SHA256SUMS
$masterSums = Join-Path $distDir "SHA256SUMS.txt"
Get-ChildItem $distDir -Filter "*.sha256" | ForEach-Object {
    Get-Content $_.FullName
} | Out-File -FilePath $masterSums -Encoding UTF8

Write-Host ("    " + $distName + " (" + $sizeMB + " MB)") -ForegroundColor White
Write-Host ("      SHA256: " + $distSha) -ForegroundColor Gray
Write-Host ("    SHA256SUMS: " + $masterSums) -ForegroundColor Gray
Write-Host ""

# ==================== Done ====================
Write-Host "######################################################" -ForegroundColor Green
Write-Host "#  Build Complete!                                      " -ForegroundColor Green
Write-Host "######################################################" -ForegroundColor Green
Write-Host ""
Write-Host "Final dist contents:" -ForegroundColor White
Get-ChildItem $distDir | Where-Object { -not $_.PSIsContainer } | ForEach-Object {
    $sz = [math]::Round($_.Length / 1MB, 2)
    Write-Host ("    " + $_.Name + " (" + $sz + " MB)") -ForegroundColor Gray
}
Write-Host ""
Write-Host "Done." -ForegroundColor Green