param(
    [string]$Target = "gitlab",
    [string]$RemoteRoot = "/data/zboard-next",
    [string]$Version = "",
    [switch]$SkipLocalChecks
)

$ErrorActionPreference = "Stop"

$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$stamp = (Get-Date).ToUniversalTime().ToString("yyyyMMddTHHmmssZ")
$buildTime = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
if ([string]::IsNullOrWhiteSpace($Version)) {
    $Version = "v0.0.1-${stamp}-intranet"
}
if ($RemoteRoot -notmatch '^/data/[A-Za-z0-9._/-]+$') {
    throw "RemoteRoot must be an absolute path below /data."
}
if ($Version -notmatch '^[A-Za-z0-9._@:+-]+$') {
    throw "Version contains unsupported characters."
}
foreach ($command in @("ssh", "scp", "tar.exe")) {
    if (-not (Get-Command $command -ErrorAction SilentlyContinue)) {
        throw "$command is required."
    }
}

if (-not $SkipLocalChecks) {
    $go = Get-Command go -ErrorAction SilentlyContinue
    $goPath = if ($go) { $go.Source } else { "C:\Users\higanbana\sdk\golang\go1.26.5\bin\go.exe" }
    if (-not $go -and -not (Test-Path -LiteralPath $goPath)) {
        throw "Go 1.26.5 is required for local verification."
    }
    $pnpm = Get-Command pnpm -ErrorAction SilentlyContinue
    $pnpmPath = if ($pnpm) { $pnpm.Source } else { "C:\Users\higanbana\.cache\codex-runtimes\codex-primary-runtime\dependencies\bin\fallback\pnpm.cmd" }
    if (-not $pnpm -and -not (Test-Path -LiteralPath $pnpmPath)) {
        throw "pnpm is required for local verification."
    }

    Push-Location (Join-Path $projectRoot "backend")
    try {
        & $goPath test ./...
        if ($LASTEXITCODE -ne 0) { throw "go test failed." }
        & $goPath vet ./...
        if ($LASTEXITCODE -ne 0) { throw "go vet failed." }
    } finally {
        Pop-Location
    }
    Push-Location (Join-Path $projectRoot "frontend")
    try {
        & $pnpmPath build
        if ($LASTEXITCODE -ne 0) { throw "frontend build failed." }
    } finally {
        Pop-Location
    }
}

