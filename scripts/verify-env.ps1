param(
    [string]$RequiredGoVersion = "",
    [switch]$AllowStale
)

function Get-GoModBaseline {
    param([string]$GoModPath)

    if (-not (Test-Path $GoModPath)) {
        return @{ GoDirective = ""; Toolchain = "" }
    }

    $text = Get-Content -Raw $GoModPath

    $goDirective = if ($text -match '(?m)^go\s+([0-9]+\.[0-9]+(?:\.[0-9]+)?)') {
        $Matches[1]
    } else {
        ""
    }

    $toolchain = if ($text -match '(?m)^toolchain\s+(go[0-9]+\.[0-9]+(?:\.[0-9]+)?)') {
        $Matches[1]
    } else {
        ""
    }

    return @{
        GoDirective = $goDirective
        Toolchain = $toolchain
    }
}

if ([string]::IsNullOrWhiteSpace($RequiredGoVersion)) {
    $RequiredGoVersion = if ([string]::IsNullOrWhiteSpace($env:ZBOARD_REQUIRED_GO_VERSION)) {
        "1.26.5"
    } else {
        $env:ZBOARD_REQUIRED_GO_VERSION
    }
}

$allowStaleEnv = if ([string]::IsNullOrWhiteSpace($env:ZBOARD_ALLOW_STALE_GO_VERSION)) {
    ""
} else {
    $env:ZBOARD_ALLOW_STALE_GO_VERSION
}
if (-not $AllowStale -and -not [string]::IsNullOrWhiteSpace($allowStaleEnv)) {
    $AllowStale = $allowStaleEnv -match '^(1|true|yes|on)$'
}
$requestedGoVersion = $RequiredGoVersion
$modBaseline = Get-GoModBaseline "$PSScriptRoot\..\backend\go.mod"

Write-Output "=== zboard environment check ==="
if ($modBaseline.GoDirective) {
    Write-Output "Go mod baseline: go $($modBaseline.GoDirective)"
} else {
    Write-Output "Go mod baseline: unavailable"
}
if ($modBaseline.Toolchain) {
    Write-Output "Go toolchain baseline: $($modBaseline.Toolchain)"
}

if ($AllowStale) {
    $env:ZBOARD_ALLOW_STALE_GO_VERSION = "1"
    & "$PSScriptRoot\ensure-go-env.ps1" -RequiredVersion $RequiredGoVersion -AllowStale "1"
} else {
    $env:ZBOARD_ALLOW_STALE_GO_VERSION = "0"
    & "$PSScriptRoot\ensure-go-env.ps1" -RequiredVersion $RequiredGoVersion
}

Write-Output "Requested Go target: ${requestedGoVersion}"
if ($env:ZBOARD_GO_VERSION_RESOLVED) {
    Write-Output "Resolved Go target: $($env:ZBOARD_GO_VERSION_RESOLVED) ($($env:ZBOARD_GO_VERSION_SOURCE))"
} elseif ($requestedGoVersion -ne "latest") {
    Write-Output "Resolved Go target: $requestedGoVersion"
}

if (Get-Command go -ErrorAction SilentlyContinue) {
    Write-Output "go: $(& go version)"
} else {
    throw "go binary unavailable after ensure-go-env"
}

if ($AllowStale) {
    Write-Output "Go stale fallback mode: enabled (use local runtime if network unavailable)"
} else {
    Write-Output "Go stale fallback mode: disabled (require the pinned repository toolchain)"
}

if (Get-Command pnpm -ErrorAction SilentlyContinue) {
    Write-Output "pnpm: $(& pnpm --version)"
} else {
    Write-Output "WARN: pnpm not found. Frontend build will require 'npm install -g pnpm'."
}

if (Get-Command node -ErrorAction SilentlyContinue) {
    Write-Output "node: $(& node -v)"
} else {
    Write-Warning "node not found. Frontend build requires node runtime."
}

if (Get-Command mysql -ErrorAction SilentlyContinue) {
    Write-Output "mysql client: found"
} else {
    Write-Warning "mysql client not found. DB bootstrap may need mysql installed or docker compose."
}

if (Get-Command docker -ErrorAction SilentlyContinue) {
    Write-Output "docker: $(& docker --version)"
} else {
    Write-Warning "docker not found. Containerized startup path unavailable."
}

Write-Output "=== check complete ==="
