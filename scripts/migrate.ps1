param(
    [string]$Config = "etc/zboard.yaml"
)

$projectRoot = Split-Path -Parent $PSScriptRoot
$backendDir = Join-Path $projectRoot "backend"

Push-Location $backendDir
try {
    & go run ./cmd/zboard -f $Config -migrate-only
    if ($LASTEXITCODE -ne 0) {
        exit $LASTEXITCODE
    }
} finally {
    Pop-Location
}
