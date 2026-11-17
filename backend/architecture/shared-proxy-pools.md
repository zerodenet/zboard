# Reusable shared proxy pools

A proxy pool is an independent infrastructure resource, not property of a VPS.
`node_proxy_pool_bindings(node_id, proxy_pool_id)` is the server-reference set.
`network_entries.proxy_pool_id` remains the actual forwarding selection. Adding a
reference never changes an existing route or a client subscription.

## Administration

- Node → Shared proxy pools (`/admin/proxy-pools`) manages reusable sources.
- Creating a VPS can reference existing pools or create a new independent pool.
- A VPS's pool tab references existing sources, creates new ones, or removes a
  reference. Removal is blocked while that VPS has entries using the pool.
- Configuration changes and changed subscription content validate every referring
  entry (including UDP capability), then queue one publication per server in the
  same transaction. An administrator reading published RAW selects a referenced
  server; only the pool's own configuration is exposed.
- Removing/archiving a VPS removes its reference, not the reusable source, source
  credentials, or other servers' references. Global pool deletion is allowed once
  no forwarding entry uses it, and removes its selection references atomically.

## Upgrade

Migration 0094 makes legacy pool.node_id nullable and backfills server references
from both old ownership and actual entry use before clearing legacy ownership.
Pool IDs, encrypted source URLs/configuration, revision numbers and sync times
remain unchanged. Existing same-name pools are not automatically merged: equal
names do not establish equal sources or independent maintenance intentions.
New servers can reference one existing pool rather than copy it. The old binary
expects single-server ownership; reverting the binary alone does not restore that
model. Use a compatible forward release or a coordinated database restore. A recorded
migration with a missing bindings table fails startup before automatic schema
creation can silently erase the reference set. Downgrade does not discard shared
references or guess a single owner.

## Automatic synchronization and recovery

Production read-only inspection on 2026-11-17 found proxy_pool_sync stuck in
unknown after a lost execution lease on 2026-09-29. The handler was absent from
its transport's explicit reconciler recovery list. Per-pool next_sync_at values
were consequently overdue even though manual sync remained possible.

The existing durable scheduler now registers proxy_pool_sync as a recoverable
internal scan, with a two-minute execution budget, bounded retries and compact
successful history. On startup it preserves uncertainty in the old attempt,
marks the lost scan interrupted, and resumes scanning durable per-pool state.
No manual row reset or capacity increase is necessary.

The scan claims one pool immediately before its fetch, up to ten per occurrence,
so later pools do not spend their leases waiting for an earlier HTTP request.
Fetch, parse and existing Zero validation occur outside the commit transaction.
Revision/source comparison fences edits made during the fetch. Failure preserves
the last good graph and schedules that pool's retry five minutes later, allowing
other due pools to continue. A process crash leaves a bounded five-minute pool
lease, after which the existing scan can reclaim it.

Equivalent JSON content reuses the existing encrypted configuration. Success
updates only synchronization metadata; it does not increase configuration
revision, append a successful audit, or publish again. Changed content commits
configuration, revision, audit and all affected publication intents atomically.
No additional polling history or unbounded detail table is introduced.

## Verification boundary

Local SQLite and isolated native MySQL cover independent creation, reference
reuse, fan-out, no-op sync, node-create rollback, node removal, old ownership
migration and interrupted scheduler recovery. Frontend component/type checks and
local desktop/mobile browser fixtures cover catalog, shared editing and creation
selection. Fixtures are not production runtime acceptance. Production remains
unchanged until an explicitly authorized release/deployment.
