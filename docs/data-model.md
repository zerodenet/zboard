# zboard data model

zboard is a modular monolith that combines subscription commerce with node operations. Product, entitlement, infrastructure and accounting data are deliberately separated so that editing a node or catalog entry cannot rewrite historical purchases.

## Identity and installation

- `users.email` is the normalized unique login identifier; `account_name` is a display name.
- Every login identity is a row in `users`. `is_admin` grants additional management capabilities to that user; it does not create a separate administrator identity or exclude the user from subscriptions, orders, traffic and tickets.
- Passwords are bcrypt hashes. Reusable API credentials live in `user_api_tokens`, which stores only a SHA-256 digest, a non-secret prefix and lifecycle timestamps.
- `installations` is the one-time installation marker. Editable site and operational settings are mirrored into typed `system_configs`. Values are validated by their declared type, revisions provide optimistic concurrency, and rows marked `is_secret` are encrypted and redacted from API responses.

## Commerce

- `plans` owns entitlement policy: one required node group, traffic quota, speed/device/family limits, subscriber capacity, renewal policy, reset policy and traffic direction. Plans neither bind protocol endpoints directly nor define a second traffic multiplier. One plan revision protects those shared fields so an older editor cannot silently overwrite a newer save or publish-state change.
- `plan_skus` is the purchasable specification. A SKU has a type (`new`, `renewal`, `upgrade`, `traffic_pack`), billing period, price, currency and commercial quota snapshot values.
- `orders` references a SKU and stores a complete commercial snapshot. Payment confirmation is admin/provider controlled; normal users cannot mark their own orders paid.
- `payment_events` reserves an idempotent provider-event boundary for signed payment integrations.
- `subscriptions` stores the granted commercial entitlement and selected node-group ID. Later plan or SKU edits do not rewrite its quota snapshot; changing membership of the referenced node group intentionally changes the endpoints delivered to every plan and subscription using that group.
- `subscription_members` represents actual family members; a numeric family limit alone does not grant membership.
- `subscription_tokens` stores a lookup digest plus encrypted token ciphertext so the owner can view and copy the complete URL again. Explicit rotation or revocation immediately invalidates previous URLs. The public URL is user-level and can aggregate multiple active subscriptions.
- `subscription_templates` stores operator-managed names, link slugs, availability, a constrained `renderer` identifier and versioned declarative customization for optional client-specific exports. Version 2 customization contains native policy-group definitions, a designated main group, final routing target and an ordered mixture of reusable rule-set references and template-only remote shortcuts. Every policy group receives all available protocol endpoints by default; optional RE2 include/exclude expressions match only the protocol configuration name, while explicit group references allow native nested selectors. Exported endpoints retain their operator-defined names without panel ID suffixes. Every manual selection group also retains `DIRECT` and `REJECT` as renderer-native permanent targets; URL-test and fallback groups exclude them because they are not probe endpoints. The built-in probe default is `http://www.gstatic.com/generate_204`; the former exact HTTPS default is normalized to HTTP on startup without rewriting custom probe URLs. Rule-set bindings target a policy group, direct routing or rejection. Renderer implementation, response Content-Type, endpoint conversion and credential injection remain backend-owned. Advanced YAML (Clash) or JSON (ZNet Sink and sing-box) is parsed as data and deep-merged, never executed as Go template code. Dynamic root arrays can be replaced only when the documented generated-content marker is retained, and `$zboard:all-nodes` expands node names in member arrays. Save, preview and live delivery all validate the final merged client structure, group members and route references. Version 1 `profile`, `group_name` and `action` records are accepted only as a read-compatible import source and normalize to the version 2 model. Selecting a template changes only response representation; the canonical manifest, endpoint authorization, per-subscription credentials and accounting inputs remain unchanged. The v0.0.1 baseline creates the non-executable renderer/customization boundary and the final policy-group target width directly; a clean install does not create or archive legacy executable template bodies.
- `subscription_rule_sets` owns reusable renderer-specific remote source metadata: administrator name, output tag, URL, client-native format/behavior, update interval, availability and revision. A template reference owns only the action and list position, so editing the source updates every referencing template. `subscription_template_rule_set_bindings` is the foreign-key-backed reference index used for usage counts and deletion integrity; ordered customization remains the API composition boundary so reusable references and quick remote entries can be interleaved. Disabling a library record removes it from new selections without breaking existing subscriptions; deletion is rejected while any template references it. The v0.0.1 baseline creates the reusable rule-set tables and bindings in their final form without rewriting inline data.
- `protocol_credentials` binds one active subscription to one sellable endpoint. VLESS/VMess receive an independent UUID; Trojan/Hysteria2 receive an independent password; Shadowsocks receives an independent PSK and dedicated UDP/TCP listen/public port. `credential_id` remains the panel-side stable identifier, while `principal_key` is compiled into Zero's native managed-user entry for runtime attribution. Secrets are encrypted at rest.

