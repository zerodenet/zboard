param(
    [string]$GoBinaryName = "go",
    [string]$RequiredVersion = "",
    [string]$FallbackRoot = "",
    [string]$AllowStale = "",
    [int]$RequestTimeoutSec = 8,
    [int]$QueryTimeoutBudgetSec = 30,
    [int]$QueryRetryLimit = 3,
    [int]$DownloadTimeoutSec = 120,
    [switch]$AutoInstall = $true,
    [string]$DownloadBase = ""
)

if (-not [string]::IsNullOrWhiteSpace($env:ZBOARD_GOROOT_FALLBACK)) {
    $FallbackRoot = $env:ZBOARD_GOROOT_FALLBACK
}
if ([string]::IsNullOrWhiteSpace($FallbackRoot)) {
    $FallbackRoot = Join-Path ([Environment]::GetFolderPath("LocalApplicationData")) "zboard\go"
}
if ([string]::IsNullOrWhiteSpace($RequiredVersion) -and -not [string]::IsNullOrWhiteSpace($env:ZBOARD_REQUIRED_GO_VERSION)) {
    $RequiredVersion = $env:ZBOARD_REQUIRED_GO_VERSION
}
if ([string]::IsNullOrWhiteSpace($RequiredVersion)) {
    $RequiredVersion = "1.26.5"
}
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
if (-not [string]::IsNullOrWhiteSpace($env:ZBOARD_GO_DOWNLOAD_TIMEOUT)) {
    try {
        $envDownloadTimeout = [int]$env:ZBOARD_GO_DOWNLOAD_TIMEOUT
        if ($envDownloadTimeout -gt 0) {
            $DownloadTimeoutSec = $envDownloadTimeout
        }
    } catch {
        Write-Warning "Invalid ZBOARD_GO_DOWNLOAD_TIMEOUT='$($env:ZBOARD_GO_DOWNLOAD_TIMEOUT)'; using script default $DownloadTimeoutSec."
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

$allowStaleEnv = if ([string]::IsNullOrWhiteSpace($AllowStale)) {
    $env:ZBOARD_ALLOW_STALE_GO_VERSION
} else {
    $AllowStale
}
$allowStaleVersion = if ([string]::IsNullOrWhiteSpace($allowStaleEnv)) {
    $false
} else {
    $allowStaleEnv -match '^(1|true|yes|on)$'
}
$fallbackGoVersion = if ([string]::IsNullOrWhiteSpace($env:ZBOARD_FALLBACK_GO_VERSION)) {
    ""
} else {
    $env:ZBOARD_FALLBACK_GO_VERSION.Trim()
}
$script:GoVersionResolvedSource = ""
$script:GoQueryStartTime = Get-Date
$script:GoQueryAttempts = 0
$script:GoQueryBudgetSec = $QueryTimeoutBudgetSec
$script:GoQueryMaxAttempts = $QueryRetryLimit

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
        return $null
    }
    try {
        $payload = Invoke-RestMethod -Uri $url -TimeoutSec $timeout
        if ($null -eq $payload) {
            return $null
        }
        $stable = $payload | Where-Object { $_.stable -eq $true } | Select-Object -First 1
        if ($null -eq $stable -or [string]::IsNullOrWhiteSpace($stable.version)) {
            return $null
        }
        $script:GoVersionResolvedSource = "remote-json"
        return $stable.version -replace "^go", ""
    } catch {
        Write-Warning "Failed to query latest go version from ${url} via Invoke-RestMethod: $($_.Exception.Message)"
    }

    $timeout = Start-GoQueryAttempt
    if ($timeout -le 0) {
        return $null
    }
    try {
        $payloadText = & curl.exe -fsSL --max-time $timeout $url 2>$null
        if ([string]::IsNullOrWhiteSpace($payloadText)) {
            throw "empty response"
        }
        $payload = $payloadText | ConvertFrom-Json -ErrorAction Stop
        $stable = $payload | Where-Object { $_.stable -eq $true } | Select-Object -First 1
        if ($null -eq $stable -or [string]::IsNullOrWhiteSpace($stable.version)) {
            return $null
        }
        $script:GoVersionResolvedSource = "remote-curl-json"
        return $stable.version -replace "^go", ""
    } catch {
        Write-Warning "Failed to query latest go version from ${url} via curl.exe: $($_.Exception.Message)"
    }

    foreach ($candidate in @("python3", "python", "py")) {
        if (-not (Get-Command $candidate -ErrorAction SilentlyContinue)) {
            continue
        }
        $timeout = Start-GoQueryAttempt
        if ($timeout -le 0) {
            return $null
        }
        try {
            $payloadText = if ($candidate -eq "py") {
                & py -3 -c "import json,urllib.request;print(json.dumps(json.load(urllib.request.urlopen('$url', timeout=$timeout)))"
            } else {
                & $candidate -c "import json,sys,urllib.request;print(json.dumps(json.load(urllib.request.urlopen('$url', timeout=$timeout)))"
            }
            if ([string]::IsNullOrWhiteSpace($payloadText)) {
                continue
            }
            $payload = $payloadText | ConvertFrom-Json -ErrorAction Stop
            $stable = $payload | Where-Object { $_.stable -eq $true } | Select-Object -First 1
            if ($null -eq $stable -or [string]::IsNullOrWhiteSpace($stable.version)) {
                return $null
            }
            $script:GoVersionResolvedSource = "remote-${candidate}"
            return $stable.version -replace "^go", ""
        } catch {
            Write-Warning "Failed to query latest go version via ${candidate}: $($_.Exception.Message)"
        }
    }

    $timeout = Start-GoQueryAttempt
    if ($timeout -le 0) {
        return $null
    }
    try {
        $versionApi = "${DownloadBase}/VERSION"
        $payloadText = & curl.exe -fsSL --max-time $timeout $versionApi 2>$null
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
        return $null
    }
    $script:GoVersionResolvedSource = "fallback-env"
    return $fallbackGoVersion -replace "^go", ""
}

