param(
    [string]$ApiBase = "http://127.0.0.1:8080",
    [int]$BackendPort = 8080,
    [int]$FrontendPort = 5173,
    [int]$StartupTimeoutSec = 120,
    [string]$GoVersion = "1.26.5",
    [int]$GoQueryTimeoutSec = 8,
    [int]$GoDownloadTimeoutSec = 120,
    [int]$GoQueryTimeoutBudgetSec = 30,
    [int]$GoQueryRetryLimit = 3,
    [int]$SmokeRequestTimeoutSec = 10,
    [string]$DataSource = "",
    [string]$RedisAddr = "",
    [string]$JwtSecret = "",
    [string]$CredentialEncryptionKey = "",
	[string]$AdminEmail = "",
    [string]$AdminPassword = "",
    [switch]$WithFrontend,
    [switch]$NoSmoke,
    [switch]$SkipDependencies,
    [switch]$StopWhenDone
)

$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"

function New-ZBoardRandomSecret {
    param([int]$ByteLength = 32)
    $bytes = New-Object byte[] $ByteLength
    $rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
    try {
        $rng.GetBytes($bytes)
    } finally {
        $rng.Dispose()
    }
    return [Convert]::ToBase64String($bytes)
}

if (-not $PSBoundParameters.ContainsKey('ApiBase')) {
    $ApiBase = "http://127.0.0.1:$BackendPort"
}

$GoVersion = if ([string]::IsNullOrWhiteSpace($GoVersion)) { "1.26.5" } else { $GoVersion }
$env:ZBOARD_REQUIRED_GO_VERSION = $GoVersion
$env:ZBOARD_ENFORCE_GO_BASELINE = if ([string]::IsNullOrWhiteSpace($env:ZBOARD_ENFORCE_GO_BASELINE)) { "1" } else { $env:ZBOARD_ENFORCE_GO_BASELINE }
$env:ZBOARD_GO_QUERY_TIMEOUT = if ([string]::IsNullOrWhiteSpace($env:ZBOARD_GO_QUERY_TIMEOUT)) { "$GoQueryTimeoutSec" } else { $env:ZBOARD_GO_QUERY_TIMEOUT }
$env:ZBOARD_GO_QUERY_BUDGET_SEC = if ([string]::IsNullOrWhiteSpace($env:ZBOARD_GO_QUERY_BUDGET_SEC)) { "$GoQueryTimeoutBudgetSec" } else { $env:ZBOARD_GO_QUERY_BUDGET_SEC }
$env:ZBOARD_GO_QUERY_RETRY_LIMIT = if ([string]::IsNullOrWhiteSpace($env:ZBOARD_GO_QUERY_RETRY_LIMIT)) { "$GoQueryRetryLimit" } else { $env:ZBOARD_GO_QUERY_RETRY_LIMIT }
$env:ZBOARD_GO_DOWNLOAD_TIMEOUT = if ([string]::IsNullOrWhiteSpace($env:ZBOARD_GO_DOWNLOAD_TIMEOUT)) { "$GoDownloadTimeoutSec" } else { $env:ZBOARD_GO_DOWNLOAD_TIMEOUT }
$env:ZBOARD_SMOKE_TIMEOUT = if ([string]::IsNullOrWhiteSpace($env:ZBOARD_SMOKE_TIMEOUT)) { "$SmokeRequestTimeoutSec" } else { $env:ZBOARD_SMOKE_TIMEOUT }

$enforceGoBaseline = if ($env:ZBOARD_ENFORCE_GO_BASELINE -match '^(1|true|yes|on)$') { $true } else { $false }

$ApiBase = $ApiBase.TrimEnd('/')
$BackendApiBase = if ($ApiBase -like "*/api/v1") { $ApiBase } else { "$ApiBase/api/v1" }
$oldApiBase = $env:VITE_API_BASE

$projectRoot = (Resolve-Path "$PSScriptRoot\..").Path
$tmpDir = Join-Path $projectRoot "tmp"
$backendLog = Join-Path $tmpDir "zboard-backend.log"
$frontendLog = Join-Path $tmpDir "zboard-frontend.log"
$runtimeConfig = Join-Path $tmpDir "zboard.local.yaml"
$null = New-Item -ItemType Directory -Path $tmpDir -Force

