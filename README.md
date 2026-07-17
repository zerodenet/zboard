# zboard

Monolith operations platform for proxy panel management and subscription billing.

- Backend: `go-zero` + `gorm`
- Frontend: `Vue 3 + Vite + Pinia + Vue Router`
- Shared API: `RESTful /api/v1` by default
- Optional UI components: `shadcn-vue`

Repository: `https://github.com/zerodenet/zboard`

## Version policy

- Current development baseline: `v0.0.1`
- First public release target: `v0.1.0`
- All engineering, security, business-loop, operability, and release-readiness work before the first public release remains `v0.0.1`.
- Internal phases are tracked by exit criteria and Git commits, not by incrementing `v0.0.x`; the version changes only when `v0.1.0` is ready to publish.
- Implementation work stages and exit criteria: [docs/roadmap.md](docs/roadmap.md)
- Then follow SemVer for maintenance versions
- Reproducible builds use the checked-in `go`/`toolchain` directives and `frontend/package.json` `packageManager` value.
- Toolchain upgrades are explicit repository changes; CI does not silently chase the latest release.

## v0.1.0 capability scope

- Admin and normal users, admin can also be a normal subscriber
- Node management with SSH operation tests
- Node APIs (including list) are authenticated; public access is no longer allowed
- Protocol template distribution to nodes
- Plans, orders, subscriptions, and traffic usage summary
- Traffic report endpoint for quota deduction and billing reconciliation
- Admin user management: list/create/update users, ban/resume, admin role toggle, password reset
- Order callback supports paid callback and subscription renew flow
- Standard API namespace `/api/v1` with `/api/v1/system/info`
- API contract file: `backend/api/openapi.yaml`
- Docker Compose deployment, health checks, backup, upgrade and rollback documentation
- Kubernetes deployment and Kubernetes-driven upgrade APIs are outside the `v0.1.0` support scope.

## v0.1.1 Planned

- 订单体系 v0.1.1 计划补齐：
  - 更多支付渠道接入（签名校验、异步回调、退款/关闭订单）
  - 订阅计费模型增强（按流量包/并发设备/阶梯折扣）
  - 节点上线质量采集与告警（离线、证书、端口检查）
- 前后端体验优化：订阅续费提醒、管理员审计、回收站与分页筛选。
## Repository structure

- `backend/` service code, configuration, model, and SQL migrations
- `frontend/` management console
- `deploy/` Docker deployment assets
- `docs/` operation and upgrade documentation
- `.github/` CI and release workflows

## Quick start

### Prerequisites

- Go toolchain: use the exact version declared by `backend/go.mod`.
- If your local `go` is missing, `scripts/ensure-go-env.*` will install to `C:\Users\higanbana\sdk\golang` by default.
- MySQL 8+ and Redis

### Backend

```powershell
cd scripts
./ensure-go-env.ps1
cd ../backend
go run ./cmd/zboard -f ./etc/zboard.yaml.example
```

`ensure-go-env.ps1` supports auto-install (default on) to `C:\Users\higanbana\sdk\golang` when local Go is missing.
Use `-AutoInstall:$false` to turn it off, or set `ZBOARD_AUTO_INSTALL_GO=0` in bash.
如需配置下载镜像源，请设置 `ZBOARD_GO_DOWNLOAD_BASE`（默认 `https://go.dev/dl`），用于在受限网络下指向可用镜像。

Quick environment verify:
```powershell
cd scripts
./verify-env.ps1
```

```bash
cd scripts
./verify-env.sh
```

`verify-env` prints the active Go target so you can confirm it matches the repository baseline.

### Local one-command startup

PowerShell:

```powershell
cd scripts
./start-dev.ps1
```

```bash
./scripts/start-dev.sh
chmod +x scripts/start-dev.sh
./scripts/start-dev.sh
```

The one-command startup flow does:

1. dependency check (`verify-env`)
2. auto start mysql/redis with docker compose (can skip with `-SkipDependencies` / `--skip-deps`)
3. generate runtime config with local `datasource`, `redis_addr`, and `Port` (from `BackendPort` / `--backend-port`)
4. start backend
5. when frontend is enabled, inject `VITE_API_BASE` automatically
6. wait for `/healthz`
7. smoke test (`smoke-test`)

Use the version declared by `backend/go.mod`; explicit overrides are intended only for toolchain upgrade work.

You can also run a dedicated version check:

```bash
cd scripts
./check-go-version.sh
```

```powershell
cd scripts
./check-go-version.ps1
```

Validate the checked-in baseline with:

```powershell
./scripts/sync-go-baseline.ps1 -RequiredVersion 1.26.5 -CheckOnly
```

```bash
./scripts/sync-go-baseline.sh --check-only --target 1.26.5
```

Recommended flags:

PowerShell:

```powershell
./start-dev.ps1 -WithFrontend -StopWhenDone
./start-dev.ps1 -ApiBase "http://127.0.0.1:8080" -StopWhenDone
./start-dev.ps1 -BackendPort 8080 -WithFrontend -StopWhenDone
./start-dev.ps1 -GoVersion 1.26.5
```

```bash
./scripts/start-dev.sh --with-frontend --stop-when-done
./scripts/start-dev.sh --backend-port 8080 --skip-deps --with-frontend
./scripts/start-dev.sh --backend-port 8080 --stop-when-done
./scripts/start-dev.sh --go-version 1.26.5
```

