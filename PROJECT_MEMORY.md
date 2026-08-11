# Zboard project memory

This file is the repository-local continuity record for completed goals. It
contains durable decisions, verification evidence and intranet synchronization
results, but never passwords, API keys, subscription URLs or private keys.

## Goal-completion protocol

Every goal is complete only after all applicable steps below finish:

1. Implement the agreed scope and run proportionate local verification.
2. Add a dated entry to this file with the outcome, ownership decisions,
   verification commands and known gaps.
3. Synchronize the verified working tree to the intranet environment with
   `scripts/sync-intranet.ps1`.
4. Verify the remote version, `/readyz`, application/MySQL/Redis container
   health and any goal-specific behavior.
5. Update the entry with the deployed version, backup path and verification
   result. A failed or skipped synchronization keeps the goal incomplete and
   must be stated explicitly.

The intranet synchronization target is the SSH alias `gitlab`, remote root
`/data/zboard-next`, Compose project `zboard_next`, and HTTP verification
endpoint `http://192.168.50.20:18080`. Authentication and deployment secrets
remain outside this repository.

## In-progress goals

### 2026-08-04 - Protocol delivery ordering

Goal outcome before synchronization:

- Added a dedicated complete-scope global protocol-order snapshot and batch
  update command with optimistic version checking. Ordinary endpoint saves
  preserve delivery position and new endpoints append to the end.
- Kept node-group relationship order authoritative for that group, with the
  global endpoint order as the fallback for legacy all-zero relationship rows.
- Normalized the aggregated user subscription endpoint slice before all native
  renderers so ZNet Sink, Clash / Mihomo and sing-box receive the same relative
  endpoint order. Zero runtime compilation remains identity ordered and is not
  published by a business reorder.
- Added an administrator ordering dialog that always loads the complete scope,
  independent of current filters, pagination or grouped display.

Verification performed in this environment:

- `gofmt` completed for the changed backend files.
- `git diff --check` passed.
- The OpenAPI document parsed successfully and exposes the new GET/PUT order
  contract and schemas.
- Focused backend and frontend tests were added for complete-scope validation,
  global/group ordering, renderer order preservation and immutable UI moves.

Remaining gaps:

- Local backend execution is unavailable because the container has Go 1.23.2
  while the repository requires Go 1.26.5, and outbound toolchain download is
  blocked. Frontend dependencies are not installed. Repository CI remains the
  authoritative test, vet, typecheck, build and contract validation.
- Intranet synchronization and deployed `/readyz`, container-health and live
  subscription checks have not been attempted from this PR environment. No
  deployment, database backup, Git release or production mutation was made.

## Completed goals

### 2026-08-08 - Workstation-local one-click intranet deployment

Goal outcome:

- Added the workstation-local `scripts/deploy-intranet.local.ps1` entry point
  for a one-command verified working-tree deployment. It pins the real
  intranet topology (`gitlab`, `/data/zboard-next`, Compose project
  `zboard_next` and `http://192.168.50.20:18080`) while continuing to consume
  database, Redis and application secrets only from the target's existing
  `/data/zboard-next/.env`.
- The wrapper delegates build, backup, switch and rollback behavior to the
  maintained `scripts/sync-intranet.ps1`, supplies the workstation's Go 1.26.5
  SDK when ordinary PowerShell sessions do not expose it on PATH, and
  independently verifies the externally reachable version and `/readyz`
  response plus application, external MySQL and external Redis container
  state.
- Kept the topology-specific entry point outside version control through the
  repository-local `.git/info/exclude`; the shared `.gitignore` and tracked
  deployment surface are unchanged.

Local verification:

- PowerShell AST parsing passed for the final local deployment script.
  `git check-ignore -v scripts/deploy-intranet.local.ps1` resolves to the
  repository-local exclude rule, `git ls-files` confirms it is untracked, and
  `git status --short` does not expose it as an untracked file.
- The wrapper's full verification path passed `go test ./...` and
  `go vet ./...` for all backend packages, then completed the frontend
  dependency check, typecheck and Vite production build with 564 transformed
  modules.
- Read-only execution of the wrapper's container-discovery checks against the
  real intranet topology found the Zboard container running and healthy and
  both external service containers running. Only environment key names were
  inspected; no secret values were printed or persisted.

Synchronization and deployed verification:

- The first invocation stopped before upload because the maintained sync
  script did not find the workstation's Go SDK on PATH. The local wrapper now
  adds the actual `sdk/golang/bin` location when necessary. No remote mutation
  occurred in that attempt.
- After all local checks passed, the first remote build stopped before backup
  or switch with only 308 MB free on the root filesystem. Read-only inspection
  identified 9.8 GB of YUM package cache; `yum clean all` removed only
  rebuildable package metadata and archives and restored 5.2 GB free without
  touching containers, images, volumes, databases or GitLab data.
- The next build and database backup succeeded, but Compose rejected the
  switch because the real environment predated the managed-rule storage
  boundary and lacked its host directory. Automatic rollback restored the
  previous source and a healthy service. The non-secret
  `ZBOARD_MANAGED_RULE_HOST_DIR=/data/zboard-next/managed-rules` setting was
  added to the target environment and the repository preparation script
  created that persistent directory with mode 0750.
- A following synchronization deployed
  `v0.0.1-20260808T102319Z-intranet-working-tree@2026-08-08T10:23:19Z` but the
  local wrapper rejected its successful result because its version assertion
  did not account for the build's `working-tree` commit suffix. The corrected
  assertion was exercised by a final complete one-click run, which exited 0
  and deployed
  `v0.0.1-20260808T102357Z-intranet-working-tree@2026-08-08T10:23:58Z`.
- The final pre-switch database backup is
  `/data/zboard-next/backups/20260808T102358Z/zboard-before-sync.sql` (83,480
  bytes), the previous source is
  `/data/zboard-next/app-prev-20260808T102358Z`, and the release archive is
  `/data/zboard-next/releases/20260808T102358Z/source.tar.gz` (930,847 bytes).
- Independent workstation requests returned the final deployed version and
  `/readyz` with `ready=true` and `db=true`. `zboard_next-zboard-1` is running
  and healthy; external MySQL `db` and Redis `cache` are running. A container
  write probe confirmed the trusted artifact parent is read-only and its
  separately mounted managed-rule child is writable. The root filesystem
  retained 3.3 GB free after the final build.

Remaining gaps:

- Failed-attempt candidate/release/backup and failed-source paths were retained
  for deployment audit and possible diagnosis; they are not active runtime
  state. The repository's existing bounded-history maintenance remains
  responsible for later cleanup.
- No Git staging, commit, push or release was performed.

### 2026-08-03 - Editable DNS/certificates and resilient ACME preflight/install

Goal outcome before synchronization:

- Added revision-protected editing for managed DNS target nodes, public IP
  values, TTL and Cloudflare proxy policy. Saving immediately starts the
  existing provider synchronization path. Provider account, FQDN and record
  type remain explicit identity fields and require delete-and-recreate.
- Added managed DNS deletion that blocks concurrent provider operations,
  deletes only the stored Cloudflare Zone/record identifier, and removes local
  desired state only after the remote deletion succeeds. Provider 404 is an
  idempotent success for recovery after a partially completed prior deletion;
  authorization and other provider failures retain the panel record.
- Added revision-protected certificate editing for display name, ACME contact
  email, HTTP-01 Webroot and renewal policy. Node, domains, environment,
  challenge type and provider identity remain immutable certificate-asset
  boundaries. Running issuance or renewal blocks edits.
- HTTP-01 preflight no longer interprets a missing IPv6 route on the Zboard
  control plane as proof that the remote AAAA target is unavailable. Concrete
  connection refusal and timeout failures still block issuance.
- DNS-01 Certbot setup first attempts the operating-system Cloudflare plugin
  package, then falls back to an isolated `/opt/zboard-certbot` Python virtual
  environment with Certbot and its Cloudflare plugin when the distribution
  does not publish `python3-certbot-dns-cloudflare`.

Local verification:

- `go test ./...` and `go vet ./...` passed for all backend packages, including
  exact Cloudflare deletion, idempotent provider 404, IPv6 route handling,
  concrete TCP failure and Certbot fallback-script coverage.
- All 63 frontend Vitest files and 147 tests passed. Frontend type checking and
  the production build passed with the DNS and certificate editor assets.
- `git diff --check` passed with only the repository's existing Windows
  LF-to-CRLF notices.

Synchronization and deployed verification:

- The first `scripts/sync-intranet.ps1 -SkipLocalChecks` attempt stopped during
  Docker linking because the intranet root filesystem had only 44 MB free. It
  had not switched the application or created a database backup. Read-only
  inspection identified 1.381 GB of reclaimable build cache; only rebuildable
  Docker builder cache was pruned, without removing containers, images or
  volumes.
- A clean synchronization retry completed successfully and deployed
  `v0.0.1-20260803T095440Z-intranet-working-tree@2026-08-03T09:54:40Z`.
  `/api/v1/version` and `/readyz` returned HTTP 200 with database and readiness
  true. The Zboard container was healthy and the external MySQL and Redis
  containers were running.
- The pre-sync database backup is
  `/data/zboard-next/backups/20260803T095440Z/zboard-before-sync.sql` (82,257
  bytes), the previous source is `/data/zboard-next/app-prev-20260803T095440Z`,
  and the release archive is
  `/data/zboard-next/releases/20260803T095440Z/source.tar.gz` (812,389 bytes).
- Unauthenticated PUT DNS, DELETE DNS and PUT certificate requests each
  returned HTTP 401, proving the deployed router owns the new protected
  methods. Deployed source checks found DNS update/deletion, IPv6 preflight and
  Certbot virtual-environment fallback paths; running frontend assets contain
  both DNS and certificate editors.
- The successful build left 947 MB free. A second builder-cache-only prune
  reclaimed 604.2 MB and restored 1.9 GB free while retaining all active
  containers, images and volumes.

Remaining gaps:

- No live Cloudflare record or production certificate was mutated merely to
  verify destructive/external behavior. Provider request contracts and UI
  paths are covered locally; one operator edit/delete and one retry of the
  previously failed DNS-01 request remain the final external checks.
- No Git staging, commit, push or release was performed.

### 2026-08-03 - Runnable subscription inbounds and concealed public delivery

Goal outcome before synchronization:

- Added one declarative `mixed_port` setting to every version-2 subscription
  template, defaulting to 7890 and validated from 1 through 65535. Existing
  version-2 and legacy records normalize to the default without a schema
  migration.
- ZNet Sink now emits a loopback Zero `mixed` inbound, Clash emits its native
  `mixed-port` plus loopback bind address, and sing-box emits its native
  loopback `mixed` inbound. Generated subscriptions are directly runnable and
  no longer depend on a GUI conversion step to invent an inbound.
- Public ZNet Sink and canonical native JSON delivery are Base64 text after
  backend rendering and validation; administrative previews remain readable.
  Clash and sing-box keep their required native representations. Base64 is
  documented as representation concealment rather than an authorization or
  encryption boundary.
- Added the private `subscription_camouflage_url` system setting. Existing
  installations reconcile the missing row at startup; a blank value falls
  back to the installation public URL. Invalid and revoked public subscription
  tokens now return a non-cacheable HTTP 302 redirect instead of the API 404
  document, without changing unrelated API 404 behavior.

Local verification:

- `go test ./...` and `go vet ./...` passed for every backend package.
- Targeted subscription renderer tests passed with the local Zero
  `0.0.15-rc.4` debug binary as `ZBOARD_ZERO_VALIDATE_BIN`; the real validator
  accepted the generated loopback mixed inbound. The known full Connector test
  cannot run with that debug binary because it lacks the `event-dispatcher`
  feature, so the ordinary full suite was run separately and passed.
- All 63 frontend Vitest files and 146 tests passed. Frontend type checking and
  the production build passed with 539 transformed modules.
- `git diff --check` passed with only the repository's existing Windows
  LF-to-CRLF notices.

Synchronization and deployed verification:

- `scripts/sync-intranet.ps1 -SkipLocalChecks` completed successfully. The
  deployed working-tree version is
  `v0.0.1-20260803T065503Z-intranet-working-tree@2026-08-03T06:55:03Z`.
- The pre-sync database backup is
  `/data/zboard-next/backups/20260803T065503Z/zboard-before-sync.sql` (81,954
  bytes), the previous source is `/data/zboard-next/app-prev-20260803T065503Z`,
  and the archived release source is
  `/data/zboard-next/releases/20260803T065503Z/source.tar.gz` (806,333 bytes).
- `/api/v1/version` returned the deployed version, `/readyz` returned HTTP 200
  with database and readiness true, the Zboard container was healthy, and the
  external MySQL and Redis containers were running.
- A random invalid subscription token returned HTTP 302 with `Cache-Control:
  no-store` and a camouflage `Location` instead of the structured API 404.
  The deployed source contains the Base64-delivery, mixed-inbound and
  camouflage paths.
- The private `subscription_camouflage_url` configuration row exists and is
  blank by default. All three existing subscription templates contain the
  reconciled `mixed_port` customization.

Remaining gaps:

- Base64 does not prevent token guessing or traffic interception. The actual
  controls remain the 256-bit subscription token, TLS, revocation and rotation.
- A live valid subscription token was deliberately not extracted from the
  deployment, so the deployed valid-token response was not decoded end to end;
  local response tests and deployed-source checks cover that path.
- No Git staging, commit, push or release was performed.

### 2026-08-03 - Managed VLESS and VMess transport selection

Goal outcome before synchronization:

- Added first-class TCP, WebSocket and gRPC transport selection to the VLESS
  and VMess protocol-service wizard. TCP is the raw default, WebSocket owns a
  validated path, and gRPC owns one canonical service name.
- Generated matching server and subscriber-client transport configuration and
  preserved the same value through Zero, Clash and sing-box subscription
  exports. Canonical gRPC `service_names` now converts correctly to the
  singular field required by Clash and sing-box.
- Matched the Zero `0.0.15` contract: VLESS Reality is automatically locked to
  raw TCP, VMess retains mandatory TLS, and protocols without configurable
  carrier fields do not show a misleading transport selector.
- Added save-boundary validation for mutually exclusive carriers, mismatched
  server/client transports, invalid WebSocket paths, mismatched gRPC service
  names and Reality combined with a non-TCP carrier.

Local verification:

- `go test ./...` and `go vet ./...` passed for the backend.
- All 63 frontend Vitest files and 146 tests passed. Frontend type checking and
  the production build passed with 539 transformed modules.
- `git diff --check` passed with only the repository's existing Windows
  LF-to-CRLF notices.

Synchronization and deployed verification:

- `scripts/sync-intranet.ps1 -SkipLocalChecks` completed successfully and
  deployed
  `v0.0.1-20260803T051104Z-intranet-working-tree@2026-08-03T05:11:04Z`.
  Independent requests confirmed `/api/v1/version` and `/readyz` returned HTTP
  200 with `db=true` and `ready=true`.
- `zboard_next-zboard-1` was running and healthy; the external `db` and
  `cache` containers were running. The deployed `Protocols-Dti8C-vr.js` asset
  contains the TCP, WebSocket and gRPC transport controls.
- The pre-sync database backup is
  `/data/zboard-next/backups/20260803T051104Z/zboard-before-sync.sql` (81,442
  bytes), the previous source is
  `/data/zboard-next/app-prev-20260803T051104Z`, and the release archive is
  `/data/zboard-next/releases/20260803T051104Z/source.tar.gz` (802,300 bytes).

Remaining gaps after synchronization:

- No live VLESS/VMess WebSocket or gRPC client connection was created during
  deployment. Config generation, validation and renderer conversion are
  covered by local tests; a real client connection remains the final
  end-to-end transport check.
- No Git staging, commit, push or release was performed.

### 2026-08-03 - Zero 0.0.15 protocol capability and managed-user templates

Goal outcome before synchronization:

- Corrected frontend Zero version precedence so the formal `0.0.15` release
  sorts after `0.0.15-rc.*`; selecting a formal-release node no longer marks
  Mieru unavailable.
- Removed endpoint-level placeholder accounts from generated VLESS templates
  and removed shared passwords from Trojan and Hysteria2 templates. These
  protocols now store only service/transport defaults; subscriber credentials
  are injected into runtime and client configurations per subscription.
- Added backend normalization at save and admin-detail boundaries so direct API
  writes and historical records cannot keep presenting placeholder VLESS IDs
  or Trojan/Hysteria2 shared passwords as supported configuration.
- Declared Trojan/Hysteria2 managed-user support as requiring Zero
  `0.0.15-rc.3` or newer. Older kernels are rejected with an upgrade message
  instead of silently falling back to a shared account.

Local verification:

- `go test ./...` and `go vet ./...` passed for the backend.
- All 63 frontend Vitest files and 146 tests passed, including formal-release
  versus prerelease ordering. Frontend type checking and the production build
  passed with 539 transformed modules.
- `git diff --check` passed with only the repository's existing Windows
  LF-to-CRLF notices.

Synchronization and deployed verification:

- `scripts/sync-intranet.ps1 -SkipLocalChecks` completed successfully and
  deployed
  `v0.0.1-20260803T045808Z-intranet-working-tree@2026-08-03T04:58:08Z`.
  Independent requests confirmed `/api/v1/version` and `/readyz` returned HTTP
  200 with `db=true` and `ready=true`.
- The public capability response advertises Mieru minimum Zero
  `0.0.15-rc.4` and Trojan/Hysteria2 minimum Zero `0.0.15-rc.3`. The running
  source hashes for the protocol editor, semantic version helper, endpoint
  handler and credential compiler exactly match the locally verified files.
- `zboard_next-zboard-1` was running and healthy; the external `db` and
  `cache` containers were running. Both external services require
  authentication, so no deployment credential was read merely to perform an
  extra CLI login; application readiness independently confirmed database
  connectivity.
- The pre-sync database backup is
  `/data/zboard-next/backups/20260803T045808Z/zboard-before-sync.sql` (81,442
  bytes), the previous source is
  `/data/zboard-next/app-prev-20260803T045808Z`, and the release archive is
  `/data/zboard-next/releases/20260803T045808Z/source.tar.gz` (799,084 bytes).

Remaining gaps after synchronization:

- No live subscriber Trojan/Hysteria2 connection was created as part of this
  deployment. Runtime injection is covered by backend tests; an operator-side
  client connection remains the final end-to-end protocol check.
- No Git staging, commit, push or release was performed.

### 2026-08-03 - SSH terminal final-row clipping

Goal outcome before synchronization:

- Corrected the SSH terminal canvas sizing in both dialog and fullscreen
  layouts. Xterm's fit addon measures its immediate host element, but that host
  previously also carried the terminal's visual padding and border. The fit
  calculation therefore used more height than the host's actual content box
  and could place the final prompt row inside the clipped area.
- Moved the visual padding, border, radius and inset decoration to the outer
  terminal stage. The measured xterm host is now an undecorated 100% content
  box, so the calculated rows match the drawable height while the terminal
  keeps the same visual spacing. Fullscreen safe-area padding follows the same
  boundary.
- Added a regression assertion that the measured host remains free of padding
  and borders. No SSH session, PTY sizing protocol or backend behavior was
  changed.

Local verification:

- The targeted SSH terminal component test passed with 2 tests.
- All 62 frontend Vitest files and 142 tests passed. Frontend type checking and
  the production build passed with 538 transformed modules.
- `git diff --check` passed for the two changed terminal files with only the
  repository's existing Windows LF-to-CRLF notices.

Synchronization and deployed verification:

- `scripts/sync-intranet.ps1 -SkipLocalChecks` completed successfully and
  deployed
  `v0.0.1-20260803T040204Z-intranet-working-tree@2026-08-03T04:02:04Z`.
  Independent requests confirmed `/api/v1/version` and `/readyz` returned HTTP
  200 with `db=true` and `ready=true`.
- `zboard_next-zboard-1` was running and healthy. The external `db` and `cache`
  containers were running (neither defines a Docker healthcheck), and an
  authenticated-independent MySQL `mysqladmin ping --silent` succeeded. Redis
  correctly rejected an unauthenticated diagnostic ping; no credential was
  read or printed merely to turn that configured denial into `PONG`.
- The pre-sync database backup is
  `/data/zboard-next/backups/20260803T040204Z/zboard-before-sync.sql` (81,093
  bytes), the previous source is
  `/data/zboard-next/app-prev-20260803T040204Z`, and the release archive is
  `/data/zboard-next/releases/20260803T040204Z/source.tar.gz` (795,696 bytes).
- The running container's `Nodes-C5plwunu.css` asset contains the decorated
  terminal stage and the undecorated full-size terminal host, confirming that
  the deployed frontend uses the corrected fit boundary.

Remaining gaps after synchronization:

- The available Chrome tab for this intranet application is currently at the
  login page, so an automated authenticated SSH session could not be opened.
  Perform one operator-side check that the final prompt and cursor now retain
  the bottom inset after refreshing the page.
- No Git staging, commit, push or release was performed.

### 2026-08-03 - Protocol business load and one-click Reality scenarios

Goal outcome before synchronization:

- Corrected the meaning of node/protocol load: business load now comes from
  attributable Zero flow events rather than Linux CPU or memory. An active
  flow is a `flow_usages` row still marked active and seen within the last two
  minutes; active users are distinct subscription owners in that same window.
  Allocated credentials are retained as an administrative capacity fact but
  are no longer presented as currently connected people.
- Added `active_users` beside `active_flows` to every administrative protocol
  endpoint summary and detail. The primary protocol table now shows active
  users and active connections for each endpoint; the earlier SSH projection
  is explicitly labeled host resources to prevent another semantic collision.
- Added the authenticated `/api/v1/subscription/protocol-loads` view for the
  account subscription page. It exposes only endpoint name, region, protocol,
  aggregate active-user/flow counts and last activity for endpoints reachable
  through the caller's currently usable subscriptions. Other identities,
  credentials, node host details and traffic records do not cross the
  subscription/node-group boundary.
- Added backend-maintained one-click VLESS Reality scenarios for general
  compatibility, global-CDN and Apple-oriented clients. Applying a scenario
  fills the recommended SNI and client fingerprint and generates a fresh
  matching X25519 key pair and short ID in one operation; operators can still
  adjust the filled result before saving. Existing advanced JSON remains
  available without making basic Reality setup depend on it.
- Updated the OpenAPI and data-model contracts. No database migration, public
  unauthenticated load endpoint or new Zero event type was introduced.

Local verification:

- `go test ./...` and `go vet ./...` passed for every backend package.
- Frontend type checking, all 62 Vitest files and 141 tests, and the production
  build passed with 538 transformed modules.
- The production MySQL-compatible aggregation query was executed read-only on
  the intranet database and completed successfully. The sampled two-minute
  window contained no active flows, which is a valid zero-load result.
- `git diff --check` passed with only the repository's existing Windows
  LF-to-CRLF notices.

Synchronization and deployed verification:

- The first synchronization attempt stopped during the Dockerfile frontend
  resolver because Docker Hub's token endpoint returned an EOF. It had not
  reached backup or application switching. A clean retry of
  `scripts/sync-intranet.ps1 -SkipLocalChecks` completed successfully and
  deployed
  `v0.0.1-20260803T032139Z-intranet-working-tree@2026-08-03T03:21:39Z`.
- Independent post-sync requests confirmed `/api/v1/version` and `/readyz`
  returned HTTP 200 with `db=true` and `ready=true`.
  `zboard_next-zboard-1` was healthy, MySQL returned `mysqld is alive`, and
  Redis returned `PONG`.
- The pre-sync database backup is
  `/data/zboard-next/backups/20260803T032139Z/zboard-before-sync.sql` (80,743
  bytes), the previous source is
  `/data/zboard-next/app-prev-20260803T032139Z`, and the release archive is
  `/data/zboard-next/releases/20260803T032139Z/source.tar.gz` (794,791 bytes).
- The deployed admin endpoint returned one protocol endpoint and supplied both
  `active_users` and `active_flows` for it. The subscriber load route returned
  its 120-second activity contract and one endpoint for the authenticated
  account; the same route returned HTTP 401 without authentication. Database
  membership checks were prepared for an active non-admin subscription, but
  production currently has no non-admin user fixture, so a positive
  non-admin scoped sample could not be exercised without creating production
  business data.
- All three deployed Reality scenarios (`compatible`, `cdn`, and `apple`)
  generated a 43-character private key, 43-character public key and
  16-character short ID with non-empty SNI/fingerprint fields. No key value was
  printed or persisted by verification.
- The running frontend contains the subscriber protocol-load view in
  `AccountSubscription-B69kIOCB.js`, one-click Reality scenarios in
  `Protocols-VV1IHN9X.js`, and the corrected host-resource terminology in
  `Nodes-DgQgwqKH.js`.

Remaining gaps:

- Exercise a positive non-admin subscriber scope when a real non-admin user
  has an active subscription. Zero-flow windows legitimately return zero; no
  synthetic production flow or user was created merely to force a non-zero
  display.
- No Git staging, commit, push or release was performed.

### 2026-08-03 - VLESS Reality configuration and on-demand node load

Goal outcome:

- Added VLESS Reality as a first-class protocol-editor security mode. The
  authenticated admin endpoint generates a matching X25519 key pair and
  random 16-hex-character short ID; the private key enters only the encrypted
  server configuration, while public subscription configuration receives the
  public key, selected short ID, server name and client fingerprint.
- Added local endpoint validation for Reality key encoding and key-pair
  correspondence, short-ID syntax/membership and both server/client names.
  Managed per-subscription VLESS UUID compilation continues to preserve all
  endpoint transport fields rather than treating Reality as a separate
  sellable resource.
- Completed Reality delivery across ZNet Sink, Clash and sing-box. The
  existing converters already mapped the core Reality fields; they now also
  retain the operator-selected client fingerprint instead of omitting it or
  falling back to Chrome.
- Added an authenticated, on-demand node-load endpoint and node-detail view.
  It requires a previously verified SSH channel with a pinned host key and
  reports CPU cores and 1/5/15-minute load averages, memory, root-filesystem
  capacity, host uptime, sample time and SSH latency. Host load remains an
  ephemeral node-asset projection and is not mixed with Zero session stats,
  Connector health or trusted traffic accounting.
- Updated the OpenAPI and data-model contracts. No schema migration, public
  unauthenticated metric surface or Zero event-contract change was introduced.

Local verification:

- `go test ./...` and `go vet ./...` passed for every backend package.
- Targeted tests with `ZBOARD_ZERO_VALIDATE_BIN` set to the local Zero
  `v0.0.15-rc.4`-derived debug binary passed: the real validator accepted both
  a generated VLESS Reality inbound and the ZNet Sink Reality subscription.
  The full Connector integration suite cannot use that particular debug build
  because it lacks Zero's `event-dispatcher` Cargo feature, so the ordinary
  full suite was also run without the optional validator override and passed.
- All 61 frontend Vitest files and 139 tests passed. Frontend type checking and
  the production build passed with 538 transformed modules.
- `git diff --check` passed with only the repository's existing Windows
  LF-to-CRLF notices.

Synchronization and deployed verification:

- `scripts/sync-intranet.ps1 -SkipLocalChecks` completed successfully. The
  deployed version is
  `v0.0.1-20260803T024810Z-intranet-working-tree@2026-08-03T02:48:10Z`.
  An independent post-sync request confirmed `/api/v1/version` and `/readyz`
  returned HTTP 200 with `db=true` and `ready=true`.
- `zboard_next-zboard-1` was healthy, the external MySQL `mysqladmin ping`
  returned `mysqld is alive`, and the external Redis `redis-cli ping` returned
  `PONG`.
- The pre-sync database backup is
  `/data/zboard-next/backups/20260803T024810Z/zboard-before-sync.sql` (80,743
  bytes), the previous source is
  `/data/zboard-next/app-prev-20260803T024810Z`, and the synchronized release
  archive is `/data/zboard-next/releases/20260803T024810Z/source.tar.gz`
  (789,752 bytes).
- Both new deployed routes returned HTTP 401 without authentication. With a
  short-lived admin token generated only inside the remote verification
  process, the Reality endpoint returned a 43-character private key,
  43-character public key and 16-character short ID without printing their
  values. The node-load endpoint exercised a previously SSH-verified node and
  returned HTTP 200 with all required fields; the sample reported 4 cores,
  load-1 of 0.62, 3,972,481,024 bytes of memory, 48,210,944,000 bytes on the
  root filesystem, 6,507,350 seconds uptime and 404 ms SSH latency.
- The running container's built frontend contains the Reality editor in
  `Protocols-DeZNA8go.js` and the node-load view in `Nodes-BgMW5O1Q.js`.

Remaining gaps:

- This goal makes VLESS Reality first-class and preserves advanced raw JSON as
  the escape hatch for other kernel fields; it does not claim a dedicated form
  control for every present or future Zero option. The optional full Connector
  integration suite still needs a Zero debug build with the
  `event-dispatcher` Cargo feature if it is to run together with the external
  validator override.
- No Git staging, commit, push or release was performed.

### 2026-08-03 - Exact small-traffic display in admin records

Goal outcome before synchronization:

- Confirmed that accounting and storage retain exact byte values. The live
  database contains two completed flows with 219 raw bytes each, matching the
  observed 438-byte raw total. Their 1.5x endpoint multiplier produced 329
  charged bytes each and an exact 658-byte subscription total.
- Located the apparent loss in the admin Traffic view only: record and
  reconciliation values were divided by 1,048,576 and rendered with at most
  two decimal places. Values below about 5 KiB therefore appeared as `0 MiB`
  even though the API and database retained them.
- Replaced the fixed-MiB record and reconciliation columns with the shared
  adaptive byte formatter, so small values remain visible as bytes and larger
  values scale through KB, MB and GB. Signed reconciliation differences keep
  an explicit positive/negative sign. Column labels no longer claim a fixed
  MiB unit.
- Added formatter regression coverage for 438-byte positive, unsigned and
  negative values. No database, API or billing calculation was changed.

Local verification:

- All 61 frontend Vitest files and 139 tests passed. Frontend type checking and
  the production build passed with 538 transformed modules.
- `go test ./...` and `go vet ./...` passed for every backend package.
- `git diff --check` passed with only the repository's existing Windows
  LF-to-CRLF notices. The obsolete fixed-MiB formatter and labels are absent
  from the admin Traffic view.

Synchronization and deployment evidence:

- Three synchronization attempts under stamps `20260803T014621Z`,
  `20260803T014641Z` and `20260803T014808Z` failed before image construction or application switching
  because the target timed out during the TLS handshake with Docker Hub's
  registry. A direct 15-second registry request also received no response, and
  the Dockerfile frontend image was not cached after the prior deliberate
  builder-cache cleanup.
- The live application therefore remains on
  `v0.0.1-20260802T154036Z-intranet-working-tree`; its `/readyz` endpoint still
  reports `db=true` and `ready=true`. The failed attempts retained their
  unactivated candidates/source archives and did not modify the running
  application or database.

Remaining gaps before synchronization:

- Retry synchronization when Docker Hub connectivity is available, then
  verify the deployed version, readiness, container health and deployed
  adaptive traffic-formatting assets. Until that succeeds, the live admin page
  still has the old fixed-MiB display.
- No Git staging, commit, push or release was performed.

### 2026-08-02 - Live latency billing diagnosis and SSH terminal scrolling

Goal outcome before synchronization:

- Traced the live latency-test path across the Zero event outbox, authenticated
  `/api/zero/events` requests, `flow_usages`, `traffic_records` and the active
  subscription counters. The reported test produced no server-side session:
  node statistics remained at zero active/started sessions and zero bytes, and
  neither a flow event nor a billing cursor existed. Requests from the local
  client address were rejected with HTTP 401 and are intentionally not trusted
  as billing authority.
- Proved the authoritative path with two bounded live checks using the active
  subscription credential without printing or persisting it. A proxied HTTP
  HEAD request produced `flow.updated` and `flow.completed`; Zero's own
  `url_test` probe then selected the same Shadowsocks outbound as healthy at
  about 400 ms and produced another `flow.completed`. Zboard accepted both
  completed flows and charged 329 bytes each, leaving the subscription at 658
  bytes used. This establishes that the current Zero rc.4 kernel reports real
  latency-probe traffic and that panel authentication, idempotent accounting
  and subscription updates do not drop it. The panel must not synthesize a
  charge when a client-side test never reaches the server node.
- Corrected the SSH terminal's non-fullscreen layout. The terminal dialog no
  longer opts into the shared fixed-body rule that forced its content region to
  `overflow:hidden`. Its workspace now has an explicit responsive height so
  xterm can still fit stable rows while the dialog body can scroll on short
  viewports. The xterm viewport explicitly retains vertical history scrolling,
  contained overscroll, touch panning and stable scrollbar space. Fullscreen
  positioning remains unchanged.
- Added a frontend regression test covering the independent dialog and xterm
  scroll contracts. No billing receiver or accounting code was changed because
  the live evidence proved that modifying that trusted boundary would be
  incorrect.

Local verification:

- All 61 frontend Vitest files and 139 tests passed. Frontend type checking and
  the production build passed with 538 transformed modules.
- `go test ./...` and `go vet ./...` passed for every backend package.
- `git diff --check` passed with only the repository's existing Windows
  LF-to-CRLF notices.
- Browser control reached the live public application but had no authenticated
  admin session, so it could not open the real SSH terminal without bypassing
  authentication. The deployed authenticated UI still needs an operator-side
  wheel/touch confirmation after synchronization.

Synchronization and deployment evidence:

- The first synchronization attempt stopped during the image build when
  Docker Hub returned EOF while resolving the Node base-image manifest. It did
  not switch the application; the unactivated candidate and release archive
  under stamp `20260802T154019Z` were retained.
- A clean retry of `scripts/sync-intranet.ps1 -SkipLocalChecks` deployed
  `v0.0.1-20260802T154036Z-intranet-working-tree@2026-08-02T15:40:36Z`.
  Independent `/api/v1/version` and `/readyz` requests returned HTTP/API 200;
  readiness reported `db=true` and `ready=true`.
- `zboard_next-zboard-1` reported healthy. The external `db` and `cache`
  containers remained running, and MySQL returned `mysqld is alive`.
- The pre-switch database backup is
  `/data/zboard-next/backups/20260802T154036Z/zboard-before-sync.sql`
  (80,289 bytes), the previous source is
  `/data/zboard-next/app-prev-20260802T154036Z`, and the synchronized archive
  is `/data/zboard-next/releases/20260802T154036Z/source.tar.gz`
  (781,868 bytes). All paths were verified present and non-empty where
  applicable.
- Deployed-source checks confirmed the explicit responsive terminal workspace
  height and xterm scroll contract, and confirmed that the SSH dialog no
  longer passes `fixed-body`. The live database retained the three diagnostic
  traffic records and the expected 658 charged bytes after deployment.
- Removing only reproducible Docker builder cache reclaimed 627.3 MB. Root
  free space recovered from 2.2 GB to 3.1 GB (94% used), above the previously
  observed Connector outbox reserve threshold. `/readyz` and all three
  containers remained healthy/running after cleanup; databases, volumes,
  backups and rollback sources were untouched.

Remaining gaps after synchronization:

- Repeat the non-fullscreen terminal wheel/touch check in an authenticated
  admin browser session; this is not available to the automated browser
  session.
- The original client-side test's local runtime/configuration must be inspected
  if it again reports latency without increasing the node counters. Server-side
  evidence currently shows that particular attempt did not reach the node.
- No Git staging, commit, push or release was performed.

### 2026-08-02 - Permanent DIRECT/REJECT subscription choices

Goal outcome before synchronization:

- Corrected the subscription renderers so every manual selection group keeps
  `DIRECT` and `REJECT` as permanent choices after node-name filtering and
  nested-group expansion. URL-test and fallback groups intentionally exclude
  both choices because they are not probe endpoints.
- Clash uses its native `DIRECT` and `REJECT` targets. ZNet Sink/Zero emits
  stable uppercase direct/block aliases while retaining the existing lowercase
  compatibility outbounds. sing-box likewise retains its existing lowercase
  direct outbound and adds uppercase direct/block aliases for selector use.
  Final routes and rule-set actions retain their existing renderer-native
  representation.
- Exported node tags now preserve the original protocol-endpoint name. The
  previous panel-generated `#<endpoint>-<subscription>` suffix is removed;
  blank names still fall back to the uppercase protocol name.
- The built-in latency probe default is consistently
  `http://www.gstatic.com/generate_204`. Startup normalization rewrites only
  the former exact HTTPS default in existing template customizations and
  persists the normalized value; operator-defined custom probe URLs are left
  unchanged.
- Updated the data-model contract and added cross-renderer regression coverage
  proving the permanent choices are present only in manual selectors.

Local verification:

- `go test ./...` and `go vet ./...` passed for every backend package.
- With `ZBOARD_ZERO_VALIDATE_BIN` set to the local Zero rc.4 debug binary, the
  real validator accepted the generated ZNet Sink subscription containing the
  permanent aliases.
- All 60 frontend Vitest files and 138 tests passed. Frontend type checking and
  the production build passed with 538 transformed modules.
- `git diff --check` passed with only the repository's existing Windows
  LF-to-CRLF notices.

Synchronization and deployment evidence:

- The first synchronization was deliberately interrupted before activation
  when the user added the original-name and HTTP-default requirements. The
  background build was terminated and the live version remained
  `v0.0.1-20260802T145229Z-intranet-working-tree`; its unactivated candidate
  and source archive under stamp `20260802T150725Z` were retained. A later
  attempt failed before build on a transient Docker Hub manifest EOF and did
  not switch the application.
- After a successful retry, `scripts/sync-intranet.ps1 -SkipLocalChecks`
  deployed
  `v0.0.1-20260802T151248Z-intranet-working-tree@2026-08-02T15:12:48Z`.
  `/api/v1/version` returned that exact build, `/readyz` returned `db=true`
  and `ready=true`, and `zboard_next-zboard-1` is healthy. The external `db`
  and `cache` containers are running and MySQL returned `mysqld is alive`.
- The pre-switch database backup is
  `/data/zboard-next/backups/20260802T151248Z/zboard-before-sync.sql`
  (78,595 bytes), the previous source is
  `/data/zboard-next/app-prev-20260802T151248Z`, and the synchronized archive
  is `/data/zboard-next/releases/20260802T151248Z/source.tar.gz`
  (780,242 bytes). All were verified present and non-empty where applicable.
- Startup normalization persisted the expected HTTP probe URL for all three
  existing ZNet Sink, Clash and sing-box templates. The deployed source
  contains `appendPermanentSelectionTargets` and returns the original endpoint
  name without an ID suffix.
- Final cleanup reclaimed 627.1 MB of reproducible Docker builder cache. Root
  free space is 3.2 GB (93% used); Zboard remained healthy and ready after the
  cleanup. Databases, volumes, deployment backups, previous sources and
  operator data were untouched.

Remaining gaps after synchronization:

- Live user subscription output still requires an active subscription. The target
  currently has no active subscription rows, so deployed source/preview output
  was checked without fabricating operator subscription data.
- No Git staging, commit, push or release was performed.

### 2026-08-02 - Published-principal readiness for Trojan/Hysteria2 and latency billing audit

Goal outcome before synchronization:

- Confirmed against Zero `0.0.15-rc.4` (`929250f13`) that Trojan and
  Hysteria2 already match the authenticated password to the correct managed
  user and propagate that user's `principal_key`. The reported failure was a
  Zboard contract split: runtime compilation followed the probed node version,
  while subscription delivery followed only the process-wide legacy/native
  switch. A legacy-configured panel could therefore publish managed users to
  an rc.3-or-newer node but still advertise the endpoint fallback password.
- Added `protocol_endpoints.managed_principal_ready` as the publication
  boundary for Trojan and Hysteria2. A successful full-node publication now
  commits readiness from the actually installed Zero version only after
  validate, activation, service/control health and Connector confirmation.
  Failed publications do not switch subscription delivery, and a successful
  downgrade switches both runtime and subscription output back to the endpoint
  fallback credential. Startup queues existing Trojan/Hysteria2 nodes for
  reconciliation.
- Corrected old-kernel runtime compilation so Trojan/Hysteria2 use one endpoint
  fallback inbound. It no longer attempts multiple credential inbounds on the
  same listen port when native managed-password support is unavailable.
  Mieru retains its stricter existing two-generation readiness boundary.
- Audited local Zero URL-test behavior and Zboard accounting. URL tests perform
  a real proxied protocol handshake and HTTP HEAD exchange. Zboard bills every
  positive attributed byte delta, including one byte; there is no latency-test
  exemption or minimum threshold. Managed-protocol credential mismatch can
  prevent an attributable server flow from being established; template
  preview or endpoint-fallback configurations also have no subscription
  principal to charge. The panel must not fabricate probe charges or trust a
  local client as the billing authority.
- Updated the squashed baseline, pre-release compatibility finalizer, data
  model documentation, OpenAPI schema and frontend API type for the readiness
  field. No credential, subscription URL or environment secret was recorded.

Local verification:

- `go test ./...` passed for every backend package with
  `ZBOARD_ZERO_VALIDATE_BIN` pointing at the local Zero rc.4 debug binary; the
  real validator accepted the managed Trojan and Hysteria2 user contract.
- `go vet ./...` passed.
- All 60 frontend Vitest files and 138 tests passed. Frontend type checking and
  the production build passed with 538 transformed modules.
- Regression tests cover publication-gated subscription delivery, actual-node
  readiness overriding the global legacy switch, safe old-kernel fallback and
  one-byte latency-probe billing. `git diff --check` passed with only existing
  Windows LF-to-CRLF notices.

Synchronization and deployment evidence:

- The initial root filesystem had only 20 KB and 261 inodes free. Reclaiming
  only reproducible Docker builder cache freed 2.2 GB and restored the inode
  pool; no database, volume, source backup or operator data was removed.
- `scripts/sync-intranet.ps1 -SkipLocalChecks` successfully deployed
  `v0.0.1-20260802T145229Z-intranet-working-tree@2026-08-02T14:52:29Z`.
  `/api/v1/version` returned that exact build and `/readyz` returned
  `db=true`, `ready=true`. `zboard_next-zboard-1` is healthy; the external
  `db` and `cache` containers are running, and MySQL returned
  `mysqld is alive`.
- The pre-switch database backup is
  `/data/zboard-next/backups/20260802T145229Z/zboard-before-sync.sql`
  (73,968 bytes), the previous source is
  `/data/zboard-next/app-prev-20260802T145229Z`, and the synchronized archive
  is `/data/zboard-next/releases/20260802T145229Z/source.tar.gz`
  (777,976 bytes). All three were verified present and non-empty where
  applicable.
- The deployed database contains non-null/default-zero
  `managed_principal_ready` and `mieru_principal_ready` columns. The active
  source contains the successful-publication readiness update, and an
  unauthenticated traffic-history request returned 401. The live node remains
  online and healthy on Zero `0.0.15-rc.4` with fresh authenticated Connector
  activity.
- The live database currently has one Shadowsocks endpoint but zero
  subscriptions, protocol credentials, flow usages and traffic records.
  Consequently this environment cannot contain a billable imported user
  subscription: an import that works here must be a preview/endpoint fallback
  or originate elsewhere, and it has no subscription principal to charge.
- A final Docker builder-cache cleanup reclaimed another 627.1 MB. Root free
  space is 3.3 GB (93% used), Zboard remains healthy and `/readyz` remains
  ready after cleanup.

Remaining gaps after synchronization:

- A real imported-subscription URL test is still required after deployment to
  prove live authentication, `flow.completed` delivery and traffic-record
  visibility end to end. Local validator and accounting tests establish the
  contract but do not manufacture operator traffic.
- Live end-to-end billing still requires creating or using a real active
  subscription and then observing its server-side `flow.completed` event. No
  operator subscription or artificial traffic was created solely for
  verification.
- No Git staging, commit, push or release was performed.

### 2026-07-30 - Version-aware published Zero Connector contract

Outcome before synchronization:

- Corrected Zero runtime configuration selection so the Connector wire
  contract follows the target or actually installed Zero version instead of
  being inferred only from `ZBOARD_ZERO_KERNEL_CONTRACT`.
- Published Zero releases through `0.0.15-rc.1` retain their historical
  `api_key_env` event-sink authentication and top-level `push` configuration.
  `0.0.15-rc.2` and later now receive the controller-neutral Webhook contract
  documented by Zero: a complete receiver URL, opaque authorization
  `headers`, subscribed events and a durable outbox, with no removed `push`
  field.
- Kernel reconciliation compiles against the release it has just resolved.
  Configuration-only publication now probes the node and compiles against the
  actual installed Zero version, preventing stale desired-version metadata
  from selecting an incompatible schema.
- The version boundary was verified against the official Zero documentation
  and the `zerodenet/core` source/tag diff: `v0.0.15-rc.1` accepts
  `api_key`/`api_key_env`, while `v0.0.15-rc.2` replaces those Webhook fields
  with opaque `headers` and removes the fixed `push` contract.

Local verification:

- `go test ./...`
- `go vet ./...`
- Targeted Connector contract tests cover stable, prerelease and invalid
  version selection plus both historical and controller-neutral serialized
  shapes.
- `TestNativeKernelAccessConfigValidatesWithCurrentZero` passed with the local
  Zero `0.0.15-rc.2` debug binary, exercising its real `zero validate`
  implementation against the generated generic Webhook contract.
- `git diff --check` passed with only existing Windows LF-to-CRLF notices.

Synchronization and deployment evidence:

- `scripts/sync-intranet.ps1 -SkipLocalChecks` completed successfully and
  deployed
  `v0.0.1-20260730T120051Z-intranet-working-tree@2026-07-30T12:00:51Z`.
- The pre-deployment database backup is
  `/data/zboard-next/backups/20260730T120051Z/zboard-before-sync.sql`
  (72,231 bytes), the previous source is
  `/data/zboard-next/app-prev-20260730T120051Z`, and the synchronized source
  archive is
  `/data/zboard-next/releases/20260730T120051Z/source.tar.gz`
  (741,265 bytes). All three paths were verified present.
- `/api/v1/version` and `/readyz` returned HTTP/API 200 with `ready=true` and
  `db=true`. `zboard_next-zboard-1` is running and healthy; the external `db`
  and `cache` containers remain running. MySQL reported `mysqld is alive` and
  an authenticated Redis ping returned `PONG`.
- The active synchronized backend source contains the
  `0.0.15-rc.2` Connector boundary, target-version selection during kernel
  reconciliation and an installed-version probe before configuration-only
  publication. No real node reconcile or configuration publication was
  started during deployment verification.

Remaining gaps:

- The external Docker deployment under `/data/zboard` that exposed the error
  is not the repository's configured intranet synchronization target. It
  requires a newly built/published Zboard image before that operator-managed
  stack can consume this correction.
- A live `0.0.15-rc.2` or later node reconcile was deliberately not started
  against the intranet. The real Zero validator, backend regression suite,
  deployed source and service health checks passed without changing node
  runtime state.
- No Git staging, commit, push or release was performed.

## 2026-07-31 - Dual-stack DNS, non-exclusive ACME and managed Mieru endpoint credentials

Goal outcome before intranet synchronization:

- Managed-DNS creation now accepts one IPv4 A value, one IPv6 AAAA value, or
  both in one request. A and AAAA remain separate `managed_dns_records`,
  provider operations and audit resources.
- Public DNS visibility no longer depends on a second manual synchronization.
  A bounded background observer queries Cloudflare-independent public
  resolvers every 15 seconds for synchronized unresolved records and marks the
  matching A/AAAA resource visible without repeating the provider write.
- New managed certificates default to Cloudflare DNS-01 and explicitly own a
  verified provider account. DNS credentials are decrypted only for
  preflight/execution and are sent to the node over SSH stdin; they are not
  embedded in commands or operation logs. HTTP-01 Webroot is also supported:
  Certbot writes challenges below a canonical remote POSIX Webroot and does
  not bind a second listener. It still preflights every public A/AAAA address
  for TCP port 80 because HTTP-01 itself requires that public service.
  Existing standalone HTTP-01 certificates remain renewal-compatible, while
  new creation cannot select standalone mode.
- The v0.0.1 schema remains one squashed `0001_init` migration. The baseline
  now stores certificate provider ownership and Webroot path; the existing
  pre-release compatibility finalizer adds the nullable column, path, index
  and foreign key to older development databases.
- Mieru creation no longer accepts administrator-entered usernames or
  passwords. Zboard creates one high-entropy endpoint password, stores it only
  inside the encrypted server configuration, redacts it from admin detail,
  injects it transiently into authorized subscriptions and maps
  `username=password` only for Clash compatibility. Startup normalizes legacy
  Mieru endpoints and queues one full node publication per affected node.
- Zero/ZNet Sink output validation now rejects Mieru outbounds without a
  server, valid port or password. The template preview fixture contains a
  valid Mieru endpoint, so advanced output replacement cannot silently reduce
  it to `{type: mieru}`. Node publication continues to run the installed
  `/usr/local/bin/zero validate` before activation.
- The Zero kernel repository was not modified. The exact kernel prerequisite
  for per-subscription Mieru attribution is recorded in
  `docs/mieru-kernel-contract.md`: `MieruUserConfig.principal_key`, matched-user
  propagation to session authentication and flow events, duplicate/empty-key
  rejection and multi-user tests.

Local verification:

- `go test ./...` passed for every backend package, including new Mieru
  credential/redaction/output checks, DNS/ACME helpers, migration inventory
  and OpenAPI coverage.
- `go vet ./...` passed.
- All 60 frontend Vitest files and 136 tests passed.
- Frontend type checking and the production build passed with 538 transformed
  modules.
- `git diff --check` reported no whitespace error; Git emitted only the
  repository's existing Windows LF-to-CRLF notices.

Remaining gaps before synchronization:

- Per-subscription Mieru credentials and accurate flow attribution remain
  intentionally disabled until the reviewed Zero kernel contract above is
  implemented and released. Zboard does not infer identity from a password.
- Template preview performs strict Zboard structural validation. A real Zero
  binary validation remains authoritative during node publication; making
  browser preview invoke a real Zero client validator requires an explicit
  compatible validator artifact/runtime contract and is not fabricated by
  this change.
- Live Cloudflare DNS propagation, Let's Encrypt issuance and a real Mieru
  client flow still require intranet/environment verification.
- No Git staging, commit, push or release was performed.

Intranet synchronization and deployed evidence:

- `scripts/sync-intranet.ps1 -SkipLocalChecks` deployed
  `v0.0.1-20260731T025508Z-intranet-working-tree@2026-07-31T02:55:08Z`.
  The version endpoint returned that exact build, `/readyz` reported
  `db=true` and `ready=true`, and `zboard_next-zboard-1` was healthy after the
  switch.
- The pre-switch database backup is
  `/data/zboard-next/backups/20260731T025508Z/zboard-before-sync.sql`, the
  previous source is `/data/zboard-next/app-prev-20260731T025508Z`, and the
  synchronized source archive is
  `/data/zboard-next/releases/20260731T025508Z/source.tar.gz`. All three paths
  were verified to exist, with the backup and archive non-empty.
- The deployed database retains one `0001_init.up.sql` migration record.
  `managed_certificates.provider_account_id` is nullable `bigint unsigned`,
  `webroot_path` is non-null `varchar(255)`, and
  `fk_managed_certificates_provider` exists.
- Unauthenticated requests to both `/api/v1/admin/dns-records` and
  `/api/v1/admin/certificates` returned 401, proving the deployed routes are
  registered behind the admin boundary. The active source contains the
  DNS-01 certificate selector and automatic public-DNS observation UI.
- The deployed database currently contains zero managed DNS records, zero
  managed certificates and zero Mieru endpoints. Schema, route, startup and
  artifact verification therefore passed, but a live dual-stack Cloudflare
  write/propagation cycle, ACME issuance and legacy Mieru normalization could
  not be truthfully qualified from existing production rows.
- The local Zero repository remained clean after deployment; no kernel file
  was changed. No Git staging, commit, push or release was performed.

Follow-up Mieru readiness work before the next synchronization:

- Added the opt-in `native-local-mieru` kernel contract without enabling it in
  the intranet environment. Under the existing contract, Zboard now prepares
  one encrypted Mieru secret and stable principal per active subscription but
  keeps the credential in `prepared` status and continues delivering the
  endpoint fallback credential.
- A node enters principal-aware Mieru delivery only after a full configuration
  publication passes the target Zero binary's `validate`, activation,
  systemd/control health and authenticated Connector callback. The first
  publication retains a bounded migration user; while holding the same node
  lock, a second publication removes it. Readiness commits only after the
  fallback-free generation succeeds. Cleanup failure rolls back to the
  compatibility generation and leaves the endpoint unready. Reverting the
  contract follows the same publication boundary before returning credentials
  to `prepared`.
- Principal-aware runtime users contain `username=password` for client/kernel
  compatibility and the stable protocol-credential `principal_key`. The
  temporary `migration:endpoint:<id>` principal is acknowledged without
  billing only during the bounded transition.
- Saving, previewing and serving a ZNet Sink template under the opt-in contract
  now invokes the checksum-pinned managed Zero artifact's real `validate`
  command. A type-only Mieru object can no longer pass through a preview or
  live subscription merely because its JSON shape is valid.
- The baseline schema now includes
  `protocol_endpoints.mieru_principal_ready`; the pre-release finalizer adds it
  to older development databases. Lifecycle expiry and revocation also cover
  prepared credentials.
- Current Zero commit `e918ee0ab` remains unmodified. Its
  `MieruUserConfig` accepts only `username` and `password`; the opt-in contract
  test with the current binary fails authoritatively on unknown field
  `principal_key`. Therefore `native-local-mieru` must remain disabled until a
  reviewed Zero release implements that contract.
- A completion audit tightened the two-stage publication invariant. The first
  compatibility generation no longer commits endpoint readiness or switches
  subscription delivery. Zboard holds the node publication lock and performs
  the fallback-free publication synchronously; only its successful validator,
  activation, health and Connector checks may commit readiness. Cleanup
  failure rolls back to the compatibility generation and leaves the endpoint
  unready for a safe retry.

Follow-up local verification:

- `go test ./...` and `go vet ./...` passed.
- All 60 frontend Vitest files and 136 tests passed; the production build
  passed with 538 transformed modules.
- The ordinary native Zero validation suite passed. The explicitly opt-in
  Mieru principal contract probe failed exactly on unsupported
  `principal_key`, which is the expected external prerequisite rather than a
  Zboard fallback.
- `git diff --check` passed. Both the Zboard and Zero repositories were
  inspected; Zero remained clean.
- The completion-audit rerun used the located Go SDK directly after the shell
  PATH no longer exposed `go`; all backend packages and `go vet` passed. The
  new readiness test proves that the compatibility publication cannot commit,
  the fallback-free publication can commit, ready endpoints cannot reintroduce
  the fallback and the reverse legacy transition remains allowed.

Follow-up intranet synchronization and deployed evidence:

- `scripts/sync-intranet.ps1 -SkipLocalChecks` deployed
  `v0.0.1-20260731T031739Z-intranet-working-tree@2026-07-31T03:17:39Z`.
  The version endpoint returned that exact build and the deliberately
  unchanged `legacy` kernel contract. `/readyz` returned `db=true` and
  `ready=true`.
- `zboard_next-zboard-1` was running and healthy. The external `db` and
  `cache` containers were running; neither defines a Docker healthcheck.
- The pre-switch database backup is
  `/data/zboard-next/backups/20260731T031739Z/zboard-before-sync.sql`
  (72,841 bytes), the previous source is
  `/data/zboard-next/app-prev-20260731T031739Z`, and the synchronized source
  archive is `/data/zboard-next/releases/20260731T031739Z/source.tar.gz`
  (757,460 bytes). All paths were verified present.
- The deployed database still has one migration record.
  `protocol_endpoints.mieru_principal_ready` exists as non-null
  `tinyint(1)` with default `0`. The deployed source contains the opt-in
  contract gate.
- The live database contains zero managed DNS records, zero managed
  certificates, zero Mieru endpoints and zero prepared Mieru credentials.
  Consequently schema/startup/contract safety are verified, while a real
  dual-stack propagation cycle, ACME issuance and Mieru flow attribution
  remain environment-dependent gaps.
- No Zero source, binary or node runtime was changed. No Git staging, commit,
  push or release was performed.
- After tightening the synchronous two-stage readiness invariant, a final
  synchronization deployed
  `v0.0.1-20260731T032925Z-intranet-working-tree@2026-07-31T03:29:25Z`.
  `/readyz` again returned `db=true` and `ready=true`; Zboard was healthy and
  the external `db` and `cache` containers were running.
- The final pre-switch backup is
  `/data/zboard-next/backups/20260731T032925Z/zboard-before-sync.sql`
  (72,902 bytes), previous source is
  `/data/zboard-next/app-prev-20260731T032925Z`, and source archive is
  `/data/zboard-next/releases/20260731T032925Z/source.tar.gz`
  (758,797 bytes). All were verified present.
- The final deployed source contains the synchronous readiness gate, the
  database retains one migration and the non-null/default-zero readiness
  column, and recent Zboard logs contain no panic, fatal, migration-startup or
  startup-failure message. The contract remains `legacy`; live data still has
  zero managed DNS records, certificates and Mieru endpoints.

### 2026-07-31 - Guarded resource deletion and Zero rc.4 Mieru activation

Goal outcome before intranet synchronization:

- Added authenticated DELETE routes and administrator controls for protocol
  endpoints, managed certificates and nodes. These resources previously had
  no delete implementation; the observed behavior was an absent capability,
  not a failing delete request.
- Protocol deletion rejects active-plan or running-publication references. An
  active endpoint is first disabled and removed through the normal full-node
  validate/activate/health/Connector publication boundary. Only then does the
  transaction remove endpoint/group/certificate bindings, revoke its
  credentials and delete the endpoint. Deployment, traffic and audit history
  remain available.
- Certificate deletion rejects endpoint references and running issuance or
  renewal operations. It deletes panel ownership but retains operation history
  and remote certificate files because those files may still be shared by
  services outside Zboard.
- Node deletion rejects protocol, certificate, DNS and running-operation
  dependencies. It removes the node record and current kernel state but does
  not remotely uninstall Zero or erase historical operations.
- Zero `0.0.15-rc.4` is now the concrete Mieru attribution boundary. The panel
  advertises that minimum version and gates endpoint creation, re-enabling,
  batch publication and subscription output by the selected or probed
  installed node version. Upgrading a node to rc.4 schedules the existing
  two-stage fallback/removal publication; older nodes continue to fail closed.
  `native-local-mieru` remains a compatible alias, while an rc.4-or-newer
  ordinary `native-local` artifact enables the same behavior automatically.
- Removed panel-only `credential_id` from all serialized legacy Zero managed
  users. The value remains encrypted-resource metadata for event attribution
  and audit, but the runtime emits only fields accepted by Zero, including the
  stable `principal_key`.
- The Zero repository source remained unmodified. Its checked-out official
  rc.4 commit is `929250f13`; only ignored local build output was regenerated
  to exercise the real validator and Connector.

Local verification:

- `go test ./...` passed for every backend package and `go vet ./...` passed.
- The OpenAPI test covers all three DELETE operations and the Mieru minimum
  Zero version. Runtime regression coverage proves that generated managed
  users do not leak `credential_id`, rc.3 remains blocked and rc.4 unlocks
  attributable Mieru users.
- All 60 frontend Vitest files and 138 tests passed. Frontend type checking and
  the production build passed with 538 transformed modules.
- A connector-enabled Zero `0.0.15-rc.4` debug binary built from the clean
  official checkout passed
  `TestNativeKernelAccessConfigValidatesWithCurrentZero`,
  `TestMieruPrincipalContractValidatesWithOptInZero` and
  `TestCurrentZeroDeliversNativeConnectorEvent`.
- `git diff --check` reported no whitespace error, only the repository's
  existing Windows LF-to-CRLF notices. The Zero source worktree remained
  clean.

Remaining gaps before synchronization:

- Destructive behavior will not be exercised against live administrator data
  during deployment verification. Route registration, authentication,
  deployed source and service health can be verified without deleting an
  operator-owned resource.
- Existing nodes must actually be upgraded to Zero rc.4 before their Mieru
  endpoints can publish. Zboard synchronization alone does not authorize or
  perform that kernel upgrade.
- Business and immutable-history resources intentionally retain their existing
  lifecycle semantics; this change addresses the reported operational node,
  certificate and protocol assets rather than introducing indiscriminate
  deletion of orders, traffic, audit or operation history.
- No Git staging, commit, push or release was performed.

Intranet synchronization and deployed evidence:

- `scripts/sync-intranet.ps1 -SkipLocalChecks` successfully deployed
  `v0.0.1-20260731T095009Z-intranet-working-tree@2026-07-31T09:50:09Z`.
  `/api/v1/version` returned that exact version and advertises
  `mieru.supported=true` with
  `minimum_zero_version=0.0.15-rc.4`. `/readyz` returned `db=true` and
  `ready=true`.
- `zboard_next-zboard-1` is running and healthy. The external `db` and `cache`
  containers are running; MySQL returned `mysqld is alive` and an
  authenticated Redis check returned `PONG`. Recent Zboard logs contain no
  panic, fatal, migration-startup or startup-failure message.
- Unauthenticated DELETE requests for a nonexistent node, certificate and
  protocol endpoint all returned HTTP 401. This verifies route registration
  and the administrator boundary without deleting live data. The deployed
  source contains all three handlers, the rc.4 Mieru version boundary and no
  runtime managed-user `credential_id`.
- The pre-switch database backup is
  `/data/zboard-next/backups/20260731T095009Z/zboard-before-sync.sql`
  (72,902 bytes), the previous source is
  `/data/zboard-next/app-prev-20260731T095009Z`, and the synchronized source
  archive is
  `/data/zboard-next/releases/20260731T095009Z/source.tar.gz`
  (769,022 bytes). All were verified present.
- The live database currently has one node, one protocol endpoint, no managed
  certificates and no Mieru endpoints. Node 1 remains healthy on Zero
  `0.0.15-rc.3`, so the version-aware gate correctly continues to block Mieru
  there until an operator explicitly upgrades that node to rc.4 or newer.
  No live kernel reconcile, protocol publication or resource deletion was
  performed.
- The Zero source worktree remained clean. No kernel source was modified, and
  no Git staging, commit, push or release was performed.

### 2026-07-31 - Kernel-aware Mieru availability gate

Goal outcome before intranet synchronization:

- The public version response now exposes protocol-kernel capabilities with a
  supported flag and operator-facing reason. Under the deployed `legacy` and
  current `native-local` contracts, Mieru is explicitly unsupported because
  Zero does not provide the required `principal_key` attribution contract.
- The backend is authoritative: it rejects new Mieru endpoints, re-enabling
  retained Mieru endpoints, batch enable/deploy and any runtime publication
  containing an active unsupported Mieru endpoint. Subscription generation
  excludes unsupported endpoints and legacy startup no longer prepares new
  per-subscription Mieru credentials.
- Historical Mieru records remain visible and searchable. They may be disabled
  and removed from runtime, but cannot be copied, deployed or re-enabled. This
  preserves an operational escape path without advertising a configuration
  the current kernel cannot run correctly.
- The protocol editor continues to show Mieru as a disabled option labelled
  `Mieru（当前内核不支持）`, displays the backend reason in list, detail and
  editor views, and fails closed for Mieru if capability discovery is
  unavailable. Other established protocols remain available.
- The future `native-local-mieru` contract remains the only explicit opt-in
  path that can expose Mieru as supported after a reviewed kernel implements
  and passes the existing contract probe. No Zero source, binary or runtime
  state was modified.

Local verification:

- `go test ./...` and `go vet ./...` passed for every backend package.
- All 60 frontend Vitest files and 137 tests passed. Frontend type checking and
  the production build passed with 538 transformed modules.
- OpenAPI coverage includes the protocol capability response and endpoint
  support fields. Backend tests prove that current contracts disable Mieru,
  ordinary protocols remain enabled and only the explicit future contract
  opens the feature.
- `git diff --check` reported no whitespace error, only the repository's
  existing Windows LF-to-CRLF notices. The Zero repository remained clean.

Remaining gaps before synchronization:

- The live database previously contained zero Mieru endpoints, so the retained
  historical-record disable flow cannot be exercised against existing
  production data without fabricating a record. The public capability response
  and deployed source/runtime gates can still be verified after synchronization.
- No Git staging, commit, push or release was performed.

Intranet synchronization and deployed evidence:

- `scripts/sync-intranet.ps1 -SkipLocalChecks` successfully deployed
  `v0.0.1-20260731T040044Z-intranet-working-tree@2026-07-31T04:00:44Z`.
  The version endpoint returned that exact build and the unchanged `legacy`
  kernel contract.
- The deployed version response reports `mieru.supported=false` with the
  operator-facing `principal_key` explanation while VLESS, VMess, Trojan,
  Shadowsocks and Hysteria2 remain supported. `/readyz` returned `db=true` and
  `ready=true`.
- `zboard_next-zboard-1` was healthy. The external `db` and `cache` containers
  were running. Recent Zboard logs contained no panic, fatal, migration-startup
  or startup-failure message.
- The pre-switch database backup is
  `/data/zboard-next/backups/20260731T040044Z/zboard-before-sync.sql`
  (72,902 bytes), the previous source is
  `/data/zboard-next/app-prev-20260731T040044Z`, and the synchronized archive is
  `/data/zboard-next/releases/20260731T040044Z/source.tar.gz`
  (763,277 bytes). All were verified present.
- The deployed database still contains zero Mieru endpoints. An unauthenticated
  protocol-endpoint request returned 401, and the deployed source contains both
  the backend kernel gate and frontend unsupported-state controls. The container
  environment still reports `ZBOARD_ZERO_KERNEL_CONTRACT=legacy`.
- An accidentally concurrent second synchronization attempt at
  `20260731T040102Z` built the same working tree and created another non-empty
  database backup, source archive and candidate directory, but exited before
  copying or activating an application because the successful first attempt had
  already moved the old `app` directory. It did not change the verified live
  version. Those unused artifacts were retained rather than removed without a
  separate retention decision.
- No Zero source, binary or node runtime was changed. No Git staging, commit,
  push or release was performed.

### 2026-07-30 - Node credential drawer state convergence

Outcome before synchronization:

- Fixed the one-time Zero connector and traffic-report credential dialogs so
  successful creation immediately patches the selected node with the returned
  non-secret prefix and then reloads the canonical node detail.
- Replaced the close icon's inline secret clearing with a shared close handler.
  Both the icon and a new explicit `完成` footer action clear the one-time
  secret and reload the selected node detail, so the underlying credential
  controls no longer remain on `生成`.
- Applied the same detail-refresh boundary to credential revocation. The
  paged node list returns only summaries and therefore is no longer treated as
  sufficient to refresh detail-only credential fields.
- A failed follow-up detail read does not discard the successful API response
  patch or expose the one-time secret outside its modal.

Local verification:

- `pnpm typecheck`
- `pnpm test` passed 60 files and 136 tests.
- `pnpm build` passed with 538 transformed modules.
- The infrastructure detail policy test pins the shared close handler,
  explicit completion action, returned-prefix patches and canonical detail
  reload.
- `git diff --check` passed with only existing Windows LF-to-CRLF notices.

Synchronization and deployment evidence:

- Final synchronization succeeded with version
  `v0.0.1-20260729T162149Z-intranet-working-tree@2026-07-29T16:21:49Z`.
- Database backup:
  `/data/zboard-next/backups/20260729T162149Z/zboard-before-sync.sql`
  (71,027 bytes).
- Previous application source:
  `/data/zboard-next/app-prev-20260729T162149Z`.
- Source release archive:
  `/data/zboard-next/releases/20260729T162149Z/source.tar.gz`
  (740,002 bytes).
- `/api/v1/version` and `/readyz` returned HTTP/API 200 with `ready=true` and
  `db=true`; `/admin/nodes` returned the SPA with HTTP 200.
- `zboard_next-zboard-1` is healthy on the external `redis_default` network.
  The existing `db` and `cache` containers remain running; MySQL reported
  `mysqld is alive` and an authenticated Redis ping returned `PONG`.
- The active node JavaScript chunk contains the one-time credential dialog,
  connector `api_key_prefix` handling and traffic `secret_prefix` handling,
  confirming that both returned-prefix state paths are in the running asset.

Remaining gaps:

- No real credential was rotated during verification, deliberately avoiding
  invalidation of active node credentials. Automated policy coverage and
  active-asset inspection passed.
- No Git staging, commit, push or release was performed.

### 2026-07-29 - Infrastructure navigation hierarchy and independent DNS workbench

Outcome before synchronization:

- Split the former combined external-provider workbench into two independent
  administrator pages. `/admin/providers` now owns only provider accounts,
  encrypted credentials, capabilities and verification; it no longer loads,
  renders or operates managed DNS records.
- Added `/admin/dns-records` as the dedicated managed-DNS workbench. It owns
  record listing, pagination, creation, synchronization, propagation state and
  certificate handoff. Its empty state links to provider-account management
  without embedding that separate resource on the page.
- Reworked the infrastructure sidebar into a three-level information
  hierarchy. Under the `基础设施` domain, `资源与接入` groups node assets,
  external providers and DNS records as separate leaf pages, while
  `服务与交付` groups certificates, protocol services, node groups and
  traffic reconciliation.
- Added restrained nested-navigation styling with compact parent labels,
  indented leaf links and leaf-only active state. Existing provider and node
  URLs remain valid, and `/dns-records` redirects to the administrator route.
- No backend resource ownership or API contract changed. Provider accounts,
  managed DNS records and nodes remain separate typed resources linked only
  through explicit identifiers.

Local verification:

- `pnpm typecheck`
- `pnpm test` passed 60 files and 135 tests.
- `pnpm build` passed with 538 transformed modules and separate lazy-loaded
  provider and managed-DNS chunks.
- Infrastructure detail and route-loading policy tests pin the independent
  page boundary, nested navigation entries and lazy route count.
- `git diff --check` passed with only existing Windows LF-to-CRLF notices.

Synchronization and deployment evidence:

- Final synchronization succeeded with version
  `v0.0.1-20260729T125332Z-intranet-working-tree@2026-07-29T12:53:32Z`.
- Database backup:
  `/data/zboard-next/backups/20260729T125332Z/zboard-before-sync.sql`
  (70,681 bytes).
- Previous application source:
  `/data/zboard-next/app-prev-20260729T125332Z`.
- Source release archive:
  `/data/zboard-next/releases/20260729T125332Z/source.tar.gz`
  (739,124 bytes).
- `/api/v1/version` and `/readyz` returned HTTP/API 200 with `ready=true` and
  `db=true`. `/admin/providers`, `/admin/dns-records` and `/admin/nodes` each
  returned the SPA with HTTP 200.
- `zboard_next-zboard-1` is healthy on the external `redis_default` network.
  The existing `db` and `cache` containers remain running; MySQL reported
  `mysqld is alive` and an authenticated Redis ping returned `PONG`.
- The active web bundle contains separate `Providers` and `ManagedDNS`
  JavaScript chunks. The provider chunk contains its provider-only page copy,
  the DNS chunk contains its provider-reference copy, and the active
  `AdminLayout` chunk contains both nested infrastructure group labels.

Remaining gaps:

- No authenticated pixel screenshot was captured. Static page-boundary tests,
  production build output, deployed route checks and active bundle inspection
  passed.
- No Git staging, commit, push or release was performed.

### 2026-07-29 - Explicit Zero prerelease selection and historical GitHub musl compatibility

Outcome before synchronization:

- Removed stable-release preference from the single-node version picker.
  Stable and prerelease tags are both ordinary published choices; the first
  platform-compatible published release is selected by default. The latest
  stable release remains only the unattended batch-reconcile default.
- The UI now labels the release fact as the latest published version and no
  longer reports that a stable version is required when no selection is
  available.
- GitHub musl resolution accepts both the current unversioned
  `zero-linux-x86_64-musl.tar.gz` pair and the historical Release-owned
  `zero-v<version>-linux-x86_64-musl.tar.gz` pair. Archive and checksum must
  use the same naming generation and the checksum must name that exact archive.
- This makes the currently published prerelease's historical musl artifact
  visible and selectable on old-glibc nodes without weakening exact-tag,
  checksum, platform or downgrade enforcement.

Local verification:

- The live published prerelease asset list was read and confirmed to contain
  the historical versioned musl archive and checksum pair.
- `go test ./...`
- `go vet ./...`
- `pnpm test` passed 60 files and 135 tests.
- `pnpm typecheck`
- `pnpm build`
- `git diff --check` passed with only existing Windows LF-to-CRLF notices.

Synchronization and deployment evidence:

- Final synchronization succeeded with version
  `v0.0.1-20260729T112447Z-intranet-working-tree@2026-07-29T11:24:47Z`.
- Database backup:
  `/data/zboard-next/backups/20260729T112447Z/zboard-before-sync.sql`
  (70,681 bytes).
- Previous application source:
  `/data/zboard-next/app-prev-20260729T112447Z`.
- Source release archive:
  `/data/zboard-next/releases/20260729T112447Z/source.tar.gz`
  (737,410 bytes).
- `/api/v1/version` and `/readyz` returned HTTP/API 200 with `ready=true` and
  `db=true`. The protected release-list route returned HTTP 401 without
  credentials rather than 404.
- `zboard_next-zboard-1` is healthy on the external `redis_default` network.
  The existing `db` and `cache` containers remain running; MySQL reported
  `mysqld is alive` and an authenticated Redis ping returned `PONG`.
- The active node JavaScript contains both the latest-published label and the
  platform-compatible published-version empty state. The old stable-only
  empty-state text is absent. Synchronized backend source contains the
  current-first, historical-versioned musl resolver and release-list matcher.

Remaining gaps:

- The authenticated release-list response was not invoked because no
  administrator credential was invented or extracted. Backend regression
  tests use the exact currently published historical prerelease filename, and
  active route/source/frontend checks passed.
- No node kernel operation was started as part of this correction.
- No Git staging, commit, push or release was performed.

### 2026-07-29 - Zero release artifact contract and explicit version selection

Outcome before synchronization:

- Legacy kernel resolution now recognizes the official release assets
  `zero-linux-x86_64.tar.gz` and `zero-linux-x86_64-musl.tar.gz`, each with its
  exact same-name `.sha256` file. Nodes with glibc below 2.34 prefer the
  official musl asset from the selected Release instead of requiring a
  versioned local filename.
- Historical `zero-v<version>-linux-x86_64-musl.tar.gz` files remain a bounded
  fallback for stable releases published before the official musl asset.
  `native-local` retains its separate exact-version trusted-directory contract
  and is not silently redirected to GitHub.
- Added an administrator release-list endpoint. It returns published stable
  and prerelease tags together with GNU/musl availability; the default UI
  selection remains the newest compatible stable version.
- Node reconciliation accepts an explicit version but never a client-supplied
  URL. The backend fetches that exact GitHub tag again, rejects drafts or tag
  mismatches, selects the artifact for the detected libc, verifies the
  checksum filename/hash and records the resolved immutable artifact.
- Selecting a version below the installed semantic version requires both a
  danger confirmation in the node drawer and an explicit
  `allow_downgrade=true` request paired with the target version. Semantic
  ordering now handles prerelease identifiers such as `rc.2` and `rc.10`.
- The node kernel drawer shows a version selector, prerelease labels and
  GNU/musl compatibility. Incompatible versions are disabled. Single-node
  operations can install, upgrade, repair, configure or explicitly downgrade;
  batch reconcile retains the latest-stable, no-downgrade behavior.
- Updated the OpenAPI contract and kernel lifecycle/data-boundary
  documentation. No node kernel operation was started as part of this goal.

Local verification:

- `go test ./...`
- `go vet ./...`
- `pnpm test` passed 60 files and 135 tests.
- `pnpm typecheck`
- `pnpm build`
- `git diff --check` passed with only existing Windows LF-to-CRLF notices.
- The current GitHub release list was read successfully. Existing stable
  releases expose GNU assets; the current prerelease still uses the historical
  versioned musl name, while the inspected Zero release workflow now packages
  the new unversioned musl contract for the next publication.

Synchronization and deployment evidence:

- Final synchronization succeeded with version
  `v0.0.1-20260729T103727Z-intranet-working-tree@2026-07-29T10:37:27Z`.
- Database backup:
  `/data/zboard-next/backups/20260729T103727Z/zboard-before-sync.sql`
  (70,681 bytes).
- Previous application source:
  `/data/zboard-next/app-prev-20260729T103727Z`.
- Source release archive:
  `/data/zboard-next/releases/20260729T103727Z/source.tar.gz`
  (736,319 bytes).
- `/api/v1/version` and `/readyz` returned HTTP/API 200 with `ready=true` and
  `db=true`. The new administrator release-list route returned HTTP 401
  without credentials, proving the protected deployed route exists rather
  than falling through to 404.
- `zboard_next-zboard-1` is healthy on the external `redis_default` network.
  The existing `db` and `cache` containers remain running without restart;
  an active MySQL ping reported `mysqld is alive` and an authenticated Redis
  ping returned `PONG`.
- The running backend binary contains
  `zero-linux-x86_64-musl.tar.gz`. The active frontend assets contain both the
  kernel-release request path and the explicit `allow_downgrade` request
  field. An outbound request from the application container reached the
  GitHub Releases API and observed the current published prerelease tag.

Remaining gaps:

- The authenticated release-list response was not invoked because no
  administrator credential was invented or extracted for deployment
  verification. Backend handler tests, the protected route check and active
  artifact inspection passed.
- A real install of a newly published unversioned musl Release cannot be
  qualified until Zero publishes that asset. This goal does not publish Zero
  and deliberately did not start a node kernel operation.
- No Git staging, commit, push or release was performed.

### 2026-07-29 - Provider workbench layout and node drawer feedback ownership

Outcome before synchronization:

- Reframed the provider-account and managed-DNS areas as two bounded panels.
  Their empty states now use a compact height instead of stacking multiple
  page-sized placeholders.
- Kept the managed-DNS create action intrinsically sized and non-wrapping so
  it cannot collapse into the narrow two-line button seen in the deployed
  empty state. Mobile layout gives the action its own row.
- Added drawer-local success and error state to the node asset detail drawer.
  SSH verification, host-key reset, kernel discovery/reconciliation, protocol
  multiplier updates and connector/report credential operations now report
  beside the control that initiated them rather than above the main node list.
- VPS create, edit and SSH configuration forms continue to use their existing
  modal-local field and form errors.
- Extended the infrastructure detail policy test to pin both the feedback
  ownership boundary and the compact provider layout.

Local verification:

- `pnpm test -- infrastructureDetailPolicy.test.ts` passed 4 tests.
- `pnpm test` passed 60 files and 134 tests.
- `pnpm typecheck`
- `pnpm build`
- `git diff --check` passed with only existing Windows LF-to-CRLF notices.

Synchronization and deployment evidence:

- Final synchronization succeeded with version
  `v0.0.1-20260729T092939Z-intranet-working-tree@2026-07-29T09:29:39Z`.
- Database backup:
  `/data/zboard-next/backups/20260729T092939Z/zboard-before-sync.sql`
  (70,037 bytes).
- Previous application source:
  `/data/zboard-next/app-prev-20260729T092939Z`.
- Source release archive:
  `/data/zboard-next/releases/20260729T092939Z/source.tar.gz`
  (730,532 bytes).
- `/api/v1/version` and `/readyz` returned HTTP/API 200 with `ready=true` and
  `db=true`. Both `/admin/providers` and `/admin/nodes` returned the SPA with
  HTTP 200.
- `zboard_next-zboard-1` is healthy on the existing `redis_default` network.
  External `db` and `cache` containers remain running.
- The active container's provider stylesheet contains the compact 150 px
  empty-state rule, and its node JavaScript asset contains the drawer-local
  SSH error path. This verifies that the goal-specific frontend assets are in
  the running image.

Remaining gap:

- Browser control could not finish navigating the existing Chrome tab to the
  intranet page before its connection timeout, so an automated screenshot of
  the authenticated deployed workbench was unavailable. Static policy tests,
  the production build and active-container asset inspection passed.

### 2026-07-29 - External MySQL/Redis intranet deployment reset

Outcome before final synchronization:

- Changed both source-build and release-image Compose bundles to start only
  Zboard and join an explicitly named external Docker network. They no longer
  declare, create, restart or own MySQL, Redis, their volumes or a private
  backend network.
- Made the complete application DSN, Redis address/password and external
  network deployment inputs. Added Redis-password environment parsing without
  exposing it through configuration JSON.
- Updated the intranet synchronization script to dump the explicitly
  configured external MySQL container and to support a clean first deployment
  with no previous application source.
- The existing MySQL root account is used only for database backup and
  provisioning. Production Zboard continues to reject a root runtime DSN, so a
  database-scoped application account is used for the running service.

Local verification:

- `go test ./...`
- `go vet ./...`
- Both `deploy/docker/docker-compose.yml` and
  `deploy/docker/docker-compose.release.yml` passed `docker compose config
  --quiet` with non-secret validation inputs.
- `git diff --check`

Destructive reset authorized by the operator:

- Confirmed `/data/zboard-next` resolved exactly to the requested target and
  was not a mount point, then removed the old `zboard_next` application,
  MySQL and Redis containers, its private network, its two Compose data volumes
  and all historical contents below `/data/zboard-next`.
- Existing external containers `db` and `cache`, their `redis_default` network
  and their data were not removed or restarted. Root filesystem free space
  increased from 4.2 GiB to 8.5 GiB after cleanup.
- Recreated `/data/zboard-next` with an empty artifact directory and a
  permission-restricted runtime environment file. No credential value is
  recorded here.

Pre-final deployment attempts:

- Build `v0.0.1-20260729T090407Z-intranet` succeeded but stopped before backup
  because the requested `zboard` database did not yet exist.
- Created the empty `zboard` database in the existing MySQL container.
- Build `v0.0.1-20260729T090520Z-intranet` reached application startup but
  correctly failed because production mode rejects a root runtime DSN.
- Provisioned a database-scoped application account in the existing MySQL
  container and updated only the protected runtime environment file.
- Build `v0.0.1-20260729T090755Z-intranet` then deployed successfully using
  only the external services; a final synchronization follows the last
  Redis-password configuration verification.

Final synchronization and deployment evidence:

- Final synchronization succeeded with version
  `v0.0.1-20260729T091035Z-intranet-working-tree@2026-07-29T09:10:35Z`.
- Database backup:
  `/data/zboard-next/backups/20260729T091035Z/zboard-before-sync.sql`
  (64 KiB).
- Previous application source:
  `/data/zboard-next/app-prev-20260729T091035Z`.
- Source release archive:
  `/data/zboard-next/releases/20260729T091035Z/source.tar.gz`
  (716 KiB).
- `/api/v1/version` and `/readyz` returned HTTP/API 200 with `ready=true` and
  `db=true`. `/setup` and `/admin/providers` both returned the SPA with HTTP
  200.
- The `zboard_next` Compose project contains only
  `zboard_next-zboard-1`; it is healthy and attached to `redis_default`.
  Existing `db` and `cache` containers remained up for two months and were not
  restarted. Authenticated Redis `PING` returned `PONG`.
- The clean MySQL database contains 37 tables including
  `schema_migrations`; one baseline migration is recorded, and
  `provider_accounts`, `managed_dns_records` and `provider_operations` exist.
- Root filesystem free space is 8.3 GiB (82% used).

Remaining gap:

- The database was intentionally initialized empty, so the operator must
  complete the one-time `/setup` flow before authenticated administrator APIs
  can be used. No bootstrap administrator credential was invented or stored.

### 2026-07-29 - Generic external providers and Cloudflare DNS management

Outcome before synchronization:

- Added a reusable external-provider account boundary with namespaced
  capabilities, encrypted/redacted credentials, verification state and
  provider operations. Cloudflare exposes `dns.records` and
  `certificate.origin`; Let's Encrypt remains cataloged separately as
  `certificate.public`. DNS, certificates and future payment channels keep
  their own typed resource models rather than sharing an opaque provider JSON
  resource.
- Added Cloudflare account creation and token verification. API tokens are
  encrypted with the existing credential cipher, never returned by APIs and
  represented only by a non-secret prefix in the administrator workbench.
- Added panel-managed A/AAAA records with handwritten FQDNs, explicit node and
  provider-account selection, optional node-address IP discovery, TTL and
  Cloudflare proxy settings. The adapter discovers the longest matching active
  Zone, refuses silent takeover of an existing remote record, upserts through
  the Cloudflare v4 API and records desired/observed hashes.
- Added asynchronous provider operations, synchronization phases, provider
  error state and a separate public-DNS observation flag. Direct records are
  verified against the desired IP; proxied records are verified by observing
  any public address because Cloudflare returns edge addresses.
- Added the `/admin/providers` workbench and linked a managed DNS record to the
  existing certificate application page with its node and domain prefilled.
  Certificate issuance remains a separate resource and provider workflow.

Local verification:

- Go 1.26.5 was installed from the configured regional mirror after the
  default `go.dev` archive download failed.
- `go test ./...`
- `go vet ./...`
- `pnpm test -- --run` passed: 60 files and 132 tests.
- `pnpm typecheck`
- `pnpm build`
- `git diff --check`
- Cloudflare adapter tests cover normalized desired-state hashing, longest
  visible Zone discovery, bearer authentication and redacted API errors via a
  mock HTTP server.

Remaining gaps before synchronization:

- No real Cloudflare API Token or public Zone was used during local
  verification, so live account permissions, record creation and DNS
  propagation still require authenticated intranet/browser evidence.
- The first phase creates and verifies provider accounts but does not yet
  expose credential rotation, account deletion or DNS record editing/deletion.
  DNS-01 and Cloudflare Origin CA issuance remain future certificate-provider
  work.

Synchronization and deployment evidence:

- `scripts/sync-intranet.ps1 -SkipLocalChecks` retained
  `/data/zboard-next/releases/20260729T083026Z/source.tar.gz` (712 KiB), then
  failed while Docker copied the build context with `no space left on device`.
  The remote root filesystem was at 100% with only 280 KiB available.
- Failure occurred before the database dump and source switch. The matching
  `/data/zboard-next/backups/20260729T083026Z` directory is empty, and there is
  no `app-prev-20260729T083026Z` or `app-failed-20260729T083026Z` source path.
- Post-failure verification returned HTTP/API 200 from `/api/v1/version` and
  `/readyz`, with `ready=true` and `db=true`.
  `zboard_next-zboard-1`, `zboard_next-mysql-1` and
  `zboard_next-redis-1` remain healthy. The active version remains
  `v0.0.1-20260726T105144Z-intranet-working-tree@2026-07-26T10:51:44Z`.

Remaining deployment gaps:

- Intranet synchronization is incomplete. The generic-provider and Cloudflare
  DNS changes are present only in the retained source archive, not the active
  application.
- Remote disk space must be recovered before another build. Even after that,
  the existing pre-release database baseline mismatch documented by the
  preceding deployment attempts may still block application startup.

### 2026-07-29 - Release-image Docker Compose deployment bundle

Outcome before synchronization:

- Added `deploy/docker/docker-compose.release.yml` for deployment from the
  release workflow's prebuilt `linux/amd64`
  `ghcr.io/zerodenet/zboard:<tag>` image. It does not rebuild source and works
  with either a registry pull or the compressed Docker image release asset.
- The bundle includes persistent MySQL 8.4 and Redis 7 services, health-gated
  application startup, private service networking, persistent data volumes,
  application readiness checks and a read-only trusted Zero artifact mount.
- Added `deploy/docker/.env.release.example` with the current
  `v0.0.1-dev.3` image tag, safe loopback HTTP binding, all required runtime
  secrets, optional bootstrap-admin settings, pull/archive modes and explicit
  legacy versus native-local kernel settings. No real credential was stored.
- Required secrets use Compose required-value guards. Empty example values
  fail before container creation rather than starting with placeholders.

Local verification:

- `docker compose --env-file deploy/docker/.env.release.example -f
  deploy/docker/docker-compose.release.yml config --quiet` passed with
  temporary non-secret validation values supplied through the process
  environment.
- The same Compose render with empty required values failed as intended at
  `ZBOARD_MYSQL_ROOT_PASSWORD`.
- `git diff --check -- deploy/docker/docker-compose.release.yml
  deploy/docker/.env.release.example`

Synchronization and deployment evidence:

- The first `scripts/sync-intranet.ps1 -SkipLocalChecks` attempt retained
  `/data/zboard-next/releases/20260729T031900Z/source.tar.gz` but ended during
  the remote image build before creating a database backup or switching source.
- A second attempt built the candidate, created
  `/data/zboard-next/backups/20260729T032135Z/zboard-before-sync.sql`
  (84,114 bytes) and
  `/data/zboard-next/releases/20260729T032135Z/source.tar.gz`
  (714,529 bytes), then switched source. The application failed startup with
  `pre-release baseline schema is incomplete: found 30 of 33 required tables`.
- The failed source is preserved at
  `/data/zboard-next/app-failed-20260729T032135Z`. The previous source was
  restored to `/data/zboard-next/app`, so the temporary
  `/data/zboard-next/app-prev-20260729T032135Z` path no longer exists.
- Because the failed build overwrote `zboard_next-zboard:latest`, the automatic
  source rollback initially left the application restarting. Retagged the
  retained healthy image
  `fa44cab95b260a86a9e7fa48e735c676ce69c51abbe8ddef7c5bc6c58f756864`
  and recreated only the Zboard container. MySQL and Redis were not restarted.
- Final verification returned HTTP/API 200 from `/api/v1/version` and
  `/readyz`, with `ready=true` and `db=true`.
  `zboard_next-zboard-1`, `zboard_next-mysql-1` and
  `zboard_next-redis-1` are healthy. The active version remains
  `v0.0.1-20260726T105144Z-intranet-working-tree@2026-07-26T10:51:44Z`.

Remaining gaps:

- Intranet synchronization is incomplete because the current database still
  fails the pre-release baseline schema signature. The release Compose bundle
  is present only in the retained failed source/archive, not the active source.
- No release image was pulled or stack started locally; runtime deployment
  remains subject to the selected published tag being present and the Docker
  host having GHCR access when the package is not public.

### 2026-07-29 - Release and GHCR prerelease history retention

Outcome before synchronization:

- Added a post-publication cleanup step to the tag-driven release workflow.
  Publishing an `-rc` tag removes every historical `-dev` GitHub Release and
  GHCR package version; publishing a stable numeric SemVer tag removes every
  historical `-rc` Release and package version.
- Git tags remain intact as source-history markers. Other prerelease families
  do not trigger cleanup, and a GHCR version carrying any non-matching tag is
  retained to avoid deleting a shared stable manifest.
- Added a standalone cleanup script with pagination, dry-run support and
  explicit repository/package configuration, plus mock-API behavior coverage.
  The workflow token must have administrator access to the GHCR package.

Local verification:

- `C:\Program Files\Git\bin\bash.exe -n scripts/cleanup-release-history.sh`
- `C:\Program Files\Git\bin\bash.exe -n scripts/tests/cleanup-release-history-test.sh`
- `C:\Program Files\Git\bin\bash.exe scripts/tests/cleanup-release-history-test.sh`
- `git diff --check`

Synchronization and deployment evidence:

- `scripts/sync-intranet.ps1 -SkipLocalChecks` created
  `/data/zboard-next/releases/20260729T015806Z/source.tar.gz` (712,772 bytes)
  and candidate source `/data/zboard-next/candidates/20260729T015806Z`, then
  failed during the remote Docker build because Docker Hub TLS handshakes timed
  out while resolving `alpine:3.20` and `golang:1.26.5-alpine`.
- The build failed before the database dump and source switch. Consequently
  `/data/zboard-next/backups/20260729T015806Z` contains no database backup and
  no new previous-source path exists. The deployed application remains
  `v0.0.1-20260726T105144Z-intranet-working-tree@2026-07-26T10:51:44Z`.
- Post-failure checks returned HTTP/API 200 from `/api/v1/version` and
  `/readyz`, with `ready=true` and `db=true`. `zboard_next-zboard-1`,
  `zboard_next-mysql-1` and `zboard_next-redis-1` all remain healthy.

Remaining gaps:

- Intranet synchronization is incomplete. The retained candidate and release
  archive contain the verified change, but the active intranet source was not
  switched because the image build failed.
- A live GitHub tag run is still required to confirm Release deletion and GHCR
  package-version deletion with the package's current Actions access settings.

### 2026-07-28 - GHCR repository association metadata

Outcome before synchronization:

- Added OCI source metadata to the final Docker image so GHCR can associate the
  `ghcr.io/zerodenet/zboard` package with the `zerodenet/zboard` repository.
- Updated the release workflow to use `docker/metadata-action@v5` for tag
  metadata and labels before `docker/build-push-action@v6` pushes the image.
- Documented that GHCR package visibility is controlled in organization
  package settings; repository metadata helps association, but public
  anonymous pulls still require an organization owner to make the package
  public.

Local verification:

- `git diff --check`
- Workflow review confirmed `docker/metadata-action@v5`,
  `steps.meta.outputs.tags`, `steps.meta.outputs.labels` and
  `org.opencontainers.image.source` are present.
- Dockerfile review confirmed the OCI source label is in the final Alpine image
  stage.

Remaining gaps:

- A live tag run is still required to confirm GHCR package association and
  whether organization package visibility has been changed to public.
- Intranet synchronization still fails against the existing database baseline
  mismatch, so this metadata-only change is not deployed to the intranet.

Synchronization and deployment evidence:

- `scripts/sync-intranet.ps1 -SkipLocalChecks` built and switched the intranet
  source to `v0.0.1-20260728T122808Z-intranet`, created
  `/data/zboard-next/backups/20260728T122808Z/zboard-before-sync.sql` and
  `/data/zboard-next/releases/20260728T122808Z/source.tar.gz`, then failed
  startup with `pre-release baseline schema is incomplete: found 30 of 33
  required tables`.
- The failed source was preserved as
  `/data/zboard-next/app-failed-20260728T122808Z`. The previous source was
  restored to `/data/zboard-next/app`, and the healthy older image
  `fa44cab95b260a86a9e7fa48e735c676ce69c51abbe8ddef7c5bc6c58f756864` was
  retagged to `zboard_next-zboard:latest`.
- Post-restore verification returned HTTP 200 from `/api/v1/version` and
  `/readyz`; `zboard_next-zboard-1`, `zboard_next-mysql-1` and
  `zboard_next-redis-1` were healthy.
- Removed only failed-build/candidate residue and Docker build cache after the
  failed sync. Root filesystem free space recovered from 704 MiB to 2.5 GiB;
  database backups, release archives and previous-source directories were
  retained.

### 2026-07-28 - Actions artifact upload and commit-based release notes

Outcome before synchronization:

- Extended the release workflow so tag runs now upload the binary archive,
  compressed Docker image archive and checksum file as an Actions artifact in
  addition to publishing the GitHub Release assets.
- Replaced the default GitHub `Full Changelog` release note generation with a
  custom note file derived from `git log` between the previous tag and the
  current tag. The first tagged release falls back to the full commit history
  reachable from that tag.
- Updated `RELEASING.md` to describe the Actions artifact and the
  commit-based release notes.

Local verification:

- `git diff --check`
- Workflow review confirmed `actions/upload-artifact` and `notes-file`
  settings are present in `.github/workflows/release.yml`

Remaining gaps:

- The new artifact upload and commit-based release notes have not yet been
  exercised in a live GitHub tag run.
- Intranet synchronization reran and failed against the same existing database
  baseline mismatch. The healthy previous image was restored afterward, so the
  workflow change remains undeployed on the intranet.

Synchronization and deployment evidence:

- `scripts/sync-intranet.ps1 -SkipLocalChecks` built and switched the intranet
  source to `v0.0.1-20260728T110417Z-intranet`, created
  `/data/zboard-next/backups/20260728T110417Z/zboard-before-sync.sql` and
  `/data/zboard-next/releases/20260728T110417Z/source.tar.gz`, then failed
  startup with `pre-release baseline schema is incomplete: found 30 of 33
  required tables`.
- The failed source was preserved as
  `/data/zboard-next/app-failed-20260728T110417Z`. The previous source was
  restored to `/data/zboard-next/app`, and the healthy older image
  `fa44cab95b260a86a9e7fa48e735c676ce69c51abbe8ddef7c5bc6c58f756864` was
  retagged to `zboard_next-zboard:latest`.
- Post-restore verification returned HTTP 200 from `/api/v1/version` and
  `/readyz`; `zboard_next-zboard-1`, `zboard_next-mysql-1` and
  `zboard_next-redis-1` were healthy.

### 2026-07-28 - Release Docker image archive attachment

Outcome before synchronization:

- Added a compressed Docker image archive to the tag-driven release flow. The
  workflow now saves the pushed GHCR image with `docker save`, compresses it,
  computes a checksum and uploads it alongside the binary archive in the GitHub
  Release.
- The release notes and checksum list still come from the same tag run, so the
  binary and image assets stay paired with one release record.
- Updated `RELEASING.md` to document the attached Docker image archive.

Local verification:

- `git diff --check`
- Workflow review confirmed the new `IMAGE_ARCHIVE`, `docker save` and
  `gh release create` asset entries are present.

Remaining gaps:

- The attachment flow has not yet been exercised by a live GitHub tag run.
- A synchronization attempt still hits the existing intranet database baseline
  mismatch during application startup; the previous healthy image is restored
  after each failed attempt.

### 2026-07-28 - Release image archive attachment

Outcome before synchronization:

- Extended the tag-driven release workflow so it now publishes both the GHCR
  image and a compressed Docker image archive (`docker save` + gzip) as a GitHub
  Release asset.
- The release asset set now includes the backend binary archive, the Docker
  image archive and a shared SHA-256 checksum list, while still generating
  release notes from commits since the previous tag.
- Updated `RELEASING.md` to state that release artifacts include an attached
  compressed Docker image archive.

Local verification:

- `git diff --check`
- Manual workflow review of `.github/workflows/release.yml` for the image
  archive export and release attachment steps

Remaining gaps:

- This change has not been exercised in a live GitHub tag run yet, so the
  image archive upload still needs runtime confirmation from Actions.
- Intranet synchronization was rerun and failed for the same existing database
  baseline mismatch; the healthy previous image was restored afterward.

### 2026-07-28 - Branch-aware release tags and GitHub release automation

Outcome before synchronization:

- Added `scripts/release-tag.sh` as the release entrypoint. It validates the
  current branch, enforces numeric-only versions on `develop`, allows SemVer
  prereleases on `main`, appends `-dev` on `develop`, and increments existing
  local tags as `vX.Y.Z-dev.1`, `vX.Y.Z-dev.2`, and so on.
- The script synchronizes `VERSION`, `backend/internal/version/version.go`
  and `frontend/package.json`, creates a release commit and annotated tag, and
  pushes the current branch plus tag to every configured remote.
- Updated `.github/workflows/release.yml` so tag pushes build the backend
  binary, package it, build and push the Docker image, and publish a GitHub
  Release with generated notes and the binary checksum archive.
- Updated `RELEASING.md` to point at the new release script and document the
  branch/tag policy.

Local verification:

- `C:\Program Files\Git\bin\bash.exe -n scripts/release-tag.sh`
- `C:\Program Files\Git\bin\bash.exe scripts/release-tag.sh --dry-run 0.0.1`
- `C:\Program Files\Git\bin\bash.exe scripts/release-tag.sh --dry-run 0.0.1-rc.1`
- `git tag v0.0.1-dev` followed by
  `C:\Program Files\Git\bin\bash.exe scripts/release-tag.sh --dry-run 0.0.1`
  to confirm the `v0.0.1-dev.1` suffix, then tag cleanup
- `git diff --check`

Synchronization and deployment evidence:

- `scripts/sync-intranet.ps1 -SkipLocalChecks` was attempted three times. The
  first two attempts failed during remote Docker build because the intranet
  root filesystem was full (`no space left on device`).
- To recover enough build space without deleting database backups, release
  archives, previous application sources, native kernel artifacts or native
  kernel source snapshots, removed stale `/data/zboard-next/candidates/*`,
  Docker BuildKit cache, unused Docker images, unused Docker volumes, and the
  reproducible native Zero musl build caches
  `/data/zboard-next/rustup-musl` and `/data/zboard-next/cargo-musl-cache`.
  The native kernel package directory remained at 19 MiB and
  `/data/zboard-next/native-kernel-builds` remained at 138 MiB.
- The third synchronization built image
  `v0.0.1-20260728T082909Z-intranet` and created the pre-switch database
  backup
  `/data/zboard-next/backups/20260728T082909Z/zboard-before-sync.sql`
  (84 KiB) plus source archive
  `/data/zboard-next/releases/20260728T082909Z/source.tar.gz` (692 KiB), but
  the new application failed health checks with
  `pre-release baseline schema is incomplete: found 30 of 33 required tables`.
- The failed source is preserved at
  `/data/zboard-next/app-failed-20260728T082909Z`. The previous source was
  restored to `/data/zboard-next/app`, and the previous healthy image
  `fa44cab95b26` was retagged as `zboard_next-zboard:latest` to restore the
  running service after the failed image had overwritten that tag.
- Post-rollback verification succeeded: `/api/v1/version` returned
  `v0.0.1-20260726T105144Z-intranet-working-tree@2026-07-26T10:51:44Z`,
  `/readyz` returned HTTP/API 200 with `ready=true` and `db=true`, and
  `zboard_next-zboard-1`, `zboard_next-mysql-1` and `zboard_next-redis-1`
  were healthy. Root filesystem free space was restored to 2.9 GiB (94% used).

Remaining gaps:

- The branch-aware release management change is implemented and locally
  verified, but it is not deployed to the intranet because synchronization
  failed against the current intranet database schema.
- The intranet database must be reconciled with the current baseline schema, or
  the migration adoption path must be repaired, before this working tree can be
  deployed.
- The release workflow has not been exercised against a live GitHub tag push,
  so GitHub Release creation and GHCR publication remain to be observed in CI.

### 2026-07-22 - Subscription templates, runtime logs and protocol editing

Outcome:

- Added operator-managed, preview-validated subscription export templates.
  Users can append `?template=<slug>` while the original
  `zboard.subscription/v1` JSON remains compatible.
- Added a runtime-log view that merges persisted protocol publishes, node
  kernel operations and background tasks without conflating them with audit
  logs.
- Added node-page editing for the multiplier owned by each protocol endpoint.
  Multiplier-only changes do not restart Zero.
- Protocol creation remains a validated linear wizard; editing can jump to any
  step and still performs full validation before save.

Local verification:

- `go test ./...`
- `go vet ./...`
- `pnpm build`
- `git diff --check`

Intranet synchronization: succeeded.

- Version: `v0.0.1-20260722T052513Z-intranet`
- Database backup:
  `/data/zboard-next/backups/20260722T052513Z/zboard-before-sync.sql`
- Previous application source:
  `/data/zboard-next/app-prev-20260722T052513Z`
- Source release archive:
  `/data/zboard-next/releases/20260722T052513Z/source.tar.gz`
- `0021_subscription_templates.up.sql` is recorded in `schema_migrations`, and
  the `subscription_templates` table exists in the intranet MySQL database.
- `/admin/operation-logs` and `/admin/subscription-templates` return the SPA;
  their APIs return `401` without a bearer token, confirming the routes exist
  without weakening authentication.
- Zboard, MySQL and Redis remained healthy. Both registered nodes resumed
  command polling and heartbeat responses with HTTP 200 after the application
  container switch. MySQL, Redis and Zero were not restarted.

Known gaps:

- Authenticated browser interaction was not repeated during this synchronization;
  route, migration, authentication-boundary and live node-activity checks were
  performed instead.

### 2026-07-22 - Admin frontend system and scalable data workbenches

Outcome:

- Replaced card-heavy admin resource lists with a shared, compact data-workbench
  pattern. Nodes, protocol endpoints, users, orders, subscriptions, tasks,
  audit logs, operation logs, traffic records, reconciliation, node groups,
  plans, templates and tickets now use comparable rows, explicit totals and
  server-backed pagination where the resource can grow.
- Added URL-backed filters, paging and selected-detail state so admin list
  workflows survive refresh and browser navigation.
- Established shared status badges with semantic icons, compact time badges,
  direct numeric quantity columns, reserved form help/error rows, persistent
  page errors, success toasts, reusable detail drawers, pagers and remote
  node/protocol endpoint lookup controls.
- Removed all-node, all-endpoint and all-user option loading from high-volume
  admin filters and editors. Node-group endpoint membership now hydrates and
  searches in bounded pages rather than rendering the entire endpoint estate.
- Added optional paged API contracts while retaining legacy array responses for
  existing consumers. Node and protocol list enrichment, node-group membership
  and plan group loading use batch queries instead of per-row queries.
- Dashboard readiness is now computed by authoritative aggregate counts and no
  longer downloads node and endpoint inventories. Traffic records and
  reconciliation are independently paginated, including a server-side
  issues-only reconciliation filter.
- Runtime and task output is normalized for line endings and Unicode, strips
  terminal control sequences, replaces non-printing controls, limits the
  initial display and keeps a copy action for the complete normalized text.
- Product choices confirmed for this system: status is icon plus text, counts
  remain numbers with context in headers, and time is formatted as compact
  labels with exact time available.

Local verification:

- `pnpm typecheck`
- `pnpm build` (Vue typecheck and Vite production build; 439 modules)
- `go test ./...`
- `go vet ./...` (through the synchronization preflight)
- `git diff --check`

Intranet synchronization:

- Synchronized the verified working tree with `scripts/sync-intranet.ps1`.
- Deployed version:
  `v0.0.1-20260722T094702Z-intranet-working-tree@2026-07-22T09:47:02Z`.
- Database backup:
  `/data/zboard-next/backups/20260722T094702Z/zboard-before-sync.sql`.
- Previous source:
  `/data/zboard-next/app-prev-20260722T094702Z`.
- Release archive:
  `/data/zboard-next/releases/20260722T094702Z/source.tar.gz`.
- `/api/v1/version` and `/readyz` returned HTTP 200; readiness reported
  `db: true` and `ready: true`.
- `zboard_next-zboard-1`, `zboard_next-redis-1` and
  `zboard_next-mysql-1` were healthy after synchronization. Only the Zboard
  application container was recreated; MySQL and Redis retained their uptime.
- Read-only authentication-boundary checks returned HTTP 401 for the paged
  node, protocol endpoint, node-group and traffic-record admin routes. The
  public plan route retained its legacy array response for unauthenticated
  callers by design.
- Added a separate `VITE_API_PROXY_TARGET` development setting so browser
  acceptance can use a same-origin `/api/v1` client while Vite forwards to an
  intranet API. This avoids falsely testing a cross-origin request path that
  production does not use.

Remaining gaps:

- The available browser sessions did not contain a valid current admin token.
  Both the deployed origin and the corrected same-origin development proxy
  redirected `/admin/nodes` to login. Authenticated post-deployment interaction,
  current mobile-overflow and accessibility acceptance therefore remain to be
  repeated after an operator signs in; no credential or browser storage was
  inspected or bypassed.
- The main node and protocol inventories are server-paginated for the target
  scale, but the node detail protocol tab intentionally loads at most 100
  endpoints, protocol parent selection is limited to the currently loaded
  endpoint candidates, and the plan editor loads at most 100 enabled node
  groups. These bounded secondary selectors need remote lookup or nested paging
  if those individual relationships grow beyond the current operational model.

### 2026-07-22 - Admin frontend contract hardening phase

Phase outcome:

- Added an explicit completion audit so implemented behavior and remaining
  system-level work are tracked route by route rather than inferred from a
  successful build or one representative page.
- Standardized paged admin responses on
  `{items,page,aggregates,facets}`. Temporary flat `total/offset/limit` aliases
  remain for compatibility, and the frontend normalizes both shapes.
- Added Vitest, Vue Test Utils and happy-dom coverage for formatting, output
  normalization, request generations, confirmation queuing, input/select/button
  contracts, FormField ARIA relationships and drawer keyboard/focus behavior.
- Node detail, node protocol rows and kernel data are now independently loaded
  on demand. The node protocol tab has its own 25/50/100 server pagination, and
  protocol parent selection performs bounded same-node remote lookup.
- Node list responses now use a summary DTO and omit node configuration, SSH
  target details and credential metadata. An authorized node-detail endpoint
  provides the non-secret operational fields required by the drawer.
- Protocol endpoint list responses now omit server/client/optional configuration,
  tags and deployment output/error bodies. The list retains only a bounded
  deployment status plus `has_error`, while full configuration remains in the
  authorized detail route.
- Subscription-template administration now uses server pagination, URL-backed
  search/status filters and summary rows; template source is fetched only when
  editing. Ticket pagination also uses the canonical response envelope.
- OpenAPI documents the canonical page envelope, node/protocol summaries,
  node/template details and ticket/template page schemas. Contract tests include
  a bounded 50-row first page for totals of 1000 nodes and 5000 endpoints, plus
  summary-response sensitive and large-field absence checks.

Local verification:

- `pnpm typecheck`
- `pnpm test -- --run` (10 files, 15 tests)
- `pnpm build` (443 modules)
- `go test ./...`
- `go vet ./...`
- `git diff --check`

Intranet synchronization: succeeded for this verified phase.

- Deployed version:
  `v0.0.1-20260722T104500Z-intranet-working-tree@2026-07-22T10:45:00Z`.
- Database backup:
  `/data/zboard-next/backups/20260722T104500Z/zboard-before-sync.sql`.
- Previous source:
  `/data/zboard-next/app-prev-20260722T104500Z`.
- Release archive:
  `/data/zboard-next/releases/20260722T104500Z/source.tar.gz`.
- `/api/v1/version` and `/readyz` returned HTTP 200; readiness reported
  `db: true` and `ready: true`.
- `zboard_next-zboard-1`, `zboard_next-mysql-1` and
  `zboard_next-redis-1` were healthy. Only the application container was
  recreated; MySQL and Redis retained three days of uptime.
- The new node detail, node/protocol/template paged lists, template detail and
  admin ticket routes all returned HTTP 401 without a bearer token. This
  confirms their deployed route and authorization boundary without using or
  exposing operator credentials.
- `/admin/nodes`, `/admin/protocol-endpoints` and
  `/admin/subscription-templates` returned the SPA with HTTP 200.

Remaining gaps:

- The overall admin frontend goal is not complete. Nodes/Protocols still need
  filter-scope batch tasks, task-tray final-state tracking, endpoint deployment
  history/detail presentation and the planned grouped protocol view.
- The 1000/5000 contract fixtures prove bounded response size but do not yet
  populate a real database or assert SQL query-count ceilings.
- `useRemoteTable`, dirty-form/error-map primitives, dedicated checkbox/domain
  inputs, removal of legacy Select/Button adapters and remaining old `.field`
  forms still require cross-page migration.
- A current authenticated administrator session is still required for the full
  15-route, multi-viewport, keyboard, ARIA, error-layer and history-navigation
  browser acceptance matrix.

### 2026-07-22 - Batch workbench and task-result phase

Phase outcome before synchronization:

- Added a dedicated `UiCheckbox` contract and selection slot for data
  workbenches. Nodes and protocol endpoints now support explicit cross-page
  selection and an all-matching scope that snapshots the current server-side
  filters rather than materializing every row in the browser.
- Added confirmed node batch operations for detection, kernel reconciliation
  and lifecycle changes, plus protocol endpoint batch publish, enable and
  disable operations. Every accepted operation is persisted as a Task and
  durable TaskItems before asynchronous execution starts.
- Protocol publish and activation tasks are collapsed to one TaskItem per
  affected node. Selected endpoint IDs remain snapshotted in the task scope;
  activation groups exact endpoint IDs by node, updates each group and
  publishes the node's complete configuration once. This avoids repeating a
  full node publish for every endpoint in a 5000-endpoint inventory.
- Operation TaskItems use four bounded workers across independent nodes. Node
  publish locking still serializes work within one node, and a five-minute
  heartbeat extends the 30-minute task claim while long work remains active.
- Added task summary item counts, `GET /api/v1/admin/tasks/{id}/items` with
  canonical pagination and status filtering, and summary-only task reads.
  The Tasks page now presents icon-plus-text status labels, direct numeric
  progress/failure columns, compact time labels and paginated target results.
- Added a session-backed global TaskTray. It tracks accepted task IDs, polls
  only unresolved tasks, stops after final state and distinguishes complete,
  failed and partially failed results without requiring the originating page
  to remain open.
- Replaced remaining naked account order/subscription/traffic list timestamps
  with `TimeBadge`, and replaced node kernel service, control-channel and
  operation-history text/dot states with semantic icon labels.
- OpenAPI now documents the new batch scope requests, grouped execution
  semantics, task summary counts and paginated TaskItem contract. Handler tests
  cover batch request decoding, scope exclusivity and target limits.

Local verification:

- `pnpm typecheck`
- `pnpm test -- --run` (12 files, 17 tests)
- `pnpm build` (449 modules)
- `go test ./...`
- `go vet ./...`
- `git diff --check`

Remaining gaps before synchronization:

- The isolated 1000-node/5000-endpoint database measurement now has a bounded
  query count, but the disposable validation workflow is not yet committed as
  a reusable repository script and is not a substitute for production traffic
  distribution or long-running workload measurements.
- Protocol endpoint detail/history paging, grouped node view, shared
  `useRemoteTable`, remaining legacy form adapters and the authenticated
  15-route browser acceptance matrix remain incomplete.
- Multi-administrator claim/retry, process interruption recovery and grouped
  publish partial-failure behavior need database-backed integration coverage.
- Authenticated interaction remains unavailable: both available browser
  surfaces redirected `/admin/nodes` to the login page. No credentials,
  browser storage or private tokens were inspected or bypassed.

Intranet synchronization: succeeded for this verified phase.

- Deployed version:
  `v0.0.1-20260722T111938Z-intranet-working-tree@2026-07-22T11:19:38Z`.
- Database backup:
  `/data/zboard-next/backups/20260722T111938Z/zboard-before-sync.sql`.
- Previous source:
  `/data/zboard-next/app-prev-20260722T111938Z`.
- Release archive:
  `/data/zboard-next/releases/20260722T111938Z/source.tar.gz`.
- `/api/v1/version` and `/readyz` returned HTTP 200; readiness reported
  `db: true` and `ready: true`.
- `zboard_next-zboard-1`, `zboard_next-mysql-1` and
  `zboard_next-redis-1` were healthy. Only the application container was
  recreated; MySQL and Redis retained three days of uptime.
- Anonymous requests to node batch operations, protocol batch publish,
  protocol batch activation and paginated TaskItems all returned HTTP 401,
  confirming the deployed route and admin boundary without exposing an
  operator credential.
- `/admin/nodes`, `/admin/protocols` and `/admin/tasks` returned the deployed
  SPA with HTTP 200. Browser navigation reached the deployment but redirected
  to login in both available browser surfaces, so authenticated table,
  selection, task-tray and drawer interaction remains to be repeated after an
  operator signs in.
- A disposable `docker-compose.validation.yml` project populated MySQL 8.4
  with exactly 1000 nodes and 5000 protocol endpoints. Authorized first-page
  requests returned 50 rows and the expected totals. MySQL general-log evidence
  counted prepared-statement Execute plus direct Query operations: the node
  page used 6 logical queries in 0.011 seconds and the protocol page used 9 in
  0.022 seconds, below the 12/15 ceilings. The project, containers, network and
  labelled data volumes were removed after verification; no validation
  credential or token was printed or retained.

### 2026-07-22 - Remote-table, form-contract and protocol-detail phase

Phase outcome before synchronization:

- Added `scripts/verify-scale-intranet.ps1`. It uploads a bounded validation
  script, creates a uniquely named Compose project outside `zboard_next`,
  generates temporary credentials without printing them, seeds exactly 1000
  nodes and 5000 protocol endpoints, asserts page totals/size and MySQL logical
  query ceilings, then removes the exact containers, network, volume, temporary
  files and default-built image. The reusable run returned node 1000/50/6 in
  0.014 seconds and protocol 5000/50/9 in 0.024 seconds, below 12/15 ceilings,
  with zero labelled resources left.
- Added the tested `useRemoteTable` composable and migrated Nodes and Protocols.
  It distinguishes initial loading from background refresh, retains existing
  rows while refreshing, ignores late responses, reports one shared error state
  and corrects an out-of-range page before reloading it. DataWorkbench exposes
  the refresh as an icon-plus-text status instead of replacing the table.
- Added a URL-restorable protocol detail drawer with numeric operational facts,
  semantic status labels, compact time tags and independently paged deployment
  history. Deployment error/output text is normalized and truncated before
  list display. A server-sorted node grouping view keeps the same bounded table
  page instead of introducing a second card collection.
- Migrated the Nodes and Protocols editors to `FormField`. Legacy `.field`
  layouts now reserve the same helper/error row even without help text, so peer
  inputs no longer shift based on whether a remark exists.
- Migrated every frontend checkbox and switch to `UiCheckbox` and removed the
  checkbox/toggle compatibility branches from `UiInput`.

Local verification:

- `pnpm test` (13 files, 20 tests)
- `pnpm build` (447 modules)
- `go test ./...`
- `go vet ./...`
- `git diff --check`

Remaining gaps before synchronization:

- Remaining admin lists still need migration to `useRemoteTable`, visible sort
  controls, request cancellation and a shared selection-scope primitive.
- Plans, Tasks, Setup, TicketCenter and a few public/account forms still use the
  legacy field markup even though its dimensions are now normalized. Structured
  domain inputs, field-error maps, first-error focus, dirty-close protection and
  revision-conflict handling remain incomplete.
- Select option VNode compatibility, Button class inference and legacy array
  response paths remain; high-volume history routes still need cursor/default
  time-window work and long-running production-distribution measurements.
- Batch claim/retry, process interruption and partial-failure recovery still
  need database integration coverage.
- Authenticated multi-route, multi-viewport, keyboard, ARIA and history browser
  acceptance still requires an operator-authenticated session; no credentials
  or browser storage will be inspected or bypassed.

Intranet synchronization: succeeded for this verified phase.

- Deployed version:
  `v0.0.1-20260722T120750Z-intranet-working-tree@2026-07-22T12:07:50Z`.
- Database backup:
  `/data/zboard-next/backups/20260722T120750Z/zboard-before-sync.sql`.
- Previous source:
  `/data/zboard-next/app-prev-20260722T120750Z`.
- Release archive:
  `/data/zboard-next/releases/20260722T120750Z/source.tar.gz`.
- `/api/v1/version` and `/readyz` returned HTTP 200; readiness reported
  `db: true` and `ready: true`.
- `zboard_next-zboard-1`, `zboard_next-mysql-1` and
  `zboard_next-redis-1` were healthy. Only the application container was
  recreated; MySQL and Redis retained three days of uptime.
- Anonymous protocol detail and deployment-history requests returned HTTP 401,
  confirming their deployed authorization boundary. `/admin/nodes`,
  `/admin/protocols` and `/admin/tasks` returned the SPA with HTTP 200.
- The reusable post-deployment scale script rebuilt an isolated validation
  image, returned node 1000/50/6 in 0.013 seconds and protocol 5000/50/9 in
  0.023 seconds, then verified containers=0, volumes=0, networks=0 and removed
  its image. The older disposable validation image created while developing the
  script was also removed explicitly; it is rebuildable and contained no data.
- Chrome's existing Zboard tab and a fresh in-app-browser navigation both
  reached the deployed login route. The fresh protocol navigation was correctly
  redirected to `/login?redirect=/admin/protocols`. No cookies, local storage,
  saved password, token or other browser storage was read, and no authentication
  bypass was attempted. Authenticated interaction remains a gap.

### 2026-07-22 - Cross-page form state, feedback and output-contract phase

Phase outcome before synchronization:

- Migrated the remaining major administration workbenches to the tested
  `useRemoteTable` contract. Users, Orders, Subscriptions, Tasks, Traffic,
  NodeGroups, Plans, SubscriptionTemplates, TicketCenter, OperationLogs and
  AuditLogs now share initial/background loading, stale-response rejection,
  out-of-range page correction and cancellation behavior with Nodes and
  Protocols. Remote node, endpoint and node-group lookups also cancel obsolete
  requests instead of allowing older searches to overwrite current results.
- Added shared `useDirtyForm` and `useFormErrors` primitives and connected modal
  mask, Escape, close-button and footer cancellation to one discard-confirmation
  path. Nodes, Protocols, NodeGroups, Plans, Users, Tasks,
  SubscriptionTemplates, Tickets, Settings and Setup now protect unsaved work;
  representative forms map field errors and focus the first invalid control.
- Removed remaining legacy `.field`, `<option>` VNode and raw checkbox paths
  from Vue templates. `FormField` reserves the same helper/error geometry for
  inputs with and without remarks, while `UiSelect`, `UiButton` and `UiCheckbox`
  use structured, explicit contracts.
- Settings now tracks site and per-row drafts independently, prevents route or
  browser-reload data loss and exposes HTTP 409 revision conflicts as a distinct
  reload action. Saving site metadata does not clear unrelated configuration
  drafts.
- Submission errors for Nodes, Plans, Tasks and Tickets are displayed inside
  their active modal or form. Successful requests continue to use transient
  Toast feedback, persistent/actionable errors remain prominent and destructive
  operations continue through queued confirmation dialogs.
- Normalized unknown enum values as `unknown label (raw value)` instead of
  rendering unexplained backend strings. Semantic states use icon-plus-text
  badges, quantities use direct numbers and times use compact `TimeBadge`
  labels. Kernel, audit, task and operation output goes through the shared
  Unicode/control-character/ANSI normalization path before display.
- Operation-log summaries no longer return output or error bodies. They expose
  only presence flags; a dedicated detail endpoint loads the complete normalized
  bodies on demand. Handler tests and OpenAPI assertions cover both halves of
  this response contract.
- Updated `docs/admin-frontend-completion-audit.md` so that its route matrix and
  cross-page gaps describe the current working tree instead of the previous
  Nodes/Protocols-only phase.

Local verification:

- `pnpm typecheck`
- `pnpm test -- --run` (15 files, 25 tests)
- `pnpm build` (450 modules; main application chunk 364.74 kB, shared UI chunk
  458.54 kB and xterm chunk 332.44 kB before gzip)
- `go test ./...` from `backend`
- `go vet ./...` from `backend`
- `git diff --check`

Remaining gaps before synchronization:

- Authenticated 15-route, multi-viewport, keyboard, ARIA, error-hierarchy and
  browser-history acceptance still requires an operator-authenticated session.
  No credential, saved password, cookie, local storage or private token will be
  inspected or bypassed.
- Operation/audit/traffic history still needs cursor pagination and a bounded
  default time range. The merged operation-log offset implementation can read
  `offset + limit` rows from each source and needs a cursor contract for very
  deep history.
- DataWorkbench still needs a shared column/sort definition, fixed-key-column
  and mobile-column strategy, plus a reusable cross-page selection-scope model.
  Server field errors, domain-specific inputs and schema-level validation also
  need a single end-to-end contract.
- Batch claim/retry, process interruption, partial-failure recovery and large
  node-group membership updates still need database-backed integration coverage.
  Production-distribution and long-running load measurements remain separate
  from the bounded 1000-node/5000-endpoint fixture.
- The shared UI bundle is 458.54 kB before gzip and should be evaluated for
  route-level component splitting after browser behavior is accepted.

Intranet synchronization: succeeded for this verified phase.

- Deployed version:
  `v0.0.1-20260722T132212Z-intranet-working-tree@2026-07-22T13:22:12Z`.
- Database backup:
  `/data/zboard-next/backups/20260722T132212Z/zboard-before-sync.sql`.
- Previous source:
  `/data/zboard-next/app-prev-20260722T132212Z`.
- Release archive:
  `/data/zboard-next/releases/20260722T132212Z/source.tar.gz`.
- The synchronization reran `go test ./...`, `go vet ./...` and the 450-module
  frontend production build before uploading. The application container alone
  was recreated; `zboard_next-zboard-1`, `zboard_next-mysql-1` and
  `zboard_next-redis-1` all reported healthy, while MySQL and Redis retained
  three days of uptime.
- Independent post-sync requests to `/api/v1/version` and `/readyz` returned
  HTTP 200; readiness reported `db: true` and `ready: true`. The deployed SPA
  returned HTTP 200 for `/admin/nodes`, `/admin/protocols` and
  `/admin/operation-logs`, each referencing `assets/index-BtPOGT3J.js`.
- Anonymous requests to the operation-log summary and detail endpoints returned
  HTTP 401. This confirms that both deployed routes are registered behind the
  administration boundary without reading or bypassing an operator credential.
- The reusable isolated scale validation created exactly 1000 nodes and 5000
  protocol endpoints. First-page requests returned 50 rows with the expected
  totals: nodes used 6 logical queries in 0.016 seconds and protocols used 9 in
  0.032 seconds, below the 12/15 ceilings. The validation containers, network,
  data volume, temporary script and build image were all removed; production
  containers were not part of the validation project.
- Authenticated browser behavior remains unverified because no valid operator
  session is available. No cookie, local storage, saved credential or private
  token was inspected or bypassed.

### 2026-07-22 - Sortable and responsive scale-workbench phase

Phase outcome before synchronization:

- Added the shared `DataTable` and `SortableHeader` contracts. Tables now expose
  a real caption, total row count, compact/comfortable density, bounded internal
  overflow and accessible `aria-sort`; sortable headers announce the next
  direction rather than relying on an unexplained icon.
- Added `tableState` URL parsing helpers and `useSelectionScope`. Invalid sort,
  direction and density query values fall back to page-declared defaults. Page
  selection, explicit cross-page IDs and the complete filtered result are
  represented separately, including indeterminate and “select all matching”
  states.
- Migrated Nodes and Protocols to the shared table layer. Node name, region and
  last heartbeat and protocol name, node, protocol and multiplier now issue
  server-side sort requests; sort field, direction and 40/48px density survive
  refresh and browser history. Changing sort resets the page and selection so a
  prior scope cannot be applied to a newly ordered result accidentally.
- Fixed selection, primary and operation columns while secondary columns carry
  explicit responsive priority. At 720px the scale workbenches retain the
  primary object, actionable state and operation column rather than compressing
  every column; horizontal overflow remains inside the table shell.
- Added control-height, table-row-height, reduced-motion and overlay-layer CSS
  tokens as the first mechanical step toward separating the still-monolithic
  global stylesheet.
- Documented the protocol endpoint sort/direction whitelist in OpenAPI and made
  `sort_order` explicit in the handler whitelist. The isolated 1000/5000 scale
  verifier now also asserts the first item from descending name sorts, proving
  that ordering is performed by the API and not by an all-data browser sort.

Local verification:

- `pnpm typecheck`
- `pnpm test -- --run` (19 files, 31 tests)
- `pnpm build` (456 modules; CSS 118.48 kB, application chunk 370.75 kB,
  shared UI chunk 458.54 kB and xterm chunk 332.44 kB before gzip)
- `go test ./...` from `backend`
- `go vet ./...` from `backend`
- `git diff --check`

Remaining gaps before synchronization:

- The remaining administration tables still use page-authored table markup;
  they need migration to shared column priority, sorting where the backend owns
  a whitelist, and a consistent row-operation contract. Column selection and a
  reusable row menu are not implemented.
- Design tokens still share `styles.css` with global layout and business CSS;
  token-file separation, hard-coded color cleanup and visual token snapshots
  remain incomplete.
- Cursor/default-window history, schema/domain form inputs, server field-error
  mapping, cross-resource `return_to` context and several detail workspaces remain
  open as recorded in `docs/admin-frontend-completion-audit.md`.
- Authenticated 15-route, four-viewport, keyboard, ARIA and browser-history
  acceptance still requires a valid operator session. No browser credential or
  storage will be inspected or bypassed.

Intranet synchronization: succeeded for this verified phase.

- Deployed version:
  `v0.0.1-20260722T134002Z-intranet-working-tree@2026-07-22T13:40:02Z`.
- Database backup:
  `/data/zboard-next/backups/20260722T134002Z/zboard-before-sync.sql`.
- Previous source:
  `/data/zboard-next/app-prev-20260722T134002Z`.
- Release archive:
  `/data/zboard-next/releases/20260722T134002Z/source.tar.gz`.
- Synchronization reran backend tests/vet and the 456-module frontend build.
  Only `zboard_next-zboard-1` was recreated; it, MySQL and Redis all reported
  healthy while the data services retained three days of uptime.
- Independent `/api/v1/version` and `/readyz` requests returned HTTP 200 with
  `db: true` and `ready: true`. Nodes and Protocols URLs containing sort,
  direction and comfortable-density state returned the deployed SPA and
  referenced `assets/index-DqgKd_Gu.js` plus `assets/index-MSHUJ5uI.css`.
- The isolated validator returned nodes 1000/50/6 in 0.012 seconds and protocols
  5000/50/9 in 0.024 seconds. Descending name sorts returned
  `scale-node-001000` and `scale-endpoint-0005000` as expected. Validation
  containers, volume, network, temporary script and image were removed, and a
  second resource check found zero leftovers.
- Authenticated rendering and input behavior remain a gap; route and asset
  delivery do not substitute for the still-missing real operator session.

### 2026-07-22 - Bounded bidirectional history phase

Phase outcome before synchronization:

- Added a versioned, bounded URL-safe history cursor carrying direction,
  timestamp, ID and (for the merged operation stream) source. Cursor payloads
  are length-bounded and strictly decoded; timestamps and IDs are required and
  operation sources are whitelisted.
- Audit records use stable `created_at, id` ordering, traffic records use
  `record_at, id`, and merged protocol publish/node kernel/task results preserve
  the global `created_at desc, source asc, id desc` order in both directions.
  “Older” and “newer” requests fetch only `limit + 1` candidates per source;
  the administration frontend no longer generates deep history offsets.
- Added inclusive date controls and bounded server windows. Operation and audit
  history default to the last 30 UTC days, traffic history defaults to 7 days,
  date-only `to` includes the selected day, and any range over 366 days is
  rejected. Composite time/ID indexes are migration `0022`.
- Added shared `useCursorTable`, `CursorPager` and URL date-state helpers.
  Traffic records, OperationLogs and AuditLogs retain filters, date range,
  limit and opaque cursor across refresh and browser history; controls use the
  explicit “newer records” and “older records” language. Traffic reconciliation
  remains an independent offset page.
- Extended PageMetadata and OpenAPI with `previous_cursor`, `cursor`, `from`
  and `to`. Offset remains an explicitly deprecated compatibility parameter;
  current history pages do not use it.
- Extended the isolated scale validator to seed 10,000 audit events, 10,000
  traffic events, and 5,000 records in each of the three operation sources. It
  asserts bounded first pages, query ceilings, no ID overlap between adjacent
  pages and exact newer-cursor round trips. Operation identity is the composite
  `source:id`, not the independently allocated numeric ID from one source.

Local verification:

- `pnpm typecheck`
- `pnpm test -- --run` (22 files, 36 tests)
- `pnpm build` (460 modules; CSS 118.48 kB, application chunk 375.86 kB,
  shared UI chunk 458.54 kB and xterm chunk 332.44 kB before gzip)
- `go test ./...` from `backend`
- `go vet ./...` from `backend`
- strict OpenAPI parsing and history cursor/window/ordering unit tests
- PowerShell AST parsing for `scripts/verify-scale-intranet.ps1`
- `git diff --check`

Runtime evidence before the final synchronization:

- Initial synchronization deployed
  `v0.0.1-20260722T140610Z-intranet-working-tree@2026-07-22T14:06:10Z` with
  backup `/data/zboard-next/backups/20260722T140610Z/zboard-before-sync.sql`,
  previous source `/data/zboard-next/app-prev-20260722T140610Z` and release
  `/data/zboard-next/releases/20260722T140610Z/source.tar.gz`.
- Independent checks returned version and ready HTTP 200, all three production
  containers healthy, migration `0022_history_cursor_indexes.up.sql` applied,
  and the two ordered columns of all five new indexes present.
- The clean isolated rerun returned nodes 1000/50/6 in 0.013 seconds,
  protocols 5000/50/9 in 0.044 seconds, audit 10000/50/4 in 0.021 seconds,
  traffic 10000/50/4 in 0.008 seconds and merged operations 15000/50/8 in
  0.027 seconds. All history streams had zero adjacent-page key overlap and
  exact previous-cursor round trips. Validation project resources were zero.
- Browser verification confirmed that `/admin/operation-logs` is reachable and
  redirects without authentication to `/login?redirect=/admin/operation-logs`.
  The existing Chrome page was blocked by an extension overlay, so an isolated
  browser confirmed the login form labels instead. No credential or browser
  storage was read and no login was attempted.

Remaining gaps before the final synchronization:

- The validator's operation overlap key was corrected from numeric ID to the
  actual `source:id` identity after the first synchronization; the verified
  script and updated evidence documents still require a final synchronization.
- Audit detail remains inline and lacks a dedicated on-demand detail contract;
  traffic aggregates and operation real-time follow remain incomplete.
- Remaining administration tables still need the shared DataTable/column
  priority layer, column selection and consistent row operations.
- Domain inputs, standardized server field errors, design-token separation,
  cross-resource return context and authenticated multi-route browser acceptance
  remain open in `docs/admin-frontend-completion-audit.md`.

Final intranet synchronization: succeeded for the bounded-history phase.

- Deployed version:
  `v0.0.1-20260722T141833Z-intranet-working-tree@2026-07-22T14:18:33Z`.
- Database backup:
  `/data/zboard-next/backups/20260722T141833Z/zboard-before-sync.sql`.
- Previous source:
  `/data/zboard-next/app-prev-20260722T141833Z`.
- Release archive:
  `/data/zboard-next/releases/20260722T141833Z/source.tar.gz`.
- Synchronization reran all backend tests/vet and the 460-module production
  frontend build. Only the application container was recreated; it, MySQL and
  Redis independently reported healthy and `/readyz` returned `db: true` and
  `ready: true`.
- Independent route requests for OperationLogs, AuditLogs and Traffic with
  explicit date state served `assets/index-DQDP6-Tt.js` and
  `assets/index-ILweG9f_.css`. The deployed validator contains the corrected
  `source_id` key mode.
- The authenticated visual and interactive history matrix remains unverified;
  a valid operator login is still required and was not inferred from the
  successful API, route-delivery or unauthenticated redirect evidence.

### 2026-07-22 - Whole-system shared table migration phase

Phase outcome before synchronization:

- Migrated every remaining administration table to the shared `DataTable`:
  Users, Orders, Subscriptions, Plans and SKU details, NodeGroups,
  SubscriptionTemplates, Tasks and TaskItems, Traffic and reconciliation,
  OperationLogs and AuditLogs. The three remaining account-side tables use the
  same component; no view now owns a raw `<table>` element.
- Each table declares an accessible caption, the full result count or bounded
  local row count, and an explicit minimum width. Primary identity and action
  columns remain reachable during horizontal scrolling, while non-essential
  columns declare priority 2/3 behavior at the shared 720/1100px breakpoints.
  Existing offset and cursor pagination behavior was not changed.
- Corrected scoped styles that targeted a class on the shared component's
  internal table. Table-specific descendant rules now use explicit `:deep()`
  selectors, while width is owned by the `DataTable` prop instead of dead page
  CSS. This also restored the intended Nodes/Protocols row and link styling.
- Replaced the SSH terminal's private colored-dot connection state with the
  shared semantic icon-plus-text `StatusBadge`. The static status/time/count
  review found no remaining direct backend status or timestamp rendering in
  table cells.
- Updated the completion audit and implementation status so shared-table
  migration is no longer listed as an open gap. Column presets and a unified
  multi-action row menu remain separate, selective enhancements rather than a
  reason to keep page-private table implementations.

Local verification:

- `pnpm test -- --run` (22 files, 36 tests)
- `pnpm typecheck`
- `pnpm build` (460 modules; CSS 117.48 kB, application chunk 380.55 kB,
  shared UI chunk 458.54 kB and xterm chunk 332.44 kB before gzip)
- source audit: only `components/DataTable.vue` contains a Vue `<table>` tag
- `git diff --check`

Remaining gaps before synchronization:

- A valid administrator session is still required for the authenticated
  15-route, multi-viewport, keyboard, ARIA, error-hierarchy and browser-history
  matrix. Anonymous route delivery cannot substitute for this evidence.
- Column presets and consolidated row menus are still candidates for the widest
  workbenches. Domain inputs, standardized server field errors, schema-level
  validation, design-token separation and hard-coded color cleanup remain open.
- Long-running production-distribution load, concurrent-write history paging,
  batch recovery database coverage and route-level shared UI splitting remain
  open engineering work.

Intranet synchronization: succeeded for the whole-system shared-table phase.

- Deployed version:
  `v0.0.1-20260722T143445Z-intranet-working-tree@2026-07-22T14:34:45Z`.
- Database backup:
  `/data/zboard-next/backups/20260722T143445Z/zboard-before-sync.sql`.
- Previous source:
  `/data/zboard-next/app-prev-20260722T143445Z`.
- Release archive:
  `/data/zboard-next/releases/20260722T143445Z/source.tar.gz`.
- Synchronization reran backend tests/vet and the 460-module frontend
  production build. The application container was recreated; it, MySQL and
  Redis independently reported healthy.
- Independent `/api/v1/version` and `/readyz` requests returned HTTP 200 with
  `db: true` and `ready: true`. `/admin/users`, `/admin/operation-logs` and
  `/account/orders` served `assets/index-k6N3mxnF.js` and
  `assets/index-DZqalrMj.css`.
- The deployed source contains zero raw view `<table>` tags and 18 shared
  `DataTable` instances. Direct source inspection also confirmed the SSH
  terminal renders its connection state through `StatusBadge`.
- The authenticated visual and interaction matrix remains unverified. The
  route and source checks prove deployment identity and contract presence, not
  an authenticated operator workflow.

### 2026-07-22 - Domain input normalization phase

Phase outcome before synchronization:

- Added `UiNumberInput` as the common PrimeVue numeric control and preserved
  FormField's label, required, `aria-describedby` and `aria-invalid` semantics
  on the real spinbutton rather than the component wrapper.
- Added semantic `MoneyInput`, `ByteSizeInput`, `MultiplierInput`, `PortInput`
  and `UiDateInput` components. Visible currency units convert to integer cents,
  MiB/GiB convert to integer bytes, readable multipliers convert to milli-units,
  and ports own the 1-65535 boundary.
- Migrated Plans/SKUs, node SSH and endpoint multipliers, protocol listen/public
  ports and sort order, task quota/attempt count, template sort order, operation
  IDs and all history dates. No Vue page now declares its own `type="number"`
  or `type="date"` input.
- Plan creation now keeps traffic as bytes throughout its form and API payload;
  the UI alone performs the GiB conversion. SKU editors no longer ask operators
  to enter raw cents or raw bytes. Node multiplier drafts now keep the endpoint-
  owned milli value rather than duplicating a page-specific divide/multiply
  conversion.
- Added component tests for real-input accessibility plus cents, bytes and
  milli conversion. The first isolated test run lacked the PrimeVue plugin;
  the corrected run uses the real component/plugin and verifies the actual DOM
  attributes instead of stubbing away the control.

Local verification:

- `pnpm typecheck`
- `pnpm test -- --run` (23 files, 39 tests)
- `pnpm build` (477 modules; CSS 117.42 kB, application chunk 384.31 kB,
  shared UI chunk 494.94 kB and xterm chunk 332.44 kB before gzip)
- source audit: zero page-owned `type="number"` or `type="date"` controls
- `git diff --check`

Remaining gaps before synchronization:

- The backend still returns free-form validation messages. A versioned field-
  error response and schema-level frontend validation are required before every
  server rejection can be mapped deterministically to its domain control.
- The added PrimeVue numeric control increased the shared UI chunk from
  458.54 kB to 494.94 kB before gzip. Route-level splitting remains an explicit
  optimization gap, not a reason to return to inconsistent native controls.
- Authenticated route/multi-viewport interaction acceptance, design-token
  separation, hard-coded color cleanup, long-running load and batch recovery
  integration coverage remain open.

Intranet synchronization: succeeded for the domain-input phase.

- Deployed version:
  `v0.0.1-20260722T144709Z-intranet-working-tree@2026-07-22T14:47:09Z`.
- Database backup:
  `/data/zboard-next/backups/20260722T144709Z/zboard-before-sync.sql`.
- Previous source:
  `/data/zboard-next/app-prev-20260722T144709Z`.
- Release archive:
  `/data/zboard-next/releases/20260722T144709Z/source.tar.gz`.
- Synchronization reran backend tests/vet and the 477-module frontend build.
  The application, MySQL and Redis containers independently reported healthy;
  `/api/v1/version` and `/readyz` returned HTTP 200 with `db: true` and
  `ready: true`.
- `/admin/plans`, `/admin/protocols` and `/admin/operation-logs` served
  `assets/index-ezGlYpyQ.js` and `assets/index-CjBXnhXx.css`. Deployed-source
  inspection found zero page-owned number/date inputs, all six new domain
  components, and the MoneyInput/ByteSizeInput plan-editor integration.
- Authenticated keyboard, formatted editing and error-focus behavior still need
  a valid operator session; deployment-source presence does not replace that
  interaction evidence.

### 2026-07-22 - Versioned field-error contract foundation

Phase outcome before synchronization:

- Extended every HTTP error response with a backward-compatible v1 `error`
  object while retaining the existing top-level code, message, data and
  timestamp. Generic errors receive stable codes such as `invalid_request`,
  `unauthenticated`, `conflict` and `internal_error`; field validation uses
  `validation_failed` plus a field-to-message map.
- Added typed backend validation failures and structured field maps for setup,
  site settings, registration, login completeness, and administration user
  create/update. Setup validation reports all invalid site/admin fields in one
  response instead of only the first free-form string.
- Documented `ApiErrorDetail` in OpenAPI and added handler/OpenAPI tests for
  version, code, fields, legacy message compatibility and the multi-field setup
  result.
- Added frontend `normalizeApiFormError` and `useFormErrors.applyApiError`.
  Consumers explicitly whitelist and map field names; malformed keys, unknown
  fields, blank values and overlong messages are discarded. Non-localized
  legacy messages remain behind a page-owned fallback.
- Setup, Settings, Login, Register and both Users editors now put server field
  errors on the corresponding FormField and focus the first invalid control.
  The Users page no longer maintains a private English-message translation
  table.

Local verification:

- `go test ./...` and `go vet ./...` from `backend`
- strict OpenAPI YAML parsing and schema assertions
- `pnpm typecheck`
- `pnpm test -- --run` (24 files, 42 tests)
- `pnpm build` (478 modules; CSS 117.42 kB, application chunk 385.23 kB,
  shared UI chunk 494.94 kB and xterm chunk 332.44 kB before gzip)
- `git diff --check`

Remaining gaps before synchronization:

- Nodes, Protocols, NodeGroups, Plans/SKUs, Tasks and SubscriptionTemplates
  still need endpoint-specific field maps and frontend FormField bindings. The
  generic v1 envelope already covers their errors without breaking legacy
  clients, but it cannot infer a field from old free-form validation text.
- Authenticated validation-focus/error-layer browser acceptance, design-token
  separation, hard-coded color cleanup, route-level shared UI splitting,
  production-distribution load and batch recovery integration remain open.

Intranet synchronization: succeeded for the field-error contract foundation.

- Deployed version:
  `v0.0.1-20260722T150335Z-intranet-working-tree@2026-07-22T15:03:35Z`.
- Database backup:
  `/data/zboard-next/backups/20260722T150335Z/zboard-before-sync.sql`.
- Previous source:
  `/data/zboard-next/app-prev-20260722T150335Z`.
- Release archive:
  `/data/zboard-next/releases/20260722T150335Z/source.tar.gz`.
- Synchronization reran all backend tests/vet and the 478-module frontend build;
  the application, MySQL and Redis independently reported healthy. Version and
  readiness returned HTTP 200 with `db: true` and `ready: true`.
- An independent credential-free empty login request returned HTTP 400 semantics
  with `error.version: 1`, `code: validation_failed`, and actionable `email`
  plus `password` fields. An anonymous protected Users request returned
  `error.version: 1` and `code: unauthenticated` while retaining its legacy
  message.
- `/admin/users` served `assets/index-DbEEKy2T.js` and
  `assets/index-DT2WmcB3.css`; deployed-source inspection confirmed both Users
  editors call the shared field-error mapper.
- Actual field focus and screen-reader announcement after a server rejection
  remain part of the authenticated browser matrix.

### 2026-07-22 - Plan and SKU field-error integration phase

Phase outcome before synchronization:

- Replaced the Plan/SKU validators' free-form English failures with typed v1
  field errors. Plan creation reports product identity, node group and policy
  fields; SKU validation accumulates code, name, type, billing, currency,
  price, traffic, device and speed failures instead of stopping at the first
  invalid value.
- Preserved nested ownership during product creation by returning SKU fields as
  `skus.<index>.<field>`. The frontend explicitly maps the first SKU into its
  local editor while keeping traffic on the shared Plan/SKU traffic control.
- Plan creation and SKU editing now render server errors on the corresponding
  FormField and focus the first invalid control. Errors clear when that field
  changes and the persistent form alert retains cross-field context.
- Expanded the SKU editor to include type, billing unit, billing value and
  currency, removing the previous create-only/edit-hidden split. Disabling the
  final active SKU of a published product now returns an actionable sales-state
  field error.
- Added backend tests that assert multi-field SKU validation, nested field
  prefixing and specific Plan policy fields.

Local verification:

- `go test ./...` and `go vet ./...` from `backend`
- `pnpm typecheck`
- `pnpm test -- --run` (24 files, 42 tests)
- `pnpm build` (478 modules; CSS 117.42 kB, application chunk 389.85 kB,
  shared UI chunk 494.94 kB and xterm chunk 332.44 kB before gzip)

Remaining gaps before synchronization:

- Nodes, Protocols, NodeGroups, Tasks and SubscriptionTemplates still require
  endpoint-specific backend field maps and frontend bindings.
- Plan/SKU field placement, first-error focus, checkbox error announcement and
  dirty close behavior still require the authenticated browser matrix.
- Frontend schema validation, design-token separation, hard-coded color cleanup,
  route-level shared UI splitting, production-distribution load and batch
  recovery integration remain open.

Intranet synchronization: succeeded for the Plan/SKU field-error phase.

- Deployed version:
  `v0.0.1-20260722T151958Z-intranet-working-tree@2026-07-22T15:19:58Z`.
- Database backup:
  `/data/zboard-next/backups/20260722T151958Z/zboard-before-sync.sql`.
- Previous source:
  `/data/zboard-next/app-prev-20260722T151958Z`.
- Release archive:
  `/data/zboard-next/releases/20260722T151958Z/source.tar.gz`.
- Synchronization reran backend tests/vet and the 478-module frontend build.
  Independent checks returned HTTP 200 for version and readiness with
  `db: true` and `ready: true`; the application, MySQL and Redis containers
  independently reported healthy.
- `/admin/plans` served `assets/index-CHhLTnP3.js` and
  `assets/index-CumJpmEg.css`. Deployed-source inspection confirmed indexed SKU
  validation paths and both Plan creation and SKU edit consumers of the shared
  API error mapper.
- A live Plan/SKU write rejection was not generated because no authenticated
  administrator session is available and deployment does not authorize reading
  stored credentials. The helper/envelope tests and deployed-source checks are
  implementation evidence; actual field placement and focus remain in the
  authenticated browser matrix.

### 2026-07-22 - Node and protocol field-error integration phase

Phase outcome before synchronization:

- Converted node creation, asset updates and SSH/privilege validation from
  free-form English strings to versioned field errors. SSH validation can report
  host, port, user and the authentication-specific credential together; private
  key and privilege failures no longer expose parser text as operator copy.
- Connected the Nodes create/edit/SSH forms to the shared field mapper, stable
  FormField IDs, inline error messages and first-error focus. The enabled state
  and SSH privilege password now have real error-bearing controls rather than
  detached checkbox or page-only messages.
- Converted protocol endpoint validation for node, protocol, name, address,
  listen/public ports, multiplier, parent relationship and server/client/
  optional/tags JSON into typed fields. JSON validation accumulates server and
  client failures rather than stopping at the first string.
- Migrated the protocol wizard's local validation to the same field model. A
  server rejection moves back to the relevant wizard step; advanced JSON errors
  open the advanced section before focus.
- Restored the endpoint-owned multiplier input in the protocol editor. Endpoint
  edits now preserve the existing active state instead of unconditionally
  sending `is_active: true`, so metadata edits cannot silently re-enable a
  disabled service.
- Added backend tests for multi-field SSH and protocol JSON validation.

Local verification:

- `go test ./...` and `go vet ./...` from `backend`
- `pnpm typecheck`
- `pnpm test -- --run` (24 files, 42 tests)
- `pnpm build` (478 modules; CSS 117.22 kB, application chunk 395.36 kB,
  shared UI chunk 494.94 kB and xterm chunk 332.44 kB before gzip)
- `git diff --check`

Remaining gaps before synchronization:

- NodeGroups, Tasks and SubscriptionTemplates remain outside the endpoint-
  specific field-error migration.
- Node/Protocol field placement, wizard step recovery, advanced-section opening,
  checkbox announcement and SSH failures need the authenticated browser matrix.
- Design-token separation, hard-coded color cleanup, route-level shared UI
  splitting, production-distribution load and batch recovery integration remain
  open.

Intranet synchronization: succeeded for the Node/Protocol field-error phase.

- Deployed version:
  `v0.0.1-20260722T153743Z-intranet-working-tree@2026-07-22T15:37:43Z`.
- Database backup:
  `/data/zboard-next/backups/20260722T153743Z/zboard-before-sync.sql`.
- Previous source:
  `/data/zboard-next/app-prev-20260722T153743Z`.
- Release archive:
  `/data/zboard-next/releases/20260722T153743Z/source.tar.gz`.
- Synchronization reran backend tests/vet and the 478-module frontend build.
  Independent version and readiness checks returned HTTP 200 with `db: true`
  and `ready: true`; the application, MySQL and Redis containers independently
  reported healthy.
- `/admin/nodes` and `/admin/protocols` both served
  `assets/index-CXyHED--.js` and `assets/index-Dynpf3Mg.css`. Deployed-source
  inspection confirmed the SSH and protocol field mappers, endpoint-owned
  multiplier payload and active-state preservation, plus typed protocol JSON
  validation.
- Live authenticated node/SSH/protocol rejection and focus behavior remain
  unverified because no administrator browser session is available. Stored
  credentials were not accessed for acceptance testing.

### 2026-07-22 - Node group, task and subscription-template field-error closure

Phase outcome before synchronization:

- Converted NodeGroup create/update validation from free-form strings into v1
  field errors for name, code, enabled-state constraints and protocol endpoint
  membership. Enabled groups must retain at least one active endpoint; missing
  or disabled endpoint IDs now identify the member picker instead of exposing a
  database-style message.
- Connected the NodeGroups editor to the shared error mapper and first-error
  focus. The remote endpoint picker now exposes a programmatically focusable
  error-bearing group, while the enabled checkbox is owned by a FormField.
- Converted task type, scope, priority, retry, quota and email validation into
  stable fields. Quota and email validation can report multiple content fields
  together; empty or oversized target scopes identify the scope control.
- Tightened the Tasks ID-list input so mixed valid/invalid tokens are rejected
  rather than silently dropping the invalid values. Server fields map to the
  currently visible quota, email and scope controls and focus the first error.
- Converted SubscriptionTemplate identity, content type, template syntax,
  sample rendering and rendered-size failures to stable field errors. Parser
  and execution internals are no longer copied directly into operator-facing
  messages, and duplicate slugs identify the slug control.
- Added backend assertions for multi-field task content, multi-field template
  identity and template-body ownership. Updated the completion audit so these
  editors are no longer listed as outside the main field-error contract.

Local verification:

- `go test ./...` and `go vet ./...` from `backend`
- `pnpm typecheck`
- `pnpm test -- --run` (24 files, 42 tests)
- `pnpm build` (478 modules; CSS 117.22 kB, application chunk 397.60 kB,
  shared UI chunk 494.94 kB and xterm chunk 332.44 kB before gzip)
- `git diff --check`

Remaining gaps before synchronization:

- Field placement, first-error focus, endpoint-picker group announcement,
  template-body errors and task scope recovery still require the authenticated
  browser matrix.
- Browser-side schema validation, design-token separation, hard-coded color
  cleanup, route-level shared UI splitting, production-distribution load and
  batch recovery integration remain open.

Intranet synchronization: succeeded for the NodeGroup/Task/template field-error
closure.

- Deployed version:
  `v0.0.1-20260722T155607Z-intranet-working-tree@2026-07-22T15:56:07Z`.
- Database backup:
  `/data/zboard-next/backups/20260722T155607Z/zboard-before-sync.sql`.
- Previous source:
  `/data/zboard-next/app-prev-20260722T155607Z`.
- Release archive:
  `/data/zboard-next/releases/20260722T155607Z/source.tar.gz`.
- Synchronization reran all backend tests/vet and the 478-module frontend build.
  Independent checks returned HTTP 200 for version and readiness with
  `db: true` and `ready: true`; the application, MySQL and Redis containers all
  independently reported healthy.
- `/admin/node-groups`, `/admin/tasks` and `/admin/subscription-templates` all
  served `assets/index-Bx_a2lZ2.js` and `assets/index-BDY6fYQ5.css`.
  Deployed-source inspection confirmed the endpoint-member FormField, nested
  task-content field mapping, template-body field mapping and the corresponding
  backend typed validators.
- Authenticated write rejections were not generated because no administrator
  session is available and stored credentials were not accessed. Actual field
  placement, focus and assistive announcement remain in the browser matrix.

### 2026-07-22 - Design-token ownership phase

Phase outcome before synchronization:

- Extracted typography, surface, text, border, semantic status, navigation,
  terminal, elevation, geometry and z-index values from the mixed global sheet
  into `frontend/src/theme/tokens.css`. `styles.css` now imports the token source
  instead of declaring a second root palette.
- Replaced 275 hexadecimal color occurrences across 17 frontend source files,
  plus all remaining RGB/RGBA literals outside the token source, with semantic
  custom properties. Status borders, code labels, selected rows, sidebar,
  public pages, setup, ticket messages and page-authored styles now share the
  same vocabulary.
- Bridged PrimeVue surface tokens to the CSS token source. The xterm canvas
  theme resolves terminal colors from computed CSS variables when it opens,
  removing its private JavaScript palette while retaining valid canvas colors.
- Added `designTokens.test.ts` to reject raw color literals outside the token
  source and to detect undeclared CSS custom-property references. Dynamic
  metric column count remains the only explicitly runtime-owned property.
- Browser baseline inspection confirmed the previously deployed login page
  visual and accessible structure plus the public pricing-page semantic tree.
  This is a pre-deployment comparison baseline, not evidence for the new token
  bundle; post-sync inspection remains required.

Local verification:

- `go test ./...` and `go vet ./...` from `backend`
- `pnpm typecheck`
- `pnpm test -- --run` (25 files, 44 tests)
- `pnpm build` (478 modules; CSS 124.16 kB, application chunk 397.94 kB,
  shared UI chunk 494.94 kB and xterm chunk 332.44 kB before gzip)
- raw-color audit: 0 hex and 0 RGB/HSL literals outside `tokens.css`
- `git diff --check`

Remaining gaps before synchronization:

- The global stylesheet still combines administration, public and account
  shell/layout rules; tokens are separated, but those business-layout sections
  need ownership-based files.
- Authenticated full-route visual comparison, keyboard/ARIA/history acceptance,
  browser-side schema validation, production-distribution load, batch recovery
  integration and route-level shared UI splitting remain open.

Intranet synchronization: succeeded for the design-token ownership phase.

- Deployed version:
  `v0.0.1-20260722T162203Z-intranet-working-tree@2026-07-22T16:22:03Z`.
- Database backup:
  `/data/zboard-next/backups/20260722T162203Z/zboard-before-sync.sql`.
- Previous source:
  `/data/zboard-next/app-prev-20260722T162203Z`.
- Release archive:
  `/data/zboard-next/releases/20260722T162203Z/source.tar.gz`.
- Synchronization reran all backend tests/vet and the 478-module frontend build.
  Independent version and readiness checks returned HTTP 200 with `db: true`
  and `ready: true`; the application, MySQL and Redis containers independently
  reported healthy.
- Login, pricing and the Nodes SPA route served `assets/index-CODkGTtZ.js` and
  `assets/index-DiftXBfr.css`. Deployed-source checks confirmed the token import,
  raw-color ownership guard, PrimeVue surface bridge and xterm computed-token
  resolver.
- Post-deployment browser checks confirmed the login and pricing semantic trees,
  the deployed version in the public footer, computed primary/surface values,
  a 40px control token, PrimeVue-rendered input geometry, plan-card border and
  button colors, and protected Nodes redirect preservation.
- No authenticated administrator session was available, so admin-route visual
  comparison, field-error focus and assistive announcements remain unverified.

### 2026-07-22 - Frontend shell-style ownership phase

Phase outcome before synchronization:

- Split login, public-site and customer-account desktop/responsive rules from
  the mixed global sheet into `frontend/src/styles/auth.css`, `public.css` and
  `account.css`. `main.ts` imports the base and three owner files explicitly in
  cascade order.
- Kept shared primitives, the administration shell, tables, feedback, dialogs
  and the cross-surface usage track in `styles.css`. The account usage styling
  no longer relies on a late global override that could silently affect the
  public preview.
- Added a stylesheet-ownership assertion to `designTokens.test.ts`. It rejects
  authentication, public and account shell selectors in the base sheet and
  rejects cross-owner shell selectors in each extracted file.
- Preserved the effective production CSS footprint: the split build emits a
  124.13 kB stylesheet versus 124.16 kB before the ownership split.

Local verification:

- `go test ./...` and `go vet ./...` from `backend`
- `pnpm typecheck`
- `pnpm test -- --run` (25 files, 45 tests)
- `pnpm build` (481 modules; CSS 124.13 kB, application chunk 397.94 kB,
  shared UI chunk 494.94 kB and xterm chunk 332.44 kB before gzip)
- `git diff --check`

Remaining gaps before synchronization:

- The new bundle still needs post-deployment browser comparison at desktop and
  narrow widths. The connected browser has no administrator session, so the
  authenticated 15-route keyboard/ARIA/history and field-error matrix remains
  open.
- Browser-side schema validation, production-distribution load, batch recovery
  integration and large frontend-chunk evaluation remain open.

Intranet synchronization: succeeded for the frontend shell-style ownership
phase.

- Deployed version:
  `v0.0.1-20260722T164001Z-intranet-working-tree@2026-07-22T16:40:01Z`.
- Database backup:
  `/data/zboard-next/backups/20260722T164001Z/zboard-before-sync.sql` (73,680
  bytes in the independent check).
- Previous source:
  `/data/zboard-next/app-prev-20260722T164001Z`.
- Release archive:
  `/data/zboard-next/releases/20260722T164001Z/source.tar.gz`.
- Synchronization reran backend tests/vet and the 481-module frontend build.
  Independent version and readiness requests returned HTTP 200 with `db: true`
  and `ready: true`; the application, MySQL and Redis containers all reported
  healthy. Login, pricing and Nodes served `assets/index-BOWFzynx.css` and
  `assets/index-C_qJOMr3.js`.
- Deployed-source inspection confirmed all three owner imports and the
  page-shell ownership test. Independent artifact checks confirmed the database
  backup, previous-source directory and release archive.
- A valid administrator browser session became available after deployment. The
  desktop matrix traversed all 15 administration routes and found zero page
  overflow, duplicate IDs, unnamed buttons, unlabeled native inputs, private
  tables, shared tables without captions or status labels without icons.
- The Nodes interaction reached the persisted URL
  `/admin/nodes?q=Docker&sort=name&direction=asc`, proving filter and server-sort
  state encoding. The browser control channel timed out during back/forward and
  could not be safely reclaimed for deeper drawer/focus checks; this is not
  counted as history acceptance.
- Login and public pricing shells were checked at 1024, 768 and 390 pixels. The
  login shell changed from two columns to one, public navigation changed to the
  mobile control, the plan grid reduced to one column at 390 pixels, and no
  page-level horizontal overflow, console error or native JavaScript dialog was
  present. The temporary viewport override was reset after inspection.

Remaining gaps after synchronization:

- Administration routes still need their 1024/768/390 interaction pass,
  detail-drawer keyboard/focus cycle, field-error assistive-technology checks
  and complete URL back/forward verification in a stable administrator browser
  control session.
- Browser-side schema validation, production-distribution load, batch recovery
  integration and large frontend-chunk evaluation remain open.

### 2026-07-22 - Shared browser-side form-validation phase

Phase outcome before synchronization:

- Added `frontend/src/utils/validation.ts` as the shared browser validation
  vocabulary for UTF-8 byte length, Unicode character length, email, safe
  HTTP(S) URL, slug, enum and bounded-integer checks. The checks match each
  backend rule's byte-versus-character semantics instead of relying on DOM
  `maxlength` alone.
- Added `useFormErrors.applyValidation` so local pre-request errors and versioned
  API errors share the same field state, form alert and first-invalid-control
  focus path. Field watchers continue to clear the corresponding error as the
  operator edits it.
- Applied pre-request validation to authentication, setup, site settings,
  Users, Nodes and SSH, Protocols, NodeGroups, Plans and SKUs, Tasks,
  SubscriptionTemplates, and ticket creation/replies. Values owned by the
  browser are normalized before submission; database uniqueness, Go-template
  execution, credential parsing and live-resource constraints remain final
  server validations.
- SSH now distinguishes an existing credential from the credential type it was
  saved under. Switching password/private-key authentication requires the new
  credential before a request, and the field label/required state reflects that
  requirement.
- Every current Vue `<form>` declares `novalidate`, preventing native browser
  validation bubbles from bypassing application feedback. A repository policy
  test recursively rejects future forms that omit this contract.
- Added focused validation and form-policy tests, including multibyte UTF-8,
  Unicode emoji, unsafe URLs, enum/integer coercion, local-error focus and the
  all-forms source-tree invariant.

Local verification:

- `go test ./...` and `go vet ./...` from `backend`
- `pnpm typecheck`
- `pnpm test -- --run` (27 files, 51 tests)
- `pnpm build` (482 modules; CSS 124.13 kB, application chunk 406.19 kB,
  shared UI chunk 494.94 kB and xterm chunk 332.44 kB before gzip)
- `git diff --check`

Remaining gaps before synchronization:

- The deployed bundle and real administrator session still need proof that
  invalid submission sends no API request, focuses the first control, clears
  corrected fields and exposes the error relationship to assistive technology.
- The administration 1024/768/390 interaction matrix, complete URL
  back/forward run, production-distribution load, batch recovery integration
  and large frontend-chunk evaluation remain open.
- Dynamic operational settings still use per-value runtime validation rather
  than a fully declarative browser schema; server and revision-conflict checks
  remain authoritative.

Intranet synchronization: succeeded for the shared browser-side
form-validation phase.

- Deployed version:
  `v0.0.1-20260722T171925Z-intranet-working-tree@2026-07-22T17:19:25Z`.
- Database backup:
  `/data/zboard-next/backups/20260722T171925Z/zboard-before-sync.sql` (73,680
  bytes in the independent check).
- Previous source:
  `/data/zboard-next/app-prev-20260722T171925Z`.
- Release archive:
  `/data/zboard-next/releases/20260722T171925Z/source.tar.gz` (60,519,732
  bytes in the independent check).
- Synchronization reran all backend tests/vet and the 482-module frontend
  build. Independent `/api/v1/version` and `/readyz` requests returned HTTP
  200 with `db: true` and `ready: true`; the application, MySQL and Redis
  containers all reported healthy.
- Login, Nodes, Tasks and SubscriptionTemplates served
  `assets/index-BkYXvOEM.js` and `assets/index-CuVeg7O6.css`. Deployed-source
  inspection confirmed `utf8ByteLength`, `useFormErrors.applyValidation`, the
  form-policy test and an exact 18 forms/18 `novalidate` match.
- A real deployed login submission with both fields empty produced both inline
  field errors plus the prominent `登录未完成` alert, focused the email field,
  and set `aria-invalid="true"` with `aria-describedby` on both controls. CDP
  observed zero network requests after the submit action, the URL remained
  `/login`, and no native JavaScript dialog appeared.

Remaining gaps after synchronization:

- The authenticated administration forms still need browser proof for
  field-error clearing and assistive announcements. The 1024/768/390
  administration interaction matrix, detail-drawer keyboard/focus cycle and
  complete URL back/forward run also remain open.
- Production-distribution load, concurrent-write cursor behavior, batch
  recovery integration and large frontend-chunk evaluation remain open.
- Dynamic operational settings still use per-value runtime validation rather
  than a fully declarative browser schema; server and revision-conflict checks
  remain authoritative.

### 2026-07-22 - Route-level frontend delivery phase

Phase outcome before synchronization:

- Replaced every eager page and shell import in the router with route-level
  dynamic imports: 26 views and three layouts now load only when their route is
  entered. The redirect and authorization contracts are unchanged.
- Added a source policy test that rejects static `views`/`layouts` imports in
  the router and locks the complete 26/3 lazy-module inventory, so a new page
  cannot silently return to the monolithic entry bundle.
- The production build reduced the entry application chunk from 406.19 kB to
  90.65 kB and the base stylesheet from 124.13 kB to 52.90 kB. Nodes and
  Protocols are now independent 55.35/44.47 kB JavaScript plus 19.94/10.93 kB
  CSS route payloads; xterm remains a separate on-demand terminal chunk.
- The 494.94 kB shared UI chunk is unchanged and gzip-compresses to 103.04 kB.
  Inspection attributes this primarily to the complete Aura theme preset and
  shared PrimeVue runtime. It is now isolated from all route business modules;
  narrowing the preset remains conditional on real first-load evidence rather
  than splitting components only to reduce the raw chunk count.

Local verification:

- `go test ./...` and `go vet ./...` from `backend`
- `pnpm typecheck`
- `pnpm test -- --run` (28 files, 52 tests)
- `pnpm build` (482 modules; 90.65 kB entry application chunk, 52.90 kB base
  CSS, 494.94 kB shared UI chunk and 332.44 kB on-demand xterm chunk before
  gzip)
- `git diff --check`

Remaining gaps before synchronization:

- The deployed login, Nodes and Protocols routes still need network proof that
  route chunks are fetched on demand and unrelated administrator pages are not
  part of the public/login entry path.
- The authenticated 1024/768/390 administration interaction matrix,
  detail-drawer keyboard/focus cycle, field-error clearing/announcements and
  complete URL back/forward run remain open.
- Production-distribution load, concurrent-write cursor behavior and batch
  recovery database integration remain open.

Intranet synchronization: succeeded for the route-level frontend delivery
phase.

- Deployed version:
  `v0.0.1-20260722T173558Z-intranet-working-tree@2026-07-22T17:35:58Z`.
- Database backup:
  `/data/zboard-next/backups/20260722T173558Z/zboard-before-sync.sql` (73,680
  bytes in the independent check).
- Previous source:
  `/data/zboard-next/app-prev-20260722T173558Z`.
- Release archive:
  `/data/zboard-next/releases/20260722T173558Z/source.tar.gz` (60,519,637
  bytes in the independent check).
- Synchronization reran backend tests/vet and the 482-module split frontend
  build. Independent `/api/v1/version` and `/readyz` requests returned HTTP
  200 with `db: true` and `ready: true`; the application, MySQL and Redis
  containers all reported healthy.
- Deployed source contains the route-loading policy and exactly 26 view plus
  three layout dynamic imports. The deployed `/login` HTML references only
  `index-0BuPESPE.js`, `vue-vendor-bLPWvTRN.js`, `ui-CMEuVC06.js` and
  `index-Bhr7JwBW.css`, with no Login, Nodes or Protocols route asset.
- Independent HTTP HEAD checks returned 200 for the entry/base assets and the
  separate Login, Nodes and Protocols JS/CSS assets. The entry bundle contains
  dynamic references to all three route scripts but not xterm; the Nodes route
  bundle contains the xterm and fit-addon dynamic references.

Remaining gaps after synchronization:

- A browser navigation trace and first-load timing should still quantify the
  shared UI transfer under an empty cache. No evidence currently justifies
  replacing the unified Aura preset with a partial private preset.
- The authenticated 1024/768/390 administration interaction matrix,
  detail-drawer keyboard/focus cycle, field-error clearing/announcements and
  complete URL back/forward run remain open.
- Production-distribution load, concurrent-write cursor behavior and batch
  recovery database integration remain open.

### 2026-07-22 - Isolated task-recovery integration phase

Phase outcome before synchronization:

- Extended `scripts/verify-scale-intranet.ps1` with destructive fault
  simulation confined to its disposable MySQL/Redis/application Compose
  project. It never mutates the production database and retains the existing
  container, volume, network and image cleanup assertions.
- The quota scenario creates two task items, records both quota effects, then
  simulates a crash with one item completed and one still running. A live task
  lock rejects the run with HTTP 409 without incrementing attempts; an expired
  lock is claimed with HTTP 202, preserves the completed item at one attempt,
  retries the interrupted item, and finishes the task at attempt two.
- Quota recovery observes the existing task-item reference events, leaving
  both subscriptions at exactly one 1 MiB adjustment and exactly two events.
  This covers the crash window between a committed business transaction and
  the later task-item status update.
- A separate three-node lifecycle task simulates completed, running and failed
  items whose node state was already committed. Expired-lock recovery uses the
  operation-worker path, finishes all items with attempt counts `1/2/2`, keeps
  all three nodes in maintenance and creates zero duplicate audit rows.
- The operation-history expected total now includes both recovery fixtures, so
  the same run continues to prove bidirectional cursor behavior after the new
  task records are present.

Local/integration verification:

- PowerShell parser check for `scripts/verify-scale-intranet.ps1`
- `git diff --check -- scripts/verify-scale-intranet.ps1`
- Disposable MySQL 8.4 run with 5 nodes, 10 endpoints, 10 audit rows, 10
  traffic rows and five records per operation source:
  - quota recovery: active lock 409, stale lock 202, task attempts 2,
    completed/interrupted item attempts 1/2, two quota events, duplicate delta
    zero;
  - node batch recovery: stale lock 202, task attempts 2, item
    status/attempts `2:1,2:2,2:2`, three applied nodes, three audit rows and
    duplicate audit zero;
  - list query counts remained 6/9, history query counts 4/4/8, all cursor
    roundtrips passed and isolated resources were fully removed.

Remaining gaps before synchronization:

- Maximum-target task recovery, simultaneous claims by multiple administrators
  and external SMTP delivery semantics are not covered by this deterministic
  database integration run.
- Production-distribution load and concurrent-write cursor behavior remain
  open, as do the authenticated responsive/keyboard/history browser matrices.

Intranet synchronization: succeeded for the isolated task-recovery integration
phase.

- Deployed version:
  `v0.0.1-20260722T174920Z-intranet-working-tree@2026-07-22T17:49:20Z`.
- Database backup:
  `/data/zboard-next/backups/20260722T174920Z/zboard-before-sync.sql` (73,680
  bytes in the independent check).
- Previous source:
  `/data/zboard-next/app-prev-20260722T174920Z`.
- Release archive:
  `/data/zboard-next/releases/20260722T174920Z/source.tar.gz` (60,521,905
  bytes in the independent check).
- Synchronization reran all backend tests/vet and the 482-module frontend
  build. Independent `/api/v1/version` and `/readyz` requests returned HTTP
  200 with `db: true` and `ready: true`; the application, MySQL and Redis
  containers all reported healthy.
- Deployed-source inspection confirmed both quota and node batch recovery
  scenarios in `scripts/verify-scale-intranet.ps1`.
- The goal-specific isolated run was repeated after synchronization against
  the deployed source. It again returned 409 for an active lock and 202 for
  both expired-lock recoveries; quota attempts/events remained `2`, completed
  and recovered item attempts remained `1/2`, node item status/attempts remained
  `2:1,2:2,2:2`, and duplicate quota delta/audit counts remained zero.
- The post-sync run also retained 6/9 list and 4/4/8 history query counts,
  passed all cursor roundtrips, and removed every disposable container, volume,
  network and image.

Remaining gaps after synchronization:

- Maximum-target task recovery, simultaneous claims by multiple administrators
  and external SMTP delivery semantics remain unverified.
- Production-distribution load and concurrent-write cursor behavior remain
  open, as do the authenticated responsive/keyboard/history browser matrices
  and empty-cache browser transfer timing.

### 2026-07-23 - Concurrent audit-cursor write phase

Phase outcome before synchronization:

- Extended `scripts/verify-scale-intranet.ps1` so the isolated audit-history
  run inserts a new audit row after fetching the first cursor page and before
  consuming that page's `next_cursor`.
- The verifier now proves that the previously issued cursor continues from
  its original boundary without overlapping the first page or admitting the
  newly inserted row. A fresh first-page request must report the incremented
  total and return that inserted row first.
- The scenario remains confined to the disposable Compose project and retains
  the existing container, volume, network and image cleanup assertions.

Local/integration verification before synchronization:

- PowerShell parser check for `scripts/verify-scale-intranet.ps1`.
- Focused disposable MySQL 8.4 run with 5 nodes, 10 endpoints, 10 audit rows,
  10 traffic rows and five records per operation source. The first attempt
  stopped before application startup on a transient Docker Hub Alpine metadata
  EOF and invoked the cleanup trap; the immediate repeat completed.
- The completed run inserted audit row 22 between pages, excluded it from the
  old cursor page, returned it first on a fresh page, retained zero overlap and
  exact previous-cursor roundtrip behavior, and kept history query counts at
  4/4/8. Task recovery, node recovery, list query ceilings, sorting and full
  resource cleanup also passed unchanged.

Remaining gaps before synchronization:

- Production-distribution and long-duration cursor load remain unverified.
- The authenticated administration 1024/768/390 matrix, drawer keyboard/focus
  cycle, field-error clearing/announcements, complete URL back/forward run and
  empty-cache transfer timing remain open. A current-run Chrome audit produced
  no screenshot evidence because both the existing-tab claim and a fresh-tab
  page-debug session timed out; no visual conclusion was inferred from that
  failed session.
- Maximum-target task recovery, simultaneous claims by multiple administrators
  and external SMTP delivery semantics remain unverified.

Intranet synchronization: succeeded for the concurrent audit-cursor write
phase.

- Deployed version:
  `v0.0.1-20260722T180352Z-intranet-working-tree@2026-07-22T18:03:52Z`.
- Database backup:
  `/data/zboard-next/backups/20260722T180352Z/zboard-before-sync.sql` (73,680
  bytes in the independent check).
- Previous source:
  `/data/zboard-next/app-prev-20260722T180352Z`.
- Release archive:
  `/data/zboard-next/releases/20260722T180352Z/source.tar.gz` (60,522,759
  bytes in the independent check).
- Synchronization reran all backend tests and vet plus the 482-module frontend
  production build. Independent version and `/readyz` requests returned HTTP
  200 with `db: true` and `ready: true`; the application, MySQL and Redis
  containers independently reported running and healthy.
- Deployed-source inspection found the new concurrent-write assertion in
  `scripts/verify-scale-intranet.ps1`.
- The focused disposable run was repeated after synchronization from the
  deployed source. It again inserted audit row 22 between pages, excluded it
  from the old cursor page, exposed it first on a fresh page, preserved exact
  previous-cursor roundtrip and zero page overlap, retained history query
  counts 4/4/8, and removed all disposable resources. Existing task recovery,
  node recovery, list query ceilings and sort assertions passed unchanged.

Remaining gaps after synchronization:

- Production-distribution and long-duration cursor load remain unverified.
- The authenticated administration 1024/768/390 matrix, drawer keyboard/focus
  cycle, field-error clearing/announcements, complete URL back/forward run and
  empty-cache transfer timing remain open because the current Chrome control
  session could not produce current-run screenshot or DOM evidence.
- Maximum-target task recovery, simultaneous claims by multiple administrators
  and external SMTP delivery semantics remain unverified.

### 2026-07-23 - Multi-administrator task-claim phase

Phase outcome before synchronization:

- Extended `scripts/verify-scale-intranet.ps1` with a second isolated
  administrator identity and a two-request race against the same pending quota
  task.
- The verifier requires exactly one HTTP 202 and one HTTP 409, then proves the
  task and its single item each attempted once, the quota mutation and event
  occurred once, only one `task.run` audit row was committed, and the task lock
  was cleared after completion.
- The operation-history expected total now includes the additional race task;
  the existing recovery, scale-list, cursor and cleanup checks remain in the
  same disposable Compose run.

Local/integration verification before synchronization:

- PowerShell parser and focused whitespace checks passed for
  `scripts/verify-scale-intranet.ps1`.
- A disposable MySQL 8.4 run with 5 nodes, 10 endpoints, 10 audit rows, 10
  traffic rows and five records per operation source returned the required
  202/409 claim pair. Task/item attempts, quota events and run audits were all
  one, with duplicate execution zero.
- Existing quota and node recovery assertions passed, list query counts stayed
  6/9, history query counts stayed 4/4/8, concurrent audit-cursor paging and all
  cursor roundtrips passed, and every disposable resource was removed.

Current browser-audit evidence:

- Chrome exposed the Zboard login tab but could not be controlled while another
  extension UI was open on that page. The in-app browser reached
  `/admin/nodes` and was redirected to `/login?redirect=/admin/nodes`.
- The current-run 1280x720 login screenshot was saved and inspected; it is only
  evidence of the authentication barrier. No administration-page visual or
  accessibility conclusion was inferred from it.

Remaining gaps before synchronization:

- The authenticated administration 1024/768/390 matrix, drawer keyboard/focus
  cycle, field-error clearing/announcements, complete URL back/forward run and
  empty-cache transfer timing still require a controllable signed-in browser.
- Maximum-target task recovery and external SMTP delivery semantics remain
  unverified. Production-distribution and long-duration cursor load also remain
  open.

Intranet synchronization: succeeded for the multi-administrator task-claim
phase.

- Deployed version:
  `v0.0.1-20260722T181815Z-intranet-working-tree@2026-07-22T18:18:15Z`.
- Database backup:
  `/data/zboard-next/backups/20260722T181815Z/zboard-before-sync.sql` (73,680
  bytes in the independent check).
- Previous source:
  `/data/zboard-next/app-prev-20260722T181815Z`.
- Release archive:
  `/data/zboard-next/releases/20260722T181815Z/source.tar.gz` (60,525,386
  bytes in the independent check).
- Synchronization reran all backend tests and vet plus the 482-module frontend
  production build. Independent version and `/readyz` requests returned HTTP
  200 with `db: true` and `ready: true`; the application, MySQL and Redis
  containers independently reported running and healthy.
- Deployed-source inspection found the multi-administrator race assertion in
  `scripts/verify-scale-intranet.ps1`.
- The focused disposable run was repeated after synchronization from the
  deployed source. It again returned the exact 202/409 pair with task/item
  attempts, quota events and `task.run` audits all one and duplicate execution
  zero. Existing recovery, 6/9 list-query, 4/4/8 history-query, concurrent
  audit-cursor, sort, cursor-roundtrip and full-cleanup assertions also passed.

Remaining gaps after synchronization:

- The authenticated administration 1024/768/390 matrix, drawer keyboard/focus
  cycle, field-error clearing/announcements, complete URL back/forward run and
  empty-cache transfer timing still require a controllable signed-in browser.
- Maximum-target task recovery and external SMTP delivery semantics remain
  unverified. Production-distribution and long-duration cursor load also remain
  open.

### 2026-07-23 - Maximum-target task-recovery phase

Phase outcome before synchronization:

- Extended `scripts/verify-scale-intranet.ps1` with a bounded
  `TaskTargetCount` parameter whose default and hard ceiling match the backend
  contract of 10,000 targets.
- The isolated fixture creates exactly that many active subscriptions for the
  second administrator, creates the quota task through the public admin API,
  and simulates a crash after business results were committed for 20 items:
  ten task items are already completed and ten remain running under an expired
  worker lock.
- Recovery is observed from the database rather than by repeatedly fetching a
  10,000-item detail response. Final assertions require every item completed,
  completed items still attempted once, interrupted items attempted twice,
  exactly one quota event and one adjustment per target, one `task.run` audit,
  a cleared lock and zero duplicate execution.
- The operation-history expected total now includes the additional maximum
  target task; all fixtures remain confined to the disposable Compose project.

Local/integration verification before synchronization:

- PowerShell AST parsing and focused whitespace checks passed for
  `scripts/verify-scale-intranet.ps1`.
- A 20-target smoke run completed the recovery in 367 ms: ten items retained
  one attempt, ten recovered items reached two attempts, and all 20 events and
  subscription adjustments were unique and complete.
- The full backend-limit run created and recovered 10,000 targets in 86,525 ms.
  It finished with task attempts `2`, item-attempt counts `9990/10` for one/two
  attempts, 10,000 completed items, 10,000 distinct quota-event references,
  10,000 adjusted subscriptions, one run audit and duplicate execution zero.
- Existing active/stale-lock quota recovery, two-administrator claim race,
  node lifecycle recovery, 6/9 list-query counts, 4/4/8 history-query counts,
  sorting, concurrent audit-cursor paging, cursor roundtrips and complete
  container/volume/network/image cleanup all passed unchanged.

Remaining gaps before synchronization:

- External SMTP delivery side effects still lack an integration fixture.
- Production-distribution and long-duration cursor load remain unverified.
- The authenticated administration 1024/768/390 matrix, drawer keyboard/focus
  cycle, field-error clearing/announcements, complete URL back/forward run and
  empty-cache transfer timing still require a controllable signed-in browser.

Intranet synchronization: succeeded for the maximum-target task-recovery
phase.

- Deployed version:
  `v0.0.1-20260722T183245Z-intranet-working-tree@2026-07-22T18:32:45Z`.
- Database backup:
  `/data/zboard-next/backups/20260722T183245Z/zboard-before-sync.sql` (73,680
  bytes in the independent check).
- Previous source:
  `/data/zboard-next/app-prev-20260722T183245Z`.
- Release archive:
  `/data/zboard-next/releases/20260722T183245Z/source.tar.gz` (60,528,704
  bytes in the independent check).
- Synchronization reran all backend tests and vet plus the 482-module frontend
  production build. Independent version and `/readyz` requests returned HTTP
  200 with `db: true` and `ready: true`; the application, MySQL and Redis
  containers independently reported running and healthy.
- Deployed-source inspection found the maximum-target recovery assertion in
  `scripts/verify-scale-intranet.ps1`.
- The full 10,000-target disposable run was repeated after synchronization.
  Recovery completed in 85,441 ms with task attempts `2`, item-attempt counts
  `9990/10`, 10,000 completed items, 10,000 distinct quota events, 10,000
  adjusted subscriptions, one run audit and duplicate execution zero.
- Existing active/stale-lock recovery, exact 202/409 administrator claim race,
  node recovery, list/history query ceilings, sorting, concurrent cursor write,
  cursor roundtrips and full disposable-resource cleanup passed unchanged.

Remaining gaps after synchronization:

- External SMTP delivery side effects still lack an integration fixture.
- Production-distribution and long-duration cursor load remain unverified.
- The authenticated administration 1024/768/390 matrix, drawer keyboard/focus
  cycle, field-error clearing/announcements, complete URL back/forward run and
  empty-cache transfer timing still require a controllable signed-in browser.

### 2026-07-23 - Authenticated admin responsive and keyboard acceptance phase

Phase outcome before synchronization:

- Extended `scripts/verify-scale-intranet.ps1` with explicit
  `KeepEnvironment` and exact-project `CleanupOnly` modes so a disposable
  authenticated dataset can be retained for browser acceptance and later
  destroyed without changing the default full-cleanup behavior. The retained
  credential is written only to a mode-600 remote temporary file and the local
  system temporary directory; its value is never logged.
- Created an isolated browser dataset with 60 nodes, 200 protocol endpoints,
  50 audit records, 50 traffic records and 20 records per operation source.
  Existing task recovery, exact 202/409 administrator claim, node recovery,
  6/9 list queries, 4/4/8 history queries, concurrent cursor insert and cursor
  roundtrips passed in that same run.
- In the authenticated in-app browser, all 15 administration routes passed at
  1024x768, 768x900 and 390x844: all 45 states rendered the expected heading,
  had no page-level horizontal overflow, no residual dialog and no page error.
  Large tables retained horizontal overflow only inside the shared table
  shell.
- Real screenshots found that desktop filter flex-basis values became huge
  vertical gaps on mobile. The shared workbench now resets that basis in the
  column layout, and mobile tables use content width with a 100% minimum.
  Node count and rows are reachable in the first mobile viewport.
- Protocol rows below 560px now retain one primary View action and move Edit,
  failure-log and Publish actions into the detail drawer. This prevented the
  action column from overlapping the 200px primary service column while
  keeping secondary work available through progressive disclosure.
- The shared detail drawer is now constrained to viewport height with an
  independently scrolling body. Tab and Shift+Tab wrapped between the first
  and last control, Escape closed the drawer, and focus returned to the exact
  protocol row trigger after URL synchronization and the leave transition.
  Browser Back removed `?endpoint=1` and closed the drawer; Forward restored
  the same endpoint and drawer.
- An authenticated empty Node-create submission kept the modal open, rendered
  the persistent form summary, associated the name error through
  `aria-invalid=true` and `aria-describedby`, and focused that field. No
  native JavaScript dialog appeared. The modal close-button accessible name
  is now explicitly localized as `关闭弹窗`.
- Current-run accepted screenshots are in
  `C:/Users/higanbana/.codex/visualizations/2026/07/22/019f88a4-a545-7821-84be-0f338ebf5661/zboard-admin-audit-2026-07-23-run3`.
  The primary evidence files are `07-nodes-fixed-table-390x844.png`,
  `11-protocols-progressive-actions-390x844.png`,
  `14-protocol-drawer-fixed-390x844.png`,
  `15-node-create-validation-390x844.png`,
  `16-tasks-390x844.png`, `17-dashboard-1024x768.png` and
  `18-protocols-1024x768.png`.

Local and browser verification before synchronization:

- `pnpm test`: 28 files and 53 tests passed.
- `pnpm build`: Vue typecheck and the 482-module production build passed.
- PowerShell AST parsing passed for `scripts/verify-scale-intranet.ps1`.
- The browser route matrix passed 15/15 at each of the desktop, tablet and
  mobile target sizes with zero page-level horizontal overflow.
- Protocol detail focus trap, Escape, focus restoration and URL Back/Forward
  were exercised with real keyboard/history operations; Node-create validation
  was exercised with a real authenticated submission.

Remaining gaps before synchronization:

- The current restricted execution context cannot read the user SSH
  configuration, so the `gitlab` alias did not resolve and the first remote
  `CleanupOnly` attempt did not run. The local temporary credential file was
  deleted and verified absent, but disposable remote project
  `zboard_scale_validation_browser_audit_20260723` and the pre-existing local
  tunnel process on port 18089 still require cleanup from an SSH-capable
  context.
- A read-only check after the failed cleanup returned HTTP 200 from both
  production `/api/v1/version` and `/readyz`; production remains on
  `v0.0.1-20260722T183245Z-intranet-working-tree@2026-07-22T18:32:45Z` with
  `db: true` and `ready: true`. The disposable `/readyz` through port 18089
  also returned HTTP 200 with `db: true` and `ready: true`, proving that the
  retained environment and tunnel are still live rather than cleaned.
- Intranet synchronization and deployed behavior verification for this phase
  have not run. This phase is not considered deployed.
- Empty-cache transfer timing, SSH failure behavior, the complete
  field-error-clearing/assistive-announcement matrix, external SMTP side
  effects and production-distribution/long-duration load remain open.

### 2026-07-23 - Semantic value and corrected-error acceptance phase

Phase outcome before synchronization:

- Confirmed the current presentation rule as a system contract rather than a
  page-specific treatment: statuses use a semantic icon plus label, quantities
  remain directly readable numbers, and temporal values use the shared compact
  time label with an exact-time fallback.
- `StatusBadge` already owns the icon-plus-label contract for all tones.
  Added focused automation proving the default semantic icon and visible label.
  `TimeBadge` automation now proves the standard `datetime`, exact title and
  accessible exact-time label while preventing raw API timestamp output.
- The dashboard refresh marker now stores an ISO timestamp and renders through
  `TimeBadge`. Account subscription expiry moved from a concatenated
  `daysRemaining` string into the same relative time label. Node status-overview
  descriptions can host a time label without creating another private
  formatter path.
- A production-build visual pass found the complete `正常` status label being
  clipped by node actions inside the 720-pixel detail drawer. Node identity and
  actions now occupy separate rows in the drawer, and the title line wraps.
  The complete status remained visible at the default 1270x714 browser size
  and at 390x844.
- Real form correction exposed a shared lifecycle defect: after the final field
  error was corrected, the persistent form summary could remain visible.
  `useFormErrors.clear()` now clears the summary only after the last field error
  disappears. Node single-field, User two-field and Task nested-field
  corrections passed in the authenticated browser; the summary correctly
  remains while another field is invalid.
- Accepted current-run screenshots are stored locally under
  `.codex-local-artifacts/zboard-admin-audit-2026-07-23-run4`:
  `10-dashboard-semantic-tags-prod-1270x714.png`,
  `12-nodes-status-label-unclipped-prod-1270x714.png`,
  `13-account-time-tags-prod-1270x714.png`,
  `14-nodes-status-label-prod-390x844.png` and
  `15-account-semantic-values-prod-390x844.png`. Blank/loading captures and a
  browser full-page stitching anomaly were inspected but rejected as evidence.

Local and browser verification before synchronization:

- `pnpm typecheck` passed.
- The standard Vitest config loader remained blocked by the restricted
  execution context trying to read above the workspace. A temporary
  config-free Vitest entry using the repository's Vue plugin ran the same suite:
  29 files and 56 tests passed. The temporary entry was deleted.
- A temporary config-free Vite entry produced the normal 482-module production
  build and was deleted. Gzip sizes were 29.50 KiB for the entry, 59.74 KiB for
  the Vue shared chunk, 87.28 KiB for the UI shared chunk and 10.86 KiB for
  base CSS. Nodes added 18.91 KiB JavaScript and 4.53 KiB CSS; Protocols added
  15.67 KiB and 2.13 KiB. The 84.18 KiB xterm chunk remains lazy.
- The retained authenticated browser session loaded that production build,
  rendered authoritative 60-node/200-endpoint data and exposed no console
  warnings or errors. Dashboard direct numbers, icon status labels and the
  relative refresh label were visible. Node list numbers/status/time semantics,
  the unclipped drawer state and the account relative expiry labels were
  confirmed from DOM snapshots and inspected screenshots.
- Raw CDP empty-cache collection was rejected by the browser security policy.
  No workaround was attempted; build artifact sizes above are explicitly not
  represented as real network transfer timing.
- `scripts/verify-scale-intranet.ps1` now gives each local script directory a
  UTC suffix, validates its system-temp boundary, removes it with
  `Directory.Delete`, and preserves the original scp/ssh failure if best-effort
  remote-script cleanup also fails. PowerShell AST parsing passed. An
  unreachable-target run reported the authoritative upload failure and left
  zero matching temporary directories and no local credential file.

Intranet cleanup and synchronization: not completed for this phase.

- Exact-project cleanup was retried for
  `zboard_scale_validation_browser_audit_20260723`. The restricted context still
  cannot resolve the `gitlab` SSH alias, so no remote cleanup command ran.
  The protected local SSH tunnel process on port 18089 also still cannot be
  stopped from this context.
- `scripts/sync-intranet.ps1 -SkipLocalChecks` was attempted after the verified
  frontend suite and production build. Source upload failed because the same
  SSH alias did not resolve, so no candidate, backup, previous-source directory,
  release archive or deployed version was created for this phase.
- Independent HTTP checks after the failed attempt returned 200 from production
  `/api/v1/version` and `/readyz`. Production remains
  `v0.0.1-20260722T183245Z-intranet-working-tree@2026-07-22T18:32:45Z` with
  `db: true` and `ready: true`. The disposable environment `/readyz` through
  local port 18089 also remains 200 with `db: true` and `ready: true`, which is
  evidence that remote cleanup is still outstanding. Container health could
  not be independently rechecked without SSH.

Remaining gaps:

- Restore an SSH-capable context, run exact-project cleanup, stop the protected
  tunnel, then rerun intranet synchronization and verify the new version,
  `/readyz`, all three container health states and goal-specific UI behavior.
- Complete remaining high-risk form announcement coverage and the real SSH
  failure flow. Empty-cache network timing still needs a user-allowed browser
  capability; current evidence is limited to the labeled build budget.
- External SMTP side effects and production-distribution/long-duration cursor
  load remain open.

### 2026-07-23 - Business detail and dense mobile table phase

Phase outcome before synchronization:

- Added bounded administration detail contracts for Users, Subscriptions and
  Orders. Each page now opens detail on demand, records the selected resource
  in the URL, cancels stale detail requests and restores focus to the exact row
  trigger after closing.
- Replaced repeated inline row buttons on those three dense business lists with
  the shared keyboard-operable `RowActionMenu`. Its teleported popover is not
  clipped by the table shell, supports arrow navigation and Escape, closes on
  outside interaction or viewport movement, and returns focus to its trigger.
- User detail returns account facts plus bounded subscription/order counts.
  Subscription detail returns entitlement, reset and credential-count facts.
  Order list/detail responses use explicit safe DTOs: payment callback bodies
  are absent from both responses, and provider/failure diagnostics are excluded
  from the list. Detail failure text is normalized and length-limited before
  display.
- Manual order payment/cancellation remains behind `ConfirmDialog`. After a
  successful mutation, the list and any open detail are refreshed together and
  the global success Toast announces the result.
- Shared workbench filters again use compact bounded widths on desktop and
  intentionally stack at 480 pixels. Users, Subscriptions and Orders preserve
  their object, complete icon-plus-label status and action columns at 390
  pixels. The final User-specific width correction truncates a long email
  inside the primary cell instead of letting it cover the status or sticky
  action column.
- Accepted local screenshots are under
  `.codex-local-artifacts/zboard-admin-audit-2026-07-23-run5`. The final mobile
  list/detail evidence is
  `14-orders-final-after-local-390x844.jpg` through
  `19-user-detail-final-after-local-390x844.jpg`. Intermediate clipped-status
  and blank captures were inspected and rejected rather than used as evidence.

Local and browser verification before synchronization:

- `go test ./...` passed for every backend package using a workspace-local Go
  cache; `go vet ./...` also passed.
- A config-free Vitest entry ran the repository suite because the restricted
  environment cannot load the normal config from above the workspace:
  30 files and 57 tests passed. The temporary entry was deleted.
- `pnpm typecheck` passed. A config-free Vite entry with the repository's normal
  manual chunk policy transformed 484 modules and passed; the temporary entry
  was deleted. Gzip sizes were 32.05 KiB for the entry, 42.42 KiB for the Vue
  shared chunk, 103.04 KiB for the UI shared chunk and 11.33 KiB for base CSS.
  Nodes remained 18.91/4.53 KiB JavaScript/CSS, Protocols 15.67/2.13 KiB and
  xterm remained an 84.18 KiB lazy chunk.
- `git diff --check` passed; its only output was the existing Windows line
  ending advisory.
- Authenticated desktop interaction verified User detail URL/focus recovery,
  row-menu arrow navigation/Escape/focus recovery, Subscription and Order
  detail URLs, and confirmed-order list/detail/Toast synchronization. At
  390x844, all three pages had no page-level horizontal overflow, exposed the
  object/status/action columns, and kept each detail drawer within the
  viewport. Final browser warnings and errors were empty.

Remaining gaps before synchronization:

- Plans still mixes detail and editing; SubscriptionTemplates lacks an
  editor-grade line/preview/revision-conflict workflow; Tickets still needs a
  fully verified split workspace; AuditLogs lacks safe on-demand detail.
- Cross-resource links still lack a common `return_to` contract, so returning
  from related Users/Subscriptions/Orders resources does not yet reconstruct
  every source filter, page and open detail.
- Production-distribution and long-duration list baselines, real SSH failure,
  remaining form announcement coverage, external SMTP effects and empty-cache
  browser transfer timing remain open.
- Intranet synchronization and deployed behavior verification have not yet run
  for this phase, so this phase is not deployed.

Intranet synchronization outcome:

- `scripts/sync-intranet.ps1 -SkipLocalChecks` was attempted after all local
  verification. The restricted context still could not resolve the `gitlab`
  SSH alias, and source upload failed before any remote candidate, database
  backup, previous-source path, archive, version or container change was
  created. This phase remains not deployed.
- Independent production requests after the failed synchronization returned
  HTTP 200 from `/api/v1/version` and `/readyz`. Production is unchanged at
  `v0.0.1-20260722T183245Z-intranet-working-tree@2026-07-22T18:32:45Z`;
  readiness reports `db: true` and `ready: true`.
- The retained disposable environment also returned HTTP 200 from `/readyz`
  through `127.0.0.1:18089` with `db: true` and `ready: true`. `netstat`
  confirms the protected tunnel is still listening as PID 8868, so exact
  project cleanup remains outstanding.
- Container health, a new deployed version and goal-specific deployed UI/API
  behavior could not be verified because no synchronization occurred and the
  SSH boundary remains unavailable.

### 2026-07-23 - Subscription template preview and revision phase

Phase outcome before synchronization:

- Added a shared template code editor with a synchronized line-number gutter,
  keyboard Tab insertion, exact error-line highlighting and ARIA forwarding to
  the actual source control. Long source and preview panes remain side by side
  at desktop width and stack without page overflow at 390 pixels.
- Added a no-save preview endpoint that parses and renders through the same
  backend template engine used by save validation. Responses contain normalized
  content type, bytes, line count and a bounded UTF-8-safe preview rather than
  raw parser diagnostics or unbounded output.
- Template parse and execution failures now return a stable, actionable field
  message with a safe line number. The editor focuses the source, marks the
  exact gutter line and keeps the error associated through `aria-invalid` and
  `aria-describedby`; correcting the source clears the invalid state and marks
  any existing preview stale.
- Added a persisted `revision` to subscription templates and migration
  `0023_subscription_template_revision`. Updates carry `expected_revision`,
  take a row lock, increment the revision atomically and return HTTP 409 when a
  stale editor attempts to overwrite newer work. The UI disables save after a
  conflict and exposes an explicit reload-latest-version action.
- Template create/edit state is represented by `?template=new` or a template
  ID. Browser Back and Forward close and restore the editor, dirty route changes
  use the shared confirmation layer, and modal closure returns focus to the
  exact originating row trigger.
- OpenAPI now documents preview, revision, expected revision and the 409
  response. Focused handler tests cover safe line diagnostics and UTF-8 preview
  truncation; component tests cover the editor gutter and URL-driven modal
  focus restoration.

Local and browser verification before synchronization:

- `go test ./...` and `go vet ./...` passed for all backend packages using a
  workspace-local Go cache. The first local run had to download the exact
  Go 1.26.5 toolchain and one interrupted module; a retry completed normally.
- `pnpm typecheck` passed. The normal Vitest config loader is still prevented
  from reading above the workspace by the restricted execution context, so a
  config-free temporary entry retained the Vue plugin, happy-dom environment
  and test options: 31 files and 59 tests passed. The entry was deleted.
- A config-free Vite entry retained the repository manual chunk policy and
  transformed 487 modules. It was deleted after the build. Gzip sizes were
  32.22 KiB for the entry, 42.43 KiB for Vue, 103.04 KiB for shared UI and
  11.33 KiB for base CSS. SubscriptionTemplates added 7.69 KiB JavaScript and
  1.08 KiB CSS; Nodes and Protocols remained 18.91/4.53 and 15.67/2.13 KiB,
  and xterm remained an 84.18 KiB lazy chunk.
- `git diff --check` passed with only the existing Windows line-ending
  advisories.
- Local browser interaction used a bounded visual fixture, not a backend
  correctness substitute. It confirmed valid preview output, a precise line-2
  error and focus/ARIA state, correction and stale-preview state, URL
  Back/Forward, exact trigger focus restoration and a simulated revision-4
  versus revision-5 conflict that disabled save until reload.
- Desktop measurements at 1280 pixels showed a 1040-pixel dialog with
  608-pixel source and 360-pixel preview panes and zero overlap. At 390x844 the
  dialog, source and stacked preview stayed within the viewport. Accepted
  screenshots are under
  `.codex-local-artifacts/zboard-admin-audit-2026-07-23-run6`:
  `01-template-editor-preview-1280x720.jpg`,
  `02-template-editor-preview-390x844.jpg` and
  `03-template-preview-output-390x844.jpg`. Final browser warnings and errors
  were empty.

Remaining gaps before synchronization:

- The revision migration and preview route still require deployment and
  authenticated verification against the intranet MySQL database. The browser
  conflict fixture proves UI behavior but does not replace a real two-editor
  integration test around the row lock and HTTP 409 response.
- A broader matrix of real production templates and rendered client formats is
  still required after deployment. Syntax completion is intentionally not
  claimed; the current editor provides line navigation, diagnostics, preview
  and conflict safety without pretending to be an IDE.
- Plans still preloads SKU detail into its paginated list and mixes read/detail
  with editing. Tickets and AuditLogs retain the gaps recorded in the frontend
  completion audit.
- Intranet synchronization and deployed behavior verification have not yet run
  for this phase, so this phase is not deployed.

Intranet synchronization outcome:

- `scripts/sync-intranet.ps1 -SkipLocalChecks` was attempted after the verified
  tests, build and audit update. The restricted execution context still could
  not resolve the `gitlab` SSH alias, and `scp` failed before upload. No remote
  candidate, database backup, previous-source path, release archive, migration,
  version or container change was created. This phase remains not deployed.
- Independent production requests after the failed upload returned HTTP 200
  from `/api/v1/version` and `/readyz`. Production is unchanged at
  `v0.0.1-20260722T183245Z-intranet-working-tree@2026-07-22T18:32:45Z`;
  readiness reports `db: true` and `ready: true`.
- The retained disposable environment also returned HTTP 200 from `/readyz`
  through `127.0.0.1:18089` with `db: true` and `ready: true`. `netstat`
  continues to show PID 8868 listening on that port, so exact-project cleanup
  and tunnel cleanup remain outstanding.
- Container health, migration `0023`, the deployed preview endpoint and a real
  two-editor revision conflict could not be verified because synchronization
  did not occur and the SSH boundary remains unavailable.

### 2026-07-23 - Plan summary, detail and SKU workspace phase

Phase outcome before synchronization:

- Replaced the administration Plan page's expanded-row model with three clear
  layers. The paginated list now receives only product summary, node-group
  summary, total SKU count and active SKU count. Product policy and the complete
  ordered SKU collection are loaded only through the new administrator detail
  endpoint.
- The paged backend list no longer preloads all SKU rows. It loads the bounded
  Plan page, its node-group summaries and one grouped SKU-count result. The
  public non-paged Plan contract remains compatible and continues to expose
  active SKUs where the public purchase flow requires them.
- Product detail, product editing and SKU new/edit are separate URL-restorable
  states. Browser Back and Forward restore the same layer, and closing returns
  focus to the exact row, product-editor or SKU-create trigger.
- Product and SKU local validation uses the shared error summary, real field
  `aria-invalid`/`aria-describedby` relations and first-error focus. Product
  publish/draft changes use the shared confirmation queue. Successful writes
  refresh the summary and any open detail together and announce through the
  global Toast; actionable detail failures retain an explicit retry action.
- Added SKU creation to the separated SKU editor using the existing backend
  contract. The direct SKU counts update immediately after creation; list and
  detail continue to format status as semantic icon labels, counts as numbers
  and time as compact tags.
- The 390-pixel Plan table has an explicit high-density projection. Product,
  complete status label, active-SKU number and View action fit in the 354-pixel
  table shell without internal or page-level horizontal overflow. Long product
  metadata truncates within the primary cell instead of passing under the
  sticky action column.
- OpenAPI now documents `PlanSummary`, `PlanDetail` and the administrator
  detail route. Backend tests prove that paged summaries omit description,
  policy and SKU rows while detail returns those fields and both count values.
- `PageAlert` now supports an optional action slot for persistent retry
  controls; its role, title, message and retry action are covered by a component
  test.

Local and browser verification before synchronization:

- `go test ./...` passed for every backend package with Go 1.26.5 and the
  existing local module cache; `go vet ./...` also passed.
- `pnpm typecheck` passed. The normal Vitest/Vite config loader remains unable
  to read above the restricted workspace, so temporary config-free entries
  retained the repository Vue plugin, happy-dom options and manual chunk
  policy. Vitest passed 32 files and 60 tests; the production build transformed
  488 modules. Both entries were deleted.
- Production gzip sizes were 32.29 KiB for the entry, 42.43 KiB for Vue,
  103.04 KiB for shared UI and 11.33 KiB for base CSS. Plans was 12.62 KiB
  JavaScript and 0.85 KiB CSS; Nodes, Protocols and the lazy xterm chunk
  remained within the previously recorded budgets.
- Browser interaction used a bounded local visual/interaction fixture and does
  not substitute for backend correctness. It verified summary-only requests,
  on-demand detail, Back/Forward, exact focus restoration, local product and SKU
  error focus/ARIA state, successful product update, successful SKU creation,
  list/detail/count/Toast synchronization and confirmed publish-state changes.
- At 390x844 the main Plan table reported equal 354-pixel client and scroll
  widths, while the page and body remained 390 pixels wide. The SKU editor was
  390 pixels wide and fully inside the 844-pixel viewport. At 1280x720 the
  detail drawer was 720 pixels wide, the Plan table had no internal overflow,
  and the product editor used consistent 42-pixel single-line controls. Final
  browser warning/error logs were empty.
- Accepted screenshots are under
  `.codex-local-artifacts/zboard-admin-audit-2026-07-23-run7`:
  `03-plans-list-mobile-fit-390x844.png`,
  `04-plan-sku-editor-390x844.png`,
  `05-plan-detail-1280x720.png`,
  `06-plans-list-1280x720.png` and
  `07-plan-editor-1280x720.png`.

Remaining gaps before synchronization:

- The Plan summary aggregate and administrator detail contract still require
  authenticated acceptance against the intranet MySQL database. Production
  data-distribution and long-duration query baselines are not yet established.
- Plans and SKUs do not yet carry optimistic revisions, so concurrent editors
  can still overwrite each other. This remains a distinct follow-up rather than
  being implied by the completed UI separation.
- Tickets still needs its fully verified master/detail workspace, and AuditLogs
  still needs safe on-demand detail and sensitive-field response tests.
- Remaining high-risk assistive-technology announcement coverage, the real SSH
  failure flow, external SMTP effects and empty-cache network timing remain
  open.
- Intranet synchronization and deployed behavior verification have not yet run
  for this phase, so this phase is not deployed.

Intranet synchronization outcome:

- `git diff --check` passed before synchronization with only the existing
  Windows line-ending advisories.
- `scripts/sync-intranet.ps1 -SkipLocalChecks` was attempted after the verified
  backend, frontend, browser and documentation checks. The restricted context
  still could not resolve the `gitlab` SSH alias, and source upload failed
  before any remote candidate, database backup, previous-source directory,
  source archive, migration, version or container change was created. This
  phase remains not deployed.
- Independent production requests after the failed upload returned HTTP 200
  from `/api/v1/version` and `/readyz`. Production remains
  `v0.0.1-20260722T183245Z-intranet-working-tree@2026-07-22T18:32:45Z`;
  readiness reports `db: true` and `ready: true`.
- The retained disposable environment also returned HTTP 200 from `/readyz`
  through `127.0.0.1:18089` with `db: true` and `ready: true`. `netstat`
  continues to show PID 8868 listening on that port, so its exact-project and
  tunnel cleanup remain outstanding.
- Container health, a new deployed version, the new Plan detail route and
  goal-specific deployed UI behavior could not be verified because no
  synchronization occurred and the SSH boundary remains unavailable.

### 2026-07-23 - Ticket conversation and sanitized audit detail phase

Phase outcome before synchronization:

- Replaced the administrator ticket list's per-row user and message-count
  lookups with two bounded batch queries after the paged ticket query. Ticket
  detail now returns at most the latest 100 messages by default and accepts
  `before_id` plus a bounded `message_limit` to load older history without
  returning an unbounded conversation.
- `TicketCenter` no longer selects the first row automatically. Desktop uses a
  compact queue and independently scrolling conversation; at 390 pixels the
  list and detail are mutually exclusive and the detail state hides the list
  filters and page introduction. Returning to the list removes the ticket URL
  state and restores focus to the exact row trigger.
- Older ticket history is prepended without duplicating message IDs and keeps
  the previous conversation anchor stable. Reply success refreshes the list and
  latest bounded detail together, clears the draft and announces through the
  shared Toast. Closing through the status selector uses the shared
  confirmation dialog, and cancellation now restores the selector's actual
  server state.
- Audit log lists now return a summary DTO only: ID, actor, action, target,
  detail availability and time. User IDs and detail bodies are loaded only by
  the new administrator detail endpoint.
- Audit detail is normalized and bounded to 16 KiB with UTF-8-safe truncation.
  Password, passwd, secret, token, private-key, Authorization and Cookie
  assignments plus Bearer credentials are redacted again at the response
  boundary. Backend tests lock summary/body separation, sensitive-value
  redaction and safe truncation.
- The AuditLogs UI uses a compact responsive summary table and URL-restorable
  detail drawer. Detail loading, retry, closure and exact focus restoration are
  explicit; sanitized output uses the shared output component instead of
  rendering raw response text.
- Browser acceptance found a shared `UiSelect` defect: ordinary structured
  options were always configured as PrimeVue option groups and therefore
  appeared as non-selectable group labels. Group props are now enabled only
  when child options exist, controlled values render correctly, and a component
  test covers both flat and grouped cases. The shared pager width was also
  increased so `25/50/100` labels are not ellipsized.
- The same acceptance pass found and fixed a missing `PageAlert` import in
  TicketCenter. A clean second browser session was required after the fix;
  both Tickets and AuditLogs then produced zero console warnings and errors.

Local and browser verification before synchronization:

- `go test ./...` and `go vet ./...` passed for every backend package using the
  exact Go 1.26.5 toolchain and workspace-local build caches.
- `pnpm typecheck` passed. A config-free temporary Vitest/Vite program retained
  the Vue plugin, happy-dom options and repository manual chunk policy because
  the restricted environment still blocks the normal config loader above the
  workspace. Vitest passed 32 files and 61 tests; the production build
  transformed 488 modules. The temporary program was deleted.
- Final gzip sizes were 32.75 KiB for the entry, 42.43 KiB for Vue,
  103.04 KiB for shared UI and 11.33 KiB for base CSS. TicketCenter was
  7.20 KiB JavaScript and 1.81 KiB CSS; AuditLogs was 3.81 KiB JavaScript and
  0.62 KiB CSS. Nodes, Protocols and the lazy xterm chunk remained within their
  existing recorded budgets.
- `git diff --check` passed with only the existing Windows line-ending
  advisories.
- A bounded local fixture with a 235-message ticket verified
  `100 -> 200 -> 235` older-history loading, anchor preservation, valid reply,
  Toast feedback, the return to a bounded latest-100 response, selectable
  status options, destructive confirmation and correct cancellation rollback.
- At 1280x720 the ticket queue and detail fit without page-level horizontal
  overflow and the 520-pixel conversation viewport scrolled independently. At
  390x844 the list and focused detail each stayed within the 380-pixel page
  content width; the reply control remained reachable and return-to-list focus
  restored to ticket 101.
- Audit acceptance proved the summary list contained no detail body, the
  390-pixel table had equal client and scroll widths, and the detail drawer fit
  both 390- and 1280-pixel viewports. The fixture detail exposed only
  `[redacted]` values, the drawer URL and close focus were correct, and the
  final pager label had no overflow.
- Accepted screenshots are under
  `.codex-local-artifacts/zboard-admin-audit-2026-07-23-run8`, including
  `02-ticket-detail-fixed-1280x720.png`,
  `03-tickets-list-390x844.png`,
  `05-ticket-detail-focused-390x844.png`,
  `06-audit-list-390x844.png`,
  `07-audit-detail-redacted-390x844.png`,
  `09-audit-detail-redacted-1280x720.png` and
  `10-audit-list-final-1280x720.png`.

Remaining gaps before synchronization:

- The ticket batching, bounded history and audit detail route still require
  authenticated verification against the intranet MySQL database. Production
  long-conversation distributions, sanitized real audit samples and
  long-duration concurrency/load baselines are not established.
- Subscription template revision migration `0023` and its real two-editor
  conflict remain pending deployment verification. Plan summary/detail still
  needs a production aggregation baseline and optimistic concurrent editing.
- Remaining high-risk assistive-technology announcements, the real SSH failure
  path, external SMTP side effects and empty-cache network timing remain open.
- Intranet synchronization and deployed behavior verification have not yet run
  for this phase, so this phase is not deployed.

Intranet synchronization outcome:

- The first required synchronization attempt did not reach `scp`: source
  archiving still included `.codex-local-artifacts` and `.codex-cache`, so the
  local command timed out after 120 seconds while creating a 495,820,800-byte
  temporary archive. The sync script now excludes both local-only directories.
  The abandoned archive directory was resolved below the system temporary
  directory, deleted, and verified absent; no `tar` or `scp` process remains.
- The corrected `scripts/sync-intranet.ps1 -SkipLocalChecks` attempt created and
  cleaned its bounded archive normally, then failed because the restricted
  context could not resolve the `gitlab` SSH alias. `scp` never uploaded the
  source archive. No remote candidate, database backup, previous-source
  directory, release archive, migration, deployed version or container change
  was created. This phase remains not deployed.
- Independent production requests after the failed upload returned HTTP 200
  from `/api/v1/version` and `/readyz`. Production remains
  `v0.0.1-20260722T183245Z-intranet-working-tree@2026-07-22T18:32:45Z`;
  readiness reports `db: true` and `ready: true`.
- The retained disposable environment also returned HTTP 200 from `/readyz`
  through `127.0.0.1:18089` with `db: true` and `ready: true`. `netstat`
  continues to show PID 8868 listening on that port, so its exact-project and
  tunnel cleanup remain outstanding.
- Container health, migration `0023`, the new Plan/Audit detail routes, ticket
  bounded-history behavior and goal-specific deployed UI behavior could not be
  verified because no synchronization occurred and the SSH boundary remains
  unavailable.

### 2026-07-23 - Cross-resource return context phase

Phase outcome before synchronization:

- Added a single administrator navigation contract for cross-resource work.
  `normalizeAdminReturnTo` accepts only bounded, control-character-free local
  `/admin` paths, strips fragments and rejects external origins, credential
  URLs and non-administrator paths. Unit tests cover valid filter/page/detail
  state, array values, fragment removal and rejected inputs.
- The administrator shell now renders one compact `返回来源` bar whenever a
  validated source exists. The source description is visible on desktop and
  intentionally collapses on narrow screens while the return action remains
  visible.
- Users, Subscriptions, Orders, Traffic, Nodes and Protocols now carry the
  complete current URL into related-resource links. OperationLogs, AuditLogs,
  Tasks, NodeGroups, SubscriptionTemplates and TicketCenter preserve the
  validated source whenever they rewrite their owned filters, page, cursor or
  detail state. Plans already preserved unmanaged query state.
- The global task tray now links to Tasks with the current page as its source,
  except when it is already on the Tasks route. This avoids a self-return loop
  while retaining the caller's list/detail state for background operations.
- Node and protocol failure-log links and protocol-to-node links now use the
  same contract as the business pages, so infrastructure workflows no longer
  lose the source workbench state.

Local and browser verification before synchronization:

- `pnpm typecheck` passed. The restricted environment still blocks the normal
  Vitest/Vite config loader above the workspace, so config-free programmatic
  entries retained the Vue plugin, happy-dom environment and repository manual
  chunk policy. Vitest passed 33 files and 64 tests. The production build
  transformed 489 modules.
- Production gzip sizes were 32.41 KiB for the entry, 42.43 KiB for Vue,
  103.04 KiB for shared UI and 11.33 KiB for base CSS. The new navigation
  utility was 0.34 KiB gzip and AdminLayout was 3.75 KiB gzip. Nodes,
  Protocols and the lazy xterm chunk remained within the previously recorded
  budgets.
- `go test ./...` passed for every backend package and `go vet ./...` passed
  with the exact Go 1.26.5 toolchain and workspace-local caches. The first
  combined cold-cache command timed out after 121 seconds after completing the
  API package; split reruns completed successfully. `git diff --check` passed
  with only the existing Windows line-ending advisories.
- A bounded local fixture started from
  `/admin/users?q=member&status=active&page=2&user=1`. Browser interaction
  proved Users detail -> Subscriptions detail -> Orders -> Subscriptions
  detail -> the original Users filter, second page and detail state. The
  authoritative return href at each layer contained the exact preceding list
  and detail URL.
- At 390x844 the return action remained visible, the explanatory copy
  collapsed, and `scrollWidth === innerWidth === 390`. At 1280x720 the return
  bar, order workbench and table shell fit without page-level overflow.
  An external `https://...` return value produced no return bar. Final browser
  warning/error logs were empty.
- Accepted screenshots are under
  `.codex-local-artifacts/zboard-admin-audit-2026-07-23-run9`:
  `01-cross-resource-return-390x844.png` and
  `02-cross-resource-return-desktop.png`. The bounded fixture on port 18129
  and a stale earlier business fixture on port 18123 were both stopped after
  acceptance.

Remaining gaps before synchronization:

- The cross-resource contract still requires authenticated acceptance against
  the intranet deployment after synchronization. No claim is made that the
  current production bundle contains this phase.
- Subscription template migration `0023`, real two-editor conflict handling,
  Plan production aggregation/concurrent-edit baselines, ticket/audit
  production distributions, long-duration cursor/load baselines, the real SSH
  failure path, external SMTP effects, empty-cache network timing and remaining
  high-risk assistive-technology announcements are still open.
- The retained disposable integration environment and protected tunnel on
  port 18089 remain a separate cleanup gap.

Intranet synchronization outcome:

- `scripts/sync-intranet.ps1 -SkipLocalChecks` was attempted after the backend,
  frontend, browser and documentation gates. The bounded archive excluded
  `.codex-cache` and `.codex-local-artifacts`, reached `scp`, then failed
  because the restricted context could not resolve the `gitlab` SSH alias.
  No source archive was uploaded and the script cleaned its local temporary
  directory; zero `zboard-intranet-sync-*` directories remain.
- No remote candidate, database backup, previous-source path, release archive,
  migration, deployed version or container change was created. There is
  therefore no new backup or previous-source path to record for this phase.
- Independent production requests after the failed upload returned HTTP 200
  from `/api/v1/version` and `/readyz`. Production remains
  `v0.0.1-20260722T183245Z-intranet-working-tree@2026-07-22T18:32:45Z`;
  readiness reports `db: true` and `ready: true`.
- The retained disposable environment also returned HTTP 200 from `/readyz`
  through `127.0.0.1:18089` with `db: true` and `ready: true`; `netstat`
  still reports PID 8868 listening. Container health, a new deployed version
  and goal-specific deployed cross-resource behavior could not be verified
  because synchronization did not occur.

### 2026-07-23 - Global API and operational error normalization phase

Phase outcome before synchronization:

- Added one Axios response-error boundary in `frontend/src/api/client.ts`.
  Every structured API error is normalized before existing page fallbacks
  consume it. The boundary accepts only a bounded localized message, a stable
  lower-case error code and legal bounded field names; non-object bodies,
  non-localized gateway text and invalid codes fall back to the page-owned
  Chinese action message.
- `frontend/src/utils/apiError.ts` now removes ANSI sequences, CRLF/control
  characters and Unicode replacement characters, collapses whitespace and
  truncates by Unicode code point. Structured `data` remains available to the
  caller and versioned form fields retain their declared mapping contract.
- Settings non-Axios failures use the same normalization helper. SSH WebSocket
  messages are normalized before entering a page alert or terminal output, and
  the global task tray normalizes and Unicode-safely bounds its first error
  summary. These explicit paths cannot bypass the Axios interceptor.
- The completion audit now records the response boundary, accepted screenshots
  and the completed cross-resource return contract; stale statements claiming
  that `return_to` was still missing were corrected.

Local and browser verification before synchronization:

- `pnpm typecheck` passed. The restricted environment again blocked the normal
  Vitest/Vite config loader above the workspace, so execution-time config-free
  programmatic entries retained the repository Vue plugin, happy-dom options
  and manual chunk policy, then were deleted. Vitest passed 33 files and 66
  tests. The production build transformed 489 modules.
- Production gzip sizes were 33.04 KiB for the entry, 42.43 KiB for Vue,
  103.04 KiB for shared UI and 11.33 KiB for base CSS. Dashboard was 3.02 KiB,
  Nodes 18.93 KiB, Protocols 15.71 KiB and the lazy xterm chunk 84.18 KiB.
  `git diff --check` passed with only existing Windows line-ending advisories.
  No backend behavior changed in this phase, so frontend type, unit, build,
  output-contract and browser gates were the proportionate local checks; the
  already-passing backend suite was not repeated.
- A bounded local server returned HTTP 503 for Dashboard with a localized
  message containing ANSI, CRLF, a control character and a replacement
  character. At 1280x720 the normalized error rendered as a full-width,
  prominent page alert directly below the page header. At 390x844 the
  administrator navigation was authoritatively closed
  (`aria-expanded=false`, shell class `app-shell`, sidebar translated outside
  the viewport), the alert was visible from x=18 to x=362, and both document
  and body had `scrollWidth === clientWidth === 380`.
- Both viewports displayed only `工作台暂时不可用。 请稍后重试。`, with no
  ANSI, newline, control or replacement character. Browser warning/error logs
  were empty. Accepted evidence is
  `.codex-local-artifacts/zboard-admin-audit-2026-07-23-run10/04-normalized-page-error-mobile-final-390x844.png`
  and `05-normalized-page-error-desktop-final-1280x720.png`. The earlier mobile
  capture with an open navigation overlay and captures that still contained a
  replacement character are not accepted evidence. The local server was
  stopped and port 18131 has zero listeners.

Remaining gaps before synchronization:

- The response boundary still requires acceptance against an authenticated
  intranet deployment after synchronization. No claim is made that the current
  production bundle contains this phase.
- Subscription template migration `0023`, real two-editor conflict handling,
  Plan production aggregation/concurrent-edit baselines, ticket/audit
  production distributions, long-duration cursor/load baselines, the real SSH
  failure path, external SMTP effects, empty-cache network timing and remaining
  high-risk assistive-technology announcements remain open.
- The retained disposable integration environment and protected tunnel on
  port 18089 remain a separate cleanup gap.

Intranet synchronization outcome:

- `scripts/sync-intranet.ps1 -SkipLocalChecks` was attempted after the
  frontend, browser and documentation gates. The bounded archive excluded
  `.codex-cache` and `.codex-local-artifacts`, reached `scp`, then failed
  because the restricted context could not resolve the `gitlab` SSH alias.
  No source archive was uploaded and zero `zboard-intranet-sync-*` temporary
  directories remain.
- No remote candidate, database backup, previous-source path, release archive,
  migration, deployed version or container change was created. There is
  therefore no new backup or previous-source path to record for this phase.
- Independent production requests after the failed upload returned HTTP 200
  from `/api/v1/version` and `/readyz`. Production remains
  `v0.0.1-20260722T183245Z-intranet-working-tree@2026-07-22T18:32:45Z`;
  readiness reports `db: true` and `ready: true`.
- The retained disposable environment also returned HTTP 200 from `/readyz`
  through `127.0.0.1:18089` with `db: true` and `ready: true`; `netstat`
  still reports PID 8868 listening. Container health, a new deployed version
  and goal-specific deployed error-boundary behavior could not be verified
  because synchronization did not occur.

### 2026-07-23 - Bounded feedback and live-region policy phase

Phase outcome before synchronization:

- The global Toast host now retains active messages in application state until
  user dismissal or lifecycle completion. Identical active
  title/message/tone tuples return the existing ID, and adding a fourth item
  evicts the oldest item before rendering, establishing a real three-Toast
  cap instead of an unbounded PrimeVue-only queue.
- The renderer reconciles removals in both directions: queue eviction removes
  the visible Toast, while manual close or life-end removes the matching queue
  entry. Every Toast is explicitly closable and the close control has the
  Chinese accessible name `关闭通知`.
- Page and form errors remain persistent rather than moving into a transient
  overlay. `PageAlert` is now an atomic live region, remote endpoint-search and
  SSH terminal errors use `role=alert`, and SSH connection state changes use a
  polite atomic status region.
- Added a repository-wide feedback policy test. A Vue file that writes success
  state without a `TransientFeedback` success outlet, writes error state
  without a persistent alert outlet, or reintroduces native
  `alert/confirm/prompt` now fails the suite.

Local and browser verification before synchronization:

- `pnpm typecheck` passed. Config-free programmatic Vitest/Vite entries were
  again required by the restricted config-loader boundary; they retained the
  Vue plugin, happy-dom options and repository chunk policy and were deleted
  after execution. Vitest passed 35 files and 70 tests. The production build
  transformed 489 modules.
- The feedback state test proves duplicate suppression and the three-item
  bound. The mounted `FeedbackHost` test proves the rendered layer contains
  exactly the three retained Toasts, every item has a manual close button, the
  evicted duplicate is absent, and clicking close reduces the shared active
  queue. PageAlert atomic semantics and the repository-wide feedback policy
  also have direct tests.
- Production gzip sizes were 33.18 KiB for the entry, 42.43 KiB for Vue,
  103.04 KiB for shared UI and 11.33 KiB for base CSS. Nodes was 18.96 KiB,
  Protocols 15.71 KiB and the lazy xterm chunk 84.18 KiB.
  `git diff --check` passed with only existing Windows line-ending advisories,
  the temporary verification entry was absent and the local feedback fixture
  on port 18132 had zero listeners. No backend behavior changed in this phase,
  so frontend type, policy, component, build and browser gates were the
  proportionate checks; the backend suite was not repeated.
- A bounded authenticated fixture exercised the real user order workflow:
  pending order -> destructive confirmation dialog -> cancel request -> list
  status `已取消` -> success Toast. At 1280x720 the Toast occupied x=870..1260,
  y=20 with width 390; at 390x844 it occupied x=12..370.4 with width 358.4.
  Both exposed one `role=alert` and one visible `关闭通知` button. The mobile
  document retained `scrollWidth === clientWidth === 390`; the order table
  used its internal horizontal scroller and the page did not overflow.
- Browser warning/error logs were empty. Accepted evidence is
  `.codex-local-artifacts/zboard-admin-audit-2026-07-23-run11/03-dismissible-success-toast-final-1280x720.png`
  and `02-dismissible-success-toast-390x844.png`. A desktop capture taken
  after the Toast lifetime ended is not accepted evidence.

Remaining gaps before synchronization:

- The feedback policy still requires acceptance against an authenticated
  intranet deployment after synchronization. No claim is made that the current
  production bundle contains this phase.
- Specific screen-reader/browser combinations for every high-risk write form,
  the real SSH failure path, Subscription Template migration `0023` and
  two-editor conflict behavior, Plan production aggregation/concurrent edits,
  ticket/audit production distributions, long-duration cursor/load baselines,
  external SMTP effects and empty-cache network timing remain open.
- The retained disposable integration environment and protected tunnel on
  port 18089 remain a separate cleanup gap.

Intranet synchronization outcome:

- `scripts/sync-intranet.ps1 -SkipLocalChecks` was attempted after the
  frontend, browser and documentation gates. The bounded archive excluded
  `.codex-cache` and `.codex-local-artifacts`, reached `scp`, then failed
  because the restricted context could not resolve the `gitlab` SSH alias.
  No source archive was uploaded and zero `zboard-intranet-sync-*` temporary
  directories remain.
- No remote candidate, database backup, previous-source path, release archive,
  migration, deployed version or container change was created. There is
  therefore no new backup or previous-source path to record for this phase.
- Independent production requests after the failed upload returned HTTP 200
  from `/api/v1/version` and `/readyz`. Production remains
  `v0.0.1-20260722T183245Z-intranet-working-tree@2026-07-22T18:32:45Z`;
  readiness reports `db: true` and `ready: true`.
- The retained disposable environment also returned HTTP 200 from `/readyz`
  through `127.0.0.1:18089` with `db: true` and `ready: true`; `netstat`
  still reports PID 8868 listening. Container health, a new deployed version
  and goal-specific deployed feedback behavior could not be verified because
  synchronization did not occur.

### 2026-07-24 - Declarative dynamic system-config input phase

Phase outcome before synchronization:

- The administrator system-config response now includes a backend-owned input
  schema for each setting. The contract covers control kind, required state,
  numeric min/max/step, UTF-8 byte limit, placeholder and structured options.
  Site metadata, SMTP fields, feature switches and JSON settings therefore no
  longer depend on a raw `value_type` string to decide what operators see.
- The Settings page renders shared text, textarea, URL, email, hostname,
  password, integer, port, switch, select and JSON controls from that schema.
  It formats JSON for editing, never reveals secret values and normalizes
  numeric, boolean and JSON values before sending them to the API. Older
  servers retain a bounded `value_type` compatibility mapping.
- Local validation now keeps errors beside the affected configuration,
  establishes `aria-invalid`/description relationships and moves focus back to
  the invalid control. Revision conflicts remain persistent and actionable:
  the current draft is preserved until the operator chooses `重新载入`.
- Browser acceptance exposed a shared wrapper defect that component-only
  coverage had missed: `UiInput` and `UiTextarea` did not recognize the
  hyphenated `model-value` vnode key used by a parent component, so values
  could render empty and edits did not update the draft. Both wrappers now
  accept camel-case and hyphenated bindings, with direct and Settings-row
  regression tests.
- SMTP hostname validation now rejects repeated dots and leading/trailing dot
  or hyphen boundaries in both the browser-domain validator and the backend
  final validator.

Local and browser verification before synchronization:

- `pnpm typecheck` passed. Config-free Vitest passed 38 files and 77 tests,
  including the schema formatter/normalizer, declared domain controls,
  controlled input/textarea wrappers, error/live-region and repository-wide
  form/feedback policies. The production build transformed 490 modules.
- Production gzip sizes were 33.22 KiB for the entry, 42.43 KiB for Vue,
  103.04 KiB for shared UI and 11.33 KiB for base CSS. Settings was 5.98 KiB
  JavaScript/1.04 KiB CSS, Nodes 18.97 KiB, Protocols 15.72 KiB and the lazy
  xterm chunk 84.18 KiB.
- The full Go suite and `go vet ./...` passed with Go 1.26.5. A first focused
  handler invocation had only paid the one-time dependency download cost; the
  warm focused schema/validation tests and the final full suite passed.
- A bounded authenticated Settings fixture on port 18133 verified localized
  control labels, SMTP port domain bounds, both TLS options, JSON formatted
  display and typed object submission, success Toasts, revision conflict and
  explicit reload, persistent local JSON errors, zero invalid JSON requests,
  `aria-invalid`/description linkage and focus on the invalid JSON editor.
  The conflict request submitted a numeric port and reload restored server
  revision 4/value 2525; the JSON request submitted
  `{retry: 5, enabled: false}` rather than a raw string.
- At 1280x720 and 390x844 the Settings layout had no page-level horizontal
  overflow. The mobile navigation remained closed, configuration rows stacked
  to the viewport and no row exceeded the document width. Browser
  warning/error logs were empty. Accepted screenshots are under
  `.codex-local-artifacts/zboard-admin-audit-2026-07-23-run12`:
  `01-settings-schema-json-success-1280x720.png`,
  `02-settings-mobile-top-390x844.png` and
  `03-settings-schema-controls-mobile-390x844.png`. The fixture was stopped
  and port 18133 has zero listeners.

Remaining gaps before synchronization:

- The declarative Settings contract still requires authenticated acceptance
  against the intranet deployment. No claim is made that the current
  production bundle contains this phase.
- Real concurrent writes from two administrator sessions and external SMTP
  delivery effects remain open. The Subscription Template migration `0023`
  and two-editor conflict path, Plan production aggregation/concurrent edits,
  ticket/audit production distributions, long-duration cursor/load baselines,
  the real SSH failure path, empty-cache network timing and remaining
  high-risk assistive-technology announcements are also still open.
- The retained disposable integration environment and protected tunnel on
  port 18089 remain a separate cleanup gap.

Intranet synchronization outcome:

- `scripts/sync-intranet.ps1 -SkipLocalChecks` was attempted after the full
  backend/frontend, browser evidence and documentation gates. The bounded
  archive excluded `.codex-cache` and `.codex-local-artifacts`, reached
  `scp`, then failed because the restricted context could not resolve the
  `gitlab` SSH alias. No source archive was uploaded and zero
  `zboard-intranet-sync-*` temporary directories remain.
- No remote candidate, database backup, previous-source path, release archive,
  migration, deployed version or container change was created. There is
  therefore no new backup or previous-source path to record for this phase.
- Independent production requests after the failed upload returned HTTP 200
  from `/api/v1/version` and `/readyz`. Production remains
  `v0.0.1-20260722T183245Z-intranet-working-tree@2026-07-22T18:32:45Z`;
  readiness reports `db: true` and `ready: true`.
- The retained disposable environment also returned HTTP 200 from `/readyz`
  through `127.0.0.1:18089` with `db: true` and `ready: true`; `netstat`
  still reports SSH PID 8868 listening on IPv4 and IPv6 loopback. Container
  health, a new deployed version and goal-specific deployed Settings behavior
  could not be verified because synchronization did not occur.
- Following a confirmed Codex in-app-browser cleanup crash pattern, all
  further IAB acceptance, tab closing and tab finalization are paused until
  the user explicitly lifts that restriction. Compilation, automated tests,
  source audits and non-browser HTTP checks may continue.

### 2026-07-24 - Bounded node-group membership phase

Phase outcome before synchronization:

- Paged node-group responses now return summary rows with direct endpoint and
  plan counts instead of embedding every `protocol_endpoint_id` in every row.
  The legacy unpaged response retains its full membership for compatibility.
- A dedicated administrator node-group detail endpoint loads the ordered full
  membership only when an operator opens that group for editing. The frontend
  edit action exposes a row-scoped loading state and keeps detail-load
  failures in the persistent page alert.
- The selected-member editor renders 50 selected endpoint IDs at a time and
  hydrates labels only for the current selected page. Previous/next controls
  expose the current range and total; removing a member uses the shared icon
  control. A 5000-member draft therefore does not create 5000 chips or issue
  50 eager hydration requests.
- Membership replacement deduplicates requested IDs, validates the complete
  active endpoint set in one `IN` query, reports the first missing requested
  ID deterministically and writes association rows in batches of 500. Node
  group creation audit output records `endpoint_count` rather than copying the
  entire endpoint ID list.
- OpenAPI now distinguishes paged `NodeGroupSummary` rows from full
  `NodeGroup` detail and documents the new GET detail route.

Local verification before synchronization:

- `pnpm typecheck` passed. Config-free Vitest passed 39 files and 78 tests
  after the selected-member fixture was increased to 5000 IDs. The component
  test proves that the first selected page renders and hydrates IDs 1-50 and
  the next selected page renders and hydrates only IDs 51-100.
- The production build transformed 490 modules. Production gzip sizes were
  33.24 KiB for the entry, 42.43 KiB for Vue, 103.04 KiB for shared UI and
  11.33 KiB for base CSS. NodeGroups was 6.17 KiB JavaScript/0.95 KiB CSS,
  Nodes 18.97 KiB, Protocols 15.73 KiB, Settings 5.98 KiB and the lazy xterm
  chunk 84.18 KiB.
- Focused OpenAPI, handler and router packages passed, followed by
  `go test ./...` and `go vet ./...`. Tests cover the summary boundary,
  deterministic missing-ID selection and the 5000-ID pure validation path.
- No IAB was started, controlled, closed or finalized. Browser acceptance
  remains paused under the confirmed app-crash restriction and no browser
  claim is inferred from the component suite.

Remaining gaps before synchronization:

- The new list/detail contract, 5000-member edit flow and batch replacement
  still require authenticated acceptance against the intranet deployment and
  a real MySQL large-membership timing baseline.
- The product still needs an explicit scope contract for bulk adding or
  removing all endpoint search matches and a concurrent node-group edit
  policy. These are not implied by the bounded selected-member pager.
- Real browser interaction, responsive layout and assistive-technology
  acceptance remain paused until the user explicitly lifts the IAB
  restriction. The retained disposable integration environment and protected
  tunnel on port 18089 remain a separate cleanup gap.

Intranet synchronization outcome:

- `scripts/sync-intranet.ps1 -SkipLocalChecks` was attempted after the full
  backend/frontend and documentation gates. The bounded archive reached
  `scp`, then failed because the restricted context could not resolve the
  `gitlab` SSH alias. No source archive was uploaded and zero
  `zboard-intranet-sync-*` temporary directories remain.
- No remote candidate, database backup, previous-source path, release archive,
  migration, deployed version or container change was created. There is
  therefore no new backup or previous-source path to record for this phase.
- Independent non-browser requests after the failed upload returned HTTP 200
  from `/api/v1/version` and `/readyz`. Production remains
  `v0.0.1-20260722T183245Z-intranet-working-tree@2026-07-22T18:32:45Z`;
  readiness reports `db: true` and `ready: true`.
- The retained disposable environment also returned HTTP 200 from `/readyz`
  through `127.0.0.1:18089` with `db: true` and `ready: true`; `netstat`
  reports SSH PID 8868 listening on IPv4 and IPv6 loopback. Container health,
  a new deployed version and goal-specific deployed node-group behavior could
  not be verified because synchronization did not occur.

### 2026-07-24 - Filter-wide traffic aggregates and safe list phase

Phase outcome before synchronization:

- Paged administrator traffic records now use a bounded list projection. The
  row keeps only the IDs, record time, raw bytes, multiplier and billed bytes
  required by the table; report IDs, flow/event fields, upload/download
  breakdowns and raw metadata are no longer copied into every list response.
  The legacy unpaged response remains unchanged for compatibility.
- The paged response includes aggregates for the complete filtered history
  window: raw bytes, billed bytes and distinct user, subscription, node and
  protocol-endpoint counts. These values are independent of the current
  cursor page and are documented as a dedicated OpenAPI schema.
- The administrator Traffic route renders a fixed four-metric strip from the
  server aggregates: matched record count, raw bytes, billed bytes and
  associated subscriptions, with related user/node counts in supporting text.
  It no longer needs to infer global results from the visible page.
- Both shared table composables now retain canonical page `aggregates` and
  `facets` alongside rows. They preserve the current values during refresh
  and apply them only after the same request-generation guard that prevents a
  stale response from replacing current rows.

Local verification before synchronization:

- `pnpm typecheck` passed. Focused API and shared table tests passed 3 files
  and 8 tests; the full config-free Vitest suite passed 39 files and 78 tests.
  The new assertions cover canonical aggregate/facet retention across
  refreshes.
- The production build transformed 490 modules. Production gzip sizes were
  33.24 KiB for the entry, 42.43 KiB for Vue, 103.04 KiB for shared UI and
  11.33 KiB for base CSS. Traffic was 4.51 KiB JavaScript/0.18 KiB CSS,
  NodeGroups 6.18 KiB, Nodes 18.98 KiB, Protocols 15.73 KiB and the lazy
  xterm chunk 84.18 KiB.
- Focused OpenAPI and handler packages passed. The first handler run rebuilt
  the restricted workspace Go cache and exceeded a 120-second wrapper
  timeout after the OpenAPI package had passed; the continued handler run
  completed successfully. The final `go test ./...` and `go vet ./...`
  passed with the workspace-local Go 1.26.5 toolchain and cache.
- Unit tests prove that the traffic summary projection contains the required
  table values and omits report, flow, event, metadata and unused byte
  breakdown fields. OpenAPI tests require the summary and aggregate schemas
  and their page references.
- No IAB was started, controlled, closed or finalized. Browser acceptance
  remains paused under the confirmed app-crash restriction.

Remaining gaps before synchronization:

- The additional filtered aggregate query requires a real MySQL 10,000-row
  timing baseline and authenticated intranet response verification. The
  earlier four-query/eight-millisecond traffic baseline predates this query
  and is not claimed for the new contract.
- Reconciliation still has independent offset pagination but does not yet
  expose a cross-mode aggregate overview. Long-duration production
  distribution and browser responsive/assistive-technology acceptance remain
  open; browser work remains paused until the user explicitly lifts the IAB
  restriction.
- The retained disposable integration environment and protected tunnel on
  port 18089 remain a separate cleanup gap.

Intranet synchronization outcome:

- `scripts/sync-intranet.ps1 -SkipLocalChecks` was attempted after the full
  backend/frontend and documentation gates. The bounded archive reached
  `scp`, then failed because the restricted context could not resolve the
  `gitlab` SSH alias. No source archive was uploaded and zero
  `zboard-intranet-sync-*` temporary directories remain.
- No remote candidate, database backup, previous-source path, release archive,
  migration, deployed version or container change was created. There is
  therefore no new backup or previous-source path to record for this phase.
- Independent non-browser requests after the failed upload returned HTTP 200
  from `/api/v1/version` and `/readyz`. Production remains
  `v0.0.1-20260722T183245Z-intranet-working-tree@2026-07-22T18:32:45Z`;
  readiness reports `db: true` and `ready: true`.
- The retained disposable environment also returned HTTP 200 from `/readyz`
  through `127.0.0.1:18089` with `db: true` and `ready: true`; `netstat`
  reports SSH PID 8868 listening on IPv4 and IPv6 loopback. Container health,
  a new deployed version and goal-specific deployed Traffic behavior could
  not be verified because synchronization did not occur.

### 2026-07-24 - Business-object discovery filters phase

Phase outcome before synchronization:

- Administrator Orders no longer requires an operator to know only a raw user
  ID. Its bounded search matches order and provider trade numbers, plan and
  SKU snapshots, channel and numeric order/user/subscription IDs. Status,
  order type and exact user context can be combined, and the workbench has an
  explicit clear action.
- Administrator Subscriptions now searches user email, plan and SKU display
  names and numeric subscription/user/plan IDs. Status, quota availability
  and exact user context can be combined. `available` means
  `flow_used < flow_total`; `exhausted` means `flow_used >= flow_total`.
- Both routes persist search, business enums, exact user, page, page size and
  detail state in the URL. Cross-resource exact-user links remain visible in
  the same filter bar instead of being silently converted into fuzzy search.
- The backend rejects zero administrator user IDs, searches longer than 128
  bytes, unknown order types, unknown subscription statuses and unknown quota
  filters. OpenAPI documents the same bounded queries and enum values.

Local verification before synchronization:

- `pnpm typecheck` passed. Config-free Vitest passed 39 files and 78 tests,
  including the shared request cancellation, URL-navigation, select and table
  policies used by these routes. The production build transformed 490
  modules.
- Production gzip sizes were 33.27 KiB for the entry, 42.43 KiB for Vue,
  103.04 KiB for shared UI and 11.33 KiB for base CSS. Orders was 5.29 KiB
  JavaScript/0.16 KiB CSS and Subscriptions was 4.67 KiB JavaScript/0.24 KiB
  CSS.
- Focused OpenAPI and handler packages passed, followed by `go test ./...`
  and `go vet ./...`. Backend tests cover the accepted and rejected order
  type, subscription status and quota-filter values; OpenAPI tests require
  every new query parameter.
- No IAB was started, controlled, closed or finalized. Browser acceptance
  remains paused under the confirmed app-crash restriction.

Remaining gaps before synchronization:

- The joined fuzzy searches and composite filters require authenticated
  intranet MySQL timing and result acceptance. No production-scale query plan
  is claimed for the new search paths.
- Orders still lacks a created-time range and provider event timeline.
  Subscriptions still lacks an explicit expiration range. Responsive,
  keyboard and assistive-technology acceptance for the expanded filter bars
  remains paused until the user explicitly lifts the IAB restriction.
- The retained disposable integration environment and protected tunnel on
  port 18089 remain a separate cleanup gap.

Intranet synchronization outcome:

- `scripts/sync-intranet.ps1 -SkipLocalChecks` was attempted after the full
  backend/frontend and documentation gates. The bounded archive reached
  `scp`, then failed because the restricted context could not resolve the
  `gitlab` SSH alias. No source archive was uploaded and zero
  `zboard-intranet-sync-*` temporary directories remain.
- No remote candidate, database backup, previous-source path, release archive,
  migration, deployed version or container change was created. There is
  therefore no new backup or previous-source path to record for this phase.
- Independent non-browser requests after the failed upload returned HTTP 200
  from `/api/v1/version` and `/readyz`. Production remains
  `v0.0.1-20260722T183245Z-intranet-working-tree@2026-07-22T18:32:45Z`;
  readiness reports `db: true` and `ready: true`.
- The retained disposable environment also returned HTTP 200 from `/readyz`
  through `127.0.0.1:18089` with `db: true` and `ready: true`; `netstat`
  reports SSH PID 8868 listening on IPv4 and IPv6 loopback. Container health,
  a new deployed version and goal-specific deployed Orders/Subscriptions
  behavior could not be verified because synchronization did not occur.

### 2026-07-24 - Dashboard authoritative action-queue phase

Phase outcome before synchronization:

- Every Dashboard action counter now opens the exact server-backed result set
  represented by that number. Administrator Tickets and Orders expose the
  URL-restorable `status=attention` filter; it maps to
  `open + pending_admin` tickets and `pending + failed` orders respectively.
  The self-service routes reject this administrator-only aggregate value.
- Offline nodes, failed tasks and failed protocol delivery counters now link
  to `connector=offline`, `status=3` and `deployment=failed` respectively.
  A dedicated frontend route map and test lock the five destinations.
- Dashboard no longer counts every historical failed protocol deployment.
  It selects the latest deployment row per protocol endpoint and counts only
  endpoints whose current deployment result remains failed, matching the
  Protocols workbench filter. A later successful delivery therefore removes
  the endpoint from the action queue.
- OpenAPI documents the administrator aggregate values separately from the
  exact self-service status enums. Orders and TicketCenter expose explicit
  work-queue options while retaining every exact status filter.

Local verification before synchronization:

- Frontend type checking passed. The config-free Vitest suite passed 40 files
  and 79 tests, including the exact Dashboard destination contract. The
  production build transformed 491 modules.
- Production gzip sizes were 32.92 KiB for the entry, 42.43 KiB for Vue,
  103.34 KiB for shared UI and 11.33 KiB for base CSS. Dashboard was
  3.11 KiB, Orders 5.32 KiB and TicketCenter 7.23 KiB JavaScript.
- Focused OpenAPI and handler packages passed, followed by `go test ./...`
  and `go vet ./...`. Backend tests cover administrator aggregate expansion,
  exact-status passthrough and rejection of `attention` outside the
  administrator scope; OpenAPI tests require the same aggregate enum.
- No IAB was started, controlled, closed or finalized. Browser acceptance
  remains paused under the confirmed app-crash restriction.

Remaining gaps before synchronization:

- Authenticated intranet MySQL verification must confirm each Dashboard count
  equals the linked result total and that resolving or successfully retrying
  an item removes it from the queue. The current latest-deployment subquery
  also requires production-distribution timing.
- Responsive, keyboard and assistive-technology acceptance for the five
  Dashboard-to-workbench journeys remains paused until the user explicitly
  lifts the IAB restriction.
- The retained disposable integration environment and protected tunnel on
  port 18089 remain a separate cleanup gap.

Intranet synchronization outcome:

- `scripts/sync-intranet.ps1 -SkipLocalChecks` was attempted after all local
  backend/frontend and documentation gates. The bounded archive reached
  `scp`, then failed because the restricted context could not resolve the
  `gitlab` SSH alias. No source archive was uploaded and zero
  `zboard-intranet-sync-*` temporary directories remain.
- No remote candidate, database backup, previous-source path, release archive,
  migration, deployed version or container change was created. There is
  therefore no new backup or previous-source path to record for this phase.
- Independent non-browser requests after the failed upload returned HTTP 200
  from production `/api/v1/version` and `/readyz`. Production remains
  `v0.0.1-20260722T183245Z-intranet-working-tree@2026-07-22T18:32:45Z`;
  readiness reports `db: true` and `ready: true`.
- The retained disposable environment returned HTTP 200 from
  `/api/v1/version` and `/readyz` through `127.0.0.1:18089`; readiness reports
  `db: true` and `ready: true`. `netstat` still reports SSH PID 8868 listening
  on IPv4 and IPv6 loopback. Container health, a new deployed version and
  goal-specific deployed Dashboard/action-queue behavior could not be
  verified because synchronization did not occur.

### 2026-07-24 - Business date-range and shared date-input phase

Phase outcome before synchronization:

- Administrator Orders accepts an inclusive `created_from + created_to`
  creation-date window. Administrator Subscriptions accepts an inclusive
  `expires_from + expires_to` expiration-date window. Both ranges combine
  with the existing search, status, business-enum, exact-user and page state,
  and both restore from the URL.
- Date range pairs are administrator-only, must be supplied together, use UTC
  natural-day boundaries, include the complete end date and cannot exceed
  366 days. Invalid, reversed, incomplete and oversized ranges return HTTP
  400 instead of silently changing scope.
- Migration `0024_business_date_filter_indexes` adds
  `orders(created_at,id)` and `subscriptions(end_at,id)` indexes for the new
  server filters. Its down migration removes only those two named indexes.
- Orders, Subscriptions, Traffic, OperationLogs and AuditLogs now share one
  `DateRangeFilter`. It visibly names the field and UTC basis, explains the
  inclusive end-date contract, binds reciprocal min/max values and preserves
  identical input geometry instead of leaving two unlabeled date controls to
  each page.

Local verification before synchronization:

- Frontend type checking passed. Config-free Vitest passed 41 files and 80
  tests, including visible date context, min/max propagation and two-way
  range updates. The production build transformed 494 modules.
- Production gzip sizes were 33.02 KiB for the entry, 42.43 KiB for Vue,
  103.34 KiB for shared UI and 11.33 KiB for base CSS. The shared date-range
  chunk was 0.76 KiB; Orders was 5.50 KiB, Subscriptions 4.84 KiB, Traffic
  4.51 KiB, OperationLogs 4.20 KiB and AuditLogs 3.79 KiB JavaScript.
- Focused handler, datastore and OpenAPI packages passed, followed by
  `go test ./...` and `go vet ./...`. Tests cover absent ranges, inclusive
  date ends, pair requirements, maximum windows, embedded migration parsing
  and required OpenAPI query parameters.
- No IAB was started, controlled, closed or finalized. Browser acceptance
  remains paused under the confirmed app-crash restriction.

Remaining gaps before synchronization:

- Migration `0024` and both date-range queries require real intranet MySQL
  migration, query-plan and production-distribution timing. No deployed index
  or performance claim is made from local parsing and build checks.
- Responsive, keyboard and assistive-technology acceptance for the shared
  range component and the two expanded business filter bars remains paused
  until the user explicitly lifts the IAB restriction.
- Orders still lacks a normalized provider payment-event timeline. The
  retained disposable integration environment and protected tunnel on port
  18089 remain separate gaps.

Intranet synchronization outcome:

- `scripts/sync-intranet.ps1 -SkipLocalChecks` was attempted after the full
  backend/frontend, migration and documentation gates. The bounded archive
  reached `scp`, then failed because the restricted context could not resolve
  the `gitlab` SSH alias. No source archive was uploaded and zero
  `zboard-intranet-sync-*` temporary directories remain.
- No remote candidate, database backup, previous-source path, release archive,
  migration, deployed version or container change was created. There is
  therefore no new backup or previous-source path to record for this phase.
- Independent non-browser requests after the failed upload returned HTTP 200
  from production `/api/v1/version` and `/readyz`. Production remains
  `v0.0.1-20260722T183245Z-intranet-working-tree@2026-07-22T18:32:45Z`;
  readiness reports `db: true` and `ready: true`.
- The retained disposable environment returned HTTP 200 from
  `/api/v1/version` and `/readyz` through `127.0.0.1:18089`; readiness reports
  `db: true` and `ready: true`. `netstat` reports SSH PID 8868 listening on
  IPv4 and IPv6 loopback. Container health, migration `0024`, a new deployed
  version and goal-specific date-filter behavior could not be verified
  because synchronization did not occur.

### 2026-07-24 - Safe paged payment-event timeline phase

Phase outcome before synchronization:

- Administrator order detail now loads payment events from the independent
  `/api/v1/admin/orders/:id/payment-events` page contract. The drawer keeps
  the main order snapshot usable while event rows load, fail or paginate.
- The payment-event DTO contains only event ID, provider, provider event
  reference, normalized event type, amount, signature result, processed time
  and received time. Provider callback `payload`, internal `order_id` and any
  arbitrary raw callback body never enter the response.
- Orders renders a compact event table instead of cards. Signature outcomes
  use complete icon-plus-label badges, amounts use the currency formatter,
  timestamps use `TimeBadge`, and provider references/event types pass through
  the shared output normalization and Unicode-safe truncation boundary.
- Event page and page-size state are scoped to the open order in the URL.
  Opening another order resets the child page, closing detail removes all
  event query keys, and event-list requests retain stale-response cancellation
  and out-of-range correction through `useRemoteTable`.

Local verification before synchronization:

- Frontend type checking passed. Config-free Vitest passed 41 files and 80
  tests. The production build transformed 494 modules.
- Production gzip sizes were 33.05 KiB for the entry, 42.43 KiB for Vue,
  103.34 KiB for shared UI and 11.33 KiB for base CSS. Orders increased to
  6.33 KiB JavaScript; the shared date-range chunk remained 0.76 KiB.
- Focused handler, router and OpenAPI packages passed, followed by
  `go test ./...` and `go vet ./...`. The projection test proves that callback
  payload content, an Authorization fragment and internal order ID are absent
  while the bounded processing fields remain. OpenAPI tests require the new
  route and both payment-event schemas.
- No IAB was started, controlled, closed or finalized. Browser acceptance
  remains paused under the confirmed app-crash restriction.

Remaining gaps before synchronization:

- Real intranet MySQL must verify event counts, ordering, empty orders,
  malformed provider strings, large event histories and query timing. No
  deployed payment-event behavior is claimed from source and unit evidence.
- Responsive table scrolling, child pagination, URL back/forward behavior,
  error recovery, keyboard order and assistive-technology announcements remain
  paused until the user explicitly lifts the IAB restriction.
- The retained disposable integration environment and protected tunnel on
  port 18089 remain separate cleanup gaps.

Synchronization outcome:

- `scripts/sync-intranet.ps1 -SkipLocalChecks` reached the source-archive upload
  step, then failed because the configured `gitlab` SSH host could not be
  resolved. No archive was uploaded and the script removed its local
  `zboard-intranet-sync-*` temporary directory; the observed temporary
  directory count was zero afterward.
- Synchronization did not create a remote candidate, database backup,
  previous-source snapshot or deployed archive. It did not run migration
  `0024`, replace the deployed source, restart containers or change the
  deployed version.
- The unchanged production service returned HTTP 200 from `/api/v1/version`
  with
  `v0.0.1-20260722T183245Z-intranet-working-tree@2026-07-22T18:32:45Z` and
  HTTP 200 from `/readyz` with `db: true` and `ready: true`.
- The retained disposable environment returned HTTP 200 from
  `/api/v1/version` and `/readyz` through `127.0.0.1:18089`; readiness reported
  `db: true` and `ready: true`. `netstat` reported SSH PID 8868 listening on
  IPv4 and IPv6 loopback. The deployed payment-event route, safe response
  projection and pagination remain unverified because synchronization did not
  occur.

### 2026-07-24 - Node-group filtered membership snapshot phase

Phase outcome before synchronization:

- The node-group member picker now distinguishes the bounded visible search
  list from the complete filtered scope. It continues to render at most 25
  search results and 50 selected members per nested page, while “add all” and
  “remove from pending members” resolve one server-side snapshot instead of
  downloading endpoint detail pages.
- `GET /api/v1/admin/protocol-endpoints/selection` applies the same endpoint
  search, node, protocol, active and latest-deployment filters as the list
  handler. It returns only sorted endpoint IDs, total and `resolved_at`; name,
  address, configuration, usage and deployment detail are intentionally
  absent. The snapshot is capped at 10000 IDs, which covers the required 5000
  endpoint baseline. A larger result or a search longer than 100 UTF-8 bytes
  returns a structured `q` field error and requires a narrower filter.
- The picker cancels stale search and selection requests. Applying a snapshot
  mutates only the unsaved member set and announces the resolved total and
  changed count. Removing existing members is not silently committed: the
  final node-group save enters the shared danger confirmation and describes
  the plan, subscription, credential and node-configuration impact.
- The resource boundary remains explicit. A filter snapshot is transient
  operator input and does not create a dynamic group or second persistence
  model; saving still validates active endpoints once and writes explicit
  `node_group_endpoints` rows in 500-row batches.
- `scripts/verify-scale-intranet.ps1` now checks the default 5000 endpoint
  snapshot total, ID count and absence of endpoint detail fields when the
  isolated environment can run. OpenAPI declares the route, five filters,
  ID-only schema, 10000-item cap and snapshot time.

Local verification before synchronization:

- Frontend type checking passed. The focused member-picker suite passed both
  tests, including a complete server-resolved add/remove flow without result
  page downloads. Config-free Vitest then passed 41 files and 81 tests.
- The config-free production build transformed 494 modules. Gzip sizes were
  33.43 KiB for the entry, 42.43 KiB for Vue, 103.04 KiB for shared UI,
  11.33 KiB for base CSS and 7.01 KiB for NodeGroups JavaScript.
- Focused handler, server and OpenAPI packages passed after the first cold
  aggregate command exceeded its 120-second runner limit. The split command
  completed successfully, followed by passing `go test ./...` and
  `go vet ./...`.
- The backend test serializes 5000 IDs and proves that the snapshot contains
  no endpoint detail fields. OpenAPI tests require the route and enforce the
  10000-item schema cap. `git diff --check` reported no whitespace errors
  (only existing Windows LF-to-CRLF notices), and the temporary config-free
  verification entry was deleted.
- No IAB was started, controlled, closed or finalized. Browser acceptance
  remains paused under the confirmed app-crash restriction.

Remaining gaps before synchronization:

- The updated scale assertion has not run against isolated intranet MySQL
  because the configured SSH target is unresolved. Real 5000/10000-ID snapshot
  latency, memory use and subsequent 5000-member replacement timing remain
  unverified.
- Node-group membership updates still lack optimistic concurrency. Two
  administrators can load the same complete membership and the later save can
  replace the earlier save; this remains a separate contract gap.
- Responsive action wrapping, screen-reader announcements, cancellation during
  a real slow request, save confirmation and keyboard focus recovery remain
  paused until the user explicitly lifts the IAB restriction.
- The retained disposable integration environment and protected tunnel on
  port 18089 remain separate cleanup gaps.

Synchronization outcome:

- `scripts/sync-intranet.ps1 -SkipLocalChecks` reached `scp`, then failed
  because the configured `gitlab` SSH host could not be resolved. No source
  archive was uploaded and the script removed its local temporary directory;
  zero `zboard-intranet-sync-*` directories remained afterward.
- No remote candidate, database backup, previous-source snapshot, deployed
  archive, migration, source replacement, container restart or deployed
  version was created or changed. There is no new backup or previous-source
  path to record for this phase.
- The unchanged production service returned HTTP 200 from `/api/v1/version`
  with
  `v0.0.1-20260722T183245Z-intranet-working-tree@2026-07-22T18:32:45Z` and
  HTTP 200 from `/readyz` with `db: true` and `ready: true`.
- The retained disposable environment returned HTTP 200 from
  `/api/v1/version` and `/readyz` through `127.0.0.1:18089`; readiness reported
  `db: true` and `ready: true`. `netstat` reported SSH PID 8868 listening on
  IPv4 and IPv6 loopback. The deployed selection route, 5000-ID snapshot,
  member-picker interaction and save confirmation remain unverified because
  synchronization did not occur.

### 2026-07-24 - Node-group optimistic concurrency phase

Phase outcome before synchronization:

- Migration `0025_node_group_revision` adds a non-null revision with default 1
  to every node group. The model, list summary, detail response, frontend
  contract and OpenAPI all expose that version.
- Node-group updates now require `expected_revision`. A missing precondition
  returns HTTP 428 with the current version. The handler locks the node-group
  row inside the same transaction that validates status and endpoint
  membership, replaces associations and increments revision. A stale version
  returns HTTP 409 with the current revision before either fields or complete
  membership can be overwritten.
- Enabling a previously disabled group without submitting members now also
  verifies that the existing association set contains an active endpoint.
  Empty membership and active-plan constraints execute through the locked
  transaction rather than from a stale pre-transaction copy.
- NodeGroups displays the revision as an icon label and the update time through
  `TimeBadge`. The update payload includes the loaded revision. HTTP 409/428
  locks the save action and leaves a persistent warning with an explicit
  “reload latest version” action; reloading replaces fields and complete
  membership, clears the conflict and establishes a new dirty baseline.
- The isolated scale script now creates a bounded node-group fixture, proves a
  missing precondition returns 428, lets the first administrator advance
  revision 1 to 2, requires the second administrator's stale revision to return
  409, and verifies that the accepted description remains. The fixture is
  removed afterward. These database assertions are committed to the script but
  have not run because the intranet SSH target is unresolved.

Local verification before synchronization:

- Frontend type checking passed. Config-free Vitest passed 41 files and 81
  tests. The config-free production build transformed 494 modules.
- Production gzip sizes were 33.43 KiB for the entry, 42.43 KiB for Vue,
  103.04 KiB for shared UI and 11.33 KiB for base CSS. NodeGroups was 7.50 KiB
  JavaScript and 1.08 KiB CSS.
- Focused handler, model, migration, OpenAPI and server packages passed,
  followed by `go test ./...` and `go vet ./...`. Datastore migration tests
  recognized the new ordered up/down pair; OpenAPI tests require revision,
  mandatory `expected_revision`, and the 409/428 responses.
- The scale script passed PowerShell parser validation. `git diff --check`
  reported no whitespace errors (only Windows LF-to-CRLF notices), and the
  temporary config-free frontend verification entry was deleted.
- No IAB was started, controlled, closed or finalized. Browser acceptance
  remains paused under the confirmed app-crash restriction.

Remaining gaps before synchronization:

- The two-administrator database assertion, migration `0025`, row locking and
  5000-member replacement still require the isolated intranet MySQL run. Local
  compilation and schema tests do not prove live InnoDB conflict behavior.
- Conflict warning layout, reload focus, dirty-state replacement, keyboard
  operation and assistive-technology announcements remain paused until the
  user explicitly lifts the IAB restriction.
- The retained disposable integration environment and protected tunnel on
  port 18089 remain separate cleanup gaps.

Synchronization outcome:

- `scripts/sync-intranet.ps1 -SkipLocalChecks` reached the source-archive
  upload, then failed because the configured `gitlab` SSH host could not be
  resolved. No archive was uploaded and zero `zboard-intranet-sync-*`
  temporary directories remained afterward.
- No remote candidate, database backup, previous-source snapshot, deployed
  archive, migration, source replacement, container restart or version change
  occurred. Migration `0025` therefore did not run and there is no new backup
  or previous-source path to record.
- The unchanged production service returned HTTP 200 from `/api/v1/version`
  with
  `v0.0.1-20260722T183245Z-intranet-working-tree@2026-07-22T18:32:45Z` and
  HTTP 200 from `/readyz` with `db: true` and `ready: true`.
- The retained disposable environment returned HTTP 200 from
  `/api/v1/version` and `/readyz` through `127.0.0.1:18089`; readiness reported
  `db: true` and `ready: true`. `netstat` reported SSH PID 8868 listening on
  IPv4 and IPv6 loopback. Deployed revision responses, 428/409 behavior,
  InnoDB locking and frontend conflict recovery remain unverified because
  synchronization did not occur.

### 2026-07-24 - Traffic reconciliation full-scope aggregate phase

Phase outcome before synchronization:

- The administrator reconciliation endpoint now returns an authoritative
  `aggregates` object with subscription, matched, missing-record and
  over-recorded counts plus subscription, recorded, missing and over-recorded
  byte totals. The aggregate query covers the complete `user_id` and
  `subscription_id` scope and intentionally ignores the current offset page
  and `issues_only` queue mode.
- `issues_only` is parsed as a strict boolean. Invalid values return HTTP 400;
  `true` still limits the queue rows and queue total, while the aggregate
  overview remains stable so switching between “仅异常” and “全部结果” does not
  change the operator's complete-scope totals.
- Traffic now renders four compact metric cards before the reconciliation
  table. Counts are direct formatted numbers, byte totals use the shared byte
  formatter, status is carried by icon labels, and the workbench explicitly
  explains that node, endpoint and date filters apply only to the record table.
- The typed frontend and OpenAPI contracts define the reconciliation item and
  all eight aggregate fields. The paged OpenAPI envelope requires both
  `items` and `aggregates`.
- The isolated scale script seeds one matched, one missing-record and one
  over-recorded subscription for a dedicated administrator. It compares
  `issues_only=true` with `false`, requires queue totals 2 and 3 respectively,
  and requires the same aggregate counts `3/1/1/1` and byte totals
  `5000/6000/2000/3000` in both responses.

Local verification before synchronization:

- Frontend type checking passed. Config-free Vitest passed 41 files and 81
  tests, and the config-free production build transformed 494 modules.
- Production gzip sizes were 33.42 KiB for the entry, 42.43 KiB for Vue,
  103.04 KiB for shared UI, 11.33 KiB for base CSS and 4.76 KiB for Traffic
  JavaScript.
- Focused handler and OpenAPI tests passed, followed by `go test ./...` and
  `go vet ./...`. The handler test fixes the aggregate JSON field contract and
  OpenAPI tests require the aggregate schema and page references.
- `scripts/verify-scale-intranet.ps1` passed PowerShell parser validation.
  `git diff --check` reported no whitespace errors (only Windows LF-to-CRLF
  notices). The config-free verification entry was deleted and the temporary
  Go build cache left no tracked workspace residue.
- No IAB was started, controlled, closed or finalized. Browser acceptance
  remains paused under the confirmed app-crash restriction.

Remaining gaps before synchronization:

- The deterministic reconciliation fixture and aggregate SQL have not yet run
  against isolated intranet MySQL. Local compilation and schema tests do not
  prove the live MySQL derived-table aggregate or its query cost at scale.
- Visual density, responsive wrapping, keyboard navigation and assistive
  technology output for the new summary remain unverified until the user
  explicitly lifts the IAB restriction.
- The retained disposable integration environment and protected tunnel on
  port 18089 remain separate cleanup gaps.

Synchronization outcome:

- `scripts/sync-intranet.ps1 -SkipLocalChecks` reached the source-archive
  upload, then failed because the configured `gitlab` SSH host could not be
  resolved. No archive was uploaded and zero `zboard-intranet-sync-*`
  temporary directories remained afterward.
- No remote candidate, database backup, previous-source snapshot, deployed
  archive, migration, source replacement, container restart or version change
  occurred. There is no new backup or previous-source path to record for this
  phase.
- Independent non-browser requests after the failed upload returned HTTP 200
  from production `/api/v1/version` and `/readyz`. Production remains
  `v0.0.1-20260722T183245Z-intranet-working-tree@2026-07-22T18:32:45Z`;
  readiness reports `db: true` and `ready: true`.
- The retained disposable environment returned HTTP 200 from
  `/api/v1/version` and `/readyz` through `127.0.0.1:18089`; readiness reports
  `db: true` and `ready: true`. `netstat` reports SSH PID 8868 listening on
  IPv4 and IPv6 loopback. Container health, the deterministic MySQL
  reconciliation fixture, a new deployed version and goal-specific deployed
  aggregate behavior could not be verified because synchronization did not
  occur.

### 2026-07-24 - Semantic administrator event output phase

Phase outcome before synchronization:

- Audit and operation histories now translate known persisted action codes into
  concise Chinese domain labels. The raw action code remains visible beside the
  label so administrators retain an exact troubleshooting and filtering key.
- System actors and typed targets are localized. Numeric target identifiers
  remain direct numbers, while unknown actions and target types are explicitly
  marked as unknown instead of receiving an invented interpretation.
- Persisted protocol revision, task progress and node-kernel summaries are
  converted into operator-facing output with formatted counts and mapped
  lifecycle actions. Unrecognized free-form summaries still pass through the
  shared normalization and length cap so historical evidence is preserved
  safely.
- The conversion rules live in the shared `adminDisplay` utility and are used
  consistently by both AuditLogs and OperationLogs. Focused unit coverage fixes
  the known, dynamic and unknown-value behavior.

Local verification before synchronization:

- Frontend type checking passed. The config-free Vitest run passed 42 files and
  84 tests, and the config-free production build transformed 495 modules.
- Production gzip sizes were 33.45 KiB for the entry, 42.43 KiB for Vue,
  103.04 KiB for shared UI, 11.33 KiB for base CSS and 1.77 KiB for the shared
  semantic display utility. AuditLogs was 3.84 KiB JavaScript and 0.65 KiB CSS;
  OperationLogs was 4.16 KiB JavaScript and 0.38 KiB CSS.
- `git diff --check` reported no whitespace errors (only a Windows LF-to-CRLF
  notice), and the temporary config-free frontend verification entry was
  deleted. No backend contract changed in this phase, so frontend type,
  behavior and production-bundle checks were the proportionate local
  verification.
- No IAB was started, controlled, closed or finalized. Browser acceptance
  remains paused under the confirmed app-crash restriction.

Remaining gaps before synchronization:

- Real browser verification of table density, responsive wrapping, focus
  recovery and assistive-technology announcements remains paused until the
  user explicitly lifts the IAB restriction.
- Historical or producer-specific free-form node summaries that do not match a
  proven contract remain normalized raw evidence; the UI deliberately does not
  guess their meaning.
- New backend action producers will require a corresponding shared mapping.
  Unknown-value labels keep that maintenance gap visible to operators.
- The retained disposable integration environment and protected tunnel on
  port 18089 remain separate cleanup gaps.

Synchronization outcome:

- `scripts/sync-intranet.ps1 -SkipLocalChecks` reached the source-archive
  upload, then failed because the configured `gitlab` SSH host could not be
  resolved. No archive was uploaded and zero `zboard-intranet-sync-*`
  temporary directories remained afterward.
- No remote candidate, database backup, previous-source snapshot, deployed
  archive, migration, source replacement, container restart or version change
  occurred. There is no new backup or previous-source path to record for this
  phase.
- Independent non-browser requests after the failed upload returned HTTP 200
  from production `/api/v1/version` and `/readyz`. Production remains
  `v0.0.1-20260722T183245Z-intranet-working-tree@2026-07-22T18:32:45Z`;
  readiness reports `db: true` and `ready: true`.
- The retained disposable environment returned HTTP 200 from
  `/api/v1/version` and `/readyz` through `127.0.0.1:18089`; readiness reports
  `db: true` and `ready: true`. Remote container health and the deployed
  semantic AuditLogs/OperationLogs behavior could not be verified because
  synchronization did not occur.

### 2026-07-24 - Bounded self-service history and dashboard phase

Phase outcome before synchronization:

- The current-user order, subscription and traffic routes now honor
  `paged=true` as well as their existing administrator counterparts. Identity
  scope is established before filtering and counting, including when the
  current user is also an administrator. Requests without `paged=true` retain
  the legacy array response for compatibility.
- Self-service orders and subscriptions use bounded offset pages. Subscription
  pages include plan and SKU display names without returning subscription
  config. Self-service traffic uses the same stable bidirectional cursor,
  validated date window and complete filtered-window aggregates as the
  administrator history while continuing to omit report IDs, event metadata
  and payload fields. Upload and download byte counts were added to the safe
  list projection because they are required account-facing output.
- AccountOrders and AccountSubscription now use the shared remote workbench,
  server totals, page-size controls and URL-restorable status/page state.
  Subscription rows replace the unbounded repeated card list, while credential
  management remains a separate focused section.
- AccountTraffic now uses a seven-day default range, URL-restorable dates,
  subscription filter and cursor state. It renders only the current bounded
  page and exposes complete range totals separately from the page.
- AccountDashboard no longer downloads every order, subscription and plan to
  calculate three numbers. It loads three recent active subscriptions, three
  recent orders and a one-row pending-order query while taking each complete
  count from the server page envelope.
- The isolated scale script now uses an administrator identity through the
  self-service routes to prove that identity scope is retained. It seeds three
  orders, three subscriptions and three traffic records; requires bounded
  two-row pages with totals `3/3/3`, a pending-order total of 1, plan/SKU
  display names, upload/download fields and cursor metadata, and rejects
  config or raw traffic metadata leakage.

Local verification before synchronization:

- Frontend type checking passed. Config-free Vitest passed 43 files and 87
  tests, including a new account scale policy that rejects the old full-array
  fetches. The config-free production build transformed 496 modules.
- Production gzip sizes were 33.43 KiB for the entry, 42.43 KiB for Vue,
  103.04 KiB for shared UI and 11.33 KiB for base CSS. AccountDashboard,
  AccountOrders, AccountTraffic and AccountSubscription were respectively
  2.57, 2.75, 2.97 and 4.37 KiB JavaScript; AccountSubscription added 0.17 KiB
  CSS.
- Focused handler and OpenAPI tests passed, followed by `go test ./...` and
  `go vet ./...`. Handler tests fix the self/admin paging switch and safe
  upload/download projection; OpenAPI tests require paging on self-service
  orders/subscriptions and cursor/date parameters on self-service traffic.
- `scripts/verify-scale-intranet.ps1` passed PowerShell parser validation.
  `git diff --check` reported no whitespace errors (only Windows LF-to-CRLF
  notices), and the temporary config-free frontend verification entry was
  deleted.
- No IAB was started, controlled, closed or finalized. Browser acceptance
  remains paused under the confirmed app-crash restriction.

Remaining gaps before synchronization:

- The self-service identity-isolation fixture and its live MySQL query behavior
  have not run. Local tests and source contracts do not prove the complete
  production database path or response time.
- Responsive table priorities, date-filter interaction, cursor round trips,
  cancel confirmation, copy feedback, focus recovery and assistive-technology
  announcements require real browser acceptance after the user explicitly
  lifts the IAB restriction.
- The public/account plan catalog and public subscription-template selector
  still use active full-array contracts. They are the next self-service scale
  audit rather than being hidden by this phase.
- The retained disposable integration environment and protected tunnel on
  port 18089 remain separate cleanup gaps.

Synchronization outcome:

- `scripts/sync-intranet.ps1 -SkipLocalChecks` reached the source-archive
  upload, then failed because the configured `gitlab` SSH host could not be
  resolved. No archive was uploaded and zero `zboard-intranet-sync-*`
  temporary directories remained afterward.
- No remote candidate, database backup, previous-source snapshot, deployed
  archive, migration, source replacement, container restart or version change
  occurred. There is no new backup or previous-source path to record for this
  phase.
- Independent non-browser requests after the failed upload returned HTTP 200
  from production `/api/v1/version` and `/readyz`. Production remains
  `v0.0.1-20260722T183245Z-intranet-working-tree@2026-07-22T18:32:45Z`;
  readiness reports `db: true` and `ready: true`.
- The retained disposable environment returned HTTP 200 from
  `/api/v1/version` and `/readyz` through `127.0.0.1:18089`; readiness reports
  `db: true` and `ready: true`. `netstat` reports SSH PID 8868 listening on
  IPv4 and IPv6 loopback. Remote container health, the self-service MySQL
  isolation fixture and deployed account-page behavior remain unverified
  because synchronization did not occur.

### 2026-07-24 - Bounded plan SKU administration phase

Phase outcome before synchronization:

- Administrator plan detail responses now return plan policy plus aggregate
  SKU and active-SKU counts. They no longer preload or embed the complete SKU
  collection, so opening one plan has bounded response size.
- The backend exposes a plan-scoped SKU page with validated name/code/currency
  search, strict active-state filtering, offset/limit metadata and stable
  sort-order/identifier ordering. A separate administrator endpoint retrieves
  one SKU by identifier for edit flows.
- The plan detail drawer now contains a nested shared SKU workbench rather than
  a repeated unbounded card collection. Search, active-state filter, page and
  page size are URL-restorable; editing loads only the requested SKU and
  verifies that it belongs to the currently open plan.
- The OpenAPI contract documents the bounded SKU page and single-SKU endpoint
  and rejects a legacy embedded `PlanDetail.skus` property.
- The isolated scale script now generates 5,000 SKUs for a dedicated plan,
  requires a bounded first page and full aggregate count, verifies single-SKU
  retrieval, and rejects SKU codes or arrays leaking into plan detail.

Local verification before synchronization:

- Frontend type checking passed. Config-free Vitest passed 44 files and 89
  tests, including the plan scale policy, and the config-free production build
  transformed 496 modules.
- Production gzip sizes were 33.48 KiB for the entry, 42.43 KiB for Vue,
  103.04 KiB for shared UI, 11.33 KiB for base CSS and 13.28 KiB JavaScript
  plus 0.85 KiB CSS for Plans.
- Focused handler, router and OpenAPI tests passed, followed by
  `go test ./...` and `go vet ./...`.
- `scripts/verify-scale-intranet.ps1` passed both PowerShell parser validation
  and `bash -n` validation of its embedded remote shell program.
  `git diff --check` reported no whitespace errors (only Windows LF-to-CRLF
  notices), and the temporary config-free frontend verification entry was
  deleted.
- No IAB was started, controlled, closed or finalized. Browser acceptance
  remains paused under the confirmed app-crash restriction.

Remaining gaps before synchronization:

- The 5,000-SKU live MySQL fixture and its response-time/query behavior have
  not run. Source contracts and local tests do not prove the complete deployed
  database path.
- Responsive nested-table priorities, filter/page restoration, drawer focus
  recovery and assistive-technology announcements require real browser
  acceptance after the user explicitly lifts the IAB restriction.
- The public and account plan catalogs still use active full-array plan/SKU
  contracts. Public subscription-template selection also consumes that
  unbounded catalog and remains the next scale migration.
- The retained disposable integration environment and protected tunnel on
  port 18089 remain separate cleanup gaps.

Synchronization outcome:

- `scripts/sync-intranet.ps1 -SkipLocalChecks` reached the source-archive
  upload, then failed because the configured `gitlab` SSH host could not be
  resolved. No archive was uploaded and zero `zboard-intranet-sync-*`
  temporary directories remained afterward.
- No remote candidate, database backup, previous-source snapshot, deployed
  archive, migration, source replacement, container restart or version change
  occurred. There is no new backup or previous-source path to record for this
  phase.
- Independent non-browser requests through the retained production tunnel on
  `127.0.0.1:18082` returned HTTP 200 from `/api/v1/version` and `/readyz`.
  Production remains
  `v0.0.1-20260722T183245Z-intranet-working-tree@2026-07-22T18:32:45Z`;
  readiness reports `db: true` and `ready: true`. `netstat` reports SSH PID
  7984 listening on loopback.
- The retained disposable scale environment returned HTTP 200 from
  `/api/v1/version` and `/readyz` through `127.0.0.1:18089`; readiness reports
  `db: true` and `ready: true`. `netstat` reports SSH PID 8868 listening on
  loopback. Remote container health, the 5,000-SKU fixture and deployed plan
  workbench behavior remain unverified because synchronization did not occur.

### 2026-07-24 - Bounded self-service catalog phase

Phase outcome before synchronization:

- `GET /api/v1/plans?paged=true` is now available to public and authenticated
  self-service consumers. It returns active plan summaries, server totals and
  at most one active new-purchase primary SKU per plan; it never embeds the
  complete SKU collection. The legacy unpaged array remains available only
  for compatibility.
- A public active-plan detail route returns the same bounded catalog item. A
  separate public plan-SKU route supplies validated search, SKU-type filtering
  and offset/limit pages while refusing inactive plans and inactive SKUs.
- The public price page now searches and pages six, twelve or twenty-four
  plans at a time. Cards retain the public comparison purpose but use only the
  bounded primary SKU, show the exact number of active choices, and link to
  the selected plan instead of downloading every SKU.
- AccountPlans now uses two independent shared workbenches: a server-paged
  plan table and a server-paged new-purchase SKU table for the selected plan.
  Plan/SKU search and both page states are URL-restorable, deep links fetch
  one bounded plan item, and order confirmation uses only the selected SKU.
- The current-user subscription-template route now supports a bounded active
  metadata page with server search. AccountSubscription loads at most 25
  matching export templates, displays the server total and asks the user to
  refine the query when more matches exist; template source is not returned.
- The scale fixture now seeds 5,000 plan SKUs and 5,000 active subscription
  templates. It requires a one-item public plan search, one primary SKU,
  bounded active-SKU and template pages, complete totals, no embedded SKU
  array and no template-source leakage.

Local verification before synchronization:

- Frontend type checking passed. Config-free Vitest passed 45 files and 92
  tests, including a new self-service catalog scale policy, and the
  config-free production build transformed 497 modules.
- Production gzip sizes were 33.54 KiB for the entry, 42.43 KiB for Vue,
  103.04 KiB for shared UI and 11.33 KiB for base CSS. PublicPlans,
  AccountPlans and AccountSubscription were respectively 2.60, 4.04 and 4.76
  KiB JavaScript; AccountPlans and AccountSubscription added 0.08 and 0.26 KiB
  CSS.
- Focused handler, router and OpenAPI tests passed, followed by
  `go test ./...` and `go vet ./...`. Projection tests require exactly one
  bounded primary SKU and reject an embedded SKU collection; OpenAPI tests
  require the public catalog routes, page schemas and current-user template
  paging switch.
- `scripts/verify-scale-intranet.ps1` passed both PowerShell parser validation
  and `bash -n` validation of its embedded remote shell program.
  `git diff --check` reported no whitespace errors (only Windows LF-to-CRLF
  notices), no frontend view consumes the legacy full-array plan/template
  helpers, and the temporary config-free verifier was deleted.
- No IAB was started, controlled, closed or finalized. Browser acceptance
  remains paused under the confirmed app-crash restriction.

Remaining gaps before synchronization:

- The public catalog's correlated primary-SKU query and the 5,000-SKU/5,000-
  template live MySQL fixture have not run. Local compilation, projections and
  source contracts do not prove deployed query plans or response times.
- Real responsive behavior of the public card pager, account nested tables,
  URL back/forward restoration, search focus, order confirmation and template
  picker announcements requires browser acceptance after the user explicitly
  lifts the IAB restriction.
- Legacy unpaged plan and template arrays remain server-side compatibility
  contracts for unknown external consumers, although no repository frontend
  view calls them.
- The retained disposable integration environment and protected tunnel on
  port 18089 remain separate cleanup gaps.

Synchronization outcome:

- `scripts/sync-intranet.ps1 -SkipLocalChecks` reached the source-archive
  upload, then failed because the configured `gitlab` SSH host could not be
  resolved. No archive was uploaded and zero `zboard-intranet-sync-*`
  temporary directories remained afterward.
- No remote candidate, database backup, previous-source snapshot, deployed
  archive, migration, source replacement, container restart or version change
  occurred. There is no new backup or previous-source path to record for this
  phase.
- Independent non-browser requests through the retained production tunnel on
  `127.0.0.1:18082` returned HTTP 200 from `/api/v1/version` and `/readyz`.
  Production remains
  `v0.0.1-20260722T183245Z-intranet-working-tree@2026-07-22T18:32:45Z`;
  readiness reports `db: true` and `ready: true`. `netstat` reports SSH PID
  7984 listening on loopback.
- The retained disposable scale environment returned HTTP 200 from
  `/api/v1/version` and `/readyz` through `127.0.0.1:18089`; readiness reports
  `db: true` and `ready: true`. `netstat` reports SSH PID 8868 listening on
  loopback. Remote container health, live catalog fixtures and deployed
  self-service catalog behavior remain unverified because synchronization did
  not occur.

### 2026-07-24 - Infrastructure child-list density phase

Phase outcome before synchronization:

- The remaining scale-sensitive infrastructure child collections were
  identified by a route/view/component audit. Node protocol multipliers and
  protocol deployment history were already server-paged but still rendered as
  repeated cards, which prevented quick column comparison and consumed excess
  vertical space.
- The node detail protocol section now uses the shared data table with service,
  status, public entry, multiplier input and save action columns. Each
  multiplier keeps a service-specific accessible label; status remains an icon
  label and the complete child total remains direct numeric output.
- Protocol detail deployment history now uses the shared data table with
  deployment/revision, icon status, normalized bounded output summary, start
  time and finish time columns. Its existing independent server page remains
  intact.
- The audit deliberately retained cards only where the interaction model
  warrants them: public plan comparison, chronological ticket conversation
  and the session-scoped task tray capped at 20 tasks. Repository frontend
  source contains no native form controls or native alert/confirm/prompt
  calls.

Local verification before synchronization:

- Frontend type checking passed. Config-free Vitest passed 46 files and 94
  tests, including a new infrastructure detail-density policy, and the
  config-free production build transformed 497 modules.
- Production gzip sizes remained 33.54 KiB for the entry, 42.43 KiB for Vue,
  103.04 KiB for shared UI and 11.33 KiB for base CSS. Protocols was 15.81 KiB
  JavaScript and 2.10 KiB CSS; Nodes was 19.06 KiB JavaScript and 4.43 KiB CSS.
- One parallel config-free build attempt reached rendering but exposed an
  environment-only Rollup path-emission error. The immediate isolated rerun
  completed successfully in 7.58 seconds; type checking and all tests were
  already green.
- `git diff --check` reported no whitespace errors (only Windows LF-to-CRLF
  notices), and the temporary config-free verifier was deleted.
- No backend contract changed in this phase, so frontend type, behavior,
  policy and production-bundle checks were the proportionate local
  verification. No IAB was started, controlled, closed or finalized.

Remaining gaps before synchronization:

- Real table widths, horizontal overflow priorities, multiplier keyboard
  editing, save focus recovery and output-summary wrapping require browser
  acceptance after the user explicitly lifts the IAB restriction.
- The route/source audit establishes component and bounded-contract policy but
  is not a visual replacement for desktop, narrow-screen, dark-mode or
  assistive-technology review.
- The retained disposable integration environment and protected tunnel on
  port 18089 remain separate cleanup gaps.

Synchronization outcome:

- `scripts/sync-intranet.ps1 -SkipLocalChecks` reached the source-archive
  upload, then failed because the configured `gitlab` SSH host could not be
  resolved. No archive was uploaded and zero `zboard-intranet-sync-*`
  temporary directories remained afterward.
- No remote candidate, database backup, previous-source snapshot, deployed
  archive, migration, source replacement, container restart or version change
  occurred. There is no new backup or previous-source path to record for this
  phase.
- Independent non-browser requests through the retained production tunnel on
  `127.0.0.1:18082` returned HTTP 200 from `/api/v1/version` and `/readyz`.
  Production remains
  `v0.0.1-20260722T183245Z-intranet-working-tree@2026-07-22T18:32:45Z`;
  readiness reports `db: true` and `ready: true`. `netstat` reports SSH PID
  7984 listening on loopback.
- The retained disposable scale environment returned HTTP 200 from
  `/api/v1/version` and `/readyz` through `127.0.0.1:18089`; readiness reports
  `db: true` and `ready: true`. `netstat` reports SSH PID 8868 listening on
  loopback. Remote container health and deployed child-table behavior remain
  unverified because synchronization did not occur.

### 2026-07-24 - Whole-route architecture and filter consistency phase

Phase outcome before synchronization:

- A source-backed architecture policy now enumerates every effective admin
  route instead of treating the Nodes and Protocols samples as proof for the
  whole console. All 15 admin surfaces must retain the shared page header,
  application feedback, semantic status labels and formatted time labels.
- Every admin collection except the non-list Dashboard and Settings surfaces
  must use the shared data workbench, remote offset/cursor ownership and
  URL-restorable state. Comparison lists must use the shared `DataTable`;
  Tickets retains its bounded master/detail queue as an explicit interaction
  exception. Account orders, plans, subscriptions and traffic are checked by
  the same bounded-list policy, while the public plan catalog remains a
  bounded comparison-card exception.
- The source policy rejects native `input`, `textarea` and `select` controls
  and rejects private tables outside `DataTable.vue`. It also requires an
  explicit clear-filter action on every admin and account high-volume list,
  preventing future pages from silently dropping the unified input and list
  contracts.
- Missing clear actions were added to Users, Plans and its SKU page,
  NodeGroups, Tasks, both admin/account Ticket queues, account Orders and
  Subscriptions, and both account plan/SKU searches. Clearing resets the
  offset and URL. If a Ticket filter change is canceled because an unsent
  reply exists, the visible controls now return to the applied URL state
  instead of disagreeing with the unchanged result set.
- Dashboard's bounded recent-deployment preview now uses a compact comparison
  table with endpoint, node, icon status and time columns rather than a
  private dot list. `MetricCard` accepts formatted metadata content, and the
  account traffic summary now renders its as-of time through `TimeBadge`
  instead of a plain formatted string.

Local verification before synchronization:

- Frontend type checking passed. Config-free Vitest passed 48 files and 100
  tests, including the whole-route architecture policy and formatted metric
  metadata coverage. The config-free production build transformed 497
  modules.
- Production gzip sizes were 33.53 KiB for the entry, 42.43 KiB for Vue,
  103.04 KiB for shared UI and 11.33 KiB for base CSS. Dashboard and
  AccountTraffic were 3.30 and 2.97 KiB JavaScript; Nodes and Protocols
  remained 19.06 and 15.81 KiB.
- `go test ./...` and `go vet ./...` passed with the pinned Go 1.26.5
  toolchain. `git diff --check` reported no whitespace errors, only the
  existing Windows LF-to-CRLF notices. The temporary config-free verifier was
  deleted after use.
- Static source inspection found no native Vue form controls and only the
  shared `DataTable.vue` owns a native table. No IAB was started, controlled,
  closed or finalized.

Remaining gaps before synchronization:

- The current-run Product Design visual audit cannot be claimed because its
  screenshot contract requires fresh browser capture and inspection. IAB
  remains disabled under the confirmed Codex cleanup crash restriction;
  component, source and build evidence do not replace responsive, keyboard,
  contrast or assistive-technology browser acceptance.
- Production-distribution MySQL baselines, concurrent write checks, long
  ticket/log sessions and the retained scale-environment cleanup remain open
  as recorded in the completion audit. Column presets remain intentionally
  unimplemented until a high-column workbench demonstrates a user need.

Synchronization outcome:

- `scripts/sync-intranet.ps1 -SkipLocalChecks` reached the source-archive
  upload, then failed because the configured `gitlab` SSH host could not be
  resolved. No archive was uploaded and zero `zboard-intranet-sync-*`
  temporary directories remained afterward.
- No remote candidate, database backup, previous-source snapshot, deployed
  archive, migration, source replacement, container restart or version change
  occurred. There is no new backup or previous-source path to record for this
  phase, and remote container health plus goal-specific deployed behavior are
  unverified.
- Independent non-browser requests through the retained production tunnel on
  `127.0.0.1:18082` returned HTTP 200 from `/api/v1/version` and `/readyz`.
  Production remains
  `v0.0.1-20260722T183245Z-intranet-working-tree@2026-07-22T18:32:45Z`;
  readiness reports `db: true` and `ready: true`. `netstat` reports SSH PID
  7984 listening on loopback.
- The retained disposable scale environment returned HTTP 200 from
  `/api/v1/version` and `/readyz` through `127.0.0.1:18089`; it remains
  `v0.0.1-scale-zboard_scale_validation_browser_audit_20260723-working-tree@2026-07-22T18:44:54Z`,
  with `db: true` and `ready: true`. `netstat` reports SSH PID 8868 listening
  on loopback. This retained version predates the current source phase and is
  not evidence for the new route policy or filter behavior.

## 2026-07-24 Admin form navigation safety phase

Phase outcome before synchronization:

- A complete Vue source recount found 19 write forms across 12 owners. Every
  form remains on the shared `FormField`, request-time validation,
  declaration-only API field mapping, per-field error clearing and
  application feedback contract. Login and Register remain intentionally
  free of leave warnings because they are short-lived credential entry forms.
- Added `useUnsavedChangesGuard` as the single route-leave and browser-unload
  owner for persistent business forms. The guard is active only while the
  corresponding editor is open and its dirty snapshot differs from the saved
  baseline.
- Users create/edit, Nodes create/edit/SSH, the Protocol wizard, NodeGroups,
  Plans create/product/SKU, Tasks, SubscriptionTemplates, Ticket
  create/reply, Setup and Settings now protect sidebar/route departure plus
  browser refresh or window closure. Internal editor changes in Plans,
  SubscriptionTemplates and Tickets retain their more specific route-update
  confirmations.
- Route departure continues to use the application confirmation queue.
  Browser unload uses only the standard `beforeunload` lifecycle because an
  application dialog cannot safely delay tab or window destruction. No page
  introduced a native JavaScript `alert`, `confirm` or `prompt`.
- The form source policy now locks the 19-form shared validation contract and
  precisely enumerates the ten persistent form owners that must install the
  shared unsaved-change guard.

Local verification before synchronization:

- Frontend type checking passed. Config-free Vitest passed 48 files and 103
  tests. The guard test covers clean and dirty browser unload, rejected and
  accepted route navigation, and listener removal after unmount.
- The config-free production build transformed 497 modules. Production gzip
  sizes remained 33.53 KiB for the entry, 42.44 KiB for Vue, 103.04 KiB for
  shared UI and 11.33 KiB for base CSS. Nodes and Protocols were 19.23 and
  15.93 KiB.
- Backend `go test ./...` and `go vet ./...` passed with the pinned Go 1.26.5
  toolchain. Static inspection found only shared `DataTable.vue` owns a native
  table and no Vue page owns a native input, textarea or select.
- `git diff --check` reported no whitespace errors, only the existing Windows
  LF-to-CRLF notices. No IAB was started, controlled, closed or finalized.

Remaining gaps before synchronization:

- The IAB cleanup crash restriction remains active. Current-run browser back,
  refresh, keyboard, screen-reader, responsive and high-volume interaction
  acceptance cannot be claimed from source, happy-dom or build evidence.
- Intranet synchronization and deployed version, readiness, container and
  goal-specific behavior verification still need to run for this phase.
- Production-distribution MySQL baselines, long ticket/log sessions,
  concurrent update checks and retained scale-environment cleanup remain open
  as recorded in the completion audit.

Synchronization outcome:

- `scripts/sync-intranet.ps1 -SkipLocalChecks` failed while uploading the
  source archive because SSH could not resolve hostname `gitlab`.
  `ssh -G gitlab` still resolved to the literal hostname `gitlab`, port 22,
  rather than a reachable host address. The failure occurred before any
  archive upload.
- No remote candidate, database backup, previous-source snapshot, deployed
  archive, migration, source replacement, container restart or version
  change occurred. There is no new backup or previous-source path to record.
  The synchronization script left zero local `zboard-intranet-sync-*`
  temporary directories.
- The retained production tunnel on `127.0.0.1:18082` still returned HTTP 200
  for `/api/v1/version` and `/readyz`. It remains
  `v0.0.1-20260722T183245Z-intranet-working-tree@2026-07-22T18:32:45Z` with
  `db: true` and `ready: true`; PID 7984 owns the listener.
- The retained scale tunnel on `127.0.0.1:18089` still returned HTTP 200 for
  both endpoints. It remains
  `v0.0.1-scale-zboard_scale_validation_browser_audit_20260723-working-tree@2026-07-22T18:44:54Z`
  with `db: true` and `ready: true`; PID 8868 owns the listener.
- Both retained environments predate this source phase. Their health is not
  evidence that the shared unsaved-change guard or updated form policy is
  deployed. Deployed container health and goal-specific behavior remain
  unverified until SSH resolution is restored and synchronization succeeds.

Follow-up form-summary and submit-semantics outcome:

- Per-form source inspection found one remaining contract gap after the route
  guard phase: Setup's site and administrator forms exposed field errors but
  did not render the shared form-level error summary.
- Both Setup forms now render a persistent `PageAlert` from
  `fieldErrors.formError`. The previous separate installation error state was
  removed so unfielded API failures have one authoritative form summary
  instead of duplicate page and form messages.
- Setup's footer actions now associate with explicit site/admin form IDs and
  are real submit controls. Keyboard Enter and pointer activation therefore
  use the same validation and submit path.
- The form source policy now checks each of all 19 forms, rather than only its
  owner file, for both a shared alert and form-error summary. A UiButton
  component assertion locks `form` and `type` attribute forwarding.

Follow-up local verification before synchronization:

- Frontend type checking passed. Config-free Vitest passed 48 files and 104
  tests, and the config-free production build transformed 497 modules.
- Production gzip sizes were 33.54 KiB for the entry, 42.44 KiB for Vue,
  103.04 KiB for shared UI and 11.33 KiB for base CSS. Setup remained 3.70
  KiB gzip.
- Backend `go test ./...` and `go vet ./...` passed with the pinned Go 1.26.5
  toolchain. The current-run browser interaction and screen-reader gap remains
  unchanged because IAB is still disabled.

Follow-up synchronization outcome:

- The required second `scripts/sync-intranet.ps1 -SkipLocalChecks` attempt
  again failed before upload because hostname `gitlab` could not be resolved.
  There was no archive upload, backup, previous-source path, candidate image,
  migration, container restart or version change, and zero local sync
  temporary directories remained.
- Production tunnel `127.0.0.1:18082` continued to report the old
  `v0.0.1-20260722T183245Z-intranet-working-tree@2026-07-22T18:32:45Z`
  version with database and readiness true; PID 7984 retained the listener.
- Scale tunnel `127.0.0.1:18089` continued to report the old
  `v0.0.1-scale-zboard_scale_validation_browser_audit_20260723-working-tree@2026-07-22T18:44:54Z`
  version with database and readiness true; PID 8868 retained the listener.
  These are unchanged old-environment checks, not deployment acceptance for
  the Setup or unsaved-change work.

### 2026-07-24 - Core business-list contract and distributed-scale baseline

Goal outcome:

- Re-audited the server and frontend call chains for Users, Subscriptions,
  Orders, Plans, NodeGroups and Traffic. The existing order, plan,
  subscription, node-group and traffic summaries already exclude raw payment
  callbacks, complete SKU/member collections, subscription config and raw
  traffic metadata; the missing common evidence was distributed production-
  shaped scale coverage rather than another summary DTO rewrite.
- The paged administrator user contract now adds four bounded business counts:
  active/all subscriptions and pending/all orders. The server resolves those
  counts with two grouped queries over only the current page of user IDs, never
  one query per row. User search covers email and account name, and the list
  has stable `id`, `email` and `created_at` ordering with an ID tie-breaker.
- Users consumes the new summary directly. Its table shows the two count pairs
  as numbers, formats account creation with `TimeBadge`, exposes creation-time
  sorting in restorable URL state and keeps account status/permission as
  semantic icon labels. Detail-only login and verification facts remain in the
  on-demand user detail response.
- Subscription list and detail reads now calculate the effective status from
  stored status, expiry time and quota without issuing an UPDATE. Dashboard,
  traffic summary and reconciliation reads likewise no longer invoke the
  expiry writer. Durable subscription/credential reconciliation remains owned
  by the existing node-heartbeat path and security-sensitive business write
  paths.
- `scripts/verify-scale-intranet.ps1` now creates distributed fixtures for
  5,000 users, 5,000 node groups, 5,000 plans and SKUs, 10,000 subscriptions
  and 10,000 orders in addition to the existing node, protocol, task, catalog
  and history fixtures. It measures Users, Plans, NodeGroups, Subscriptions,
  Orders and Traffic reconciliation with bounded 50-row responses, a 12-query
  ceiling, a 256 KiB response ceiling, repeated deep-page stability, zero GET
  writes and detail-field leak checks. It also proves an expired subscription
  can be filtered and returned as expired without persistence on the read path.

Local verification:

- Pinned Go 1.26.5 `go test ./...` and `go vet ./...` passed. Handler and
  OpenAPI tests cover the new user summary, effective subscription status,
  documented list fields and user sort parameters.
- Frontend type checking passed. Vitest passed all 48 files and 104 tests;
  the updated architecture policy includes the bounded user business-summary
  contract. The production build transformed 497 modules.
- Production gzip sizes were 33.57 KiB for the entry, 42.44 KiB for Vue,
  103.04 KiB for shared UI and 11.33 KiB for base CSS. Users was 6.64 KiB.
- The scale validator passed PowerShell AST parsing and its embedded remote
  script passed Bash syntax parsing. `git diff --check` reported no whitespace
  errors, only existing Windows LF-to-CRLF notices.

Integration and scale verification:

- The first distributed run exposed a real query-contract defect in
  `TrafficRecordsHandler`: reusing one GORM query object allowed the aggregate
  `SELECT` to contaminate the item query, so a page with `total=3` returned one
  zero-ID synthetic aggregate row. Traffic filters are now parsed once and
  applied through a fresh query factory for aggregate, count and item reads.
  The isolated self-service fixture then returned the requested two items and
  all three user-owned records without leaking report metadata.
- The validator now reports the failing remote line without printing command
  contents, targets the maximum recovery task by its exact subscription IDs,
  excludes the response-envelope timestamp from byte-stability comparison and
  counts all five task fixtures in the merged operation-history total. A
  reduced full-path diagnostic run passed before the default-size run.
- The default isolated MySQL 8.4 run passed with 5,000 users, 5,000 plans and
  SKUs, 5,000 node groups, 10,000 subscriptions, 10,000 orders, 20,006
  reconciliation rows, 1,000 nodes, 5,000 endpoints, 10,000 audit rows,
  10,003 traffic rows and 15,005 merged operation rows.
- The six business pages returned 50 items with 6/6/6/4/4/6 logical queries
  for Users/Plans/NodeGroups/Subscriptions/Orders/Reconciliation, below the
  12-query ceiling. Their response sizes were 11,940 / 20,845 / 15,222 /
  29,842 / 18,151 / 7,339 bytes, all below 256 KiB. Repeated deep pages were
  stable after excluding the envelope timestamp, detail-only fields remained
  absent, and MySQL general-log evidence showed zero write statements for
  every GET.
- Effective expired-status filtering returned 1,899 distributed subscriptions
  without persisting changes. The maximum 10,000-target recovery completed in
  94.958 seconds with 10,000 quota events and adjustments, one task-run audit
  and zero duplicate execution. Nodes and endpoints stayed at 6/9 queries;
  audit, traffic and operation histories stayed at 4/5/8 queries with cursor
  round trips and no adjacent-page overlap.
- The validation Compose project
  `zboard_scale_validation_full_20260724` removed its containers, volume,
  network and image. A separate remote inventory check confirmed none of
  those resources remained.

Deployment evidence:

- The final verified working tree, including the scale validator and audit
  evidence, was synchronized to the intranet as
  `v0.0.1-20260724T052138Z-intranet-working-tree@2026-07-24T05:21:38Z`.
  `/readyz` returned `ready=true, db=true`; `zboard_next-zboard-1`, MySQL and
  Redis were healthy.
- The database backup is
  `/data/zboard-next/backups/20260724T052138Z/zboard-before-sync.sql`, the
  previous source is `/data/zboard-next/app-prev-20260724T052138Z`, and the
  release archive is
  `/data/zboard-next/releases/20260724T052138Z/source.tar.gz`.
- Authenticated deployed checks for Users, Plans, NodeGroups, Subscriptions,
  Orders and Reconciliation all returned HTTP 200 with their required summary
  marker and without the tested detail-only fields. Responses for one row were
  437 / 538 / 449 / 744 / 541 / 519 bytes.
- Final pinned Go 1.26.5 `go test ./...` and `go vet ./...` passed after the
  traffic query fix. The scale validator passed PowerShell AST parsing. The
  already completed frontend gates remain 48 Vitest files / 104 tests, type
  checking and the 497-module production build.

Remaining gaps:

- IAB remains disabled because its cleanup path can terminate the Codex AppX
  container. No IAB tab was opened, closed or finalized; this phase therefore
  does not claim new live browser or screen-reader acceptance.
- Long-running production-distribution load, node-group large-member write
  timing, dual-administrator node-group integration and plan edit concurrency
  remain later milestones; they do not block the completed business-list
  contract and frontend migration baseline.

### 2026-07-24 - Infrastructure control contract and minimum protocol workflow

Goal outcome:

- The infrastructure workbenches still use the shared PrimeVue-backed
  `UiInput`, `UiSelect`, number, lookup and date components; the apparent
  native-control drift came from sizing only direct `.p-inputtext` and
  `.p-select` children. The shared workbench contract now includes wrapped
  search, date-range, numeric, autocomplete and button controls at one 36px
  compact height. Standard form inputs use the 40px control token, lookup
  roots fill their `FormField`, and note/hint rows retain the same reserved
  message structure.
- Protocol configuration now has a minimum template-like workflow without
  creating a second runtime resource: any service can be copied into an
  independent inactive draft, renamed and assigned to another node. Existing
  services can switch nodes after an explicit destructive confirmation.
  Runtime endpoints remain bound to exactly one node when saved so publishing,
  credentials and traffic attribution stay unambiguous.
- A protocol node switch moves the endpoint's credentials transactionally.
  Shared-port credentials follow the new listen/public ports; Shadowsocks
  credentials receive collision-checked dedicated ports on the target node.
  The publisher now honors its explicit node target so both the previous and
  target node receive complete configuration reconciliation.
- Plans now have a database-backed revision. Every plan update and publish
  toggle requires `expected_revision`; the update increments the revision
  atomically and returns 428 for a missing precondition or 409 for a stale
  editor. The frontend displays the loaded revision, blocks a conflicted save
  and provides an explicit reload-latest action.
- Node-group member saves retain the complete explicit ID contract and existing
  group revision, but the write path no longer deletes and recreates every
  relationship. It validates IDs in 500-row batches, deletes only removed
  links and upserts desired links and ordering in 500-row batches, keeping the
  10,000-member UI snapshot closure bounded.

Local verification:

- Pinned Go 1.26.5 `go test ./...` passed across the backend, including
  handler, OpenAPI, migration and server packages.
- Frontend type checking passed. Vitest passed all 49 files and 108 tests,
  including the new infrastructure-control policy. The production build
  transformed 497 modules.
- No Codex built-in browser tab was opened, closed or finalized. Live browser
  acceptance remains intentionally disabled because the confirmed IAB cleanup
  path can terminate the Codex AppX container.

Deployment evidence:

- The verified working tree was synchronized to the intranet as
  `v0.0.1-20260724T060446Z-intranet-working-tree@2026-07-24T06:04:46Z`.
  `/readyz` returned `ready=true, db=true`; `zboard_next-zboard-1`, MySQL and
  Redis were healthy.
- The database backup is
  `/data/zboard-next/backups/20260724T060446Z/zboard-before-sync.sql`, the
  previous source is `/data/zboard-next/app-prev-20260724T060446Z`, and the
  release archive is
  `/data/zboard-next/releases/20260724T060446Z/source.tar.gz`.
- Migration `0026_plan_revision.up.sql` was present exactly once and the
  deployed `plans.revision` column was `bigint unsigned`, non-null, default 1.
  Authenticated plan list and detail requests returned HTTP 200 with
  `revision`. An empty update without `expected_revision` returned HTTP 428
  with `current_revision`; database checks proved the plan revision and
  `updated_at` were unchanged by that acceptance request.
- The deployed source snapshot contains the protocol copy and node-switch
  credential/publish paths, the 500-row node-group delta upsert, and the
  unified 36px infrastructure filter-control contract. Runtime protocol moves
  and large production node-group writes were not executed against real
  business data during deployment acceptance.

Remaining gaps:

- No Codex built-in browser tab was opened, closed or finalized. Live browser,
  responsive and screen-reader acceptance remains deferred until the confirmed
  IAB cleanup crash is isolated or a safe external-browser workflow is used.
- Dual-administrator integration and long-running distributed-load validation
  were intentionally excluded from this minimum closure. The requested plan
  revision and node-group large-member write contracts are implemented and
  locally verified, but production timing for a real 10,000-member write was
  not measured because that would alter live business data.

### 2026-07-24 - Protocol editor step and remote lookup component correction

Goal outcome:

- Replaced the protocol editor's page-private connected step strip with the
  shared `UiStepNav`. Each step is now an independent bordered control with an
  8px gap, explicit current-step semantics, completed state and bounded
  navigation; the compact layout keeps the same separation on narrow screens.
- Added the shared `UiAutocomplete` remote-search control. Its text input and
  dropdown trigger form one 40px control with one focus ring, one outer border,
  a tokenized divider and the shared icon. `NodeLookup`, `NodeGroupLookup` and
  `EndpointLookup` now consume this component instead of importing PrimeVue
  AutoComplete directly.
- Removed the protocol editor's private 42px native-element selector and all
  obsolete `wizard-steps` styling so page CSS can no longer override the
  shared control-height contract.

Local verification:

- Frontend type checking passed. Vitest passed all 49 files and 109 tests,
  including the expanded infrastructure-control policy and design-token
  ownership checks. The production build transformed 503 modules.
- `git diff --check` reported no whitespace errors for the changed frontend
  files; only the existing Windows LF-to-CRLF notice remained.
- Browser automation was not used: the installed in-app Browser plugin is
  missing its required `scripts/browser-client.mjs`. The user's open tab was
  not closed, finalized or otherwise changed. Visual acceptance remains
  pending until the synchronized page is refreshed and inspected safely.

Deployment evidence:

- The verified correction was synchronized to the intranet as
  `v0.0.1-20260724T072806Z-intranet-working-tree@2026-07-24T07:28:06Z`.
  `/readyz` returned `ready=true, db=true`; `zboard_next-zboard-1`, MySQL and
  Redis were healthy, and `/admin/protocols` returned HTTP 200.
- The database backup is
  `/data/zboard-next/backups/20260724T072806Z/zboard-before-sync.sql`, the
  previous source is `/data/zboard-next/app-prev-20260724T072806Z`, and the
  release archive is
  `/data/zboard-next/releases/20260724T072806Z/source.tar.gz`.
- The deployed source and built web assets both contain `UiStepNav` and
  `UiAutocomplete` markers. The obsolete `wizard-steps` marker is absent from
  the built assets.

Remaining gap:

- The synchronized visual state still needs a human refresh/inspection in the
  already-open browser. Codex did not control that tab because the installed
  Browser plugin lacks its required client module; no tab cleanup was run.

### 2026-07-24 - Adaptive row actions and unified list filters

Goal outcome before synchronization:

- Added the shared `RowActions` contract for dense list tables. A row with one
  available operation renders that operation directly; a row with multiple
  operations renders one compact ellipsis trigger and exposes every operation
  in the existing accessible floating menu.
- Migrated protocol services, users, subscriptions, orders, tasks and
  subscription templates away from persistent multi-button action columns.
  The menu retains `aria-haspopup`, expanded state, keyboard navigation,
  Escape-to-close and focus return to the row trigger.
- Added shared `UiSearchInput` and `WorkbenchFilterActions` components and
  migrated the administrative list workbenches. Search, select, numeric/date
  filters and query/reset actions now share the compact control-height and
  spacing contract. Administrative select filters update draft state and the
  explicit query action applies the request, avoiding page-specific mixtures
  of immediate and submitted filtering.
- The supplied `/admin/protocols` screenshot was used as the visual audit
  source. It demonstrated that four persistent row operations forced
  horizontal scrolling and that the search/select/button group mixed
  different wrappers, widths and request triggers.

Local verification:

- Frontend type checking and the production build passed; Vite transformed
  509 modules.
- Vitest passed all 50 files and 113 tests, including direct-versus-menu row
  action rendering, popup accessibility/focus behavior and architecture
  policies requiring shared administrative filter actions.
- `git diff --check` reported no whitespace errors; only existing Windows
  LF-to-CRLF notices remained.

Deployment evidence:

- The verified working tree was synchronized to the intranet as
  `v0.0.1-20260724T082106Z-intranet-working-tree@2026-07-24T08:21:06Z`.
  `/readyz` returned `ready=true, db=true`; `zboard_next-zboard-1`, MySQL and
  Redis were healthy, and `/admin/protocols` returned HTTP 200.
- The database backup is
  `/data/zboard-next/backups/20260724T082106Z/zboard-before-sync.sql`, the
  previous source is `/data/zboard-next/app-prev-20260724T082106Z`, and the
  release archive is
  `/data/zboard-next/releases/20260724T082106Z/source.tar.gz`.
- The deployed protocol source contains `RowActions`, `UiSearchInput` and
  `WorkbenchFilterActions`. The running container's built assets contain the
  corresponding `data-row-action-mode`, `ui-search-input` and
  `workbench-filter-actions` markers.

Remaining gap:

- Codex did not drive, close or finalize the built-in browser tab. Responsive,
  popup-positioning and final visual acceptance remain pending a safe manual
  refresh and inspection of the synchronized page.

### 2026-07-24 - Stripe-style progressive list filters

Goal outcome before synchronization:

- Replaced the administrative list filter action row with a shared
  Stripe-inspired progressive filter contract. Search remains a dedicated
  field; inactive conditions render as compact dashed `+ filter` chips;
  clicking a chip opens an anchored floating picker; active conditions render
  as `field value` chips with an independent clear action and the bar exposes
  one clear-all action.
- Added `WorkbenchFilterBar`, `WorkbenchFilterChip`,
  `WorkbenchFilterSelect`, `WorkbenchFilterInput`,
  `WorkbenchFilterNumber` and `WorkbenchFilterDate`. Select conditions apply
  immediately, while text, numeric and date conditions apply from their
  popovers so requests are not emitted for every keystroke. Popovers expose
  dialog/listbox semantics, outside-click and Escape close behavior, focus
  return, viewport clamping and narrow-screen wrapping.
- Migrated the protocol, node, node-group, plan/SKU, user, order,
  subscription, subscription-template, task, operation-log, audit-log,
  traffic/reconciliation and ticket workbenches. The account order, plan/SKU,
  subscription and traffic surfaces and the public plan search now use the
  same filter bar. Zboard's existing tokens and typography were retained; the
  Stripe reference was used for information hierarchy, chip geometry and
  progressive disclosure rather than copying Stripe branding.

Local verification:

- Frontend type checking passed. Vitest passed all 51 files and 116 tests,
  including direct chip selection/clear, Escape close/focus return and
  architecture policies requiring shared filters across administrative and
  account list surfaces.
- The production build passed and transformed 519 modules.
  `git diff --check` reported no whitespace errors; only the existing Windows
  LF-to-CRLF notices remained. No `WorkbenchFilterActions` reference remains
  in the frontend source.
- The Codex built-in browser was not driven, closed or finalized. An
  independent Chrome session can validate public pages, but its current
  Zboard admin tab is unauthenticated; authenticated protocol-page visual and
  responsive acceptance remains pending after synchronization.

Deployment evidence:

- The verified working tree was synchronized to the intranet as
  `v0.0.1-20260724T101319Z-intranet-working-tree@2026-07-24T10:13:19Z`.
  `/readyz` returned `ready=true, db=true`; `zboard_next-zboard-1`, MySQL and
  Redis were healthy. Both `/admin/protocols` and `/pricing` returned HTTP
  200.
- The database backup is
  `/data/zboard-next/backups/20260724T101319Z/zboard-before-sync.sql`, the
  previous source is `/data/zboard-next/app-prev-20260724T101319Z`, and the
  release archive is
  `/data/zboard-next/releases/20260724T101319Z/source.tar.gz`.
- The deployed protocol source contains `WorkbenchFilterBar` and
  `WorkbenchFilterSelect`. The running container's built assets contain the
  `workbench-stripe-filters`, `workbench-filter-chip-trigger` and
  `workbench-filter-popover` markers.

Remaining gap:

- An independent Chrome tab reached the deployed public pricing URL, but page
  inspection and screenshot capture timed out. That public surface only
  exercises the shared search shell, and the available Chrome administrator
  tab is unauthenticated. Authenticated filter-chip spacing, anchored-popover
  position and narrow-screen wrapping on `/admin/protocols` therefore remain
  a manual visual acceptance item. The built-in browser and its cleanup flow
  were not used.

### 2026-07-24 - Search input closure inside Stripe-style filters

Goal outcome before synchronization:

- Corrected the incomplete exception left by the first Stripe-style filter
  migration. List keyword search no longer renders as a persistent 280px
  rectangular input next to filter chips. It now renders as the same dashed
  inactive `+ 搜索` chip, opens its text input only in the anchored popover,
  and renders the committed query in the active chip.
- Migrated keyword search on protocol, node, node-group, plan/SKU, user,
  order, subscription, subscription-template, ticket, account-plan/SKU and
  public-plan list surfaces. Removed the now-unused `UiSearchInput` component
  and its direct-workbench width rules, and expanded the architecture policy
  to reject its reintroduction on core list pages.
- Text, numeric and date popovers now own local drafts. Typing or changing a
  value without applying it no longer mutates the active chip or request
  parameters; Apply commits the draft, while direct chip clear still commits
  immediately.

Local verification:

- Frontend type checking passed. Vitest passed all 52 files and 118 tests,
  including new coverage proving that a text-filter draft does not update the
  parent model before Apply.
- The production build passed and transformed 517 modules.
  `git diff --check` reported no whitespace errors for the frontend changes;
  only existing Windows LF-to-CRLF notices remained.
- No Codex built-in browser tab was driven, closed or finalized.

Deployment evidence:

- The verified working tree was synchronized to the intranet as
  `v0.0.1-20260724T104959Z-intranet-working-tree@2026-07-24T10:49:59Z`.
  `/readyz` returned `ready=true, db=true`; `zboard_next-zboard-1`, MySQL and
  Redis were healthy, and `/admin/subscriptions` returned HTTP 200.
- The database backup is
  `/data/zboard-next/backups/20260724T104959Z/zboard-before-sync.sql`, the
  previous source is `/data/zboard-next/app-prev-20260724T104959Z`, and the
  release archive is
  `/data/zboard-next/releases/20260724T104959Z/source.tar.gz`.
- The deployed subscription source contains two
  `WorkbenchFilterInput` uses and no `UiSearchInput` use. The obsolete
  component file is absent, the running assets contain the filter form/chip
  markers, and the legacy `ui-search-input` marker is absent.

Remaining gap:

- Authenticated visual acceptance of the newly deployed search chip and its
  input popover remains a manual refresh item. The Codex built-in browser and
  its cleanup path were not used.

### 2026-07-24 - Protocol node-group table geometry correction

Goal outcome before synchronization:

- Fixed the protocol list's `按节点分组` layout regression. The node-group
  heading used a 13-column table cell but page CSS changed that `<td>` to
  `display:flex`, which removed its table-cell formatting context and caused
  the browser's shared column-width calculation to collapse.
- The spanning `<td>` now retains native table-cell layout. A nested
  `protocol-group-content` wrapper owns the icon/name/ID flex alignment,
  truncates long node names and keeps the node ID stable without affecting
  service rows.
- Added an infrastructure layout policy that requires the nested group
  wrapper and rejects direct flex styling on the group heading table cell.

Local verification:

- Frontend type checking passed. The focused infrastructure and data-table
  suites passed 2 files and 8 tests.
- The production build passed and transformed 517 modules.
  `git diff --check` reported no whitespace errors for the correction; only
  the existing Windows LF-to-CRLF notice remained.
- No Codex built-in browser tab was driven, closed or finalized.

Deployment evidence:

- The verified correction was synchronized to the intranet as
  `v0.0.1-20260724T111252Z-intranet-working-tree@2026-07-24T11:12:52Z`.
  `/readyz` returned `ready=true, db=true`; `zboard_next-zboard-1`, MySQL and
  Redis were healthy, and `/admin/protocols?view=nodes` returned HTTP 200.
- The database backup is
  `/data/zboard-next/backups/20260724T111252Z/zboard-before-sync.sql`, the
  previous source is `/data/zboard-next/app-prev-20260724T111252Z`, and the
  release archive is
  `/data/zboard-next/releases/20260724T111252Z/source.tar.gz`.
- The deployed protocol source and running web assets contain the
  `protocol-group-content` wrapper. The invalid
  `.protocol-group-row td{display:flex` rule is absent from the deployed
  source.

Remaining gap:

- The synchronized grouped view still needs a manual refresh in the existing
  authenticated browser for final pixel inspection. The Codex built-in
  browser and its cleanup path were not used.

### 2026-07-24 - Stripe-style protocol services page

Goal outcome before synchronization:

- Redesigned only `/admin/protocols` around the Stripe dashboard information
  hierarchy. A compact title/action row is followed by five deployment-status
  selectors, the shared filter-chip row and view utilities, then one dense
  table workbench. Other administrator pages do not receive the
  `protocol-stripe-page` or page-specific visual overrides.
- The status selectors show server-backed totals for all, succeeded, running,
  failed and never-published services within the current keyword, protocol and
  service-status scope. Selecting one updates the existing URL-backed
  deployment filter instead of creating parallel filter state.
- Kept protocol copy, node switching, bulk selection, node grouping,
  pagination and the shared ellipsis row-action contract intact. The corrected
  node-group table-cell geometry remains unchanged.

Local verification:

- Frontend type checking passed. The full Vitest suite passed all 52 files and
  120 tests; the focused infrastructure/data-table suites passed 2 files and
  9 tests.
- The production build passed and transformed 517 modules. Built protocol CSS
  and JavaScript contain the page-scoped status overview and workbench markers.
- `git diff --check` reported no whitespace errors for the protocol page,
  policy test and design QA note; only the existing Windows LF-to-CRLF notice
  remained.
- Authenticated same-viewport visual comparison remains blocked. The Codex
  built-in browser and its cleanup path were not used.

Deployment evidence:

- The verified working tree was synchronized to the intranet as
  `v0.0.1-20260724T115114Z-intranet-working-tree@2026-07-24T11:51:14Z`.
  `/readyz` returned `ready=true, db=true`; `zboard_next-zboard-1`, MySQL and
  Redis were healthy. `/admin/protocols?view=nodes&deployment=failed` returned
  HTTP 200.
- The database backup is
  `/data/zboard-next/backups/20260724T115114Z/zboard-before-sync.sql`, the
  previous source is `/data/zboard-next/app-prev-20260724T115114Z`, and the
  release archive is
  `/data/zboard-next/releases/20260724T115114Z/source.tar.gz`.
- The deployed source contains the isolated protocol page, status-count loader
  and node-group wrapper. The running container's protocol CSS and JavaScript
  contain the `protocol-status-overview`, current-scope count and
  waiting-to-publish markers.

Remaining gap:

- Final pixel comparison at matching desktop and 390 px viewports remains a
  manual authenticated-browser acceptance item. The Codex built-in browser
  and its cleanup path were not used.

### 2026-07-24 - Protocol Stripe pagination correction

Goal outcome before synchronization:

- Corrected the oversized protocol-list footer identified in the authenticated
  screenshot. The protocol page now requests an explicit Stripe pagination
  variant with a 42px footer, 28px numeric page-size selector, concise
  result/page counters and 28px icon-only previous/next controls.
- The shared `TablePager` default remains unchanged for every other caller.
  Only the main `/admin/protocols` workbench enables `variant="stripe"`; the
  deployment-history pager inside the detail drawer also retains the default
  contract.
- Added component coverage for the compact range, page count, numeric size
  options and next-page event, plus a page-isolation policy assertion.

Local verification:

- Frontend type checking passed. The focused pager and infrastructure suites
  passed 2 files and 9 tests.
- The full Vitest suite passed all 53 files and 121 tests. The production build
  passed and transformed 518 modules.
- `git diff --check` reported no whitespace errors for the pager, its test and
  the protocol page; only the existing Windows LF-to-CRLF notice remained.
- The Codex built-in browser and its cleanup path were not used.

Deployment evidence:

- The verified working tree was synchronized to the intranet as
  `v0.0.1-20260724T121953Z-intranet-working-tree@2026-07-24T12:19:53Z`.
  `/readyz` returned `ready=true, db=true`; `zboard_next-zboard-1`, MySQL and
  Redis were healthy, and `/admin/protocols` returned HTTP 200.
- The database backup is
  `/data/zboard-next/backups/20260724T121953Z/zboard-before-sync.sql`, the
  previous source is `/data/zboard-next/app-prev-20260724T121953Z`, and the
  release archive is
  `/data/zboard-next/releases/20260724T121953Z/source.tar.gz`.
- The deployed source contains exactly one
  `<TablePager variant="stripe">` use. The running container assets contain
  the compact pager summary and the page-scoped 42px footer/28px control
  markers.

Remaining gap:

- The authenticated screenshot identified the original sizing problem, but
  the deployed compact footer still needs a manual refresh for final pixel
  confirmation. The Codex built-in browser and its cleanup path were not used.

### 2026-07-24 - Built-in ZNet Sink, Clash and sing-box subscription templates

Goal outcome before synchronization:

- Added three active, migration-seeded subscription templates with stable
  slugs: `znet-sink`, `clash` and `sing-box`. Existing rows with the same slug
  are preserved, and a down migration removes only unchanged seeded bodies.
- The rows invoke bounded backend exporters instead of copying endpoint
  `Config` objects into a different wrapper. The exporters normalize server
  and public port values and convert credential, protocol, TLS, Reality and
  WS/gRPC fields to each target schema.
- The ZNet Sink exporter produces a complete Zero JSON document with direct
  and block outbounds, a selector and an explicit final route. Its per-protocol
  whitelist removes client-only fields and reserved WebSocket headers. The
  result passed the current local Zero binary's strict `zero validate`.
- The Clash exporter emits Mihomo-compatible YAML with unique node names, all
  six supported zboard protocols, a selector group and a default MATCH rule.
  The sing-box exporter emits standard outbounds, a selector and default route
  for VLESS, VMess, Trojan, Shadowsocks and Hysteria2. Mieru is deliberately
  omitted from sing-box because the current official outbound schema does not
  provide that type; it remains available in the ZNet Sink and Clash exports.
- Selecting no template still returns the unchanged canonical
  `zboard.subscription/v1` manifest. Template selection changes only response
  representation and does not alter endpoint authorization, subscription
  credentials, accounting or token ownership.
- Source contract inspection used `zerodenet/znet-sink` tag `v0.0.15` and main
  commit `4664c5f9ce9e8793aff19cc022ae40b70f424318`. Both parser implementations
  accept raw Zero JSON in auto mode; the older prose document that says only
  Base64 JSON is supported is stale relative to the released parser.

Local verification:

- `go test ./...` passed, including exporter coverage for all six zboard
  protocols, missing-credential rejection, JSON/YAML parsing and the optional
  real Zero validator. `go vet ./...` passed.
- The ZNet Sink fixture passed
  `zero.exe validate <rendered-subscription.json>` against the local Zero
  binary after the exporter removed fields rejected by the strict schema.
- Frontend type checking passed. Vitest passed all 53 files and 121 tests.
  The production build passed and transformed 518 modules.
- Migration SQL passed the embedded migration parser tests. Formatting and
  focused whitespace checks passed; only existing Windows LF-to-CRLF notices
  remained.
- No Codex built-in browser tab was driven, closed or finalized.

Remaining gap before synchronization:

- Migration application, seeded-row inspection, deployed readiness/container
  health and real subscription-response checks for all three slugs remain to
  be completed by the intranet synchronization and acceptance phase.

Synchronization and deployment evidence:

- `scripts/sync-intranet.ps1` completed successfully and deployed
  `v0.0.1-20260724T130455Z-intranet-working-tree@2026-07-24T13:04:55Z`.
- The pre-deployment database backup is
  `/data/zboard-next/backups/20260724T130455Z/zboard-before-sync.sql`; the
  previous source is `/data/zboard-next/app-prev-20260724T130455Z`; and the
  synchronized source archive is
  `/data/zboard-next/releases/20260724T130455Z/source.tar.gz`.
- Post-deployment `/readyz` reported `ready=true` and `db=true`.
  `zboard_next-zboard-1`, `zboard_next-mysql-1` and
  `zboard_next-redis-1` all reported healthy.
- Migration `0027_builtin_subscription_templates.up.sql` was present exactly
  once in `schema_migrations`. The database contained the expected active
  `znet-sink`, `clash` and `sing-box` rows with the expected content types,
  bounded template calls and sort orders.
- A live eligible subscription token was decrypted only inside a disposable
  verifier and was never printed. The deployed public subscription route
  returned HTTP 200 for the unchanged native manifest and all three template
  slugs. The native response reported two protocol endpoints; ZNet Sink
  returned four outbounds, one selector group and a route; Clash returned
  proxies, proxy groups and rules; sing-box returned four outbounds and a
  route. Content types matched JSON, YAML and JSON respectively.
- The deployed source contained the migration, all three registered template
  functions, all three exporter implementations and the frontend help text.
  No Codex built-in browser tab was driven or finalized during acceptance.

Remaining gaps after synchronization:

- None for this subscription-template goal. No Git staging, commit, push or
  release was performed.

### 2026-07-24 - Protocol-service visual baseline across administrator pages

Goal outcome before synchronization:

- Promoted the visual and interaction geometry previously isolated to
  `/admin/protocols` into one `admin-stripe-surface` owned by
  `AdminLayout`. All 15 registered administrator routes now receive the same
  compact page header, 16px page rhythm, bordered workbench/section carrier,
  56px list toolbar, 38px table header and 52px row-action column.
- Kept resource-specific information architecture intact. Protocol deployment
  selectors remain on the protocol page because they own real server-backed
  deployment facets; other pages do not receive decorative or client-derived
  status cards.
- Added the shared `OverviewCard` contract and migrated both protocol
  deployment selectors and numeric metric summaries to it. Cards now share
  icon tone, number typography, caption geometry, focus treatment and selected
  state without copying protocol-page CSS.
- Added the shared icon-only `PageRefreshButton` with an accessible name and
  migrated every administrator page header, including the shared ticket
  surface. Primary create actions remain labeled and resource-specific.
- Made the compact Stripe-style `TablePager` the shared default instead of a
  protocol-only variant. Cursor-based traffic and log pages now use matching
  42px footers, numeric page-size controls and 28px accessible icon
  navigation.
- Removed the `protocol-stripe-page`, `protocol-stripe-workbench` and private
  status-card styling paths. The protocol page now composes the same shared
  page, workbench, pager, overview and refresh primitives as every other
  administrator page.
- Existing large-data contracts, URL-backed filters, shared filter chips,
  adaptive row actions, semantic status/time labels, form-field sizing,
  feedback ownership and admin/account/public resource boundaries were
  preserved. No backend API or database contract changed.

Local verification:

- Frontend type checking passed.
- Vitest passed all 55 files and 123 tests. New coverage exercises selectable
  overview cards, accessible compact refresh, default compact pagination and
  cursor pagination. Architecture policy verifies the shared baseline across
  every registered administrator route.
- The production frontend build passed and transformed 525 modules.
- `go test ./...` and `go vet ./...` passed.
- Focused whitespace checking reported no errors; only existing Windows
  LF-to-CRLF notices remained.
- The Codex built-in browser was not driven, closed or finalized.

Remaining gap before synchronization:

- The verified working tree still needs intranet synchronization, deployed
  version/readiness/container checks, route and built-asset marker checks, and
  final human visual inspection in the existing authenticated browser.

Synchronization and deployment evidence:

- `scripts/sync-intranet.ps1` completed successfully and deployed
  `v0.0.1-20260724T143937Z-intranet-working-tree@2026-07-24T14:39:37Z`.
- The pre-deployment database backup is
  `/data/zboard-next/backups/20260724T143937Z/zboard-before-sync.sql`; the
  previous source is `/data/zboard-next/app-prev-20260724T143937Z`; and the
  synchronized source archive is
  `/data/zboard-next/releases/20260724T143937Z/source.tar.gz`.
- Post-deployment `/readyz` reported `ready=true` and `db=true`.
  `zboard_next-zboard-1`, `zboard_next-mysql-1` and
  `zboard_next-redis-1` all reported healthy.
- All 15 registered administrator route URLs returned HTTP 200. The deployed
  source contained exactly 15 page-header `PageRefreshButton` owners, the
  shared admin surface, default compact table pager, compact cursor pager and
  shared protocol overview composition.
- Running web assets contained `admin-stripe-surface`,
  `page-refresh-button`, `overview-card`, `cursor-pager-nav` and compact table
  pager markers. The obsolete `protocol-stripe-page` and
  `protocol-stripe-workbench` markers were absent from both deployed source
  and runtime assets.

Remaining gap after synchronization:

- Final pixel and interaction inspection across all authenticated routes at
  desktop and 390px widths remains a human refresh item in the existing
  browser. The Codex built-in browser and its cleanup path were not used.
- No Git staging, commit, push or release was performed.

## 2026-07-28 - GitHub Actions gate repair

Goal outcome:

- Formatted the three Go sources rejected by the CI `gofmt` gate.
- Rebuilt `frontend/pnpm-lock.yaml` with pnpm 11.9.0 against the official npm
  registry, removing workstation-specific mirror tarball URLs rejected by the
  active lockfile supply-chain policy.
- Quoted comma-bearing inline OpenAPI descriptions so Redocly parses them as
  scalar descriptions instead of unexpected object properties.
- Rewrote the two one-line ticket wrapper scripts as standard multiline Vue
  SFC script blocks; the refreshed supported transitive toolchain now parses
  and type-checks them correctly.
- After the first CI retry reached backend compilation, updated the protocol
  summary test call for the new optional managed-certificate ID argument.

Verification:

- The Go source tree is idempotent under `@wasm-fmt/gofmt` 0.7.3. The formatter
  changed only `certificate_management.go`, `handlers.go` and `models.go`.
- `pnpm install --frozen-lockfile --registry=https://registry.npmjs.org`
  passed, and the lockfile contains no `npmmirror` or legacy Taobao registry
  URLs.
- `pnpm test -- --run` passed: 60 files and 132 tests.
- `pnpm build` passed with TypeScript/Vue type checking and 532 transformed
  modules.
- Redocly CLI 1.34.3 reports the OpenAPI description valid with zero errors.
  The existing 158 recommended-rule warnings remain non-blocking.
- `git diff --check` passed with only the existing Windows LF-to-CRLF notices.
- The follow-up test change is idempotent under the same Go formatter and
  passes `git diff --check`; the GitHub runner must rerun the full Go suite.

Synchronization:

- Not performed. The workstation has no Go installation, Docker Desktop is
  unavailable, and both the repository installer and winget were unable to
  download Go 1.26.5 from `go.dev`. Backend test/vet therefore remain
  unverified locally, so deploying this backend working tree would violate the
  repository completion protocol.

Remaining gaps:

- Push the repair through GitHub Actions so the runner's official Go 1.26.5
  `gofmt`, test and vet gates confirm the formatter-compatible result.
- After backend verification succeeds, synchronize with
  `scripts/sync-intranet.ps1` and record the deployed version, database backup,
  previous-source path, `/readyz`, container health and goal-specific evidence.
- The 158 Redocly recommended-rule warnings, primarily missing operation IDs,
  tag descriptions and explicit 4xx responses, should be reduced separately;
  they are not the cause of the current contract job failure.

## 2026-07-28 - Managed certificates and commit preparation

Goal outcome:

- Added panel-managed Let's Encrypt certificate resources bound to nodes and
  protocol endpoints, with issue/renew operations, renewal policy, public
  metadata only in the panel and private key material retained on each node.
- Added the admin certificate workbench and API contracts, including bounded
  paging, status filtering, operation state, protocol binding and Zero
  configuration publication after certificate changes.
- Integrated both certificate forms with the shared validation, API-error and
  unsaved-change contracts, and updated the lazy-route policy inventory.

Verification:

- `pnpm test -- --run` passed: 60 files and 132 tests.
- `pnpm typecheck` passed.
- `pnpm build` passed with the certificate page emitted as a lazy chunk.
- Targeted form and route policy tests passed: 2 files and 5 tests.
- `git diff --check` passed with only the existing Windows LF-to-CRLF notices.

Synchronization:

- Not performed. The configured Go 1.26.5 fallback directory was empty, and
  `scripts/ensure-go-env.ps1` could not download the pinned Windows archive
  from `go.dev`. Deploying backend changes without a successful local backend
  verification would violate the repository completion protocol.

Remaining gaps:

- `go test ./...` and `go vet ./...` must be rerun with the pinned Go 1.26.5
  toolchain.
- After backend verification succeeds, synchronize with
  `scripts/sync-intranet.ps1` and record the deployed version, database backup,
  previous-source path, `/readyz`, container health and certificate-specific
  route/behavior evidence.

## 2026-07-25 - Squashed v0.0.1 database baseline

Goal outcome before synchronization:

- All pre-release schema work formerly spread across migrations `0001` through
  `0032` is now expressed by the single
  `backend/migrations/0001_init.{up,down}.sql` baseline. The up baseline creates
  the final 30-table business schema directly, seeds 13 system settings and
  three version-2 subscription templates, and contains no development-only
  `ALTER TABLE` sequence, environment-specific auto-increment counters, legacy
  archive table or stale access-group index.
- The retired `0002` through `0032` SQL files were removed. Future schema
  changes remain squashable while the product is v0.0.1; after v0.1.0 is
  published, every released migration becomes immutable and later changes use
  ordered append-only up/down pairs.
- The migration runner accepts an empty database or an existing development
  database that recorded the original `0001` and terminal `0032` migrations.
  It rejects partial histories and non-empty unversioned schemas, validates the
  final table, column and index signature, removes only an empty legacy
  template archive, and renames the stale node-group index.
- Existing development migration rows are intentionally preserved as rollback
  metadata. This allows the immediately previous development binary to be
  restored. A fresh database created by the squashed baseline must not be
  opened by a pre-squash binary.
- `README.md`, `backend/README.md`, `RELEASING.md`,
  `docs/data-model.md`, `docs/admin-frontend-completion-audit.md` and the new
  `docs/database-migrations.md` now document the same baseline, upgrade,
  rollback and post-release immutability policy.

Local and isolated database verification:

- `go test ./internal/datastore`, backend `go test ./...` and `go vet ./...`
  passed after the final runner guard was formatted.
- A temporary MySQL database applied the SQL baseline directly and reported 30
  business tables, 13 system settings, three version-2 subscription templates,
  the final 96-character rule-set action column, the final node-group index and
  no legacy archive table.
- The final embedded up/down pair was also executed against a disposable MySQL
  database after deployment: the up baseline created 30 tables and the down
  baseline returned that database to 0 tables. Its database and uploaded SQL
  files were removed by the cleanup trap.
- A temporary MySQL database exercised the compiled migration runner. A partial
  old history was rejected; after the terminal `0032` record was present, the
  runner preserved all existing history rows, renamed the old index and
  removed the empty legacy archive.
- Migration inventory checks found only `0001_init.up.sql`,
  `0001_init.down.sql` and `assets.go`; the baseline has 30 `CREATE TABLE`
  statements, no `ALTER TABLE`, no serialized auto-increment counter and no
  obsolete schema marker. `git diff --check` passed with only existing Windows
  line-ending notices.
- Before synchronization, the live database reported 32 applied development
  migrations with the terminal `0032` record exactly once. The empty legacy
  archive and old index were present, while temporary verification databases,
  grants and files were absent. Core row counts were recorded for
  post-deployment comparison.
- No Codex built-in browser tab was driven, closed or finalized.

Synchronization and deployment evidence:

- `scripts/sync-intranet.ps1 -SkipLocalChecks` completed successfully and
  deployed
  `v0.0.1-20260725T105631Z-intranet-working-tree@2026-07-25T10:56:31Z`.
- The pre-deployment database backup is
  `/data/zboard-next/backups/20260725T105631Z/zboard-before-sync.sql`; the
  previous source is `/data/zboard-next/app-prev-20260725T105631Z`; and the
  synchronized source archive is
  `/data/zboard-next/releases/20260725T105631Z/source.tar.gz`. The exact paths
  were verified; the backup was 83,131 bytes and the source archive was 676,527
  bytes.
- Every recorded core row count was unchanged after startup: users 2, nodes 2,
  protocol endpoints 2, node groups 1, plans 1, subscriptions 1, orders 1,
  traffic records 0, tickets 1, templates 3, rule sets 0 and system settings
  13. All 32 old migration rows remain, with terminal `0032` exactly once.
- The deployed schema has 30 business tables. Startup removed the empty legacy
  template archive, renamed the old node-group index to
  `uk_node_groups_code`, and retained the final rule-set binding action type as
  `varchar(96)`.
- The synchronized migration directory contains only the `0001` up/down pair
  and `assets.go`. The deployed README contains the database baseline policy
  and `docs/database-migrations.md` is present.
- `/admin/protocols`, `/admin/subscription-templates` and its rule-set child
  route returned HTTP 200. `/readyz` returned HTTP/API code 200 with
  `ready=true` and `db=true`; the application, MySQL and Redis containers all
  reported healthy.

Remaining gaps:

- The intranet root filesystem has 1.9 GB free (96% used). This is an immediate
  operational capacity risk before another synchronized build.
- No Git staging, commit, push or release was performed.

### 2026-07-25 - Xboard-inspired filled subscription defaults

Goal outcome:

- The three typed renderers now expose an explicit `balanced` standard profile
  in addition to the backward-compatible `minimal` profile. New administration
  drafts choose the standard profile, while an old customization that has no
  profile continues to render its former minimal output until an administrator
  opts in.
- The standard profile adapts the useful behavior of Xboard's current default
  subscriptions without copying or executing Xboard template source. Clash /
  Mihomo receives a dynamic main selector, URL-test and fallback groups, DNS,
  LAN and China direct routes, common-service routing and advertisement
  rejection. sing-box receives a dynamic selector and URL-test outbound,
  private and China routing, remote China rule sets and cache settings. ZNet
  Sink receives the smaller target-native advertisement, local-domain and
  private-network defaults supported by Zero.
- Existing visual rule-set controls and the advanced YAML / JSON overlay are
  retained. The profile is a backend-owned typed preset, so operators can
  start from a usable default, add rule sets visually and still use direct
  configuration as an advanced capability.
- Empty generated node sets remain valid: automatic and failover groups are
  omitted instead of emitting invalid empty groups. Reality sing-box nodes now
  default to a Chrome uTLS fingerprint when none was specified, matching the
  current sing-box validator requirement.
- Migration `0030_xboard_inspired_subscription_defaults` upgrades only the
  exact untouched built-in rows. Existing administrator-edited customization
  is not overwritten, and the down migration applies the same conservative
  exact-state guard.

Local and migration verification:

- Complete backend `go test ./...` and `go vet ./...` passed with the pinned Go
  1.26.5 toolchain. Focused renderer tests also validated generated output with
  Mihomo 1.19.29, sing-box 1.13.14 and the current local Zero executable.
- Vitest passed all 59 files and 130 tests. Frontend type checking and the
  production build passed; Vite transformed 533 modules.
- A disposable intranet MySQL schema exercised migration `0030` in both
  directions. All three exact built-in defaults upgraded and rolled back; a
  Clash row with an administrator-added rule set retained its customization
  and revision through both operations. The temporary schema was deleted and
  verified absent.
- `git diff --check` passed with only existing Windows LF-to-CRLF notices.
- No Codex built-in browser tab was driven, closed or finalized.

Synchronization and deployment evidence:

- `scripts/sync-intranet.ps1 -SkipLocalChecks` completed successfully after
  the full local verification. The running version reports
  `v0.0.1-20260725T044612Z-intranet-working-tree@2026-07-25T04:46:12Z-working-tree@2026-07-25T04:46:12Z`.
  The duplicated `working-tree@build-time` suffix is cosmetic: the supplied
  version already contained metadata that `FullVersion` appends itself.
- The pre-deployment database backup is
  `/data/zboard-next/backups/20260725T044612Z/zboard-before-sync.sql`; the
  previous source is `/data/zboard-next/app-prev-20260725T044612Z`; and the
  synchronized source archive is
  `/data/zboard-next/releases/20260725T044612Z/source.tar.gz`. All three exact
  paths were verified present and the two files are non-empty.
- The live database records
  `0030_xboard_inspired_subscription_defaults.up.sql` exactly once. All three
  built-in slugs have `profile=balanced`; ZNet Sink retains the `proxy` main
  group while Clash and sing-box use `节点选择`.
- A three-minute administrator token was generated without printing the JWT
  secret, token or administrator information. Authenticated live previews
  returned HTTP 200 for all three renderers. ZNet Sink contained its three
  target-native rules; Clash contained URL-test, fallback, DNS, China direct
  and final selector entries; sing-box contained selector and URL-test
  outbounds, its two China rule sets and interface detection.
- The running `SubscriptionTemplates-CpwqRJEe.js` asset contains the standard
  profile interaction marker. No Codex built-in browser tab was driven,
  closed or finalized.
- Post-deployment `/readyz` reported HTTP/API code 200, `ready=true` and
  `db=true`. `zboard_next-zboard-1`, `zboard_next-mysql-1` and
  `zboard_next-redis-1` all reported healthy. The root filesystem retained
  3,995,092 KB free (92% used).

Remaining gaps:

- The duplicated runtime version suffix should be avoided on the next
  synchronization by passing only the base version to the sync script; no
  additional deployment was performed solely for cosmetic metadata.
- Final pixel and interaction review remains a human refresh item because the
  Codex built-in browser and its crash-prone cleanup path are intentionally
  not used.
- Database credential rotation remains pending explicit operator authorization
  after the previously recorded diagnostic-output exposure. No credential
  value is copied or repeated here.
- No Git staging, commit, push or release was performed.

## 2026-07-25 - Independent subscription rule-set library

Goal outcome before synchronization:

- Rule sets are now an independently managed admin resource. A reusable source
  owns its name, renderer, tag, URL, format, behavior, refresh interval,
  enabled state and revision. Template customization stores only
  `rule_set_id`, per-template action and order for library references.
- Existing inline remote rule sets remain supported as template-local quick
  actions. Existing customization JSON therefore requires no rewrite and keeps
  the prior API behavior.
- A foreign-key-backed binding index provides usage counts, prevents deleting
  a referenced rule set and cascades bindings when a template is deleted.
  Ordered mixed library and quick-remote entries remain in template
  customization JSON as the rendering and API composition boundary.
- The template editor now provides a bounded remote rule-set lookup for the
  current renderer plus a separate `快捷添加远端` action. The new
  `/admin/subscription-rule-sets` page uses the shared workbench, table,
  pagination, form and feedback controls. Disabling a rule set removes it from
  new selections while stored references continue to resolve for delivery.
- Renderer changes and deletion are rejected while a rule set is referenced.
  Source changes use optimistic revision checks and are picked up at render
  time without copying source configuration into every template.

Local and migration verification:

- `go test ./...`, `go vet ./...`, the strict OpenAPI parser and
  `git diff --check` passed. The frontend passed type checking, all 59 Vitest
  files and 130 tests, and the production build with 539 transformed modules.
- Tests cover source validation, reference-only persistence, duplicate
  references, current-source resolution, inactive/missing/renderer-mismatched
  references and rendered rule-set output.
- A disposable intranet MySQL schema exercised migration `0031` up and down.
  It verified both tables, renderer/tag uniqueness, referenced-delete
  rejection, template-delete cascade, successful deletion after unbinding,
  clean rollback and exact temporary-schema removal.
- No Codex built-in browser tab was driven, closed or finalized.

Synchronization and deployment evidence:

- `scripts/sync-intranet.ps1 -SkipLocalChecks` completed successfully and
  deployed
  `v0.0.1-20260725T061333Z-intranet-working-tree@2026-07-25T06:13:33Z`.
- The pre-deployment database backup is
  `/data/zboard-next/backups/20260725T061333Z/zboard-before-sync.sql`; the
  previous source is `/data/zboard-next/app-prev-20260725T061333Z`; and the
  synchronized source archive is
  `/data/zboard-next/releases/20260725T061333Z/source.tar.gz`. All three exact
  paths were verified present and the two files were non-empty.
- The live database records migration
  `0031_subscription_rule_sets.up.sql` exactly once. Both tables and both
  foreign keys exist.
- Authenticated live acceptance created a temporary Clash rule set and a
  template containing only `rule_set_id` and action. Preview resolved the
  current library source to `RULE-SET,<tag>,REJECT`, list usage became one,
  referenced deletion returned HTTP 409, template deletion cascaded the
  binding, and the rule set could then be deleted. API searches confirmed
  both temporary records were removed.
- The admin route returned HTTP 200. Running assets
  `SubscriptionRuleSets-ZO-gueof.js` and
  `SubscriptionTemplates-B04Bpokh.js` contain the library page, lookup and
  quick-remote interaction markers.
- `/readyz` returned HTTP/API code 200 with `ready=true` and `db=true`.
  `zboard_next-zboard-1`, `zboard_next-mysql-1` and
  `zboard_next-redis-1` all reported healthy.
- The root filesystem retained 3.4 GB free (93% used), which is an operational
  capacity risk to monitor but did not block this deployment.

Remaining gaps:

- Final pixel and interaction review remains a human refresh item because the
  Codex built-in browser and its crash-prone cleanup path were intentionally
  not used.
- Database credential rotation remains pending explicit operator authorization
  after the previously recorded diagnostic-output exposure. No credential
  value is copied or repeated here.
- No Git staging, commit, push or release was performed.

### 2026-07-24 - User-Agent subscription delivery and client metadata

Goal outcome before synchronization:

- The canonical client subscription URL now uses a conservative,
  case-insensitive User-Agent mapping when `template` is omitted or is
  `auto`: ZNet Sink maps to `znet-sink`, literal sing-box clients map to
  `sing-box`, Clash/Mihomo clients map to `clash`, and unknown clients keep
  the canonical `zboard.subscription/v1` JSON.
- Explicit selection always wins. `template=native` forces the canonical JSON
  response, while any other explicit value continues to select an active
  operator-managed template. If an automatically selected built-in template
  is unavailable, the response falls back to native JSON so an existing
  canonical URL does not fail because an administrator disabled a template.
- Automatically selected responses include `Vary: User-Agent`. Every
  successful response identifies its effective representation through
  `X-Zboard-Subscription-Format`; rendered templates retain
  `X-Zboard-Subscription-Template`.
- The existing `Subscription-Userinfo` contract remains attached to native
  and templated responses in
  `upload=0; download=FLOW_USED; total=FLOW_TOTAL; expire=UNIX_SECONDS`
  form. Zboard has one aggregate used-traffic counter rather than directional
  upload/download counters, so the standard header carries aggregate usage
  in `download`. The native JSON body additionally retains RFC3339 expiry,
  total, used and remaining byte fields.
- The account subscription page now defaults to
  `自动识别（推荐）`, offers an explicit `Zboard 原生 JSON` choice, preserves
  all active operator templates, and explains that client-visible
  traffic/expiry metadata is carried separately from the selected config
  body. URL construction is isolated in a tested frontend utility.
- `auto` and `native` are reserved from future operator-template creation so
  their URL semantics cannot become ambiguous. The intranet database had no
  conflicting rows, and all three built-in target templates were active
  before synchronization.
- Current ZNet Sink source confirms that its native format sends a
  `ZNet-Sink/<version>` User-Agent, its automatic/Clash paths send
  `Clash.Meta`, and it parses `subscription-userinfo` into upload, download,
  total and expiry sync metadata. Mihomo source also reads and retains the
  header. sing-box core does not define a universal subscription-manager
  User-Agent, so literal detection is intentionally conservative and the
  explicit account-page selection remains the reliable compatibility path.
- Authorization, reusable token ownership, endpoint eligibility, credential
  generation, accounting and billing boundaries were not changed.

Local verification:

- `go test ./...` and `go vet ./...` passed. Resolver tests cover ZNet Sink,
  Clash.Meta, Mihomo, sing-box, unknown-client fallback, explicit override,
  explicit native, explicit auto and custom operator templates.
- Vitest passed all 56 files and 126 tests, including URL construction for
  auto, native and explicit template modes.
- Frontend type checking and the production build passed; Vite transformed
  526 modules.
- `git diff --check` passed with only existing Windows LF-to-CRLF notices.
- No Codex built-in browser tab was driven, closed or finalized.

Remaining gap before synchronization:

- The verified tree still needs intranet synchronization, deployed
  version/readiness/container verification and a real eligible-token
  acceptance matrix for automatic UA selection, explicit override, native
  fallback and `Subscription-Userinfo` values.

Synchronization and deployment evidence:

- `scripts/sync-intranet.ps1 -SkipLocalChecks` completed successfully after
  the full local checks above and deployed
  `v0.0.1-20260724T150434Z-intranet-working-tree@2026-07-24T15:04:34Z`.
- The pre-deployment database backup is
  `/data/zboard-next/backups/20260724T150434Z/zboard-before-sync.sql`; the
  previous source is `/data/zboard-next/app-prev-20260724T150434Z`; and the
  synchronized source archive is
  `/data/zboard-next/releases/20260724T150434Z/source.tar.gz`.
- Post-deployment `/readyz` reported `ready=true` and `db=true`.
  `zboard_next-zboard-1`, `zboard_next-mysql-1` and
  `zboard_next-redis-1` all reported healthy, and
  `/account/subscription` returned HTTP 200.
- A live eligible token was decrypted only inside a one-use verifier and was
  never printed. Automatic ZNet Sink, Clash.Meta, Mihomo, literal sing-box
  and unknown-User-Agent requests all returned HTTP 200 with the expected
  `znet-sink`, `clash`, `clash`, `sing-box` and `native` formats,
  respectively. Explicit sing-box over a Clash UA and explicit native over a
  ZNet Sink UA won as required; explicit `auto` retained UA behavior.
- Every automatic response included `Vary: User-Agent`, every explicit
  response omitted that variance, and all eight responses carried a valid
  four-field `Subscription-Userinfo` header with a positive quota, valid
  used/total relation and future expiry. JSON/YAML body-shape checks also
  passed for each effective format.
- The first disposable Go-container verifier could not compile because the
  remote root filesystem was full. Acceptance instead used a locally
  cross-compiled 6.2 MB verifier uploaded to one explicit `/tmp` path; it was
  removed immediately and verified absent. No Docker cache, image, container
  or volume was deleted.
- The synchronized source and running web assets contain the UA resolver and
  `自动识别（推荐）` selector markers.

Remaining gaps after synchronization:

- No functional gap remains for User-Agent selection or subscription
  metadata delivery. The existing authenticated account page still needs a
  human refresh for final pixel inspection because the Codex built-in browser
  and its cleanup path were not used.
- The intranet host root filesystem is at 100% with approximately 13 MB free.
  The current deployment is healthy, but future builds and diagnostics are at
  risk until unused build cache or old artifacts are reviewed and cleaned
  under separate authorization.
- No Git staging, commit, push or release was performed.

### 2026-07-25 - Durable node-group delivery reconciliation

Goal outcome before synchronization:

- Creating a node group, or updating its complete endpoint membership, now
  persists a `node_group_reconcile` task and its concrete task items in the
  same database transaction as the group mutation. A successful API response
  therefore no longer depends on an untracked fire-and-forget goroutine.
- The first task item is the credential phase. It revokes active credentials
  for active subscriptions whose endpoints are no longer members of the
  subscription's node group, then creates or reactivates credentials for the
  current active endpoints. Node publication does not start when this phase
  fails.
- After the credential phase succeeds, node-config publication runs with the
  existing four-worker bound. The task scope contains the union of the nodes
  affected before and after the membership replacement, so a node that lost
  its final group endpoint is still republished and cannot retain stale
  runtime configuration.
- Each affected node now carries a representative, existing protocol endpoint
  ID into the durable task content. Publication uses that endpoint as the
  deployment trigger instead of the synthetic endpoint ID `0`; older persisted
  tasks without the new map resolve an existing endpoint on retry.
- Runtime credential selection now also joins the current
  `Subscription -> NodeGroup -> ProtocolEndpoint` membership. This is a
  defense-in-depth authorization check while a failed publication is pending
  or retrying; a stale credential row alone can no longer enter a newly
  compiled node configuration.
- The node-group mutation response exposes the persisted task without
  changing the existing top-level node-group fields. The administration UI
  adds it to the shared task tray, reports the task ID after save and labels
  the new type, scope and credential phase consistently in the task center.
- The OpenAPI contract and `docs/data-model.md` document the mutation
  response, ordered task semantics and preserved resource boundary. No
  endpoint, subscription, accounting or billing ownership was moved.

Local verification:

- `go test ./...` passed for every backend package with the pinned Go 1.26.5
  toolchain. Focused tests prove deterministic task identity, the credential
  item ordering, deduplicated old/new node scope, the sequential preflight
  constraint and representative endpoint selection with current membership
  taking precedence.
- `go vet ./...` passed.
- Vitest passed all 56 files and 126 tests.
- Frontend type checking and the production build passed; Vite transformed
  526 modules.
- The OpenAPI YAML strict parser test passed and includes
  `NodeGroupMutationResponse` plus the new persisted task type.
- `git diff --check` passed with only existing Windows LF-to-CRLF notices.
- No Codex built-in browser tab was driven, closed or finalized.

Intranet capacity recovery:

- The `gitlab` host initially had only 20 KiB free and 442 free inodes. After
  explicit authorization, cleanup reclaimed 3.441 GB from Docker build cache,
  1.779 GB from images not referenced by any container and approximately
  2.9 GB from the exact `/data/zboard-next/candidates` subtree after a
  `realpath` boundary check.
- Candidate copies and build/image cache are reproducible. No Docker volume,
  database data, current application tree, database backup, source release or
  retained previous-source directory was deleted. The final root filesystem
  has 5.8 GB free (88% used), with 4% inode usage.

Synchronization and deployment evidence:

- The first post-cleanup synchronization deployed
  `v0.0.1-20260725T021931Z-intranet-working-tree@2026-07-25T02:19:31Z`.
  A live equivalent-membership update created durable task `#1`: its
  credential item succeeded, while both node items failed because publication
  attempted to insert a deployment with synthetic endpoint ID `0`, violating
  the `protocol_deployments.protocol_endpoint_id` foreign key.
- The task now persists representative endpoint IDs and has a backward
  compatible lookup for older task content. Full backend tests and vet passed
  again after the repair. A second synchronization deployed
  `v0.0.1-20260725T022814Z-intranet-working-tree@2026-07-25T02:28:14Z`.
- The final pre-deployment database backup is
  `/data/zboard-next/backups/20260725T022814Z/zboard-before-sync.sql`; the
  previous source is `/data/zboard-next/app-prev-20260725T022814Z`; and the
  synchronized source archive is
  `/data/zboard-next/releases/20260725T022814Z/source.tar.gz`.
- Post-deployment `/readyz` reported `ready=true` and `db=true`.
  `zboard_next-zboard-1`, `zboard_next-mysql-1` and
  `zboard_next-redis-1` all reported healthy.
- Retrying task `#1` preserved the already completed credential item at one
  attempt and reran only the failed node items. The task completed 3/3 with
  zero failures; node 1 and node 2 each completed on their second attempt.
  Their successful deployments reference existing protocol endpoints 1 and 2,
  respectively.
- Acceptance credentials were generated only inside a short-lived verifier
  and were never printed. All temporary verifier and result files were removed
  and verified absent.

Remaining gaps:

- No functional or deployment gap remains for durable node-group
  reconciliation. The administration UI still needs a human pixel inspection
  because the Codex built-in browser and its cleanup path were not used.
- Repeated build candidates, releases and Docker cache can refill the host;
  an explicit retention policy remains a sensible operational follow-up but
  is not a blocker for this goal.
- No Git staging, commit, push or release was performed.

### 2026-07-25 - Progressive subscription-template editor

Goal outcome:

- The subscription-template editor now treats the three maintained exporters
  as operator choices instead of exposing their Go template implementation:
  ZNet Sink, Clash/Mihomo and sing-box each atomically select the expected
  template body and response Content-Type.
- New templates start in the standard Clash output mode. Existing templates
  whose trimmed body is one of the maintained exporter calls reopen in the
  matching standard mode; all other bodies reopen in advanced custom mode
  without rewriting their source.
- Switching from custom mode to a standard exporter retains the unsaved custom
  body and Content-Type in the current editor session, so switching back does
  not discard work. Saving still sends the original backend contract and
  remains protected by the existing optimistic revision.
- Advanced custom mode is the only mode that exposes Content-Type and source
  editing. Available data, safe functions and standard exporter references are
  now permanently visible beside the code editor rather than hidden below the
  preview.
- Preview and validation moved below the output configuration and use the
  unchanged backend preview endpoint. Standard modes therefore require no
  template knowledge, while advanced operators still receive line diagnostics
  and exact rendered output.
- Authorization, template rendering, subscription delivery, credential,
  accounting and billing boundaries were not changed.

Local verification:

- Vitest passed all 58 files and 129 tests. New tests cover recognition of the
  three exact maintained exporters, preservation of custom classification,
  atomic body/Content-Type preset selection and accessible four-option mode
  selection.
- Frontend type checking and the production build passed; Vite transformed
  533 modules.
- `git diff --check` passed with only existing Windows LF-to-CRLF notices.
- No Codex built-in browser tab was driven, closed or finalized.

Synchronization and deployment evidence:

- `scripts/sync-intranet.ps1 -SkipLocalChecks` completed successfully after
  the full frontend checks above and deployed
  `v0.0.1-20260725T025210Z-intranet-working-tree@2026-07-25T02:52:10Z`.
- The pre-deployment database backup is
  `/data/zboard-next/backups/20260725T025210Z/zboard-before-sync.sql`; the
  previous source is `/data/zboard-next/app-prev-20260725T025210Z`; and the
  synchronized source archive is
  `/data/zboard-next/releases/20260725T025210Z/source.tar.gz`.
- Post-deployment `/readyz` reported `ready=true` and `db=true`.
  `zboard_next-zboard-1`, `zboard_next-mysql-1` and
  `zboard_next-redis-1` all reported healthy. The authenticated SPA route
  `/admin/subscription-templates?template=1` returned HTTP 200.
- The deployed source contains the standard output picker, permanent template
  reference and lower preview composition. The running
  `SubscriptionTemplates` asset contains the corresponding
  `无需编写代码`, `编辑代码时始终可见` and `校验与示例结果` markers.
- The root filesystem retained 5.4 GB free after the build and deployment.

Remaining gap after synchronization:

- Final pixel and interaction inspection of standard/custom switching remains
  a human refresh item in the existing authenticated browser. The Codex
  built-in browser and its cleanup path were not used.
- No Git staging, commit, push or release was performed.

### 2026-07-25 - Typed subscription-renderer boundary

Goal outcome:

- The previous progressive editor changed presentation but still exposed the
  backend's Go template execution model as an operator capability. This goal
  corrects that ownership boundary: operators now manage only template name,
  link slug, description, availability, sort order and one constrained
  `renderer` identifier.
- ZNet Sink, Clash/Mihomo and sing-box are backend-owned typed renderers.
  Response Content-Type is derived by the selected renderer and is read-only
  API output. Preview and live subscription delivery call the same renderer
  function directly; no database value is parsed or executed as Go code.
- Migration `0028_subscription_renderers` maps the three exact maintained
  legacy bodies to renderer identifiers. Any unknown legacy body is copied to
  a non-executable archive and disabled as `unsupported` before
  `template_body` and `content_type` are removed from the active table.
- The intranet database contained exactly three subscription-template records,
  one for each maintained renderer, and zero custom records before migration.
  Current behavior can therefore migrate without disabling an active format.
- The administration UI no longer contains custom-template, Content-Type,
  available-data, safe-function or source-editor controls. It presents only
  the three system capabilities and a sample-output action. An archived legacy
  row, if present on another deployment, remains visible and requires choosing
  a supported renderer before reactivation.
- The public template selection and canonical native subscription remain
  unchanged. Authorization, endpoint eligibility, credentials, accounting and
  billing ownership were not changed.

Local and migration verification:

- `go test ./...` and `go vet ./...` passed for the complete backend. Exporter
  tests now invoke typed renderers directly and verify backend-owned response
  types; OpenAPI strict parsing passed with renderer-only write and preview
  requests.
- Vitest passed all 58 files and 128 tests. Frontend type checking and the
  production build passed; Vite transformed 527 modules.
- A disposable intranet MySQL schema tested both directions with three
  maintained rows and one unknown legacy row. Upgrade preserved the three
  maintained rows, archived and disabled the unknown row, and left zero
  executable columns in the active table. Downgrade restored all four row
  classes. The disposable schema was deleted and verified absent.
- `git diff --check` passed with only existing Windows LF-to-CRLF notices.
- No Codex built-in browser tab was driven, closed or finalized.

Synchronization and deployment evidence:

- `scripts/sync-intranet.ps1 -SkipLocalChecks` completed successfully after
  the full local checks and deployed
  `v0.0.1-20260725T032607Z-intranet-working-tree@2026-07-25T03:26:07Z`.
  A final terminology-only correction changed the empty state from a custom
  template concept to a subscription-format concept; the complete 58-file,
  128-test frontend suite, type check and production build passed again before
  this final synchronization.
- The pre-deployment database backup is
  `/data/zboard-next/backups/20260725T032607Z/zboard-before-sync.sql`; the
  previous source is `/data/zboard-next/app-prev-20260725T032607Z`; and the
  synchronized source archive is
  `/data/zboard-next/releases/20260725T032607Z/source.tar.gz`. All three exact
  paths were verified present after deployment.
- The live database records migration
  `0028_subscription_renderers.up.sql`, exposes the required `renderer`
  column and has zero `template_body` or `content_type` columns in the active
  table. Its legacy archive is empty. The three active rows are one each for
  `znet-sink`, `clash` and `sing-box`.
- A two-minute admin token was generated without printing the JWT secret,
  token or user information. Authenticated live previews returned HTTP 200
  for all three renderers. ZNet Sink and sing-box returned valid JSON with
  outbound and route structures; Clash returned YAML with proxies, groups and
  rules. Each preview reported the renderer-owned response type.
- Post-deployment `/readyz` reported HTTP/API code 200, `ready=true` and
  `db=true`. `zboard_next-zboard-1`, `zboard_next-mysql-1` and
  `zboard_next-redis-1` all reported healthy. The root filesystem retained
  4.5 GB free (91% used).
- No Codex built-in browser tab was driven, closed or finalized.

Remaining gap:

- A diagnostic intended to list only container environment variable names was
  misquoted and returned full environment values to this task's tool output.
  No credential value was copied into source, repository memory or deployment
  logs, and none is repeated here. Database credential rotation remains
  pending explicit operator authorization.
- No Git staging, commit, push or release was performed.

### 2026-07-25 - Layered subscription-template customization

Goal outcome:

- The typed-renderer boundary remains: public templates and operator-created
  templates bind to ZNet Sink, Clash/Mihomo or sing-box, while protocol
  conversion, endpoint credentials and response Content-Type remain
  backend-owned. The correction is that a template is no longer limited to
  metadata; it now owns versioned, declarative customization.
- The default administration path uses structured controls for the main
  selector name, unmatched-traffic policy and up to 64 ordered remote rule
  sets. Each rule set has a bounded identifier, HTTP(S) URL, client-specific
  format, update interval and proxy/direct/reject action.
- Rule sets compile to the native target shape: Zero `route.rule_sets` and
  typed actions, Mihomo `rule-providers` plus `RULE-SET` rules, or sing-box
  remote `route.rule_set` entries and modern route/reject actions.
- Advanced customization is available on an explicit second tab. It accepts a
  YAML fragment for Clash or a JSON fragment for ZNet Sink and sing-box,
  validates syntax and deep-merges it after the visual configuration. Arrays
  replace their target value. Generated `proxies` or `outbounds` are protected
  so advanced input cannot replace dynamic nodes or credentials.
- Draft customization is retained separately while an administrator switches
  among the three output renderers in one edit session. Preview fingerprints
  include the complete customization, so a change to rules or advanced source
  marks the prior output stale.
- Public and admin list queries remain summary-only and omit customization.
  Only the authenticated admin detail endpoint returns the complete
  configuration. No database value is parsed or executed as Go code.

Local and migration verification:

- Complete backend tests and `go vet ./...` passed with Go 1.26.5. The OpenAPI
  strict parser passed. The current local Zero binary also accepted the
  default ZNet Sink output.
- Backend tests cover unsafe rule-set URLs, renderer-incompatible formats,
  protected-field overrides, advanced YAML merging and per-renderer rule-set
  translation.
- Vitest passed all 59 files and 130 tests. Frontend type checking and the
  production build passed; Vite transformed 533 modules. The customizer test
  proves that common fields and rule-set creation are visible by default while
  direct configuration remains behind the advanced tab.
- A disposable intranet MySQL schema tested migration `0029` in both
  directions. Upgrade gave all three built-in rows non-null, version-1 default
  customization with `Zboard` for Clash and `proxy` for the JSON renderers;
  downgrade removed only the customization column. The exact temporary schema
  was deleted and verified absent.
- `git diff --check` passed with only existing Windows LF-to-CRLF notices.
- No Codex built-in browser tab was driven, closed or finalized.

Synchronization and deployment evidence:

- `scripts/sync-intranet.ps1 -SkipLocalChecks` completed successfully after
  the full local verification and deployed
  `v0.0.1-20260725T040904Z-intranet-working-tree@2026-07-25T04:09:04Z`.
- The pre-deployment database backup is
  `/data/zboard-next/backups/20260725T040904Z/zboard-before-sync.sql`; the
  previous source is `/data/zboard-next/app-prev-20260725T040904Z`; and the
  synchronized source archive is
  `/data/zboard-next/releases/20260725T040904Z/source.tar.gz`. All three exact
  paths were verified present.
- The live database records
  `0029_subscription_template_customization.up.sql`. The customization column
  is non-null JSON, the active table still has zero `template_body` or
  `content_type` columns, and all three built-in templates have version-1
  customization with zero initial rule sets.
- Authenticated detail acceptance proved list responses omit customization
  while each of the three admin detail responses returns its version-1
  defaults. A three-minute token was generated without printing the JWT
  secret, token or administrator information.
- Authenticated previews with one remote rule set and an advanced fragment
  succeeded for ZNet Sink, Clash and sing-box. Each output retained generated
  nodes, contained the native rule-set structure and applied the advanced
  field. A Clash attempt to replace `proxies` returned HTTP 400.
- The running `SubscriptionTemplates-DH8ltcPy.js` asset contains the basic,
  advanced and add-rule-set interaction markers. No Codex built-in browser
  tab was driven, closed or finalized.
- Post-deployment `/readyz` reported HTTP/API code 200, `ready=true` and
  `db=true`. `zboard_next-zboard-1`, `zboard_next-mysql-1` and
  `zboard_next-redis-1` all reported healthy. The root filesystem retained
  4.1 GB free (92% used).

Remaining gaps:

- Final pixel and interaction review remains a human refresh item because the
  Codex built-in browser and its crash-prone cleanup path are intentionally
  not used.
- Database credential rotation remains pending explicit operator authorization
  after the previously recorded diagnostic-output exposure. No credential
  value is copied or repeated here.
- No Git staging, commit, push or release was performed.

## 2026-07-25 - Rule-set naming and template sub-navigation

Goal outcome before synchronization:

- The operator-facing resource name is now only `规则集`. Page headings,
  refresh/save/delete feedback, audit action labels and backend validation
  messages no longer call it `订阅规则集`.
- Rule sets no longer occupy a primary admin navigation item. The commercial
  menu contains only `订阅模板`, which stays active for its rule-set
  descendant.
- Subscription templates and rule sets share a local `模板 / 规则集`
  navigation component. The canonical rule-set UI path is now
  `/admin/subscription-templates/rule-sets`; the prior
  `/admin/subscription-rule-sets` path remains as a compatibility redirect.
- Template customization copy now says `选择规则集`; an inline remote source
  remains a separate `快捷远端` action.

Local verification:

- Frontend type checking passed. All 59 Vitest files and 131 tests passed,
  including a new architecture assertion that prevents a primary rule-set
  menu item, requires both local sub-navigation instances and rejects the old
  visible name.
- The production frontend build passed with 541 transformed modules.
- Focused backend handler and router tests passed. `git diff --check` passed
  with only existing Windows LF-to-CRLF notices.
- No Codex built-in browser tab was driven, closed or finalized.

Synchronization and deployment evidence:

- `scripts/sync-intranet.ps1 -SkipLocalChecks` completed successfully and
  deployed
  `v0.0.1-20260725T064925Z-intranet-working-tree@2026-07-25T06:49:25Z`.
- The pre-deployment database backup is
  `/data/zboard-next/backups/20260725T064925Z/zboard-before-sync.sql`; the
  previous source is `/data/zboard-next/app-prev-20260725T064925Z`; and the
  synchronized source archive is
  `/data/zboard-next/releases/20260725T064925Z/source.tar.gz`. All exact paths
  were verified present and the two files were non-empty.
- The template page, canonical rule-set descendant and compatibility path all
  returned HTTP 200. The running router asset contains both the descendant
  route and legacy redirect.
- Running assets contain the shared local `模板 / 规则集` navigation and no
  `订阅规则集` visible string. The admin-layout asset contains `订阅模板` but
  not the removed primary rule-set item.
- `/readyz` returned HTTP/API code 200 with `ready=true` and `db=true`.
  `zboard_next-zboard-1`, `zboard_next-mysql-1` and
  `zboard_next-redis-1` all reported healthy.
- The root filesystem retained 3.0 GB free (94% used), an increasing
  operational capacity risk that should be addressed before many more
  synchronized builds.

Remaining gaps:

- Final pixel and interaction review remains a human refresh item because the
  Codex built-in browser and its crash-prone cleanup path were intentionally
  not used.
- No Git staging, commit, push or release was performed.

## 2026-07-25 - Compact Stripe-style template sub-navigation

Goal outcome before synchronization:

- The shared `模板 / 规则集` control no longer uses PrimeVue Tabs to navigate
  between routes. It is now a semantic local `nav` containing two real router
  links.
- The navigation is text-only with no icon, pill or panel treatment. It uses a
  36 px compact line, 24 px item spacing, one shared divider and a 2 px active
  indicator. The active destination exposes `aria-current="page"` and both
  links retain a visible keyboard focus ring.
- The canonical and compatibility routes, page ownership and main-navigation
  hierarchy are unchanged.

Local verification:

- Frontend type checking passed. All 60 Vitest files and 132 tests passed.
  The new component test verifies link destinations, exactly one current
  section and absence of the old tabs carrier.
- The architecture policy requires semantic route links and rejects a future
  return to `UiTabs`. The production build passed with 542 transformed
  modules.
- `git diff --check` passed with only existing Windows LF-to-CRLF notices.
- No Codex built-in browser tab was driven, closed or finalized.

Synchronization and deployment evidence:

- `scripts/sync-intranet.ps1 -SkipLocalChecks` completed successfully and
  deployed
  `v0.0.1-20260725T071844Z-intranet-working-tree@2026-07-25T07:18:44Z`.
- The pre-deployment database backup is
  `/data/zboard-next/backups/20260725T071844Z/zboard-before-sync.sql`; the
  previous source is `/data/zboard-next/app-prev-20260725T071844Z`; and the
  synchronized source archive is
  `/data/zboard-next/releases/20260725T071844Z/source.tar.gz`. All exact paths
  were verified present, and the database backup and source archive were
  non-empty.
- The template page, canonical rule-set descendant and compatibility path all
  returned HTTP 200.
- Both live page chunks import the same
  `subscriptionTemplateEditor-D6Q9R2fN.js` shared chunk. That chunk contains
  the semantic navigation class, route links, `aria-current` and canonical
  rule-set route, while the old `ui-tabs` carrier is absent.
- The live component stylesheet contains the expected 36 px minimum height,
  24 px item gap, 1 px shared divider and 2 px active indicator.
- `/readyz` returned HTTP/API code 200 with `ready=true` and `db=true`.
  `zboard_next-zboard-1`, `zboard_next-mysql-1` and
  `zboard_next-redis-1` all reported healthy.

Remaining gaps:

- Final human pixel and interaction review remains after refreshing the
  deployed page because the Codex built-in browser and its crash-prone cleanup
  path were intentionally not used.
- The root filesystem retained 2.7 GB free (95% used), an increasing
  operational capacity risk that should be addressed before more synchronized
  builds.
- No Git staging, commit, push or release was performed.

## 2026-07-25 - Native subscription policy groups and validated injection

Goal outcome before synchronization:

- Subscription customization now uses an explicit version-2 policy-group
  contract instead of presenting invented `minimal` or `balanced` profiles.
  Each group has an operator-defined name and a renderer-supported native type,
  may include other groups, receives all matching generated nodes by default,
  and can designate a child group as its default selection.
- Administrators can designate the main policy group, bind every selected rule
  set to a concrete group, direct or reject, and choose the final route. Clash,
  sing-box and ZNet Sink render those choices to their native group and route
  structures. Unsupported group types are rejected for the selected renderer.
- Include and exclude patterns are RE2 regular expressions evaluated against
  the protocol configuration name before the generated node tag or numeric
  suffix is added. Empty groups, bad expressions, missing references, duplicate
  names and cyclic group references are rejected.
- Advanced source editing remains available. Clash can preserve safe generated
  proxy injection with `$zboard:generated-proxies`; JSON renderers use a
  `$zboard:generated-outbounds` object; supported member arrays can use
  `$zboard:all-nodes`. Markers are expanded server-side and must not leak into
  the returned configuration.
- The final merged native document is structurally validated before save
  preview or delivery. Validation covers native group types, non-empty members,
  references, defaults, probe fields, rule targets, final routes and cycles.
  Existing version-1 customizations are converted strictly for compatibility;
  the old profile fields are not part of the current editor or renderer model.
- Migration `0032_subscription_policy_group_targets` expands the persisted
  rule-set target column to hold policy-group identifiers. OpenAPI, the data
  model, API types and frontend editor now describe the same version-2
  contract.

Local verification:

- Backend `go test ./...` passed with real Mihomo 1.19.29, sing-box 1.13.14
  and the current Zero debug validator enabled. The generated native
  configurations, policy-group references, exact protocol-name regex behavior,
  injection markers and invalid-reference rejection were exercised.
- Backend `go vet ./...` passed.
- Frontend type checking passed. All 60 Vitest files and 132 tests passed,
  including editor and architecture assertions. The production build passed
  with 542 transformed modules.
- `git diff --check` passed with only existing Windows LF-to-CRLF notices.
  Active source contains no visible standard-routing or minimal-profile
  concept; legacy field names remain only in strict version-1 conversion code.
- No Codex built-in browser tab was driven, closed or finalized.

Synchronization and deployment evidence:

- `scripts/sync-intranet.ps1 -SkipLocalChecks` completed successfully and
  deployed
  `v0.0.1-20260725T084814Z-intranet-working-tree@2026-07-25T08:48:14Z`.
- The pre-deployment database backup is
  `/data/zboard-next/backups/20260725T084814Z/zboard-before-sync.sql`; the
  previous source is `/data/zboard-next/app-prev-20260725T084814Z`; and the
  synchronized source archive is
  `/data/zboard-next/releases/20260725T084814Z/source.tar.gz`. The paths were
  verified present; the database backup was 83,055 bytes and the source archive
  was 60,750,684 bytes.
- Migration `0032_subscription_policy_group_targets.up.sql` was recorded
  exactly once. The running database reports the binding `action` column as
  `varchar(96)`.
- The template list, selected-template editor and rule-set descendant routes
  all returned HTTP 200. The running `SubscriptionTemplates-BPRa0XJn.js` asset
  contains the native policy-group, protocol-name filter and both advanced
  injection marker interactions; the old standard-routing and minimal-node
  labels are absent.
- The synchronized backend source contains the regex contract, final native
  document validator and node injection marker implementation.
- `/readyz` returned HTTP/API code 200 with `ready=true` and `db=true`.
  `zboard_next-zboard-1`, `zboard_next-mysql-1` and
  `zboard_next-redis-1` all reported healthy.

Remaining gaps:

- Final human pixel and interaction review remains after refreshing the
  deployed page because the Codex built-in browser and its crash-prone cleanup
  path are intentionally not used.
- The root filesystem now retains 2.2 GB free (96% used). This is an immediate
  operational capacity risk before another synchronized build and should be
  cleaned up deliberately without deleting unverified backups or releases.
- No Git staging, commit, push or release was performed.

## 2026-07-25 - Local cache ignore policy and workspace cleanup

Goal outcome before synchronization:

- `.codex-cache`, `.codex-local-artifacts`, `.pnpm-store` and `.tmp-go-build`
  are now explicitly root-anchored in `.gitignore`. The existing
  `.codex-build-*` rule continues to cover one-off Codex build directories.
- Removed the existing Codex caches, local verification artifacts, project
  pnpm store, temporary Go build directory, old Codex build archive, empty
  `.agents` directory and generated `frontend/dist` directory. These were all
  reproducible local artifacts and were not source inputs.
- The cleanup removed approximately 3.6 GiB. `frontend/node_modules` remains
  intentionally present and ignored because it is the active local dependency
  installation; source files, project records and other uncommitted work were
  not removed.

Local verification:

- All five cache/build path patterns resolve through the intended
  root-anchored `.gitignore` rules.
- The removed directories no longer exist or appear in `git status`.
  `git clean -ndX` now lists only `frontend/node_modules`.
- `git diff --check -- .gitignore` passed with only the existing Windows
  LF-to-CRLF notice.

Synchronization and deployment evidence:

- `scripts/sync-intranet.ps1 -SkipLocalChecks` completed successfully and
  deployed
  `v0.0.1-20260725T101511Z-intranet-working-tree@2026-07-25T10:15:11Z`.
- The pre-deployment database backup is
  `/data/zboard-next/backups/20260725T101511Z/zboard-before-sync.sql`; the
  previous source is `/data/zboard-next/app-prev-20260725T101511Z`; and the
  synchronized source archive is
  `/data/zboard-next/releases/20260725T101511Z/source.tar.gz`. The paths were
  verified present; the database backup was 83,131 bytes and the compact
  source archive was 676,954 bytes.
- The remote `.gitignore` contains all five expected root-anchored patterns.
  None of the four cache directories exists in the synchronized application
  source.
- `/readyz` returned HTTP/API code 200 with `ready=true` and `db=true`.
  `zboard_next-zboard-1`, `zboard_next-mysql-1` and
  `zboard_next-redis-1` all reported healthy.

Remaining gaps:

- The intranet root filesystem retains 2.0 GB free (96% used). Local workspace
  cleanup does not address remote Docker, release or previous-source retention;
  that requires a separately verified remote retention cleanup.
- No Git staging, commit, push or release was performed.

## 2026-07-25 - Product-focused root README

Goal outcome:

- Replaced the root README's contributor runbook structure with a concise
  product introduction. It now explains Zboard's positioning, resource flow,
  infrastructure, subscription, commerce and operations capabilities, the
  high-level technical architecture, a minimal Docker experience path and
  focused document navigation.
- Removed repository-agent workflow, intranet synchronization, migration
  history identifiers, Go installation tuning, script timeout parameters,
  smoke-test commands and restricted-network troubleshooting from the project
  overview.
- Added `docs/development.md` for the pinned toolchain, local launch, manual
  backend/frontend startup, verification and restricted-network guidance.
  `CONTRIBUTING.md` now directs contributors to that guide and the database
  migration contract.
- Detailed v0.0.1 baseline, compatibility and rollback behavior remains in
  `docs/database-migrations.md`; the README mentions only the release boundary
  and links to that document.

Verification:

- Every local Markdown link in `README.md`, `CONTRIBUTING.md` and
  `docs/development.md` resolves to an existing repository path.
- `git diff --check` passed for the three documentation files with only the
  existing Windows LF-to-CRLF notices.
- The README contains none of the internal memory/synchronization references,
  migration-history filenames, environment-query knobs or contributor test
  commands that previously made it read like a development manual.

Synchronization:

- Not performed. The user explicitly established that documentation-only
  changes do not require intranet synchronization.

Remaining gaps:

- No code or runtime behavior changed, so backend/frontend builds and runtime
  deployment checks were not repeated.
- No Git staging, commit, push or release was performed.

## 2026-07-26 - Staged native Zero access and Connector contract

Goal outcome before synchronization:

- Added an explicit `ZBOARD_ZERO_KERNEL_CONTRACT` deployment boundary.
  `legacy` remains the default and preserves the contract of the currently
  published kernel. `native-local` enables the contract implemented only in
  the local Zero working tree; synchronizing zboard does not publish, upload or
  activate that kernel.
- The native-local compiler emits Zero managed users for VLESS, VMess,
  Shadowsocks, Trojan and Hysteria2 with stable `principal_key` attribution.
  It no longer emits the removed `credential_id` runtime field. Existing
  Shadowsocks subscriptions keep their dedicated ports and PSKs; 2022 ciphers
  preserve a stable server identity password and use each credential as a
  separate user PSK.
- A subscription with exactly one active protocol credential can project its
  byte-per-second speed limit and device limit into Zero. Multi-credential or
  multi-node subscriptions keep those policies panel-owned so the full
  allowance is not duplicated in independent kernel processes. Remaining
  quota is not projected because panel traffic direction, multipliers and
  Connector/outbox acknowledgement are not yet a distributed quota protocol.
- Native-local configuration replaces the removed fixed push/heartbeat/command
  client with a generic Webhook event sink. It sends to the complete
  `/api/zero/events` URL, uses an opaque Bearer header and a disk-backed outbox,
  and subscribes lifecycle, statistics and flow events. Authenticated
  non-flow events update Connector activity; `stats.sampled` updates bounded
  node counters; flow events retain idempotent panel billing and server-side
  principal lookup.
- Kernel publication now waits for a fresh authenticated Connector event
  instead of assuming the removed fixed heartbeat protocol. A real local run
  showed that the dispatcher's initial cursor does not replay the
  `engine.started` event created before dispatcher startup, so
  `stats.sampled` is intentionally subscribed as the periodic remote
  liveness signal. Local control-socket health remains a separate requirement.
- Quota adjustment tasks now queue affected node configurations after the
  database transaction succeeds. Runtime version metadata exposes the active
  `zero_kernel_contract` so a synchronized instance can be checked without
  reading its environment.
- The GitHub release source remains the existing published
  `zerodenet/zero` channel. The native contract was verified against the local
  Zero `0.0.15-rc.1` debug binary only; no assumption was made that
  `zerodenet/core`, a GitHub release or the online nodes already contain these
  local changes.

Local verification:

- `go test ./...` passed with
  `ZBOARD_ZERO_VALIDATE_BIN` set to the local Zero debug binary. The test
  configuration covers managed VLESS, Shadowsocks, Trojan and Hysteria2 users,
  native policy fields, opaque Connector headers and outbox configuration.
- A spawned local Zero process delivered a real authenticated
  `zero.event.v1` `stats.sampled` Webhook event to the receiver in about one
  second. This verifies actual local Connector delivery in addition to schema
  validation.
- `go vet ./...` passed.
- All 60 frontend Vitest files and 132 tests passed. Type checking and the
  production build passed with 542 transformed modules.
- All three Docker Compose variants rendered successfully with the default
  `legacy` contract. `git diff --check` passed with only existing Windows
  LF-to-CRLF notices.

Synchronization and deployment evidence:

- `scripts/sync-intranet.ps1 -SkipLocalChecks` completed successfully and
  deployed
  `v0.0.1-20260726T094103Z-intranet-working-tree@2026-07-26T09:41:03Z`.
- The version endpoint returned `zero_kernel_contract=legacy`, and container
  inspection confirmed `ZBOARD_ZERO_KERNEL_CONTRACT=legacy`. The matching
  native Zero build was not uploaded, published or activated.
- `/readyz` returned HTTP/API code 200 with `ready=true` and `db=true`.
  `zboard_next-zboard-1`, `zboard_next-mysql-1` and
  `zboard_next-redis-1` all reported healthy.
- A credential-free `stats.sampled` request to `/api/zero/events` returned
  HTTP 401, confirming that the synchronized receiver does not accept
  unauthenticated Connector activity. The synchronized source contains the
  native-local gate, `stats.sampled` Connector subscription and managed
  `principal_key` compiler.
- The pre-deployment database backup is
  `/data/zboard-next/backups/20260726T094103Z/zboard-before-sync.sql`
  (82,028 bytes); the previous source is
  `/data/zboard-next/app-prev-20260726T094103Z`; and the synchronized source
  archive is `/data/zboard-next/releases/20260726T094103Z/source.tar.gz`
  (684,832 bytes). All paths were verified present.

Remaining gaps:

- Package and qualify the local Zero build for the target Linux/libc matrix,
  then deploy it deliberately before changing the intranet environment to
  `native-local`. After activation, run a real node flow and verify
  `principal_key` attribution, outbox replay, final traffic settlement and
  policy enforcement against the live panel.
- The native configuration path still uses generation replacement plus a
  controlled restart; acknowledged `config.apply` hot updates, distributed
  speed/device aggregation and distributed quota enforcement remain future
  work.
- The current local Zero `MieruUserConfig` has no `principal_key` or managed
  policy fields. Mieru therefore remains endpoint-level and is not part of
  native per-subscription attribution in this stage.
- The intranet root filesystem has only about 799 MiB free and is 99% used.
  This is an immediate capacity risk before another image build, database
  backup or source synchronization; cleanup must preserve verified backups and
  rollback sources unless a separate retention decision authorizes removal.
- No Git staging, commit, push or release was performed.

## 2026-07-26 - Local Zero 0.0.15-rc.1 intranet rollout

Goal outcome before synchronization:

- Archived the current local Zero working tree and uploaded that exact source
  snapshot to
  `/data/zboard-next/native-kernel-builds/20260726T100453Z/source.tar.gz`.
  Its SHA-256 is
  `7f965e32b85dd9dce1abb0c1beb7e039873dd789cf62efdca9b122999e328f76`.
  The build did not clone or download source from GitHub.
- Built Zero `0.0.15-rc.1` with the `full,status-api,connector` feature set
  for `x86_64-unknown-linux-musl`. The release binary reports build time
  `2026-07-26T10:23:41.547709336Z`, has SHA-256
  `451fa483449369f7f4dfa562d990b41bdf9eda248049ec407540085de5ced038`,
  and executes successfully on the CentOS 7 intranet host.
- Packaged the verified binary as
  `/data/zboard-next/artifacts/zero-v0.0.15-rc.1-linux-x86_64-musl.tar.gz`
  with SHA-256
  `e9d8f5ca2e2debfe95c30889289e04904265fca2303f59bf4d1440d2c9aae500`.
  The package has no ELF `INTERP` program header or `GLIBC_*` version
  requirement. The existing `0.0.14` artifact was retained for rollback.
- Extended the native-local deployment gate with an explicit
  `ZBOARD_ZERO_LOCAL_VERSION`. In native-local mode both the release discovery
  endpoint and node reconciliation resolve only the exact matching local musl
  package and checksum; GitHub release discovery is not used.
- Updated the node-operation UI and summary parser for the generic Connector
  event verification phase while preserving display compatibility with older
  panel-heartbeat operations.
- Replaced both rollback paths' direct copy over `/usr/local/bin/zero` with
  an installed temporary file plus atomic rename. This avoids Linux
  `ETXTBSY` when the failed generation's executable is still running.
- Unified successful reconciliation metadata so both changed and already
  converged operations update the node's installed-version summary and SSH
  verification timestamp in the same transaction as kernel state and
  operation completion.

Local verification:

- With `ZBOARD_ZERO_VALIDATE_BIN` set to the local Zero `0.0.15-rc.1` debug
  binary, `go test ./...` passed for every backend package. This includes
  runtime configuration validation and a real local Connector Webhook
  delivery test. `go vet ./...` passed.
- The backend suite also covers both install-trap and post-activation rollback
  scripts and rejects restoration that directly overwrites the running
  executable.
- All 60 frontend Vitest files and 132 tests passed. Type checking and the
  production build passed with 542 transformed modules.
- `git diff --check` passed with only existing Windows LF-to-CRLF notices.

Synchronization state:

- The pre-switch deployment environment was backed up to
  `/data/zboard-next/backups/20260726T103239Z/env-before-native-local`.
  Only `ZBOARD_ZERO_KERNEL_CONTRACT=native-local` and
  `ZBOARD_ZERO_LOCAL_VERSION=0.0.15-rc.1` were changed.
- The first synchronization deployed
  `v0.0.1-20260726T103406Z-intranet-working-tree@2026-07-26T10:34:06Z`.
  `/readyz` and all three zboard-next containers were healthy. Its database
  backup is
  `/data/zboard-next/backups/20260726T103406Z/zboard-before-sync.sql`, previous
  source is `/data/zboard-next/app-prev-20260726T103406Z`, and source archive
  is `/data/zboard-next/releases/20260726T103406Z/source.tar.gz`.
- Node 1 activated Zero `0.0.15-rc.1` and passed systemd/control checks, but
  its Connector event was paused because the durable outbox required about
  2.41 GB of reserved free space while the root filesystem had about 1.1 GB.
  The attempted rollback then exposed the direct-copy `ETXTBSY` defect.
- Only reproducible build data was removed: 624.4 MB of Docker builder cache
  and the 1.3 GB musl Cargo target directory. The verified local and rollback
  artifacts, source snapshot, Rust toolchain, Cargo registry cache, databases,
  volumes, deployment backups and previous sources were retained. Root free
  space recovered to 3.4 GB. Node 1 then resumed outbox delivery and its
  authenticated Connector activity advanced to `2026-07-26T10:45:04.114Z`.
- The second synchronization deployed
  `v0.0.1-20260726T104612Z-intranet-working-tree@2026-07-26T10:46:12Z`.
  Its database backup is
  `/data/zboard-next/backups/20260726T104612Z/zboard-before-sync.sql`
  (82,996 bytes), previous source is
  `/data/zboard-next/app-prev-20260726T104612Z`, and source archive is
  `/data/zboard-next/releases/20260726T104612Z/source.tar.gz`
  (688,088 bytes). Version, `/readyz` and all zboard-next container checks
  passed.
- Node operation 18 reconciled node 1 as already matching the expected binary
  and configuration. Node operation 19 upgraded node 2 and passed systemd,
  control-socket and Connector-event checks at
  `2026-07-26T10:50:08Z`. Both authoritative kernel states report healthy
  Zero `0.0.15-rc.1` with binary SHA-256
  `451fa483449369f7f4dfa562d990b41bdf9eda248049ec407540085de5ced038`
  and no recommended action.
- The node 1 summary row still retained `0.0.14` after its no-change
  reconciliation even though its authoritative kernel state was correct.
  The unified success-metadata fix requires one final synchronization and a
  no-change reconciliation to prove that summary converges.
- The final synchronization deployed
  `v0.0.1-20260726T105144Z-intranet-working-tree@2026-07-26T10:51:44Z`.
  Its database backup is
  `/data/zboard-next/backups/20260726T105144Z/zboard-before-sync.sql`
  (83,669 bytes), previous source is
  `/data/zboard-next/app-prev-20260726T105144Z`, and source archive is
  `/data/zboard-next/releases/20260726T105144Z/source.tar.gz`
  (688,397 bytes). Version, `/readyz` and all zboard-next container health
  checks passed.
- Node operation 20 completed a no-change reconcile for node 1. Both node
  summary rows and authoritative kernel states now report Zero `0.0.15-rc.1`.
  Both states are healthy, have matching desired/applied configuration
  hashes, matching binary SHA-256, active systemd, healthy control sockets and
  no recommended action.
- Authenticated Connector activity remained fresh on both nodes at
  `2026-07-26T10:54:01Z`. Node 1's release binary executed directly on the
  target host, and its control socket returned a valid runtime status. There
  were no Zero warnings after the final cleanup.
- The final Docker builder cleanup reclaimed 648.4 MB. The root filesystem
  retained 3.3 GB free (93% used), above the outbox's observed 2.41 GB reserve
  threshold. Volumes, databases, local and rollback kernel artifacts, source
  archives, deployment backups and previous sources were retained.

Remaining gaps:

- A real client flow is still required to qualify live `principal_key`
  attribution, outbox replay and final traffic settlement. A successful
  installation and periodic `stats.sampled` delivery do not prove those flow
  semantics.
- No Git staging, commit, push or release was performed.
