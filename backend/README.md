# Backend

`backend/` stores all server-side capabilities of zboard.

## Start backend

The example config intentionally contains no datasource, JWT secret, or bootstrap password.
Set them explicitly before first startup:

```bash
cd backend
export ZBOARD_ENVIRONMENT=development
export ZBOARD_DATA_SOURCE='zboard:<local-db-password>@tcp(127.0.0.1:3306)/zboard?charset=utf8mb4&parseTime=true&loc=Local'
export ZBOARD_JWT_SECRET='<at-least-32-random-bytes>'
export ZBOARD_BOOTSTRAP_ADMIN_USERNAME='<local-admin-name>'
export ZBOARD_BOOTSTRAP_ADMIN_EMAIL='<local-admin-email>'
export ZBOARD_BOOTSTRAP_ADMIN_PASSWORD='<at-least-12-random-bytes>'
go run ./cmd/zboard -f ./etc/zboard.yaml.example
```

If Go is not in PATH, use local fallback:

```powershell
cd ../scripts
./ensure-go-env.ps1
```

To refresh `backend/go.mod` baseline before release:

```powershell
cd ../scripts
./sync-go-baseline.ps1
```

```bash
./sync-go-baseline.sh
```

`ensure-go-env.ps1` auto-installs missing Go into `C:\Users\higanbana\sdk\golang` by default; pass `-AutoInstall:$false` to disable.
The checked-in `go` and `toolchain` directives are the reproducible build baseline:

- `go` directive: compatibility family in `backend/go.mod`
- `toolchain`: exact repository build toolchain

If your environment blocks `go.dev` download, preinstall Go in `C:\Users\higanbana\sdk\golang` and run with `-AutoInstall:$false`.

You can also disable bash auto install by setting `ZBOARD_AUTO_INSTALL_GO=0`.

You can run a quick environment check:

```powershell
cd ../scripts
./verify-env.ps1
```

```bash
cd ../scripts
./check-go-version.sh
```

```powershell
cd ../scripts
./check-go-version.ps1
```

## Local startup script

If you want a quick end-to-end local launch flow (backend auto-start + dependency bootstrap + smoke test):

PowerShell:

```powershell
cd ../scripts
./start-dev.ps1
```

```bash
./scripts/start-dev.sh
```

Script behavior:

- auto ensure Go environment
- auto start `mysql` and `redis` via docker compose (optional)
- rewrite runtime config datasource/redis to local addresses
- generate a random local JWT secret and bootstrap password when not supplied
- rewrite runtime config `Port` from `--backend-port`
- wait `/healthz` ready and run smoke test
- keep running until you stop the script (or use `-StopWhenDone` / `--stop-when-done`)

The generated bootstrap password is printed once. Reuse it through
`ZBOARD_BOOTSTRAP_ADMIN_PASSWORD` when starting against the same database; existing users are never overwritten.

By default, start scripts enforce the checked-in baseline (`--check-only`) and use Go 1.26.5.
To downgrade to non-failing mode for local/offline flows, set:

```bash
export ZBOARD_ENFORCE_GO_BASELINE=0
```

You can also tune lookup/download/smoke timeouts via:

```bash
export ZBOARD_GO_QUERY_TIMEOUT=8
export ZBOARD_GO_QUERY_BUDGET_SEC=30
export ZBOARD_GO_QUERY_RETRY_LIMIT=3
export ZBOARD_GO_DOWNLOAD_TIMEOUT=120
export ZBOARD_SMOKE_TIMEOUT=10
```

## Directory layout

- `cmd/zboard/` app bootstrap and startup
- `internal/config/` runtime configuration
- `internal/handler/` request handlers and response contracts
- `internal/model/` GORM entities
- `internal/datastore/` DB connection helper
- `internal/server/` route registration
- `internal/version/` build time version metadata
- `migrations/` SQL migration templates
- `api/openapi.yaml` API contract

## Core feature set

- Auth: register / login / user info, admin role control
- Nodes: list/create/SSH test/protocol config push (all node APIs require login; node create/config operations require admin)
- Plans: list and create
- Orders: user order create, callback update
- Flow metering: traffic report and remaining quota deduction
- Subscriptions: query
- Traffic summary: usage statistics and remaining flow
- Admin dashboard metrics

## Version and release baseline

- Go baseline checks require the repository-pinned toolchain unless an explicit maintenance override is supplied.
- `verify-env` prints the effective `go.mod` minimum go directive and toolchain baseline so startup policy is visible.
- CI/release policy is also sourced from `backend/go.mod`:
  - `go` minimum compatibility floor
  - `toolchain` baseline version
- If network policy blocks go.dev, preinstall the pinned SDK or configure `ZBOARD_GO_DOWNLOAD_BASE` to a trusted mirror.
- Version policy in this repo starts at `v0.0.1`, and first public release is `v0.1.0`.
- Runtime version metadata is set via build tags and `VERSION`; release and build scripts inject release version/commit/time with ldflags.
- Go build baseline in this repo is controlled by `backend/go.mod`; upgrades are reviewed repository changes.
