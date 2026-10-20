# P4 diagnostic retention: independent bounded maintenance

This batch removes the unbounded fifteen-day `DELETE` hooks from flow-start
reception and Fair Use evaluation inserts. Accepted traffic settlement and
evaluation state no longer share a transaction with diagnostic cleanup. The
existing fifteen-second `fair_use` cycle attempts maintenance and evaluation
independently; joined errors reach the existing job failure/last-error view.

## Ownership and transactions

Only `subscription_flow_start_events` and `subscription_fair_use_events` are
eligible. They contain behavioural diagnostics and derived evaluation
explanations. Current Fair Use state, orders, wallet, entitlement history,
publication intents, business audit, `TrafficRecord`, `FlowUsage`, cumulative
cursors and pending statistics projections remain outside retention.

Each cycle discovers at most 1,000 IDs per table without a write transaction or
locking range read, ordered by the relevant time and ID. A separate READ
COMMITTED transaction locks those exact primary keys in ascending order,
rechecks the cutoff, and deletes eligible primary keys only. Each table commits
independently. There is no unbounded drain loop. The strict cutoff remains UTC
now minus fifteen days: flow starts use `received_at`; evaluation events use
`occurred_at`. Old backlog can remain for later cycles.

## Migration 0056 and rollback

Inspection exposed a second problem: evaluation history had only indexes led by
subscription or event type. Before this batch, SQLite EXPLAIN showed an index
scan and a temporary sort for global retention; LIMIT bounded output rather
than discovery work. That failed performance assumption remains in the private
initial receipt. New additive migration 0056 supplies
`idx_fair_use_event_retention(occurred_at,id)`. Flow starts already have a global
received-time index. The candidate query can now seek the relevant time range
and consume its first bounded IDs without sorting unrelated subscriptions.

RunMigrations installs the MySQL operational table through the existing
reconciler when absent, then installs and verifies the exact index definition.
SQLite installs its existing operational tables before applying the same
index. Both paths restore a missing index even when the migration was recorded,
accept DDL committed before the receipt, and retain diagnostic rows. An existing
same-name index with different columns stops startup for inspection. No
published migration was edited. The down migration preserves this additive
index; older binaries can ignore it. No production migration was executed.

Rollback does not restore expired diagnostics. Restoring such history requires
the operator's backup; it must never recreate billing facts from observations.
An older binary also restores the old inline cleanup behaviour, so keeping the
new binary with maintenance paused is preferable during investigation.

## Validation and limits

Before the index addition, fourteen top-level tests and six subtests passed:
bounded batches, exact cutoff, refreshed timestamp revalidation, delete-fault
rollback, independent tables, independent evaluation and visible task failures.
A real SQLite accounting/evaluation test commits new usage during delete
failures (120 to 240 bytes), then checks failed/successful maintenance and late
replay leave financial/state/queue/publication/audit snapshots unchanged.

After index integration, the four-package targeted run passed (metering 0.002s,
store 0.068s, handler 1.815s, datastore 0.918s). The final handler/schema review
passed again (0.528s/1.061s); migration inventory and embedded SQL parsing passed
(0.015s). SQLite EXPLAIN now requires the global covering-index search and no
temporary sort. Exact commands and historical failures are recorded in the
[validation receipt](validation/2026-10-20-observation-retention.json).

Real MySQL schema/EXPLAIN, delete-failure accounting, and paused point-lock
receive/settlement tests are included but await their isolated database slot.
Full backend, new CI, capacity and long-duration retention validation are still
required; earlier commits' green CI is not evidence for this batch.

The diagnostic unique `(node_id,event_id)` identity expires with its raw row,
as it did before this change. A late flow-start replay can appear diagnostically
again. No permanent per-connection receipt was added; financial replay remains
protected by separate authoritative identities. This is not permission to
archive the traffic ledger or a claim of archive-safe historical statistics.
Nor does a 1,000-row cycle establish a database-growth bound at an unmeasured
ingestion rate, physical I/O bound, or P4/P6 completion.
