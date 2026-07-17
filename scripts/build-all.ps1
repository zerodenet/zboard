param(
    [string]$Version = "",
    [switch]$SkipGoBaselineCheck
)

if ([string]::IsNullOrWhiteSpace($Version)) {
    $versionFile = Join-Path (Resolve-Path "$PSScriptRoot\..").Path "VERSION"
    if (Test-Path $versionFile) {
        $Version = (Get-Content -Path $versionFile -Raw).Trim()
    } else {
        $Version = "v0.0.1"
    }
}

& "$PSScriptRoot\ensure-go-env.ps1"
$CurrentCheckPolicy = if ($SkipGoBaselineCheck) { "skipped" } else { "required" }
Write-Output "Go baseline check: ${CurrentCheckPolicy}."
if (-not $SkipGoBaselineCheck) {
    & "$PSScriptRoot\sync-go-baseline.ps1" -RequiredVersion 1.26.5 -CheckOnly
}
$Commit = "local"
$BuildTime = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")

Write-Output "Build backend..."
Set-Location -Path "..\backend"
go mod tidy
go build -ldflags "-X github.com/zerodenet/zboard/backend/internal/version.Version=$Version -X github.com/zerodenet/zboard/backend/internal/version.Commit=$Commit -X github.com/zerodenet/zboard/backend/internal/version.BuildTime=$BuildTime" -o bin/zboard ./cmd/zboard

Write-Output "Build frontend..."
Set-Location -Path "..\frontend"
pnpm install --frozen-lockfile
pnpm build
Write-Output "Build done."