$goModPath = Join-Path $projectRoot "backend\go.mod"
if (Test-Path $goModPath) {
    $goModText = Get-Content -Raw $goModPath
    $goDirective = if ($goModText -match "(?m)^go\s+([0-9]+\.[0-9]+(?:\.[0-9]+)?)") { $Matches[1] } else { "" }
    $toolchain = if ($goModText -match "(?m)^toolchain\s+(go[0-9]+\.[0-9]+(?:\.[0-9]+)?)") { $Matches[1] } else { "" }
    if ($goDirective) {
        Write-Output "Go mod baseline: go $goDirective"
    } else {
        Write-Output "Go mod baseline: unavailable"
    }
    if ($toolchain) {
        Write-Output "Go toolchain baseline: $toolchain"
    }
}

Write-Output "=== zboard local startup ==="

& "$PSScriptRoot\verify-env.ps1"
if ($env:ZBOARD_GO_VERSION_RESOLVED) {
    Write-Output "Resolved Go target: ${env:ZBOARD_GO_VERSION_RESOLVED} (${env:ZBOARD_GO_VERSION_SOURCE})"
} else {
    Write-Output "Resolved Go target: $env:ZBOARD_REQUIRED_GO_VERSION"
}
$goBaselineTarget = if ($env:ZBOARD_GO_VERSION_RESOLVED) { $env:ZBOARD_GO_VERSION_RESOLVED } else { $env:ZBOARD_REQUIRED_GO_VERSION }

try {
    if ($enforceGoBaseline) {
        & "$PSScriptRoot\sync-go-baseline.ps1" -RequiredVersion $goBaselineTarget -CheckOnly
    } else {
        & "$PSScriptRoot\sync-go-baseline.ps1" -RequiredVersion $goBaselineTarget -DryRun | Out-Null
    }
} catch {
    if ($enforceGoBaseline) {
        throw $_
    }
    Write-Warning "Go baseline check skipped: $($_.Exception.Message)"
}

if (-not $SkipDependencies) {
    if (Get-Command docker -ErrorAction SilentlyContinue) {
        Write-Output "Starting dependency services via docker compose (mysql, redis)..."
        $oldMySqlRootPassword = $env:ZBOARD_MYSQL_ROOT_PASSWORD
        $oldMySqlPassword = $env:ZBOARD_MYSQL_PASSWORD
        if ([string]::IsNullOrWhiteSpace($env:ZBOARD_MYSQL_ROOT_PASSWORD)) {
            $env:ZBOARD_MYSQL_ROOT_PASSWORD = "zboard-local-root-password"
        }
        if ([string]::IsNullOrWhiteSpace($env:ZBOARD_MYSQL_PASSWORD)) {
            $env:ZBOARD_MYSQL_PASSWORD = "zboard-local-db-password"
        }
        Push-Location (Join-Path $projectRoot "deploy\docker")
        try {
            docker compose -f docker-compose.yml up -d mysql redis
        } finally {
            Pop-Location
            if ($null -eq $oldMySqlRootPassword) {
                Remove-Item Env:ZBOARD_MYSQL_ROOT_PASSWORD -ErrorAction SilentlyContinue
            } else {
                $env:ZBOARD_MYSQL_ROOT_PASSWORD = $oldMySqlRootPassword
            }
            if ($null -eq $oldMySqlPassword) {
                Remove-Item Env:ZBOARD_MYSQL_PASSWORD -ErrorAction SilentlyContinue
            } else {
                $env:ZBOARD_MYSQL_PASSWORD = $oldMySqlPassword
            }
        }
    } else {
        Write-Warning "docker not found, skipped mysql/redis bootstrap."
    }
}

if ([string]::IsNullOrWhiteSpace($DataSource)) {
    if ([string]::IsNullOrWhiteSpace($env:ZBOARD_LOCAL_DSN)) {
        $DataSource = "zboard:zboard-local-db-password@tcp(127.0.0.1:3306)/zboard?charset=utf8mb4&parseTime=true&loc=Local"
    } else {
        $DataSource = $env:ZBOARD_LOCAL_DSN
    }
}

if ([string]::IsNullOrWhiteSpace($RedisAddr)) {
    if ([string]::IsNullOrWhiteSpace($env:ZBOARD_LOCAL_REDIS)) {
        $RedisAddr = "127.0.0.1:6379"
    } else {
        $RedisAddr = $env:ZBOARD_LOCAL_REDIS
    }
}