- Version 2 subscription-template customization owns one configurable local loopback mixed HTTP/SOCKS inbound port. The backend renders it in the native ZNet Sink, Clash and sing-box shape so every generated document is directly runnable without a GUI-side repair step.
- Public ZNet Sink template delivery and the canonical native manifest base64-encode their already validated JSON documents while administrative preview remains readable JSON. This is representation-level concealment only: subscription authorization still depends on the 256-bit token, TLS, revocation and rotation. Clash and sing-box remain in their client-native representations. Invalid or revoked public subscription tokens return a no-store redirect to the private `subscription_camouflage_url` system setting, falling back to the installation's public site URL; other API 404 responses keep their machine-readable contract.

## Operations and node groups

- `provider_accounts` is the reusable external-supplier boundary. A provider
  key declares adapter capabilities such as `dns.records`,
  `certificate.public` or `payment.checkout`; each configured account owns an
  encrypted, redacted credential and verification lifecycle. Domain resources
  reference the account, but DNS, certificates and future payment channels
  retain separate typed tables and invariants rather than sharing an opaque
  provider-resource JSON table.
- `managed_dns_records` stores one handwritten FQDN, explicit A/AAAA target,
  selected node and Cloudflare desired/observed state. The target remains an
  operator-owned value rather than a live pointer to the node. An on-demand,
  admin-only address-candidate projection can read literal node fields, resolve
  their hostnames and inspect global interface addresses through an already
  verified SSH channel. It returns only publicly routable IPv4/IPv6 candidates,
  never mutates a record and never enrolls SSH trust. The create UI may fill an
  empty field from the first candidate, but operator edits and existing record
  values are never replaced by refreshes; choosing another candidate is an
  explicit action. Zboard discovers the longest matching Zone, refuses to
  overwrite an unowned remote record unless takeover was explicit, records
  provider operations, and distinguishes API synchronization from public-DNS
  observation. The create API may accept one A and one AAAA value together, but
  persists them as two independently owned records. A background observer
  retries public resolvers until each synced record is visible; it never repeats
  the Cloudflare write merely because propagation is pending. Mutable target,
  address, TTL and proxy policy use optimistic concurrency and resynchronize
  after editing. Deletion removes the exact provider-owned record before
  deleting local desired state; identity changes use explicit
  delete-and-recreate so old remote names cannot become orphaned. A node remains
  an infrastructure asset and does not acquire a single canonical domain.
- `managed_certificates` explicitly owns either a verified Cloudflare provider
  account for DNS-01 or a canonical node webroot for HTTP-01. DNS-01 does not
  require public TCP port 80. HTTP-01 Webroot does not bind a second listener,
  and verifies resolvable targets on TCP port 80 before issuance because that
  is part of the ACME HTTP-01 contract. A missing IPv6 route on the control
  plane is treated as an unobservable family rather than proof that the remote
  node is down; concrete refusal and timeout errors still block issuance.
  Certificate display name, ACME contact, Webroot and renewal policy are
  revision-protected mutable fields; node, domains, environment and challenge
  identity require a new certificate asset. DNS-01 first uses the operating
  system plugin package and falls back to an isolated Certbot Python virtual
  environment when the distribution does not publish that package. Legacy
  standalone certificates remain renewal-compatible but cannot be newly
  created.
