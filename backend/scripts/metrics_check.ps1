# metrics_check.ps1 - 自动抓取 /metrics 接口并验证 Round 16 指标
#
# Usage:
#   powershell -ExecutionPolicy Bypass -File backend/scripts/metrics_check.ps1
#
# Pre-req:
#   - Backend running via backend/run.ps1 on :8091
#
# Output:
#   - Console PASS/FAIL summary
#   - backend/scripts/metrics_check_<ts>.log (full log + first 50 metric lines)
#   - backend/scripts/metrics_check_<ts>.csv (results table)

$ErrorActionPreference = 'Continue'
$baseUrl  = 'http://localhost:8091'
$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
$OutDir   = $PSScriptRoot
$ts       = Get-Date -Format 'yyyyMMdd_HHmmss'
$logPath  = Join-Path $OutDir ('metrics_check_' + $ts + '.log')
$csvPath  = Join-Path $OutDir ('metrics_check_' + $ts + '.csv')

function Write-Both {
    param([string]$Msg, [string]$Color = '')
    if ($Color -ne '') { Write-Host $Msg -ForegroundColor $Color } else { Write-Host $Msg }
    Add-Content -Path $logPath -Value $Msg
}

function Record {
    param(
        [string]$Id,
        [string]$Name,
        [string]$Req,
        [string]$Expected,
        [string]$Actual,
        [string]$Result,
        [string]$Note = ''
    )
    $script:results += [PSCustomObject]@{
        Id = $Id; Name = $Name; Req = $Req; Expected = $Expected; Actual = $Actual; Result = $Result; Note = $Note
    }
    $color = if ($Result -eq 'PASS') { 'Green' } else { 'Red' }
    Write-Both ('[' + $Result + '] ' + $Id + ' - ' + $Name + ' : ' + $Actual) $color
}

function Get-Raw {
    param([string]$Path)
    try {
        $resp = Invoke-WebRequest -Uri ($baseUrl + $Path) -Method GET -TimeoutSec 10 -ErrorAction Stop
        return @{
            ok     = $true
            status = [int]$resp.StatusCode
            text   = $resp.Content
            ct     = $resp.Headers['Content-Type']
        }
    } catch {
        $code = $null
        $body = ''
        if ($_.Exception.Response) {
            $code = [int]$_.Exception.Response.StatusCode
            try {
                $stream = $_.Exception.Response.GetResponseStream()
                $reader = New-Object System.IO.StreamReader($stream)
                $body   = $reader.ReadToEnd()
                $reader.Close()
            } catch {
                # body still empty
            }
        }
        return @{ ok = $false; status = $code; text = $body; ct = $null }
    }
}

function Post-Raw {
    param([string]$Path, [string]$Body)
    try {
        $resp = Invoke-WebRequest -Uri ($baseUrl + $Path) -Method POST -ContentType 'application/json' -Body $Body -TimeoutSec 10 -ErrorAction Stop
        return @{ ok = $true; status = [int]$resp.StatusCode; text = $resp.Content }
    } catch {
        $code = $null
        $body = ''
        if ($_.Exception.Response) {
            $code = [int]$_.Exception.Response.StatusCode
            try {
                $stream = $_.Exception.Response.GetResponseStream()
                $reader = New-Object System.IO.StreamReader($stream)
                $body   = $reader.ReadToEnd()
                $reader.Close()
            } catch {
                # body still empty
            }
        }
        return @{ ok = $false; status = $code; text = $body }
    }
}

# Parse Prometheus text and return hashtable of metric series.
function Parse-Metric {
    param([string]$Text, [string]$Name)
    $out = @{}
    $lines = $Text -split "`n"
    $nameEscaped = [regex]::Escape($Name)
    $rx = '^\s*' + $nameEscaped + '(\{[^}]*\})?\s+([0-9eE+\-\.]+)\s*$'
    foreach ($line in $lines) {
        if ($line -match '^\s*#') { continue }
        if ($line -match '^\s*$') { continue }
        if ($line -match $rx) {
            $labelStr = $matches[1]
            $val      = [double]$matches[2]
            $key      = $Name
            if ($labelStr) { $key = $Name + $labelStr }
            $out[$key] = $val
        }
    }
    return $out
}

