# Data growth and retention

This replaces permanent per-event connection statistics with bounded metric
buckets. Billing stays exact; display precision is deliberately lower. The
operator requires unlimited historical accounting reports, so billing/replay
state has no time-only expiry. Historical backup copies were explicitly removed
at the operator's request; this change does not create local or offsite backups.

## Ingress and storage contracts

| Data | Write/retention contract |
| --- | --- |
| Current principal snapshot | One row per node/principal; complete runtime identity and increasing revision fence duplicates and old generations. |
| Connection peaks | Write directly to scope/minute and scope/hour aggregates, in the same transaction as the current snapshot. No new raw principal observation, scope observation, statistics outbox or permanent event sample. |
| Minute peaks | 30 days, with UTC-hour-aligned expiry. |
| Hourly peaks | Long-term history; peak=max, count=sum. Growth follows active business scopes and hours rather than events. |
| Legacy event samples | Convert at most 200 to minute contributions per transaction, then remove the converted samples atomically. Never add their already projected hourly values again. Samples outside the minute window need no new minute row. |
| Legacy scope diagnostics | Remove bounded expired primary-key prefixes after legacy coverage and pending checks; 24-hour grace, retaining the original highest-ID allocator anchor. |
| High-frequency core reconcilers | Reuse one latest clean successful/yielded run and attempt; keep cumulative schedule counters. Failure, retry, unknown, interruption, explicit requests and plugin execution retain their own evidence under existing outcome policies. |
| Registration polling | Read-only work probe; empty queues create no execution history. |
| Native financial settlement | One minute row per user/subscription/epoch/node/endpoint/calculation-mode/rate, occurred UTC minute and received UTC minute/reset phase. Add already settled deltas; never re-round the minute's raw total. Fixed-size arithmetic proof replaces event bodies. |
| Report identities / live-node cumulative cursors | Permanent replay identities and exact cumulative counters. No age cutoff for historical financial reports. Multiple original receipts may reference one settlement ID. |
| Existing financial history | Retain old rows and sealed proof compatibility unchanged; no startup financial rewrite or bulk deletion. |

Minute rows are bounded by active scopes times 43,200 minutes, plus the current
hour and temporary maintenance lag. Future diagnostic timestamps are clamped
to the receiving clock. Hourly rows still grow with elapsed time.
This is not a constant-size promise for billing history or indefinitely delayed
reports. The minute writer preserves exact settled sums, epoch, pricing and reset
boundaries. Required report identities and flow counters still grow with business
history; indefinite exact replay cannot be implemented by expiring that authority.

## Read semantics

Recent connection trends use minute peaks at partial-hour calendar edges and
hourly peaks for complete interiors. During finite legacy conversion, remaining
samples and converted minutes are disjoint: inserting a minute contribution and
removing its event sample commit together. Older ranges use adjacent UTC-hour
boundaries; fractional-offset local days therefore have hour precision. Adjacent
days share exactly one boundary and never count one hour twice. Responses expose
`connection_resolution_seconds`, and the chart labels minute/hour precision.
No raw event body or full history scan is required by a trend request.
Normal relative timestamps have minute precision (sub-minute values read
“刚刚”/“即将”); exact tooltip times remain available for investigation.

Accounting and entitlement calculations do not use rounded display metrics.
Late principal snapshots retain the existing generation/revision rejection;
financial historical reports continue through their independent exact meter.

## Execution and maintenance fences

Compaction is limited to explicitly opted-in, idempotent core reconcilers.
Every reuse gets a new lease token, clears previous attempt/lease/finish state,
and requires one clean terminal attempt. Late completions cannot finish a new
execution. A definition change, retry or uncertain outcome creates a fresh run.
Dormant reusable slots remain pinned against ordinary history retention.
Retention takes only relevant schedule/record locks; it never takes the global
job admission lock. Scheduling pins its plan before editing a reusable run.