$devSecretsPath = Join-Path $tmpDir "zboard.dev.secrets"
$storedSecrets = @{}
if (Test-Path -LiteralPath $devSecretsPath) {
    Get-Content -LiteralPath $devSecretsPath | ForEach-Object {
        $parts = $_ -split '=', 2
        if ($parts.Count -eq 2) {
            $storedSecrets[$parts[0]] = $parts[1]
        }
    }
}

if ([string]::IsNullOrWhiteSpace($JwtSecret)) {
    if (-not [string]::IsNullOrWhiteSpace($env:ZBOARD_JWT_SECRET)) {
        $JwtSecret = $env:ZBOARD_JWT_SECRET
    } elseif ($storedSecrets.ContainsKey("ZBOARD_JWT_SECRET")) {
        $JwtSecret = $storedSecrets["ZBOARD_JWT_SECRET"]
    } else {
        $JwtSecret = New-ZBoardRandomSecret
    }
}
if ([string]::IsNullOrWhiteSpace($CredentialEncryptionKey)) {
    if (-not [string]::IsNullOrWhiteSpace($env:ZBOARD_CREDENTIAL_ENCRYPTION_KEY)) {
        $CredentialEncryptionKey = $env:ZBOARD_CREDENTIAL_ENCRYPTION_KEY
    } elseif ($storedSecrets.ContainsKey("ZBOARD_CREDENTIAL_ENCRYPTION_KEY")) {
        $CredentialEncryptionKey = $storedSecrets["ZBOARD_CREDENTIAL_ENCRYPTION_KEY"]
    } else {
        $CredentialEncryptionKey = New-ZBoardRandomSecret
    }
}
if ([string]::IsNullOrWhiteSpace($AdminEmail)) {
	$AdminEmail = if ([string]::IsNullOrWhiteSpace($env:ZBOARD_BOOTSTRAP_ADMIN_EMAIL)) { "admin@zboard.local" } else { $env:ZBOARD_BOOTSTRAP_ADMIN_EMAIL }
}
$generatedAdminPassword = $false
if ([string]::IsNullOrWhiteSpace($AdminPassword)) {
    if ([string]::IsNullOrWhiteSpace($env:ZBOARD_BOOTSTRAP_ADMIN_PASSWORD)) {
        if ($storedSecrets.ContainsKey("ZBOARD_BOOTSTRAP_ADMIN_PASSWORD")) {
            $AdminPassword = $storedSecrets["ZBOARD_BOOTSTRAP_ADMIN_PASSWORD"]
        } else {
            $AdminPassword = New-ZBoardRandomSecret -ByteLength 24
            $generatedAdminPassword = $true
        }
    } else {
        $AdminPassword = $env:ZBOARD_BOOTSTRAP_ADMIN_PASSWORD
    }
}

@(
    "ZBOARD_JWT_SECRET=$JwtSecret"
    "ZBOARD_CREDENTIAL_ENCRYPTION_KEY=$CredentialEncryptionKey"
    "ZBOARD_BOOTSTRAP_ADMIN_PASSWORD=$AdminPassword"
) | Set-Content -Encoding ASCII -LiteralPath $devSecretsPath

$sourceConfig = Join-Path $projectRoot "backend/etc/zboard.yaml.example"
$configText = Get-Content -Raw $sourceConfig
$configText = [regex]::Replace($configText, '(?m)^(\s*)environment:\s*.*$', '$1environment: development')
$configText = [regex]::Replace($configText, '(?m)^(\s*)datasource:\s*".*?"', '$1datasource: "' + $DataSource + '"')
$configText = [regex]::Replace($configText, '(?m)^(\s*)redis_addr:\s*".*?"', '$1redis_addr: "' + $RedisAddr + '"')
$configText = [regex]::Replace($configText, '(?m)^(\s*)Port:\s*[0-9]+', '$1Port: ' + $BackendPort)
Set-Content -NoNewline -Encoding UTF8 -Path $runtimeConfig -Value $configText

Write-Output "Local bootstrap admin: $AdminEmail"
if ($generatedAdminPassword) {
    Write-Output "Generated local bootstrap password: $AdminPassword"
    Write-Output "Save this value for subsequent runs against the same database."
}