function Has-Metric-Decl {
    param([string]$Text, [string]$Name)
    $nameEscaped = [regex]::Escape($Name)
    $helpRx = '#\s+HELP\s+' + $nameEscaped + '\b'
    $typeRx = '#\s+TYPE\s+' + $nameEscaped + '\b'
    return (($Text -match $helpRx) -or ($Text -match $typeRx))
}

# ===== script body =====
Write-Both ('=== /metrics verification started ' + (Get-Date -Format 'yyyy-MM-dd HH:mm:ss') + ' ===') 'Cyan'
Write-Both ('Base URL : ' + $baseUrl)
Write-Both ('Log file : ' + $logPath)
Write-Both ('CSV file : ' + $csvPath)
Write-Both ''

$results = @()

# Step 0a: trigger a failed login so business_events_total{event="auth.login.failed"} appears.
Write-Both '[Step 0a] trigger one failed login (for auth.login.failed event)' 'Yellow'
$trig = Post-Raw '/api/login' '{"username":"admin","password":"metrics_check_wrong"}'
Record 'TRIG-01' 'trigger failed login (for metrics event)' 'POST /api/login' '401' ('status=' + $trig.status) $(if (-not $trig.ok -and $trig.status -eq 401) {'PASS'} else {'FAIL'})

# Step 0b: trigger a successful login so business_events_total{event="auth.login.success"} appears.
Write-Both '[Step 0b] trigger one successful login (for auth.login.success event)' 'Yellow'
$trigOk = Post-Raw '/api/login' '{"username":"admin","password":"admin123"}'
Record 'TRIG-02' 'trigger successful login (for metrics event)' 'POST /api/login' '200' ('status=' + $trigOk.status) $(if ($trigOk.ok -and $trigOk.status -eq 200) {'PASS'} else {'FAIL'})

# Step 1: health probes.
Write-Both ''
Write-Both '[Step 1] health probes' 'Yellow'
$hz = Get-Raw '/healthz'
Record 'HEALTH-01' 'healthz' 'GET /healthz' '200' ('status=' + $hz.status) $(if ($hz.ok -and $hz.status -eq 200) {'PASS'} else {'FAIL'})

$rd = Get-Raw '/readyz'
Record 'HEALTH-02' 'readyz' 'GET /readyz' '200' ('status=' + $rd.status) $(if ($rd.ok -and $rd.status -eq 200) {'PASS'} else {'FAIL'})

# Step 2: fetch /metrics.
Write-Both ''
Write-Both '[Step 2] fetch /metrics' 'Yellow'
$m = Get-Raw '/metrics'
Record 'METRICS-01' '/metrics HTTP 200' 'GET /metrics' '200' ('status=' + $m.status) $(if ($m.ok -and $m.status -eq 200) {'PASS'} else {'FAIL'})
Record 'METRICS-02' '/metrics Content-Type' 'text/plain; version=0.0.4' ('ct=' + $m.ct) ('ct=' + $m.ct) $(if (($m.ct -ne $null) -and ($m.ct -match 'text/plain.*version=0\.0\.4')) {'PASS'} else {'FAIL'})

if (-not $m.ok -or [string]::IsNullOrEmpty($m.text)) {
    Write-Both '/metrics fetch failed, aborting remaining checks' 'Red'
    $results | Export-Csv -Path $csvPath -NoTypeInformation -Encoding UTF8
    Write-Both ('CSV: ' + $csvPath) 'Cyan'
    exit 1
}

$metricsText = $m.text

Write-Both ''
Write-Both '[metrics sample (first 50 lines)]' 'DarkGray'
$metricsText -split "`n" | Select-Object -First 50 | ForEach-Object { Write-Both ('  ' + $_) 'DarkGray' }
Write-Both ''

