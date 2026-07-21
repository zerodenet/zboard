# zboard data model

zboard is a modular monolith that combines subscription commerce with node operations. Product, entitlement, infrastructure and accounting data are deliberately separated so that editing a node or catalog entry cannot rewrite historical purchases.

## Identity and installation

- `users.email` is the normalized unique login identifier; `account_name` is a display name.
- Every login identity is a row in `users`. `is_admin` grants additional management capabilities to that user; it does not create a separate administrator identity or exclude the user from subscriptions, orders, traffic and tickets.
- Passwords are bcrypt hashes. Reusable API credentials live in `user_api_tokens`, which stores only a SHA-256 digest, a non-secret prefix and lifecycle timestamps.
- `installations` is the one-time installation marker. Editable site and operational settings are mirrored into typed `system_configs`. Values are validated by their declared type, revisions provide optimistic concurrency, and rows marked `is_secret` are encrypted and redacted from API responses.

## Commerce

- `plans` owns entitlement policy: one required node group, traffic quota, speed/device/family limits, subscriber capacity, renewal policy, reset policy and traffic direction. Plans neither bind protocol endpoints directly nor define a second traffic multiplier.
- `plan_skus` is the purchasable specification. A SKU has a type (`new`, `renewal`, `upgrade`, `traffic_pack`), billing period, price, currency and commercial quota snapshot values.
- `orders` references a SKU and stores a complete commercial snapshot. Payment confirmation is admin/provider controlled; normal users cannot mark their own orders paid.
- `payment_events` reserves an idempotent provider-event boundary for signed payment integrations.
- `subscriptions` stores the granted commercial entitlement and selected node-group ID. Later plan or SKU edits do not rewrite its quota snapshot; changing membership of the referenced node group intentionally changes the endpoints delivered to every plan and subscription using that group.
- `subscription_members` represents actual family members; a numeric family limit alone does not grant membership.
- `subscription_tokens` stores only token digests. The current public URL is user-level and can aggregate multiple active subscriptions; `subscription_id` is available for future scoped tokens.

## Operations and node groups

- `nodes` is an independent VPS asset. It can exist without a protocol and owns lifecycle state, encrypted management/report credentials, communication mode, runtime status, enablement, version and synchronization timestamps.
- SSH client authentication selects password or private key. Server identity verification is automatic: an empty fingerprint is enrolled after the first successful SSH handshake, a recorded fingerprint is always enforced, and an administrator must explicitly reset trust after confirming a legitimate VPS reinstall or host-key change.
- SSH login identity and system privilege are separate node settings. `ssh_privilege_mode=none` requires a root login for managed system changes; `sudo` supports passwordless or password-based sudo; `su` requires a separately encrypted root password. Privilege passwords are sent only on the SSH session stdin and are never embedded in remote commands, operation output or audit details.
- Browser SSH terminals remain a node-operations capability. The browser receives only a short-lived, single-use terminal ticket; the backend keeps the encrypted SSH credential, enforces same-origin WebSocket upgrades, proxies a bounded PTY session, and audits session metadata without recording terminal contents.
- SSH reachability, Zero installation, applied runtime configuration, Zero process health, connector heartbeat and trusted traffic reporting are separate operational states. A successful SSH test or protocol-config upload must not mark the kernel healthy.
- `protocol_endpoints` is the sellable network resource and must reference exactly one node. It separates listen and public ports, encrypted server configuration, deliverable client configuration and the sole traffic multiplier. Optional protocol configuration and tags are JSON.
- `parent_protocol_id` is a same-node, acyclic self-reference for a single protocol stack parent.
- `node_groups` groups sellable endpoints. `node_group_endpoints` is the many-to-many relationship; endpoint group membership is never stored as comma-separated IDs.
- A plan and the resulting subscription reference one node group. Subscription delivery joins through that group and only returns active endpoints on enabled, online nodes.
- Saving a protocol endpoint never connects to its node. Explicit deployment records a `protocol_deployments` attempt and atomically stages the saved server configuration over the node's verified SSH connection.
- The current protocol deployment record represents SSH staging only. The planned kernel lifecycle compiles every active endpoint on a node into one versioned Zero runtime configuration, validates it with the target binary, atomically activates the generation and verifies the local control socket before marking the revision applied.

## Traffic and quota accounting

- A signed node report stores raw upload and download bytes independently.
- `traffic_calc_mode` selects upload plus download (`0`), upload only (`1`) or download only (`2`).
- Billed traffic is calculated with integer thousandths: `selected_bytes * protocol_multiplier_milli / 1_000`, rounded up.
- `traffic_records` stores the direction policy and protocol multiplier snapshot, so later endpoint changes never alter historical accounting.
- `(node_id, report_id)` provides retry idempotency and `(node_id, nonce)` prevents replay.
- `quota_events` is the auditable allocation/usage ledger. Subscription counters remain the fast balance projection and can be reconciled against traffic and quota events.

## Background work

- `tasks` contains common locking, retry and progress fields. Creation uses an idempotency key and resolves the JSON scope into concrete targets before commit.
- `task_items` stores independently retryable recipients or quota targets. Batch progress is therefore derived from concrete items rather than an opaque JSON list.
- Quota tasks lock each subscription, reject reductions below already-used traffic and write an idempotent `quota_events` adjustment before completing the item.
- Email tasks are disabled by default. They require encrypted SMTP credentials and either STARTTLS or implicit TLS. Completed recipients are skipped on retry; SMTP delivery remains at-least-once if the process stops after a remote server accepts a message but before the item status commits.
- Execution is initiated through the admin API or the management page. The lock expiry permits recovery of a task left running by a terminated process.

## Relationship summary

```text
nodes -> protocol_endpoints
protocol_endpoints <-> node_group_endpoints <-> node_groups
plans -> node_groups
plans -> plan_skus
users -> orders -> plan_skus
orders -> subscriptions -> node_groups
subscriptions -> subscription_members
subscriptions -> traffic_records -> protocol_endpoints
subscriptions -> quota_events
tasks -> task_items
```

## Migration ownership

The embedded, ordered SQL files under `backend/migrations` are the production schema source of truth. Startup records applied files in `schema_migrations`; GORM `AutoMigrate` is not used in production startup.

Migration `0014` compares each legacy plan's direct endpoint set with its access-group membership before removing `plan_protocol_endpoints`. Matching sets reuse the renamed node group; a drifted plan receives a private node group and subscriptions still following its old group move with it, preserving the previous delivery boundary without merging endpoints across plans.
