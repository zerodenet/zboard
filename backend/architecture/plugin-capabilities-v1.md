# ZBoard plugin capability contract

This document distinguishes the signed-manifest grant from the operation API. A
capability name is a stable permission, not a direct database/model interface.
Every invocation is checked against the package digest-bound admission receipt,
current installation generation/state, caller session and current account role.
The plugin never supplies its own authenticated actor. Unknown capabilities and
operations fail closed. `capabilities.list` is the discovery surface for page
sessions and lists only authorized operations with their version and schemas.

## Current inventory and rollout

The released `origin/main` baseline (7da9cfb) has signed package admission,
dynamic pages, limited identity slots, HTTP routes, encrypted config, private
storage, account assertion, active subscription projection, message projection,
tasks, DNS/certificate providers, metering and commerce read. `origin/develop`
(23f7a2c) reverted the native host services; plugin-related edits in the dirty
local `develop` tree are drafts, not published contracts. This branch starts
from `origin/main` and does not import those drafts.

The local draft files for UI bridge, admission, manager/runtime, storage host,
public routes, page actions, protobuf/SDK, and their tests overlap this area.
They are preserved in the original checkout; only the generic route/host-service
design is retained through the already released `main` implementation. Draft
direct table/handler shortcuts and plugin-specific paths are not promoted.
The dirty node-pool and service-statistics work is unrelated and remains untouched.

Before this branch, general account-profile reads, scoped subscription summary
reads, separately authorized subscription writes, separately granted message
reads/acknowledgements, business slots, and native capability discovery were
missing. The older projection grants are compatibility surfaces, not substitutes
for this read/write split. Neither a local source file nor this contract counts
as a shipped capability until it is merged, released, and deployed.

Phase 1 in this branch adds scoped page-session and native-process account,
subscription, message and quota operations and general-purpose controlled UI slots. Existing v1
capabilities remain admitted for compatibility. Phase 2 adds domain-owned
subscription term extension and cancellation commands with transactional
credential/publication updates, replay receipts and audit, plus a separately
granted owned configuration projection using the host renderer. Other state
transitions are not admitted until their domain rules exist. Phase 3 should split config/storage
read and write grants; the existing combined v1 grants remain compatibility
surfaces meanwhile.

Acceptance gates are sequential: signed admission of two independent packages;
one shared operation set with distinct grants; cross-account and stale-admin
denial; quota/term/status replay and audit; disabled/upgraded/uninstalled entry
revocation; configuration save/load/restart; full Go and frontend regression.
Only after these gates and a reviewed capability matrix should a release be
considered. Deployment and external-plugin E2E are separate later evidence.

## Permission matrix