function Install-GoSDK {
    param(
        [string]$Version,
        [string]$InstallRoot
    )

    if ([string]::IsNullOrWhiteSpace($Version)) {
        throw "Unable to determine target Go version."
    }

    $ver = if ($Version -like "go*") { $Version } else { "go$Version" }
    $url = "${DownloadBase}/${ver}.windows-amd64.zip"
    $tempDir = Join-Path $env:TEMP ("zboard-go-install-" + [Guid]::NewGuid().ToString("N"))
    New-Item -ItemType Directory -Path $tempDir -Force | Out-Null
    $zipPath = Join-Path $tempDir "go.zip"

    Write-Output "Install Go ${Version} to ${InstallRoot}"
    try {
        Invoke-WebRequest -Uri $url -OutFile $zipPath -TimeoutSec $DownloadTimeoutSec
    } catch {
        Remove-Item -Recurse -Force $tempDir
        throw "Failed to download ${url}. If downloads are blocked, set -RequiredVersion to an installed local version and place it in ${FallbackRoot}\\bin\\go.exe, or pass -AutoInstall:`$false to skip auto-install."
    }

    if (Test-Path $InstallRoot) {
        Remove-Item -Recurse -Force $InstallRoot
    }
    New-Item -ItemType Directory -Path $InstallRoot -Force | Out-Null

    try {
        Expand-Archive -Path $zipPath -DestinationPath $tempDir -Force
        Copy-Item -Path (Join-Path $tempDir "go\*") -Destination $InstallRoot -Recurse -Force
    } finally {
        Remove-Item -Recurse -Force $tempDir
    }
}

function Get-BaseVersion {
    param([string]$VersionText)
    if ([string]::IsNullOrWhiteSpace($VersionText)) {
        return $null
    }
    $raw = $VersionText -replace "^go", ""
    $parts = $raw.Split(".")
    try {
        return @{
            Major = [int]$parts[0]
            Minor = if ($parts.Length -ge 2) { [int]$parts[1] } else { 0 }
            Patch = if ($parts.Length -ge 3) { [int]$parts[2] } else { 0 }
        }
    } catch {
        return $null
    }
}

function Get-GoVersion {
    param([string]$VersionLine)
    if ($VersionLine -match "go(\d+)\.(\d+)\.(\d+)") {
        return @{
            Major = [int]$Matches[1]
            Minor = [int]$Matches[2]
            Patch = [int]$Matches[3]
        }
    }
    if ($VersionLine -match "go(\d+)\.(\d+)") {
        return @{
            Major = [int]$Matches[1]
            Minor = [int]$Matches[2]
            Patch = 0
        }
    }
    return $null
}

