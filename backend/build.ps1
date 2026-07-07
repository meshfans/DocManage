# Doc Server Build Script
# Pipeline:
#   1. Resolve version info (git describe / manual / dev default)
#   2. pnpm install (if needed)
#   3. pnpm build (frontend)
#   4. Copy frontend dist to server\web\dist
#   5. go build (with -ldflags version injection + -s -w compression)
#
# Usage:
#   .\build.ps1                  # default (dev version)
#   .\build.ps1 -Version 1.2.0   # specify version
#   .\build.ps1 -Clean           # clean before build
#   .\build.ps1 -SkipFrontend    # skip frontend (already built)
#
# Output: bin\doc-server.exe (with version info embedded)

[CmdletBinding()]
param(
    [string]$Version = "",
    [switch]$Clean,
    [switch]$SkipFrontend,
    [switch]$SkipInstall
)

$ErrorActionPreference = "Stop"
$ProgressPreference = "Continue"

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $ScriptDir

# ==================== Banner ====================
Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  Doc Server Build Pipeline" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

# ==================== Step 0: Clean ====================
if ($Clean) {
    Write-Host "[0/6] Cleaning build artifacts..." -ForegroundColor Yellow
    $pathsToClean = @(
        (Join-Path $ScriptDir "bin\doc-server.exe"),
        (Join-Path $ScriptDir "bin\doc-server-pure.exe"),
        (Join-Path $ScriptDir "web\dist")
    )
    foreach ($p in $pathsToClean) {
        if (Test-Path $p) {
            Remove-Item -Recurse -Force $p
            Write-Host "    Removed: $p" -ForegroundColor Gray
        }
    }
    Write-Host ""
}

# ==================== Step 1: Version Info ====================
Write-Host "[1/6] Resolving version info..." -ForegroundColor Yellow

# Version resolution (no git calls; git was removed per ops request):
#   - If user passes -Version "1.2.0", use it.
#   - Otherwise fall back to "dev".
# GitCommit is always "unknown" (no git rev-parse).
$GitCommit = "unknown"
if ([string]::IsNullOrEmpty($Version)) {
    $Version = "dev"
}

# Build time (RFC3339)
$BuildTime = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")

# Sanitize for ldflags injection
$VersionSafe = $Version -replace '[^A-Za-z0-9._-]', '_'
$GitCommitSafe = $GitCommit -replace '[^A-Za-z0-9]', '_'
$BuildTimeSafe = $BuildTime

Write-Host "    Version   : $Version" -ForegroundColor Gray
Write-Host "    GitCommit : $GitCommit" -ForegroundColor Gray
Write-Host "    BuildTime : $BuildTime" -ForegroundColor Gray
Write-Host ""

# ==================== Step 2: pnpm install (optional) ====================
# Frontend directory is resolved relative to the script location (no hard-coded D:\ paths)
$frontDir = Join-Path $ScriptDir "..\frontend"
$pnpmLock = Join-Path $frontDir "pnpm-lock.yaml"
$nodeModules = Join-Path $frontDir "node_modules"

if (-not $SkipInstall) {
    $needInstall = $false
    if (-not (Test-Path $nodeModules)) {
        $needInstall = $true
    } elseif ((Test-Path $pnpmLock) -and (Get-Item $pnpmLock).LastWriteTime -gt (Get-Item $nodeModules).LastWriteTime) {
        $needInstall = $true
    }

    if ($needInstall) {
        Write-Host "[2/6] Installing frontend dependencies..." -ForegroundColor Yellow
        Push-Location $frontDir
        try {
            pnpm install --frozen-lockfile
            if ($LASTEXITCODE -ne 0) {
                Write-Host "    --frozen-lockfile failed, retrying without..." -ForegroundColor DarkYellow
                pnpm install
                if ($LASTEXITCODE -ne 0) { throw "pnpm install failed" }
            }
        } finally {
            Pop-Location
        }
        Write-Host "    Dependencies installed" -ForegroundColor Gray
    } else {
        Write-Host "[2/6] Frontend dependencies up-to-date, skipping" -ForegroundColor Yellow
    }
} else {
    Write-Host "[2/6] Skipped (SkipInstall)" -ForegroundColor DarkYellow
}
Write-Host ""