# Step 3: Round 15 runtime / HTTP metrics.
Write-Both '[Step 3] Round 15 runtime + HTTP metrics' 'Yellow'
# Note: histograms emit `<name>_bucket`, `<name>_sum`, `<name>_count` series.
# Parse-Metric only matches the bare name. For histograms we instead assert the
# existence of the `*_bucket` series. Prometheus only emits `# HELP`/`# TYPE`
# lines for the histogram's bare name (not for `_bucket`), so the histogram
# check below only relies on series presence, not on Has-Metric-Decl.
$runtimeExpected = @(
    @{ Name = 'go_goroutines';                              IsHistogram = $false },
    @{ Name = 'go_memstats_alloc_bytes';                    IsHistogram = $false },
    @{ Name = 'go_memstats_sys_bytes';                      IsHistogram = $false },
    @{ Name = 'go_memstats_num_gc';                         IsHistogram = $false },
    @{ Name = 'process_start_time_seconds';                 IsHistogram = $false },
    @{ Name = 'http_requests_total';                        IsHistogram = $false },
    @{ Name = 'http_request_duration_seconds_bucket';       IsHistogram = $true  },
    @{ Name = 'http_in_flight_requests';                    IsHistogram = $false }
)
foreach ($item in $runtimeExpected) {
    $name = $item.Name
    $isHist = $item.IsHistogram
    $declared = Has-Metric-Decl $metricsText $name
    $series   = (Parse-Metric $metricsText $name).Count
    $actual   = ('declared=' + $declared + ' series=' + $series)
    if ($isHist) {
        # Histograms don't have a HELP/TYPE for `_bucket`; pass on series>=1 alone.
        $pass = ($series -gt 0)
    } else {
        $pass = ($declared -and $series -gt 0)
    }
    Record ('R15-' + $name) ('Round 15 metric: ' + $name) 'declared + >=1 series' $actual $actual $(if ($pass) {'PASS'} else {'FAIL'})
}

# Step 4: Round 16 WebSocket metrics.
Write-Both ''
Write-Both '[Step 4] Round 16 WebSocket metrics' 'Yellow'
$wsExpected = @('ws_clients_connected','ws_online_users','ws_messages_sent_total','ws_messages_failed_total')
foreach ($name in $wsExpected) {
    $declared = Has-Metric-Decl $metricsText $name
    $series   = (Parse-Metric $metricsText $name).Count
    $actual   = ('declared=' + $declared + ' series=' + $series)
    Record ('WS-' + $name) ('Round 16 metric: ' + $name) 'declared + >=1 series' $actual $actual $(if ($declared -and $series -gt 0) {'PASS'} else {'FAIL'})
}

$failedHas = (Parse-Metric $metricsText 'ws_messages_failed_total').Count -gt 0
$sentHas   = (Parse-Metric $metricsText 'ws_messages_sent_total').Count -gt 0
$actual    = ('sent=' + $sentHas + ' failed=' + $failedHas)
Record 'WS-COUNTER-EXISTS' 'ws_messages_sent_total & ws_messages_failed_total both declared' 'both declared' $actual $actual $(if ($sentHas -and $failedHas) {'PASS'} else {'FAIL'})

# Step 5: Round 16 Database metrics.
Write-Both ''
Write-Both '[Step 5] Round 16 Database metrics' 'Yellow'
$dbExpected = @('db_open_connections','db_in_use_connections','db_idle_connections','db_wait_count_total','db_max_idle_closed_total','db_max_idle_time_closed_total','db_max_lifetime_closed_total')
foreach ($name in $dbExpected) {
    $declared = Has-Metric-Decl $metricsText $name
    $series   = (Parse-Metric $metricsText $name).Count
    $actual   = ('declared=' + $declared + ' series=' + $series)
    Record ('DB-' + $name) ('Round 16 metric: ' + $name) 'declared + >=1 series' $actual $actual $(if ($declared -and $series -gt 0) {'PASS'} else {'FAIL'})
}

$openMap = Parse-Metric $metricsText 'db_open_connections'
$openVal = -1
if ($openMap.Count -gt 0) { $openVal = ($openMap.Values | Measure-Object -Maximum).Maximum }
$actual  = ('max=' + $openVal)
Record 'DB-OPEN-GE1' 'db_open_connections >= 1' '>=1' $actual $actual $(if ($openVal -ge 1) {'PASS'} else {'FAIL'})