| Grant / operation | Context and use | Read or write; scope | Status |
| --- | --- | --- | --- |
| `zboard.account.self.read.v1` / `account.self.get` | Authenticated account page/slot; profile display | Read current active account from session; no target ID | Phase 1 |
| `zboard.account.admin.read.v1` / `account.admin.get` | Admin business page/slot; support tooling | Read one specified account, requiring current admin role | Phase 1 |
| `zboard.account.assertion.v1` / `account.assert` | Signed native runtime; credential verification | Password check; returns plugin-bound restricted principal, never hash/token | Released |
| `zboard.subscription.read.v1` / `subscriptions.owned.list/get` | Account page/slot; owned subscription status, quota, period and plan fields | Read current account only; ID still owner-filtered; no configuration content | Phase 1 |
| `zboard.subscription.config.read.v1` / `subscriptions.owned.config` | Account page/slot or native runtime; client configuration delivery | Read one active owned subscription through host renderer; separate grant from summary reads | Phase 2 |
| `zboard.subscription.admin.read.v1` / `subscriptions.admin.list/get` | Admin business page/slot | Read target only with independent grant and current admin role | Phase 1 |
| `zboard.subscription.quota.write.v1` / `subscriptions.quota.adjust` | Admin business page/slot; one subscription per request | Write positive/negative MB through quota batch command, required plugin-scoped idempotency key, task audit and transactional execution | Phase 1 |
| `zboard.subscription.projection.v1` / `subscriptions.list/get` | Signed native runtime serving authorized principal | Read active owned subscriptions and configuration content; no raw credentials | Released |
| `zboard.subscription.term.write.v1` / `subscriptions.term.extend` | Admin business page/native runtime | Extend one currently active, finite subscription by 1–3650 days; credentials and publication update in the same row-locked transaction, replay returns the same receipt | Phase 2 |
| `zboard.subscription.status.write.v1` / `subscriptions.status.cancel` | Admin business page/native runtime | Cancel one currently active subscription; revoke credentials and publish in the same row-locked transaction, replay returns the same receipt | Phase 2 |
| `zboard.message.read.v1` / `messages.owned.list/get` | Account page/slot | Read current account's audience-visible messages only | Phase 1 |
| `zboard.message.ack.v1` / `messages.owned.ack` | Account page/slot | Write own read receipt, conditional on message revision | Phase 1 |
| `zboard.message.projection.v1` / `messages.list/get/mark-read` | Signed native runtime with plugin-bound principal | Compatibility projection and acknowledgement | Released; split on new page-session API |
| `zboard.ui.page.v1` | Signed page/menu declarations | Host renders sandboxed frame, authenticates page session | Released |
| `zboard.ui.slot.v1` | Signed slot declaration at a named host slot | Host renders sandboxed component frame; no arbitrary host DOM access | Phase 1; legacy identity slots remain compatible |
| `zboard.http.route.v1` | Signed `.well-known` GET/POST route declaration | Host dispatches only to active generation; no core route override | Released |
| `zboard.host.discovery.v1` / `capabilities.list` | Signed native runtime | Read this installation's admitted grant names, host version and bridge/protocol versions | Phase 1 |
| `zboard.config.v1` | Admin configuration page/native runtime | Validate, apply and encrypt persistent config; v1 combined read/write | Released |
| `zboard.storage.v1` | Native runtime/admin configuration | Plugin-private namespace; v1 combined read/write, no core table handles | Released |

The catalog descriptor version is `1.0` for Phase 1 operations. Native callers
send a plugin-bound `principal_id` obtained from account assertion or the
verified page action context; the host removes it before invoking the same
application catalog. Additive
response fields are allowed within v1; permission, actor, and write semantics
are not. A breaking change requires a new grant and operation version. Presence
in this design table is **not** discovery: only manifest-admitted, registered
operations appear in UI `capabilities.list`; native callers can request
`zboard.host.discovery.v1` to read only their admitted grant names and host
version. The native runtime uses its own manifest-gated host call surface; it receives no page-session grant and must
provide a valid plugin-bound principal for account-scoped calls.

## Trust and lifecycle

The package signature, manifest hash, admitted capability set and generation
form one authorization unit. Installation validates declared routes/pages/
slots; enable publishes them; disable, upgrade or uninstall invalidates old
sessions and route/slot catalogs. A slot is an isolated frame with the bridge,
not a script injected into host DOM. Admin page/slot grants require both the
transport claim and a fresh account-domain admin check. A target ID in JSON
never elevates an account-page grant. Native assertion-derived principal is
HMAC-bound to one plugin and rechecked against current account state. No
capability returns a database connection, password hash, host admin token or
internal HTTP endpoint.

For quota changes, plugins compose one-target commands. The task repository
atomically creates the task/items/audit record; the executor row-locks the
subscription, applies domain quota validation, and records per-item replay
state. A common aggregate response can be layered over these commands without
creating any plugin-specific endpoint.

## Local draft disposition

Keep the unrelated dirty `develop` tree untouched. Reuse only the architecture
ideas of generic routes/host callbacks already incorporated in `main`; do not
copy the older handler/SQL implementation wholesale. Refactor direct user-table
checks in plugin page authority through `identity.Accounts.Me`. Replace the
identity-only slot restriction with the explicit slot grant while preserving
existing identity slot declarations. Do not merge plugin-specific page actions
or paths from local drafts.
