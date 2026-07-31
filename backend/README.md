# Backend

`backend/` stores all server-side capabilities of zboard.

## Start backend

The example config intentionally contains no datasource, JWT secret, or credential-encryption key.
Set those infrastructure secrets explicitly before first startup:

```bash
cd backend
export ZBOARD_ENVIRONMENT=development
export ZBOARD_DATA_SOURCE='zboard:<local-db-password>@tcp(127.0.0.1:3306)/zboard?charset=utf8mb4&parseTime=true&loc=Local'
export ZBOARD_JWT_SECRET='<at-least-32-random-bytes>'
export ZBOARD_CREDENTIAL_ENCRYPTION_KEY='<exactly-32-random-bytes-as-base64-or-hex>'
export ZBOARD_ZERO_ARTIFACT_DIR='/var/lib/zboard/artifacts'
export ZBOARD_ZERO_KERNEL_CONTRACT='legacy'
go run ./cmd/zboard -f ./etc/zboard.yaml.example
```

`ZBOARD_ZERO_KERNEL_CONTRACT=native-local` enables the locally validated managed-user
and generic Webhook Connector contract. Keep the default `legacy` value until that
matching local Zero build has been packaged and deployed to the target nodes; this
flag does not download or publish the local kernel. Native-local also requires an
explicit `ZBOARD_ZERO_LOCAL_VERSION`; zboard reads
`zero-v<version>-linux-x86_64-musl.tar.gz` and its matching `.sha256` only from
`ZBOARD_ZERO_ARTIFACT_DIR`, without consulting GitHub.

`native-local-mieru` is a separate opt-in contract for a future pinned Zero
artifact that accepts and propagates Mieru `principal_key`. Only that contract
creates or delivers per-subscription Mieru credentials, and it switches
delivery only after the artifact passes `zero validate`, activation, health
and Connector confirmation on the target node.

Without that explicit contract, `/api/v1/version` reports Mieru as unsupported
in `protocol_capabilities`. The backend rejects Mieru creation, re-enabling and
publication, excludes retained records from subscriptions, and permits an
existing Mieru record to be saved only while disabling it. The administrator
protocol picker displays the unavailable option and the kernel reason rather
than silently hiding it.

With an empty database the backend remains available in installation mode. Open the
frontend `/setup` route to configure the site and first administrator. The database,
JWT, and credential-encryption values deliberately remain deployment-owned secrets and
are never accepted from a browser. Bootstrap admin environment variables are optional
and intended only for unattended or legacy deployment automation.

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
- generate and cache random local JWT, bootstrap, and credential-encryption secrets when not supplied
- rewrite runtime config `Port` from `--backend-port`
- wait `/healthz` ready and run smoke test
- keep running until you stop the script (or use `-StopWhenDone` / `--stop-when-done`)

The generated bootstrap password is printed once. Reuse it through
`ZBOARD_BOOTSTRAP_ADMIN_PASSWORD` when starting against the same database; existing users are never overwritten.
Local generated secrets are cached in ignored `tmp/zboard.dev.secrets`.
Back up the production credential-encryption key separately from the database.

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
- `migrations/` embedded v0.0.1 schema baseline and future release migrations tracked in `schema_migrations`
- `api/openapi.yaml` API contract

## Database schema

Before the first public release, `0001_init.up.sql` is the only migration
shipped by the repository and directly describes the complete current schema.
Development-only `ALTER`, backfill and temporary compatibility steps are
squashed into that file instead of becoming permanent migration history.

Startup accepts either an empty database or a fully migrated earlier
`v0.0.1` database. Earlier databases must contain the original baseline record
and the terminal pre-squash migration record; the runner then verifies the
final table, column and index signature. Existing applied rows are retained for
rollback compatibility with the immediately previous development binary; only
new databases contain a single baseline row. Partial histories and unversioned
non-empty schemas fail startup with an explicit recovery message. Production
startup does not call GORM `AutoMigrate`.

See [`../docs/database-migrations.md`](../docs/database-migrations.md) for the
upgrade, verification and post-`v0.1.0` append-only rules.

## Core feature set

- Auth: register / login / user info, admin role control
- Nodes: independent VPS asset lifecycle, Zero/report credentials, SSH connection verification, and an administrator-only interactive browser terminal using one-time tickets (all node management APIs require admin)
- Protocol endpoints: separately saved node-bound service configuration, explicit SSH deployment, and endpoint-only traffic multiplier
- Node groups: reusable endpoint membership boundaries selected by plans
- Plans: list/create/update with one required node group; plans never bind endpoints or add another multiplier
- Orders: user order create, callback update
- Flow metering: signed node traffic reports, replay-safe idempotency, and serialized remaining-quota deduction
- Subscriptions: query
- Traffic summary: usage statistics and remaining flow
- Admin dashboard metrics
- Typed system configuration: revision-checked updates with encrypted secret values
- Operational tasks: itemized quota adjustments and opt-in TLS-protected SMTP email batches

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

## Trusted node traffic reports

An administrator creates or rotates a node credential with
`POST /api/v1/nodes/{id}/report-credential`. The plaintext `secret` is returned once; only its
encrypted value and non-secret prefix are stored. `DELETE` on the same path revokes it.

Nodes submit `POST /api/v1/traffic/report` without a bearer token. The request must include:

- `X-Zboard-Node-ID`: canonical positive decimal node ID
- `X-Zboard-Timestamp`: canonical Unix seconds within five minutes of server time
- `X-Zboard-Nonce`: unique 16-64 character URL-safe value
- `X-Zboard-Signature`: hexadecimal HMAC-SHA256

Hash the exact JSON request bytes with SHA-256. Sign this canonical UTF-8 value with the node secret:

```text
node_id + "\n" + unix_timestamp + "\n" + nonce + "\n" + lowercase_hex(body_sha256)
```

The JSON body contains `report_id`, `user_id`, `protocol_endpoint_id`, raw `upload_bytes` and
`download_bytes`, and optional `meta`. `raw_bytes` remains a legacy aggregate fallback. The endpoint
must belong to the authenticated node and the subscription's node group. Stored billed bytes apply
the protocol endpoint multiplier after selecting both, upload-only, or download-only traffic
according to the subscription policy. Reusing the same
`(node_id, report_id)` returns `duplicate: true` without another deduction; reusing a nonce for a
different report is rejected. See `api/openapi.yaml` for field constraints and response schemas.

Order settlement locks both the order and the subscriber renewal boundary. Repeated paid results are
idempotent, paid orders cannot be moved back to failed/canceled, and expired or exhausted subscriptions
are reconciled before list, manifest, summary, dashboard, renewal, and traffic-deduction operations.
The current `pay-callback` route is an admin-authenticated internal operation; external payment-provider
signature verification remains required before it can be exposed directly to a provider.

Administrators can query paginated audit events through `GET /api/v1/admin/audit-logs` and the Audit
Logs console page. Audit details identify changed fields or state transitions but intentionally omit
passwords, credential plaintext, subscription tokens, and raw payment callback bodies.

Every newly accepted traffic report stores the exact `subscription_id` whose quota was deducted.
`GET /api/v1/traffic/reconciliation` compares each subscription's `flow_used` counter with its
attributed traffic records and reports `matched`, `missing_records`, or `over_recorded`. Pre-attribution
records remain visible as legacy records and may intentionally produce `missing_records` for an older
subscription.