function Is-GoVersionLower {
    param(
        [int]$Major,
        [int]$Minor,
        [int]$Patch,
        [hashtable]$Base
    )
    if ($Major -lt $Base.Major) {
        return $true
    }
    if ($Major -eq $Base.Major -and $Minor -lt $Base.Minor) {
        return $true
    }
    if ($Major -eq $Base.Major -and $Minor -eq $Base.Minor -and $Patch -lt $Base.Patch) {
        return $true
    }
    return $false
}

function Use-FallbackGo {
    param(
        [string]$VersionForInstall = "",
        [switch]$Force = $false
    )

    $goExe = Join-Path $FallbackRoot "bin\go.exe"
    if (Test-Path $goExe) {
        if (-not $AutoInstall -and -not $Force) {
            $env:GOROOT = $FallbackRoot
            $env:Path = "$FallbackRoot\bin;$env:Path"
            Write-Output "Switched to fallback SDK at ${FallbackRoot} (auto-install disabled)"
            return
        }

        $targetText = $VersionForInstall
        $targetBase = Get-BaseVersion -VersionText $targetText
        if ($null -eq $targetBase) {
            $env:GOROOT = $FallbackRoot
            $env:Path = "$FallbackRoot\bin;$env:Path"
            Write-Output "Switched to fallback SDK at ${FallbackRoot}"
            return
        }

        if (-not $Force) {
            try {
                $current = Get-GoVersion -VersionLine (& $goExe version)
                if ($null -ne $current -and -not (Is-GoVersionLower -Major $current.Major -Minor $current.Minor -Patch $current.Patch -Base $targetBase)) {
                    $env:GOROOT = $FallbackRoot
                    $env:Path = "$FallbackRoot\bin;$env:Path"
                    Write-Output "Switched to fallback SDK at ${FallbackRoot}"
                    return
                }
                if ($null -ne $current) {
                    Write-Output "Fallback SDK ($($current.Major).$($current.Minor).$($current.Patch)) is below required ${targetText}; reinstalling..."
                } else {
                    Write-Warning "Unable to parse fallback go version, reinstalling fallback SDK at ${FallbackRoot}."
                }
            } catch {
                Write-Warning "Failed to inspect fallback go version, reinstalling fallback SDK at ${FallbackRoot}: $($_.Exception.Message)"
            }
            Remove-Item -Recurse -Force $FallbackRoot -ErrorAction SilentlyContinue
        }
    }

    if (-not $AutoInstall) {
        throw "Go runtime not found. Expected ${FallbackRoot} to contain bin\\go.exe."
    }

    $target = if ([string]::IsNullOrWhiteSpace($VersionForInstall)) {
        Get-LatestGoVersion
    } else {
        $VersionForInstall
    }
    if ([string]::IsNullOrWhiteSpace($target)) {
        throw "Unable to determine Go install target version."
    }
    Install-GoSDK -Version $target -InstallRoot $FallbackRoot
    $env:GOROOT = $FallbackRoot
    $env:Path = "$FallbackRoot\bin;$env:Path"
    Write-Output "Installed go ${target} to fallback SDK: ${FallbackRoot}"
}

if ([string]::IsNullOrWhiteSpace($RequiredVersion)) {
    $latest = Get-LatestGoVersion
    if ([string]::IsNullOrWhiteSpace($latest) -and (Get-Command $GoBinaryName -ErrorAction SilentlyContinue)) {
        if ($allowStaleVersion) {
            $latest = (& (Get-Command $GoBinaryName).Path version | ForEach-Object { ($_ -split " ")[2] } | ForEach-Object { $_ -replace "^go" })
            $env:ZBOARD_GO_VERSION_SOURCE = "stale-local"
            Write-Warning "WARN: unable to resolve latest go version from network; allowed stale mode is on, using current local go ${latest}."
        } else {
            throw "Unable to resolve latest go version from stable index. Set -RequiredVersion manually or set -AllowStale=1 / ZBOARD_ALLOW_STALE_GO_VERSION=1."
        }
    }
    $RequiredVersion = $latest
}

