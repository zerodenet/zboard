# Local development

This guide contains the contributor setup that does not belong in the project
overview. For product architecture and supported capabilities, start with the
repository `README.md`.

## Toolchain

Use the versions declared by the repository instead of choosing local defaults:

- Go and the exact toolchain are declared in `backend/go.mod`;
- Node.js is pinned by the frontend and Docker build;
- pnpm is declared by `frontend/package.json#packageManager`;
- MySQL 8 is required for the current local service.

The environment checks can resolve the repository Go toolchain automatically:

```powershell
.\scripts\verify-env.ps1
```

```bash
./scripts/verify-env.sh
```

## One-command startup

The development launcher verifies dependencies, starts MySQL through Docker
Compose when necessary, creates ignored local runtime configuration,
starts the backend and optionally starts the frontend.

PowerShell:

```powershell
.\scripts\start-dev.ps1 -WithFrontend
```

Bash:

```bash
./scripts/start-dev.sh --with-frontend
```

Use `-SkipDependencies` or `--skip-deps` when MySQL already runs
outside the repository Compose stack. Both launchers expose additional port,
timeout and datasource options in their built-in help or parameter list.

The backend is available at `http://127.0.0.1:8080`; the frontend development
server defaults to `http://127.0.0.1:5173`.

## Manual backend startup

Manual startup requires a datasource, a JWT secret of at least 32 bytes and one
stable 32-byte credential-encryption key:

```powershell
Set-Location backend
$env:ZBOARD_ENVIRONMENT = "development"
$env:ZBOARD_DATA_SOURCE = "zboard:<password>@tcp(127.0.0.1:3306)/zboard?charset=utf8mb4&parseTime=true&loc=Local"
$env:ZBOARD_JWT_SECRET = "<at-least-32-random-bytes>"
$env:ZBOARD_CREDENTIAL_ENCRYPTION_KEY = "<32-random-bytes-as-base64-or-hex>"
go run ./cmd/zboard -f ./etc/zboard.yaml.example
```

On an empty database the service enters installation mode. Open `/setup` to
create the first administrator and finish site initialization.

The embedded SQL baseline is applied during startup. To run migrations without
starting the HTTP service, use `scripts/migrate.ps1` or `scripts/migrate.sh`.
See [database-migrations.md](database-migrations.md) before opening an existing
development database with a newer build.

## Manual frontend startup

```powershell
Set-Location frontend
$env:VITE_API_BASE = "http://127.0.0.1:8080/api/v1"
pnpm install --frozen-lockfile
pnpm dev
```

On Bash-compatible shells:

```bash
cd frontend
VITE_API_BASE=http://127.0.0.1:8080/api/v1 pnpm install --frozen-lockfile
VITE_API_BASE=http://127.0.0.1:8080/api/v1 pnpm dev
```

## Verification

Run checks in proportion to the changed area. A complete local verification is:

```powershell
Set-Location backend
go test ./...
go vet ./...

Set-Location ..\frontend
pnpm test
pnpm build
```

`pnpm build` includes Vue and TypeScript type checking. API changes must also
update `backend/api/openapi.yaml` and its contract tests.

The repository also provides:

- `scripts/smoke-test.*` for a running service;
- `scripts/build-all.*` for combined backend and frontend builds;
- `scripts/check-go-version.*` and `scripts/sync-go-baseline.*` for toolchain
  maintenance.

## Performance and stability verification

Run these commands from the repository root with the pinned Go toolchain
(`GO_BIN` can select its executable):

```bash
bash scripts/benchmark-accounting.sh local
bash scripts/benchmark-accounting.sh container
bash scripts/acceptance-mixed.sh
```

The accounting benchmark defaults to 100 batches per scenario and three runs;
`BENCH_TIME` and `BENCH_COUNT` override these settings. Container checks require
Docker. The mixed workload defaults to 10 nodes, 1,000 subscriptions, 100,000
historical records, four concurrent readers and 100 events/second for 300 seconds.
`DURATION_SECONDS`, `EVENT_RATE` and `READERS` select other workload profiles.
The application and database share the 1 CPU / 1 GiB budget; the load generator
runs outside it. The [roadmap](roadmap.md#资源和性能预算) defines latency and memory
budgets. Also verify exact accounting under replay and reordering, no OOM,
explained failures and a fully drained backlog.

Real Zero revocation checks use isolated nodes and test credentials:

```bash
ZERO_ARTIFACT_DIR=/path/to/verified-linux-zero-artifact \
  NODE_SCENARIO=expiry bash scripts/acceptance-node.sh
```

The artifact directory must contain `zero` and its matching `verification.json`.
Other scenarios are `exhaustion`, `group_change` and `recovery`. Verify data-plane
access after revocation and publication recovery, including existing connections;
control-plane status or a mock SSH server alone is insufficient. See also
[node publication](node-config-delivery.md#重跑-mysql-验证) for real MySQL checks.

Keep raw logs, profiles, environment details, source/build hashes and per-run
reports under the ignored `.codex-local-artifacts/acceptance/` directory. A dirty
build needs source hashes as well as Git HEAD. Share selected evidence through
CI/release artifacts when needed, without committing workstation journals.

A 500-events/second, 60-second burst and a 24-hour soak are separate acceptance
profiles. Record fault injection, restarts, queue recovery and resource trends;
restart the soak clock after changing the build or resetting the environment.
Short runs do not establish long-term stability or production capacity.

## Restricted networks

`scripts/ensure-go-env.*` can install the pinned Go toolchain when it is
missing. Set `ZBOARD_GO_DOWNLOAD_BASE` to an approved mirror when direct
downloads from `go.dev` are unavailable. A preinstalled Go directory can be
selected with `ZBOARD_GOROOT_FALLBACK`.

Do not weaken production secrets or commit generated runtime configuration to
work around a local environment problem.
