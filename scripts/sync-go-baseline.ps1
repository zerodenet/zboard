param(
    [string]$GoModPath = "$PSScriptRoot\..\backend\go.mod",
    [string]$RequiredVersion = "",
    [string]$DownloadBase = "",
    [int]$RequestTimeoutSec = 8,
    [int]$QueryTimeoutBudgetSec = 30,
    [int]$QueryRetryLimit = 3,
    [switch]$CheckOnly,
    [switch]$DryRun
)

if ([string]::IsNullOrWhiteSpace($DownloadBase)) {
    $DownloadBase = if ([string]::IsNullOrWhiteSpace($env:ZBOARD_GO_DOWNLOAD_BASE)) {
        "https://go.dev/dl"
    } else {
        $env:ZBOARD_GO_DOWNLOAD_BASE
    }
}

if (-not [string]::IsNullOrWhiteSpace($env:ZBOARD_GO_QUERY_TIMEOUT)) {
    try {
        $envTimeout = [int]$env:ZBOARD_GO_QUERY_TIMEOUT
        if ($envTimeout -gt 0) {
            $RequestTimeoutSec = $envTimeout
        }
    } catch {
        Write-Warning "Invalid ZBOARD_GO_QUERY_TIMEOUT='$($env:ZBOARD_GO_QUERY_TIMEOUT)'; using script default $RequestTimeoutSec."
    }
}
if (-not [string]::IsNullOrWhiteSpace($env:ZBOARD_GO_QUERY_BUDGET_SEC)) {
    try {
        $envBudget = [int]$env:ZBOARD_GO_QUERY_BUDGET_SEC
        if ($envBudget -gt 0) {
            $QueryTimeoutBudgetSec = $envBudget
        }
    } catch {
        Write-Warning "Invalid ZBOARD_GO_QUERY_BUDGET_SEC='$($env:ZBOARD_GO_QUERY_BUDGET_SEC)'; using script default $QueryTimeoutBudgetSec."
    }
}
if (-not [string]::IsNullOrWhiteSpace($env:ZBOARD_GO_QUERY_RETRY_LIMIT)) {
    try {
        $envRetry = [int]$env:ZBOARD_GO_QUERY_RETRY_LIMIT
        if ($envRetry -gt 0) {
            $QueryRetryLimit = $envRetry
        }
    } catch {
        Write-Warning "Invalid ZBOARD_GO_QUERY_RETRY_LIMIT='$($env:ZBOARD_GO_QUERY_RETRY_LIMIT)'; using script default $QueryRetryLimit."
    }
}
$fallbackGoVersion = if ([string]::IsNullOrWhiteSpace($env:ZBOARD_FALLBACK_GO_VERSION)) {
    ""
} else {
    $env:ZBOARD_FALLBACK_GO_VERSION.Trim()
}
$script:GoVersionResolvedSource = ""
$script:GoQueryStartTime = Get-Date
$script:GoQueryAttempts = 0
$script:GoQueryBudgetSec = if ($QueryTimeoutBudgetSec -lt 1) { 30 } else { $QueryTimeoutBudgetSec }
$script:GoQueryMaxAttempts = if ($QueryRetryLimit -lt 1) { 3 } else { $QueryRetryLimit }
$script:QueryBudgetSource = "remote"

if ($RequestTimeoutSec -lt 1) {
    $RequestTimeoutSec = 8
}
if ([string]::IsNullOrWhiteSpace($RequiredVersion)) {
    $RequiredVersion = if ([string]::IsNullOrWhiteSpace($env:ZBOARD_REQUIRED_GO_VERSION)) { "1.26.5" } else { $env:ZBOARD_REQUIRED_GO_VERSION }
}

function Get-QueryTimeout {
    $elapsed = [int]((Get-Date) - $script:GoQueryStartTime).TotalSeconds
    $remaining = $script:GoQueryBudgetSec - $elapsed
    if ($remaining -le 0) {
        return 0
    }
    $defaultTimeout = if ($RequestTimeoutSec -gt 0) {
        $RequestTimeoutSec
    } else {
        8
    }
    if ($remaining -lt $defaultTimeout) {
        return $remaining
    }
    return $defaultTimeout
}