function Stop-ZBoardProcess {
    param(
        [System.Diagnostics.Process]$ProcessObj,
        [string]$Name
    )
    if ($null -eq $ProcessObj) { return }
    if (-not $ProcessObj.HasExited) {
        Write-Output "Stopping $Name..."
        try {
            Stop-Process -Id $ProcessObj.Id -Force -ErrorAction Stop
        } catch {
            Write-Warning "Failed to stop ${Name}: $($_.Exception.Message)"
        }
    }
}

Write-Output "Starting backend..."
$securityEnvironment = @{
    ZBOARD_ENVIRONMENT = "development"
    ZBOARD_JWT_SECRET = $JwtSecret
    ZBOARD_CREDENTIAL_ENCRYPTION_KEY = $CredentialEncryptionKey
	ZBOARD_BOOTSTRAP_ADMIN_EMAIL = $AdminEmail
    ZBOARD_BOOTSTRAP_ADMIN_PASSWORD = $AdminPassword
}
$previousSecurityEnvironment = @{}
foreach ($name in $securityEnvironment.Keys) {
    $previousSecurityEnvironment[$name] = [Environment]::GetEnvironmentVariable($name, "Process")
    [Environment]::SetEnvironmentVariable($name, $securityEnvironment[$name], "Process")
}
try {
    $backendProc = Start-Process -FilePath "go" -ArgumentList @(
        "run", "./cmd/zboard", "-f", $runtimeConfig
    ) -WorkingDirectory (Join-Path $projectRoot "backend") -RedirectStandardOutput $backendLog -RedirectStandardError $backendLog -PassThru
} finally {
    foreach ($name in $previousSecurityEnvironment.Keys) {
        [Environment]::SetEnvironmentVariable($name, $previousSecurityEnvironment[$name], "Process")
    }
}

$deadline = (Get-Date).AddSeconds($StartupTimeoutSec)
$ready = $false
while ((Get-Date) -lt $deadline) {
    try {
        Invoke-RestMethod -Uri "$ApiBase/healthz" -Method Get -TimeoutSec 2 | Out-Null
        $ready = $true
        break
    } catch {
        if ($backendProc.HasExited) {
            break
        }
        Start-Sleep -Seconds 1
    }
}

if (-not $ready) {
    if ($backendProc.HasExited) {
        $tail = Get-Content -Path $backendLog -Tail 40 -ErrorAction SilentlyContinue
        Write-Error "Backend exited before readiness. Last log:`n$tail"
    }
    Write-Error "Backend not ready within ${StartupTimeoutSec}s. Check logs: $backendLog"
}

Write-Output "Backend ready: $ApiBase/healthz"

$frontendProc = $null
if ($WithFrontend) {
    if (Get-Command pnpm -ErrorAction SilentlyContinue) {
        Write-Output "Starting frontend..."
        Push-Location (Join-Path $projectRoot "frontend")
        $env:VITE_API_BASE = $BackendApiBase
        & pnpm install
        $frontendProc = Start-Process -FilePath "pnpm" -ArgumentList @(
            "dev", "--host", "0.0.0.0", "--port", "$FrontendPort"
        ) -RedirectStandardOutput $frontendLog -RedirectStandardError $frontendLog -PassThru
        Pop-Location
        Write-Output "Frontend started: http://127.0.0.1:$FrontendPort"
    } else {
        Write-Warning "pnpm not found, skip frontend startup."
    }
}

if (-not $NoSmoke) {
	& "$PSScriptRoot\smoke-test.ps1" -ApiBase $ApiBase -Email $AdminEmail -Password $AdminPassword -RequestTimeoutSec ([Math]::Max(1,$SmokeRequestTimeoutSec))
}

if ($StopWhenDone) {
    Stop-ZBoardProcess -ProcessObj $backendProc -Name "backend"
    Stop-ZBoardProcess -ProcessObj $frontendProc -Name "frontend"
    Write-Output "Finished."
    return
}

try {
    Write-Output "Services running. Press Ctrl+C to stop."
    while ($true) {
        if ($backendProc.HasExited) {
            throw "Backend process exited unexpectedly. Check $backendLog"
        }
        Start-Sleep 2
    }
} finally {
    Stop-ZBoardProcess -ProcessObj $backendProc -Name "backend"
    Stop-ZBoardProcess -ProcessObj $frontendProc -Name "frontend"
    if ($null -ne $oldApiBase) {
        $env:VITE_API_BASE = $oldApiBase
    } else {
        Remove-Item Env:VITE_API_BASE -ErrorAction SilentlyContinue
    }
}