- `nodes` is an independent VPS asset. It can exist without a protocol and owns lifecycle state, encrypted management/report credentials, communication mode, runtime status, enablement, version and synchronization timestamps.
- SSH client authentication selects password or private key. Server identity verification is automatic: an empty fingerprint is enrolled after the first successful SSH handshake, a recorded fingerprint is always enforced, and an administrator must explicitly reset trust after confirming a legitimate VPS reinstall or host-key change.
- SSH login identity and system privilege are separate node settings. `ssh_privilege_mode=none` requires a root login for managed system changes; `sudo` supports passwordless or password-based sudo; `su` requires a separately encrypted root password. Privilege passwords are sent only on the SSH session stdin and are never embedded in remote commands, operation output or audit details.
- Browser SSH terminals remain a node-operations capability. The browser receives only a short-lived, single-use terminal ticket; the backend keeps the encrypted SSH credential, enforces same-origin WebSocket upgrades, proxies a bounded PTY session, and audits session metadata without recording terminal contents.
- SSH reachability, Zero installation, applied runtime configuration, Zero process health, authenticated Connector activity and trusted traffic reporting are separate operational states. A successful SSH test, protocol-config upload or Connector delivery must not imply the other states are healthy.
- Current host resources are an on-demand administrative projection owned by the node asset. CPU cores/load averages, memory, root-filesystem capacity and host uptime are read through the already verified SSH channel and are not persisted as Zero session statistics or exposed without administrator authentication. This projection is deliberately named host resources rather than protocol load.
- `protocol_endpoints` is the sellable runtime network resource and must reference exactly one node when saved. Its configuration is template-like in the administration workflow: an operator may copy it into an independent inactive draft or switch its carrier node, while the saved runtime instance retains one unambiguous node for credentials, publishing and accounting. A node switch updates credential placement, reallocates dedicated Shadowsocks ports when necessary, and publishes only the active runtime sides of the move. The endpoint separates listen and public ports, encrypted server configuration, deliverable client configuration and the sole traffic multiplier. Optional protocol configuration and tags are JSON. Endpoint mutations are classified before server configuration encryption: management, billing and delivery changes remain control-plane updates, while active runtime and credential-placement changes publish only the nodes whose running configuration changes.
- VLESS and VMess transport selection is endpoint template configuration, not a separate protocol resource. Raw TCP is represented by the absence of an additional carrier; managed WebSocket and gRPC choices write matching server and subscriber-client fields, and every renderer converts those canonical values to its native representation. VLESS Reality is an endpoint transport capability, not a separate sellable protocol resource, and Zero `0.0.15` restricts it to raw TCP. Zboard generates a matching X25519 key pair and short ID, encrypts the private key only in the endpoint server configuration, and publishes only the public key, selected short ID, server name and client fingerprint. Maintained one-click scenario presets choose the SNI and client fingerprint together with fresh key material; operators may still adjust the result before saving. The generated Zero, Clash and sing-box representations preserve their renderer-native Reality fields; managed subscription UUID replacement does not rewrite the transport configuration.
- `parent_protocol_id` is a same-node, acyclic self-reference for a single protocol stack parent.
- `node_groups` groups sellable endpoints. `node_group_endpoints` is the many-to-many relationship; endpoint group membership is never stored as comma-separated IDs. One node-group revision protects descriptive fields, status and complete membership together so concurrent administrators cannot silently replace each other's delivery boundary. `protocol_endpoints.sort_order` is the global client-delivery order. A node group's `node_group_endpoints.sort_order` overrides it for that group; legacy groups whose relationship rows are all zero fall back to the global order. A complete-scope, versioned batch command updates global order and never publishes Zero.
- A node-group editor may resolve one bounded, ID-only protocol-endpoint filter snapshot for bulk selection. The snapshot is transient operator input, not a second persisted group or dynamic membership rule; saving still writes explicit `node_group_endpoints` rows. Large saves validate and upsert membership in bounded batches and delete only removed links instead of deleting and recreating the complete relationship set.
- A plan and the resulting subscription reference one node group. Subscription delivery joins through that group and only returns active endpoints on enabled, online nodes.
- Subscription delivery normalizes endpoint order before rendering. Active subscriptions are aggregated by expiry then ID; credential-backed endpoints keep their owning subscription's group position, and legacy endpoints use the earliest applicable subscription group. ZNet Sink, Clash / Mihomo and sing-box renderers all consume that same ordered endpoint slice, including policy-group members, so one saved business order produces the same relative client order across formats.
- Saving a protocol endpoint does not inherently publish a node. Name, client address or port, global delivery order, tags and traffic multiplier are applied without a Zero restart. Runtime fields publish the current node only while the endpoint was or becomes active; carrier-node moves publish the previous and target sides only when each side has an active runtime to remove or add. The publisher still compiles every active endpoint and active subscription credential on an affected node into one complete Zero runtime configuration, records the desired SHA-256, validates it with the installed binary, atomically switches the generation, restarts Zero, checks the local control socket and waits for a fresh authenticated Connector event. Runtime compilation is ordered by stable endpoint identity rather than client-delivery order, so later unrelated publications do not change runtime hashes after a business reorder.
- `protocol_deployments` records desired and applied configuration hashes. A deployment is successful only after the running node passes local health and Connector-event verification; validation or verification failure restores the previous generation and environment. The explicit deploy action is a retry of the same full publish path, not a single-file staging action.
- The runtime-log API merges `protocol_deployments`, `node_operations` and `tasks` into one newest-first operational view while retaining each source record and its error/output. `audit_logs` remains a separate security and business-change trail.