if ($RequiredVersion -ieq "latest") {
    $latest = Get-LatestGoVersion
    if ([string]::IsNullOrWhiteSpace($latest)) {
        if ($allowStaleVersion -and (Get-Command $GoBinaryName -ErrorAction SilentlyContinue)) {
            $latest = (& (Get-Command $GoBinaryName).Path version | ForEach-Object { ($_ -split " ")[2] } | ForEach-Object { $_ -replace "^go" })
            $env:ZBOARD_GO_VERSION_SOURCE = "stale-local"
            Write-Warning "WARN: unable to resolve latest go version from network; allowed stale mode is on, using current local go ${latest}."
        } else {
            throw "Unable to resolve latest go version from network."
        }
    }
    if ([string]::IsNullOrWhiteSpace($latest)) {
        throw "Unable to determine target go version."
    }
    $RequiredVersion = $latest
}

if ([string]::IsNullOrWhiteSpace($RequiredVersion)) {
    throw "Unable to resolve required go version from network or local environment. Set -RequiredVersion manually."
}

$env:ZBOARD_GO_VERSION_SOURCE = if ($env:ZBOARD_GO_VERSION_SOURCE) { $env:ZBOARD_GO_VERSION_SOURCE } else { "remote-stable" }
$env:ZBOARD_GO_VERSION_SOURCE = if ($script:GoVersionResolvedSource) { $script:GoVersionResolvedSource } else { $env:ZBOARD_GO_VERSION_SOURCE }
$env:ZBOARD_GO_VERSION_RESOLVED = $RequiredVersion
$env:ZBOARD_GO_QUERY_BUDGET_SEC = "$QueryTimeoutBudgetSec"
$env:ZBOARD_GO_QUERY_RETRY_LIMIT = "$QueryRetryLimit"

$requiredBase = Get-BaseVersion -VersionText $RequiredVersion
if ($null -eq $requiredBase) {
    throw "Unable to parse required Go version '${RequiredVersion}'."
}

$goCmd = Get-Command $GoBinaryName -ErrorAction SilentlyContinue
if (-not $goCmd) {
    Use-FallbackGo -VersionForInstall $RequiredVersion
    $goCmd = Get-Command $GoBinaryName -ErrorAction SilentlyContinue
} else {
    Write-Output "Using PATH go binary: $($goCmd.Path)"
}

if ($goCmd) {
    $versionLine = & $goCmd.Path version
    Write-Output "Go version check: $versionLine"
    $goVersion = Get-GoVersion -VersionLine $versionLine
    if ($null -ne $goVersion -and (Is-GoVersionLower -Major $goVersion.Major -Minor $goVersion.Minor -Patch $goVersion.Patch -Base $requiredBase)) {
        if ($AutoInstall) {
            Write-Output "Current Go ${goVersion.Major}.${goVersion.Minor}.${goVersion.Patch} is below baseline ${RequiredVersion}. Upgrading fallback SDK..."
            Use-FallbackGo -VersionForInstall $RequiredVersion -Force
            $goCmd = Get-Command $GoBinaryName -ErrorAction SilentlyContinue
            if ($goCmd) {
                $versionLine = & $goCmd.Path version
                Write-Output "Upgraded Go version check: $versionLine"
            }
        } else {
            Write-Warning "Current Go ${goVersion.Major}.${goVersion.Minor}.${goVersion.Patch} is below project baseline ${RequiredVersion}. Current env may fail with this minimum-version policy."
        }
    } elseif ($null -eq $goVersion) {
        Write-Warning "Unable to parse go version string: $versionLine"
    }
}

if ($goCmd) {
    Write-Output "Resolved Go target: $env:ZBOARD_GO_VERSION_RESOLVED (source: $env:ZBOARD_GO_VERSION_SOURCE)"
    Write-Output "Go query budget: ${RequestTimeoutSec}s per attempt, ${QueryTimeoutBudgetSec}s total, up to $QueryRetryLimit attempts."
    Write-Output "Project baseline requires Go >= $RequiredVersion (or auto-install path at ${FallbackRoot})."
} else {
    throw "Go command not found after environment setup."
}