function Start-GoQueryAttempt {
    if ($script:GoQueryAttempts -ge $script:GoQueryMaxAttempts) {
        Write-Warning "Go version query attempt limit reached ($script:GoQueryMaxAttempts)."
        return 0
    }

    $script:GoQueryAttempts++
    return Get-QueryTimeout
}

function Get-LatestGoVersion {
    $url = "${DownloadBase}/?mode=json"
    $timeout = Start-GoQueryAttempt
    if ($timeout -le 0) {
        return ""
    }
    try {
        [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12 -bor [Net.SecurityProtocolType]::Tls13
        $payload = Invoke-RestMethod -Uri $url -TimeoutSec $timeout
        if ($null -eq $payload) {
            throw "empty response"
        }
        $stable = $payload | Where-Object { $_.stable -eq $true } | Select-Object -First 1
        if ($null -eq $stable -or [string]::IsNullOrWhiteSpace($stable.version)) {
            return ""
        }
        $script:GoVersionResolvedSource = "remote-json"
        return $stable.version -replace "^go", ""
    } catch {
        Write-Warning "Failed to query go stable index via Invoke-RestMethod: $($_.Exception.Message)"
    }

    $timeout = Start-GoQueryAttempt
    if ($timeout -le 0) {
        return ""
    }
    try {
        $payloadText = & curl.exe -sS --max-time "$timeout" "$url"
        if ([string]::IsNullOrWhiteSpace($payloadText)) {
            throw "empty response"
        }
        $payload = $payloadText | ConvertFrom-Json -ErrorAction Stop
        $stable = $payload | Where-Object { $_.stable -eq $true } | Select-Object -First 1
        if ($null -eq $stable -or [string]::IsNullOrWhiteSpace($stable.version)) {
            return ""
        }
        $script:GoVersionResolvedSource = "remote-curl-json"
        return $stable.version -replace "^go", ""
    } catch {
        Write-Warning "Failed to query go stable index via curl: $($_.Exception.Message)"
    }

    foreach ($candidate in @("python3", "python")) {
        if (-not (Get-Command $candidate -ErrorAction SilentlyContinue)) {
            continue
        }
        $timeout = Start-GoQueryAttempt
        if ($timeout -le 0) {
            return ""
        }
        try {
            $payloadText = & $candidate -c "import json,urllib.request;print(json.dumps(json.load(urllib.request.urlopen('$url', timeout=$timeout)))"
            if ([string]::IsNullOrWhiteSpace($payloadText)) {
                throw "empty response"
            }
            $payload = ConvertFrom-Json $payloadText
            $stable = $payload | Where-Object { $_.stable -eq $true } | Select-Object -First 1
            if ($null -eq $stable -or [string]::IsNullOrWhiteSpace($stable.version)) {
                return ""
            }
            $script:GoVersionResolvedSource = "remote-${candidate}"
            return $stable.version -replace "^go", ""
        } catch {
            Write-Warning "Failed to query go stable index via ${candidate}: $($_.Exception.Message)"
        }
    }

    $timeout = Start-GoQueryAttempt
    if ($timeout -le 0) {
        return ""
    }
    try {
        $versionApi = "${DownloadBase}/VERSION"
        $payloadText = & curl.exe -fsSL --max-time "$timeout" "$versionApi" 2>$null
        if ([string]::IsNullOrWhiteSpace($payloadText)) {
            throw "empty response"
        }
        $first = ($payloadText -split "`r?`n" | Where-Object { $_ -match "^go" })[0]
        if (-not [string]::IsNullOrWhiteSpace($first) -and $first -match "^go(\d+\.\d+(?:\.\d+)?)$") {
            $script:GoVersionResolvedSource = "remote-version-file"
            return $Matches[1]
        }
    } catch {
        Write-Warning "Failed to query go version from ${versionApi}: $($_.Exception.Message)"
    }

    if ([string]::IsNullOrWhiteSpace($fallbackGoVersion)) {
        return ""
    }
    $script:GoVersionResolvedSource = "fallback-env"
    return $fallbackGoVersion -replace "^go", ""
}

if ($RequiredVersion -ieq "latest") {
    $RequiredVersion = ""
}

$allowStale = if ($env:ZBOARD_ALLOW_STALE_GO_VERSION -match '^(1|true|yes|on)$') { $true } else { $false }

if ([string]::IsNullOrWhiteSpace($RequiredVersion)) {
    $required = Get-LatestGoVersion
    if ([string]::IsNullOrWhiteSpace($required) -and (Get-Command go -ErrorAction SilentlyContinue)) {
        if ($allowStale) {
            $required = (& go version | Select-String -Pattern "go([0-9]+\.[0-9]+(?:\.[0-9]+)?)" | ForEach-Object { $_.Matches[0].Groups[1].Value })
            if (-not [string]::IsNullOrWhiteSpace($required)) {
                Write-Warning "WARN: unable to resolve latest from network, using local go ${required} because stale mode is enabled."
                $script:GoVersionResolvedSource = "stale-local"
            }
        }
    }
    if ([string]::IsNullOrWhiteSpace($required) -and -not [string]::IsNullOrWhiteSpace($fallbackGoVersion)) {
        $required = $fallbackGoVersion -replace "^go", ""
        Write-Warning "WARN: using fallback override version from ZBOARD_FALLBACK_GO_VERSION (${required}) because remote resolution failed."
    }
    if ([string]::IsNullOrWhiteSpace($required)) {
        throw "Unable to determine target Go version. Set -RequiredVersion or set ZBOARD_REQUIRED_GO_VERSION explicitly."
    }
    $RequiredVersion = $required
}

if ($script:GoVersionResolvedSource) {
    Write-Output "Resolved go version source: $script:GoVersionResolvedSource"
    Write-Output "Go query budget used: ${RequestTimeoutSec}s/attempt, ${script:GoQueryBudgetSec}s total, max $($script:GoQueryMaxAttempts) attempts."
}

if (-not (Test-Path $GoModPath)) {
    throw "go.mod not found: ${GoModPath}"
}

$goModText = Get-Content -Raw $GoModPath
$matchGo = if ($goModText -match "(?m)^go\s+([0-9]+\.[0-9]+(?:\.[0-9]+)?)") { $Matches[1] } else { "" }
$matchToolchain = if ($goModText -match "(?m)^toolchain\s+(go[0-9]+\.[0-9]+(?:\.[0-9]+)?)") { $Matches[1] } else { "" }

$TargetToolchain = if ($RequiredVersion -match "^go") { $RequiredVersion } else { "go$RequiredVersion" }
$TargetGo = $TargetToolchain -replace "^go([0-9]+)\.([0-9]+).*", '$1.$2'

if ([string]::IsNullOrWhiteSpace($matchGo) -or [string]::IsNullOrWhiteSpace($matchToolchain)) {
    throw "Malformed go.mod: missing go/toolchain directive. Expected: go <major.minor> and toolchain go<major.minor.patch>."
}

if ($matchGo -eq $TargetGo -and $matchToolchain -eq $TargetToolchain) {
    Write-Output "go.mod already aligned: go $matchGo / toolchain $matchToolchain."
    if ($script:GoVersionResolvedSource) {
        Write-Output "Resolved source: $script:GoVersionResolvedSource"
    }
    return
}

Write-Output "Syncing go.mod baseline to official stable floor."
Write-Output " - go     : ${matchGo} => ${TargetGo}"
Write-Output " - toolchain: ${matchToolchain} => ${TargetToolchain}"

if ($DryRun) {
    Write-Output "Dry-run mode: no files updated."
    if ($CheckOnly) {
        throw "go.mod baseline is out of date; target is go $TargetGo / toolchain $TargetToolchain."
    }
    return
}

if ($CheckOnly) {
    throw "go.mod baseline is out of date; target is go $TargetGo / toolchain $TargetToolchain."
}

$updated = $goModText `
    -replace "(?m)^go\s+[0-9]+\.[0-9]+(?:\.[0-9]+)?", "go ${TargetGo}" `
    -replace "(?m)^toolchain\s+go[0-9]+\.[0-9]+(?:\.[0-9]+)?", "toolchain ${TargetToolchain}"

Set-Content -NoNewline -Path $GoModPath -Value $updated -Encoding UTF8
Write-Output "Updated go.mod baseline: go ${TargetGo}, toolchain ${TargetToolchain}."