## Traffic and quota accounting

- `flow_usages` is the idempotent cursor for Connector-delivered `flow.updated` and `flow.completed` events. Live events charge only the new cumulative delta; the completed event settles any missing final delta.
- Protocol business load is derived from active `flow_usages` seen within the last two minutes. `active_flows` counts current flow IDs and `active_users` counts distinct subscription owners, so allocated credentials are never presented as currently connected people. Administrators see the aggregate on every protocol endpoint; subscribers see only aggregate load for endpoints reachable through their own currently usable node groups. No other user's identity, credential, host detail or traffic record crosses that boundary.
- `protocol_endpoints.managed_principal_ready` gates subscription-specific Trojan and Hysteria2 credentials on the successfully published kernel generation. Older kernels continue to receive and advertise the endpoint fallback credential; a failed publication never exposes credentials that the running node has not accepted.
- Zero uses a generic Webhook event sink with a disk-backed outbox and an opaque authorization header. Every event is authenticated with the node Connector credential. Lifecycle events update Connector activity; flow events are mapped through the native `principal_key` and its protocol credential. Request-provided user IDs are never trusted for billing. The legacy signed node-report endpoint and the old heartbeat/command routes remain compatibility-only.
- Native speed and device policies are projected only when a subscription has one active protocol credential. Copying a subscription-wide limit to multiple independent Zero processes would multiply its allowance. Cross-node speed/device aggregation, directional or weighted traffic calculation and quota balance therefore remain panel-owned until an acknowledged distributed policy protocol exists; `quota_remaining_bytes` is not emitted by zboard.
- Zero `0.0.15-rc.4` adds attributable `principal_key` support to
  `MieruUserConfig`. Zboard gates Mieru by the selected or actually installed
  node version: older nodes reject creation, re-enabling and publication while
  retaining records for disable/delete recovery; rc.4 and newer generate one
  encrypted password and principal per subscription. A successful
  fallback-bearing migration publication is followed under the same node lock
  by a fallback-free publication. Only after both validate, activate, pass
  health checks and receive Connector confirmation does
  `mieru_principal_ready` switch subscription delivery. `credential_id`
  remains panel-side metadata and is never emitted into Zero configuration.
  The contract and rollout behavior are documented in
  `docs/mieru-kernel-contract.md`.