Maintenance uses the existing owned job executor; no extra service, scheduler,
process, external archive or startup backfill is introduced. Conversion faults
roll back contributions and checkpoints together. Deleting rows permits InnoDB
to reuse table space; it does not imply that `.ibd` files immediately shrink.

## Index budget

Core flow lookups use node/flow uniqueness and endpoint/status/seen. Unused
principal and standalone status/seen indexes are removed. Credential and
subscription indexes needed by foreign keys/cascades remain. Traffic scope/time
and history cursor indexes replace redundant single-column indexes. Report and
nonce uniqueness remain intact.

The schema owner validates fixed names and column shapes before removal, checks
replacement access paths, and batches MySQL removals with `ALGORITHM=INPLACE,
LOCK=NONE`. A pinned session caps metadata-lock waiting at five seconds and
restores the session setting. SQLite reuses its existing `(record_at,rowid)`
access path rather than building a new large history index. Fresh models do not
recreate the removed indexes. Operator-defined unique/unknown shapes fail closed.

## Measured evidence and delivery boundary

Production sampling on 2026-10-31 found 10,000 different flows in 10,000 recent
meter records; zero deltas were only 6.69%. Filtering empty reports alone is
insufficient. Roughly 4,800 of 5,000 recent job rows came from high-frequency
registration/statistics polling. Selected redundant/unused production indexes
accounted for about 1.9 GiB of logical index pages; that is not an OS-space
reclamation estimate.

Local regressions exercise 1,000 principal snapshots in one minute: two minute
rows, two hourly rows and zero rows in the four event-history layers. Another
1,000 clean periodic executions keep one run and one attempt, while cumulative
runs remain 1,000 and old completion tokens are fenced. Fault injection verifies
minute conversion/current writes roll back; conversion does not alter hourly
counts; minute expiry retains hourly history; node deletion recomputes surviving
scopes while retaining financial/legacy evidence. Index tests preserve ledger
rows and unique/FK lookup contracts across upgrade/restart.

These measurements establish row and access-path behavior, not a production I/O
or disk-growth forecast. Source tests, actual MySQL acceptance, image publication,
deployment and production growth measurements are separate delivery states.

## Minute settlement and upgrades

Migration 0078 adds two ledger columns and five pending-delta columns. MySQL uses
INSTANT metadata changes, no large-table index build, history scan or backfill;
partial DDL resumes after validating types, defaults and nullability. Existing
rows implicitly have contribution count one and remain unaggregated. Old sealed
proof digests remain byte-compatible. SQLite preserves nanosecond receive clocks;
MySQL uses its existing millisecond storage precision.

Each original charge is calculated first, with its multiplier ceiling and quota
cap, inside the existing authoritative transaction. The minute writer then sums
charged/raw/directional bytes, contribution count, accounting sample count and
rounding numerators. It keeps no list of flow IDs, credentials, event sequences
or per-event timestamps. Live flow cursors and independent receipts still fence
older cumulative snapshots and arbitrary historical replay.

Received-minute keys split at the subscription anniversary's sub-minute offset;
epoch and rate are separate dimensions. This preserves exact reset carry queries
on created_at, including reports arriving on opposite sides of a reset. The owned
transaction clock, rather than a second wall clock, supplies receive time.

The durable projection checkpoint accumulates only NEW deltas. A worker locks
that checkpoint, applies the increment exactly once and deletes it atomically.
A concurrent writer recreates/updates the next checkpoint after the worker;
it never projects the whole mutable minute twice. Recovery builds hourly totals
from settlement totals minus outstanding increments, leaving those increments
for the normal worker. Page totals count physical displayed rows, while usage
charts weight contribution counts. Statistics continue to use exact byte sums.

Once minute settlements exist, deploy only a compatible writer. Additive schema
columns stay in place; a pre-0078 binary cannot recover discarded event bodies
or understand the new delta checkpoint contract. No automatic downgrade or
reconstruction of per-event history is offered.