$temporaryRoot = Join-Path ([System.IO.Path]::GetTempPath()) "zboard-intranet-sync-$stamp"
$archivePath = Join-Path $temporaryRoot "source-$stamp.tar.gz"
New-Item -ItemType Directory -Path $temporaryRoot | Out-Null
try {
    & tar.exe -czf $archivePath `
        --exclude=.git `
        --exclude=.codex-cache `
        --exclude=.codex-local-artifacts `
        --exclude=.env `
        --exclude=frontend/node_modules `
        --exclude=frontend/dist `
        --exclude=backend/bin `
        -C $projectRoot .
    if ($LASTEXITCODE -ne 0 -or -not (Test-Path -LiteralPath $archivePath)) {
        throw "Failed to create the source archive."
    }

    $remoteUpload = "/tmp/zboard-source-$stamp.tar.gz"
    & scp $archivePath "${Target}:$remoteUpload"
    if ($LASTEXITCODE -ne 0) { throw "Failed to upload the source archive." }

    $remoteScript = @"
set -euo pipefail
ROOT='$RemoteRoot'
STAMP='$stamp'
VERSION='$Version'
BUILD_TIME='$buildTime'
UPLOAD='$remoteUpload'
CANDIDATE="`$ROOT/candidates/`$STAMP"
RELEASE_DIR="`$ROOT/releases/`$STAMP"
BACKUP_DIR="`$ROOT/backups/`$STAMP"
PREVIOUS="`$ROOT/app-prev-`$STAMP"
FAILED="`$ROOT/app-failed-`$STAMP"

test -f "`$ROOT/.env"
test ! -e "`$CANDIDATE"
test ! -e "`$PREVIOUS"
mkdir -p "`$CANDIDATE" "`$RELEASE_DIR" "`$BACKUP_DIR"
mv "`$UPLOAD" "`$RELEASE_DIR/source.tar.gz"
tar -xzf "`$RELEASE_DIR/source.tar.gz" -C "`$CANDIDATE"

cd "`$CANDIDATE"
set -a
source "`$ROOT/.env"
set +a
export ZBOARD_ZERO_ARTIFACT_HOST_DIR="`${ZBOARD_ZERO_ARTIFACT_HOST_DIR:-`$ROOT/artifacts}"
export ZBOARD_MANAGED_RULE_HOST_DIR="`${ZBOARD_MANAGED_RULE_HOST_DIR:-`$ROOT/managed-rules}"
export ZBOARD_ZERO_EVENT_SPOOL_HOST_DIR="`${ZBOARD_ZERO_EVENT_SPOOL_HOST_DIR:-`$ROOT/zero-events}"
export ZBOARD_ZERO_EVENT_SPOOL_BLUE_HOST_DIR="`${ZBOARD_ZERO_EVENT_SPOOL_BLUE_HOST_DIR:-`$ROOT/zero-events-blue}"
export ZBOARD_ZERO_EVENT_SPOOL_GREEN_HOST_DIR="`${ZBOARD_ZERO_EVENT_SPOOL_GREEN_HOST_DIR:-`$ROOT/zero-events-green}"
export ZBOARD_VERSION="`$VERSION"
export ZBOARD_COMMIT=working-tree
export ZBOARD_BUILD_TIME="`$BUILD_TIME"
sh deploy/docker/prepare-host-dirs.sh
docker compose -p zboard_next -f deploy/docker/docker-compose.yml build zboard

docker exec "`$ZBOARD_EXTERNAL_MYSQL_CONTAINER" sh -c 'exec mysqldump -uroot -p"`$MYSQL_ROOT_PASSWORD" --single-transaction --routines --triggers zboard' > "`$BACKUP_DIR/zboard-before-sync.sql"
test -s "`$BACKUP_DIR/zboard-before-sync.sql"

if [ -d "`$ROOT/app" ]; then mv "`$ROOT/app" "`$PREVIOUS"; fi
cp -a "`$CANDIDATE" "`$ROOT/app"
cd "`$ROOT/app"
if ! docker compose -p zboard_next -f deploy/docker/docker-compose.yml up -d --no-deps zboard; then
    mv "`$ROOT/app" "`$FAILED"
    if [ -d "`$PREVIOUS" ]; then
        mv "`$PREVIOUS" "`$ROOT/app"
        cd "`$ROOT/app"
        docker compose -p zboard_next -f deploy/docker/docker-compose.yml up -d --no-deps zboard
    fi
    exit 1
fi

healthy=false
for attempt in `$(seq 1 45); do
    state=`$(docker inspect -f '{{.State.Health.Status}}' zboard_next-zboard-1 2>/dev/null || true)
    if [ "`$state" = healthy ]; then healthy=true; break; fi
    sleep 2
done
if [ "`$healthy" != true ]; then
    docker logs --tail 120 zboard_next-zboard-1 >&2 || true
    mv "`$ROOT/app" "`$FAILED"
    if [ -d "`$PREVIOUS" ]; then
        mv "`$PREVIOUS" "`$ROOT/app"
        cd "`$ROOT/app"
        docker compose -p zboard_next -f deploy/docker/docker-compose.yml up -d --no-deps zboard
    fi
    exit 1
fi

curl -fsS http://127.0.0.1:18080/api/v1/version
printf '\n'
curl -fsS http://127.0.0.1:18080/readyz
printf '\n'
docker ps --filter label=com.docker.compose.project=zboard_next --format '{{.Names}} {{.Status}}'
printf 'BACKUP=%s\nPREVIOUS=%s\nRELEASE=%s\n' "`$BACKUP_DIR/zboard-before-sync.sql" "`$PREVIOUS" "`$RELEASE_DIR/source.tar.gz"
"@
    $encoded = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($remoteScript))
    & ssh $Target "echo $encoded | base64 -d | bash"
    if ($LASTEXITCODE -ne 0) { throw "Intranet synchronization failed." }
} finally {
    if (Test-Path -LiteralPath $temporaryRoot) {
        $resolvedTemporary = (Resolve-Path -LiteralPath $temporaryRoot).Path
        $resolvedTempBase = (Resolve-Path -LiteralPath ([System.IO.Path]::GetTempPath())).Path
        if ($resolvedTemporary.StartsWith($resolvedTempBase, [System.StringComparison]::OrdinalIgnoreCase)) {
            Remove-Item -LiteralPath $resolvedTemporary -Recurse -Force
        }
    }
}
