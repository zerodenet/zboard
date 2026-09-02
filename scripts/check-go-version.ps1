param(
    [string]$RequiredGoVersion = "",
    [int]$RequestTimeoutSec = 8,
    [int]$QueryTimeoutBudgetSec = 30,
    [int]$QueryRetryLimit = 3,
    [int]$DownloadTimeoutSec = 120,
    [int]$SmokeRequestTimeoutSec = 10,
    [string]$FallbackRoot = "",
    [switch]$AllowStale
)

if ([string]::IsNullOrWhiteSpace($RequiredGoVersion)) {
    $RequiredGoVersion = if ([string]::IsNullOrWhiteSpace($env:ZBOARD_REQUIRED_GO_VERSION)) { "1.26.5" } else { $env:ZBOARD_REQUIRED_GO_VERSION }
}
if ([string]::IsNullOrWhiteSpace($FallbackRoot)) {
    $FallbackRoot = if ([string]::IsNullOrWhiteSpace($env:ZBOARD_GOROOT_FALLBACK)) {
        Join-Path ([Environment]::GetFolderPath("LocalApplicationData")) "zboard\go"
    } else {
        $env:ZBOARD_GOROOT_FALLBACK
    }
}
if ($RequestTimeoutSec -lt 1) {
    $RequestTimeoutSec = 8
}
if ($QueryTimeoutBudgetSec -lt 1) {
    $QueryTimeoutBudgetSec = 30
}
if ($QueryRetryLimit -lt 1) {
    $QueryRetryLimit = 3
}
if ($DownloadTimeoutSec -lt 1) {
    $DownloadTimeoutSec = 120
}
if ($SmokeRequestTimeoutSec -lt 1) {
    $SmokeRequestTimeoutSec = 10
}

$allowStaleEnv = if ([string]::IsNullOrWhiteSpace($env:ZBOARD_ALLOW_STALE_GO_VERSION)) {
    "0"
} else {
    $env:ZBOARD_ALLOW_STALE_GO_VERSION
}
$staleEnabled = if (-not $AllowStale -and -not [string]::IsNullOrWhiteSpace($allowStaleEnv)) {
    $allowStaleEnv -match '^(1|true|yes|on)$'
} else {
    $AllowStale.IsPresent
}

Write-Output "=== zboard go version check ==="
Write-Output "Requested Go target: $RequiredGoVersion"
Write-Output "Go query timeout: ${RequestTimeoutSec}s"
Write-Output "Go query budget: ${QueryTimeoutBudgetSec}s total"
Write-Output "Go query retry limit: ${QueryRetryLimit}"
Write-Output "Go download timeout: ${DownloadTimeoutSec}s"
Write-Output "Smoke test timeout: ${SmokeRequestTimeoutSec}s"
if ($staleEnabled) {
    Write-Output "Go stale fallback mode: enabled (if remote is unavailable)"
} else {
    Write-Output "Go stale fallback mode: disabled (require the pinned repository toolchain)"
}
Write-Output "Preferred local SDK root: $FallbackRoot"

$goModPath = Join-Path (Resolve-Path "$PSScriptRoot\..").Path "backend\go.mod"
if (Test-Path $goModPath) {
    $goModText = Get-Content -Raw $goModPath
    $goDirective = if ($goModText -match '(?m)^go\s+([0-9]+\.[0-9]+(?:\.[0-9]+)?)') {
        $Matches[1]
    } else {
        ""
    }
    $goToolchain = if ($goModText -match '(?m)^toolchain\s+(go[0-9]+\.[0-9]+(?:\.[0-9]+)?)') {
        $Matches[1]
    } else {
        ""
    }
    if ($goDirective) {
        Write-Output "go.mod baseline: go $goDirective"
    } else {
        Write-Output "go.mod baseline: unavailable"
    }
    if ($goToolchain) {
        Write-Output "go.mod toolchain baseline: $goToolchain"
    }
}

Write-Output "--- resolving ..."

$env:ZBOARD_REQUIRED_GO_VERSION = $RequiredGoVersion
$env:ZBOARD_GO_QUERY_TIMEOUT = "$RequestTimeoutSec"
$env:ZBOARD_GO_QUERY_BUDGET_SEC = "$QueryTimeoutBudgetSec"
$env:ZBOARD_GO_QUERY_RETRY_LIMIT = "$QueryRetryLimit"
$env:ZBOARD_GO_DOWNLOAD_TIMEOUT = "$DownloadTimeoutSec"
$env:ZBOARD_GOROOT_FALLBACK = $FallbackRoot
if ($staleEnabled) {
    $env:ZBOARD_ALLOW_STALE_GO_VERSION = "1"
} else {
    $env:ZBOARD_ALLOW_STALE_GO_VERSION = "0"
}

& "$PSScriptRoot\ensure-go-env.ps1" -RequiredVersion $RequiredGoVersion -RequestTimeoutSec $RequestTimeoutSec -QueryTimeoutBudgetSec $QueryTimeoutBudgetSec -QueryRetryLimit $QueryRetryLimit -DownloadTimeoutSec $DownloadTimeoutSec -AllowStale:$staleEnabled

if (Get-Command go -ErrorAction SilentlyContinue) {
    Write-Output "go executable: $(& go version)"
}

if ($env:ZBOARD_GO_VERSION_RESOLVED) {
    Write-Output "Resolved Go target: $($env:ZBOARD_GO_VERSION_RESOLVED) ($($env:ZBOARD_GO_VERSION_SOURCE))"
} else {
    Write-Output "Resolved Go target: unavailable"
}

Write-Output "=== check done ==="