# Step 6: Round 16 Scheduler metrics.
Write-Both ''
Write-Both '[Step 6] Round 16 Scheduler metrics' 'Yellow'
$schedExpected = @('scheduler_running','scheduler_entries','scheduler_running_tasks')
foreach ($name in $schedExpected) {
    $declared = Has-Metric-Decl $metricsText $name
    $series   = (Parse-Metric $metricsText $name).Count
    $actual   = ('declared=' + $declared + ' series=' + $series)
    Record ('SCHED-' + $name) ('Round 16 metric: ' + $name) 'declared + >=1 series' $actual $actual $(if ($declared -and $series -gt 0) {'PASS'} else {'FAIL'})
}

$runningMap = Parse-Metric $metricsText 'scheduler_running'
$runningVal = -1
if ($runningMap.Count -gt 0) { $runningVal = ($runningMap.Values | Measure-Object -Maximum).Maximum }
$actual     = ('val=' + $runningVal)
Record 'SCHED-RUNNING-1' 'scheduler_running = 1' '1' $actual $actual $(if ($runningVal -eq 1) {'PASS'} else {'FAIL'})

$entriesMap = Parse-Metric $metricsText 'scheduler_entries'
$entriesVal = -1
if ($entriesMap.Count -gt 0) { $entriesVal = ($entriesMap.Values | Measure-Object -Maximum).Maximum }
$actual     = ('val=' + $entriesVal)
Record 'SCHED-ENTRIES-GE1' 'scheduler_entries >= 1' '>=1' $actual $actual $(if ($entriesVal -ge 1) {'PASS'} else {'FAIL'})

# Step 7: business events.
Write-Both ''
Write-Both '[Step 7] Round 16 business events' 'Yellow'
$expectedEvents = @('auth.login.failed','auth.login.success')
foreach ($evt in $expectedEvents) {
    $key = 'business_events_total{event="' + $evt + '"}'
    if ($metricsText.Contains($key)) {
        $val  = $null
        $rx   = [regex]::Escape($key) + '\s+([0-9eE+\-\.]+)'
        $mm   = [regex]::Match($metricsText, $rx)
        if ($mm.Success) { $val = [double]$mm.Groups[1].Value }
        $actual = ('val=' + $val)
        Record ('BE-' + $evt) ('business_events_total{event=' + $evt + '}') 'present' $actual $actual 'PASS'
    } else {
        Record ('BE-' + $evt) ('business_events_total{event=' + $evt + '}') 'present' 'absent' 'absent' 'FAIL'
    }
}

# Failed-login counter >= 1.
$failKey = 'business_events_total{event="auth.login.failed"}'
$failRx  = [regex]::Escape($failKey) + '\s+([0-9eE+\-\.]+)'
$mm      = [regex]::Match($metricsText, $failRx)
if ($mm.Success) {
    $failVal = [double]$mm.Groups[1].Value
    $actual  = ('val=' + $failVal)
    Record 'BE-LOGIN-FAILED-GE1' 'auth.login.failed count >= 1' '>=1' $actual $actual $(if ($failVal -ge 1) {'PASS'} else {'FAIL'})
} else {
    Record 'BE-LOGIN-FAILED-GE1' 'auth.login.failed count >= 1' '>=1' 'absent' 'absent' 'FAIL'
}

# Summary.
Write-Both ''
Write-Both '=== Summary ===' 'Cyan'
$total = $results.Count
$pass  = ($results | Where-Object { $_.Result -eq 'PASS' }).Count
$fail  = ($results | Where-Object { $_.Result -eq 'FAIL' }).Count
$rate  = 0
if ($total -gt 0) { $rate = [Math]::Round(($pass / $total) * 100, 1) }
Write-Both ('Total: ' + $total + ' | PASS: ' + $pass + ' | FAIL: ' + $fail + ' | Rate: ' + $rate + '%')

$results | Export-Csv -Path $csvPath -NoTypeInformation -Encoding UTF8
Write-Both ''
Write-Both ('CSV : ' + $csvPath)
Write-Both ('LOG : ' + $logPath)

if ($fail -gt 0) { exit 1 } else { exit 0 }