可调超时参数（按你网络情况设置）：
- PowerShell:
  - `-GoQueryTimeoutSec`（默认 8）：go 稳定版本查询超时
  - `-QueryTimeoutBudgetSec`（默认 30）：查询总预算时长
  - `-QueryRetryLimit`（默认 3）：远端查询尝试上限
  - `-GoDownloadTimeoutSec`（默认 120）：缺失 go 自动安装包下载超时
  - `-SmokeRequestTimeoutSec`（默认 10）：启动冒烟检测接口超时
- Bash:
  - `--go-query-timeout`（默认 8）
  - `--go-query-budget-sec`（默认 30）：查询总预算时长
  - `--go-query-retry-limit`（默认 3）：远端查询尝试上限
  - `--go-download-timeout`（默认 120）
  - `--smoke-timeout`（默认 10）

你也可以通过环境变量直接控制（脚本都读取）：
- `ZBOARD_GO_QUERY_TIMEOUT`
- `ZBOARD_GO_QUERY_BUDGET_SEC`
- `ZBOARD_GO_QUERY_RETRY_LIMIT`
- `ZBOARD_GO_DOWNLOAD_TIMEOUT`
- `ZBOARD_SMOKE_TIMEOUT`
- `ZBOARD_ENFORCE_GO_BASELINE=0`（可选）：默认关闭基线严格模式时使用 `--dry-run`，设置为 `1`/`true` 启用严格检查（bash/ps1 都适用），并在 `start-dev*` 检测到 `go.mod` 与官方最新不一致时直接退出。

If you omit `-StopWhenDone` / `--stop-when-done`, the script keeps backend/frontend running and returns on `Ctrl+C`.

### Frontend

If you manually run frontend dev server, set backend API base explicitly:

```bash
cd frontend
VITE_API_BASE=http://127.0.0.1:8080/api/v1 pnpm install
VITE_API_BASE=http://127.0.0.1:8080/api/v1 pnpm dev
```

PowerShell:

```powershell
cd frontend
$env:VITE_API_BASE = "http://127.0.0.1:8080/api/v1"
pnpm install
pnpm dev
```

### Single-binary deployment

```bash
docker compose -f deploy/docker/docker-compose.yml up --build
```

- Backend exposes `/api/v1` and serves frontend bundle when `ZBOARD_WEB_DIR` is configured by `deploy/docker/Dockerfile`.

## v0.0.1 playbook

1. Start dependencies:
```bash
docker compose -f deploy/docker/docker-compose.yml up -d mysql redis
```
2. Start backend (bootstrap admin created automatically):
```bash
cd backend
go run ./cmd/zboard -f ./etc/zboard.yaml.example
```
3. Start frontend in dev mode:
```bash
cd frontend
pnpm install
pnpm dev
```
4. Basic smoke test:
- Login default admin: `admin / admin123`
- Create nodes, create plans, create order from plans
- Run SSH test and protocol publish on node APIs when real SSH target exists

## Quick smoke validation

```powershell
cd scripts
./smoke-test.ps1
```

```bash
cd scripts
./smoke-test.sh
```

```bash
chmod +x scripts/smoke-test.sh
```

```bash
cd scripts
./verify-env.sh
```

## Release

- Binary and image build version comes from release tag or `VERSION` file.
- v0.1.0 首发落地清单见 `docs/release/v0.1.0-launch-checklist.md`.
- Kubernetes assets and Kubernetes-based rollout automation are not part of the supported v0.1.0 path.

## Release notes

- `RELEASING.md` + `CHANGELOG.md` are the source of release content.
- For v0.1.0, use `docs/release/v0.1.0-launch-checklist.md` as the final execution checklist.

## Go version strategy

- `backend/go.mod` is the source of truth for the Go compatibility and toolchain versions.
- CI reads that file through `actions/setup-go`; the Docker builder uses the matching pinned image.
- Updating Go is a reviewed maintenance change that must pass tests and image builds before merging.

PowerShell 环境的 `ensure-go-env.ps1` 也会读取 `ZBOARD_GOROOT_FALLBACK`（未设置则回退到 `C:\Users\higanbana\sdk\golang`）。

### 离线/受限网络应急

若启动时无法获取仓库锁定的 Go 版本，请按顺序处理：

1. 先确认本机已有可用 `go`，并临时使用本地版本：
   ```powershell
   $env:ZBOARD_ALLOW_STALE_GO_VERSION = "1"
   ./scripts/verify-env.ps1
   ```
   ```bash
   export ZBOARD_ALLOW_STALE_GO_VERSION=1
   ./scripts/verify-env.sh
   ```
2. 如需固定版本避免再次拉取，可设置 `ZBOARD_REQUIRED_GO_VERSION`（例如 `<version>`）：
   ```powershell
   $env:ZBOARD_REQUIRED_GO_VERSION = "<version>"
   ```
   ```bash
   export ZBOARD_REQUIRED_GO_VERSION=<version>
   ```
3. 如需在企业镜像网络下下载 Go，请设置：
   ```powershell
   $env:ZBOARD_GO_DOWNLOAD_BASE = "<internal-golang-dl-url>"
   ./scripts/verify-env.ps1
   ```
   ```bash
   export ZBOARD_GO_DOWNLOAD_BASE=<internal-golang-dl-url>
   ./scripts/verify-env.sh
   ```
4. 若需脱网构建且本地无可用 `go`，请先放置预装的 Go 到 `C:\Users\higanbana\sdk\golang\bin\go.exe`（或设置 `ZBOARD_GOROOT_FALLBACK` 到你的本机 Go 目录），再重跑脚本。