- The native access contract is an explicit staged boundary: `ZBOARD_ZERO_KERNEL_CONTRACT=legacy` keeps the latest stable GitHub tag only as the unattended batch default, while an operator may explicitly select any published stable or prerelease tag. The selected release uses its immutable `zero-linux-x86_64.tar.gz` GNU artifact or musl artifact plus exact same-name `.sha256`; musl resolution accepts both the current `zero-linux-x86_64-musl.tar.gz` contract and the historical release-owned `zero-v<version>-linux-x86_64-musl.tar.gz` contract. The backend re-resolves every selected version rather than accepting a client URL; an explicit older target also requires a separate downgrade confirmation. Connector serialization follows the selected or actually installed Zero version independently of that access switch: releases through `0.0.15-rc.1` receive the historical `api_key_env` plus `push` contract, while `0.0.15-rc.2` and later receive the controller-neutral Webhook contract with opaque `headers`, the complete `/api/zero/events` URL and a durable outbox. Configuration-only publication probes the node first so it cannot reuse a stale desired-version contract. A trusted-directory historical versioned musl file remains a bounded fallback when the corresponding older GitHub Release has no usable musl pair. `native-local` enables managed users only with an explicit `ZBOARD_ZERO_LOCAL_VERSION`; it still resolves the exact `zero-v<version>-linux-x86_64-musl.tar.gz` plus `.sha256` from the trusted artifact directory and never substitutes a GitHub release. Synchronizing zboard alone does not publish or upgrade the local kernel.
- `traffic_calc_mode` selects upload plus download (`0`), upload only (`1`) or download only (`2`).
- Billed traffic is calculated with integer thousandths: `selected_bytes * protocol_multiplier_milli / 1_000`, rounded up.
- `traffic_records` stores the direction policy and protocol multiplier snapshot, so later endpoint changes never alter historical accounting.
- `(node_id, report_id)` provides retry idempotency and `(node_id, nonce)` prevents replay.
- `quota_events` is the auditable allocation/usage ledger. Subscription counters remain the fast balance projection and can be reconciled against traffic and quota events.

## Background work

- `tasks` contains common locking, retry and progress fields. Creation uses an idempotency key and resolves the JSON scope into concrete targets before commit.
- `task_items` stores independently retryable recipients or quota targets. Batch progress is therefore derived from concrete items rather than an opaque JSON list.
- Node-group membership mutations persist a `node_group_reconcile` task in the same database transaction. Its first item revokes active subscription credentials outside the current group membership and ensures credentials for current endpoints; only after that item succeeds may the previous and current affected nodes publish their compiled configuration. This keeps `Subscription -> NodeGroup -> ProtocolEndpoint` as the authorization boundary while making failed node publications visible and retryable.
- Quota tasks lock each subscription, reject reductions below already-used traffic, write an idempotent `quota_events` adjustment and queue affected node configurations for reconciliation before completing the item.
- Email tasks are disabled by default. They require encrypted SMTP credentials and either STARTTLS or implicit TLS. Completed recipients are skipped on retry; SMTP delivery remains at-least-once if the process stops after a remote server accepts a message but before the item status commits.
- Execution is initiated through the admin API or the management page. The lock expiry permits recovery of a task left running by a terminated process.

## Relationship summary

```text
nodes -> protocol_endpoints
provider_accounts -> managed_dns_records -> nodes
protocol_endpoints <-> node_group_endpoints <-> node_groups
plans -> node_groups
plans -> plan_skus
users -> orders -> plan_skus
orders -> subscriptions -> node_groups
subscriptions -> protocol_credentials -> protocol_endpoints
subscriptions -> subscription_members
users -> subscription_tokens; subscription_templates -> export representation
protocol_credentials -> flow_usages -> traffic_records
subscriptions -> quota_events
tasks -> task_items
```

## Migration ownership

The embedded SQL under `backend/migrations` is the production schema source of truth. Before the first public release, `0001_init.up.sql` directly expresses the complete v0.0.1 resource model: plans reference one node group, node groups own explicit protocol-endpoint membership, subscriptions retain their granted plan/SKU/node-group snapshot, and no legacy plan-to-endpoint or access-group tables are created. Startup records applied files in `schema_migrations`; GORM `AutoMigrate` is not used in production startup.

Existing databases from the former v0.0.1 development chain are accepted only after they reached its terminal migration. Startup verifies the final schema signature, removes only the empty legacy template archive and renames the stale access-group index. Previously applied migration rows remain as rollback compatibility metadata even though their SQL files are no longer shipped; a fresh database records only `0001_init.up.sql`. Partial and unversioned non-empty schemas are rejected. After v0.1.0 is released, the baseline becomes immutable and all schema changes use append-only migrations. See [database-migrations.md](database-migrations.md).