# ==================== Step 3: Frontend build ====================
if (-not $SkipFrontend) {
    Write-Host "[3/6] Building frontend (pnpm build)..." -ForegroundColor Yellow
    Push-Location $frontDir
    try {
        pnpm build
        if ($LASTEXITCODE -ne 0) { throw "Frontend build failed" }
    } finally {
        Pop-Location
    }
    Write-Host "    Frontend built" -ForegroundColor Gray
} else {
    Write-Host "[3/6] Skipped frontend (SkipFrontend)" -ForegroundColor DarkYellow
}
Write-Host ""

# ==================== Step 4: Copy frontend dist ====================
Write-Host "[4/6] Copying frontend dist to backend\web\dist..." -ForegroundColor Yellow
$webDistDir = Join-Path $ScriptDir "web\dist"
$frontDistDir = Join-Path $frontDir "dist"
if (-not (Test-Path $frontDistDir)) {
    throw "Frontend dist not found: $frontDistDir (run without -SkipFrontend)"
}
if (Test-Path $webDistDir) {
    Remove-Item -Recurse -Force $webDistDir
}
New-Item -ItemType Directory -Path $webDistDir -Force | Out-Null
Copy-Item -Path "$frontDistDir\*" -Destination $webDistDir -Recurse -Force
$distSize = (Get-ChildItem -Path $webDistDir -Recurse -File | Measure-Object -Property Length -Sum).Sum / 1MB
Write-Host "    Copied $([math]::Round($distSize, 2)) MB frontend assets" -ForegroundColor Gray
Write-Host ""

# ==================== Step 5: Go build ====================
Write-Host "[5/6] Building Go backend (with version injection)..." -ForegroundColor Yellow
$binDir = Join-Path $ScriptDir "bin"
if (-not (Test-Path $binDir)) {
    New-Item -ItemType Directory -Path $binDir -Force | Out-Null
}

$env:CGO_ENABLED = "1"
$ldflags = @(
    "-s",
    "-w",
    "-X main.Version=$VersionSafe",
    "-X main.GitCommit=$GitCommitSafe",
    "-X main.BuildTime=$BuildTimeSafe"
) -join " "

$exePath = Join-Path $binDir "doc-server.exe"
Write-Host "    ldflags: $ldflags" -ForegroundColor DarkGray
Write-Host "    output : $exePath" -ForegroundColor DarkGray

# Build from server directory (go.work is in root)
Push-Location $ScriptDir
try {
    go build -trimpath -ldflags $ldflags -o $exePath main.go
    if ($LASTEXITCODE -ne 0) { throw "Go build failed" }
} finally {
    Pop-Location
}
Write-Host ""

# ==================== Step 6: Report ====================
Write-Host "[6/6] Build summary" -ForegroundColor Yellow
$fileInfo = Get-Item $exePath
$sizeMB = [math]::Round($fileInfo.Length / 1MB, 2)
Write-Host "    Executable : $($fileInfo.Name)" -ForegroundColor White
Write-Host "    Size       : $sizeMB MB" -ForegroundColor White
Write-Host "    Version    : $Version" -ForegroundColor White
Write-Host "    Commit     : $GitCommit" -ForegroundColor White
Write-Host "    Built at   : $BuildTime" -ForegroundColor White
Write-Host ""

# Write build metadata to bin\build-info.json (for downstream steps)
$buildInfo = @{
    version    = $Version
    git_commit = $GitCommit
    build_time = $BuildTime
    go_exe     = $exePath
    go_size_mb = $sizeMB
}
$buildInfoJson = $buildInfo | ConvertTo-Json
$buildInfoPath = Join-Path $binDir "build-info.json"
$buildInfoJson | Out-File -FilePath $buildInfoPath -Encoding UTF8
Write-Host "    Build info: $buildInfoPath" -ForegroundColor DarkGray
Write-Host ""

Write-Host "========================================" -ForegroundColor Green
Write-Host "  Server build complete!" -ForegroundColor Green
Write-Host "========================================" -ForegroundColor Green
Write-Host ""
