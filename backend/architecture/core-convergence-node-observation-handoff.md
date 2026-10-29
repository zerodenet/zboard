# Durable node observation handoff (P4/P5, partial)

This increment separates auxiliary node counters from financial settlement. It
builds on [0059 admission identity](core-convergence-node-asset-foundation.md).
The 0059 document remains evidence for that earlier increment; its historical
statements about missing handoff describe that version, not this increment.

## Authority and behavior

The existing financial transaction settles its original flow batch and appends
immutable observation headers/parts. A reliable append failure rolls back that
transaction and leaves the spool checkpoint available. A later node counter,
reference or ACK failure cannot roll back committed accounting. Only the new
worker applies buffered node observations; there is no simultaneous old stats
writer. The original 2000-envelope window, financial sample reduction, checkpoint
selection, report identity, first_sequence and per-sample rounding are unchanged.

Every original envelope retains its own authenticated0059 asset tag, routing
identity, payload, SourceID, event position and receiver time. A versioned binary
stream/hash identifies this auxiliary material, not a new billing identity.
Parts are at most60KiB; identities are not narrowed into VARCHAR(128). Unknown
labels/times are never filled from a current row or replay clock. Original
nanoseconds remain in material and the receiver comparison stamp. The API cache
uses portable millisecond precision on both drivers and each position keeps its
own time. This does not attest the actual installed runtime.

Stats and Latest advance independently: flow100 does not discard stats99. A
Latest from runtimeB retains historical Stats from runtimeA without representing
those counters as current. Explicit heartbeat/legacy counter writes share the
node→asset-state order and mark current provenance unknown to buffered Stats;
liveness-only heartbeat preserves known counters and provenance. Empty current
asset labels cannot join the old unattributed floor.

A short READ COMMITTED claim releases the queue lock before node application.
Apply locks node, exact asset state and finally an exact owned token/lease ACK.
No node/financial foreign key couples producer inserts to node locks. Expired or
replaced ownership rolls back counters/state. Busy/bad nodes defer independently;
real failures remain visible. Deletion retains an exact-asset tombstone and all
material; numeric ID reuse cannot adopt old counters. No MAX-ID ACK is used.

## Bounds and remaining limits

One drain permits at most200 claimed items,2MiB decoded source and8MiB raw
material reads, including old floor/Latest/Stats references; the periodic task
requests32 items. Actual part length is checked before blob fetch, including
against the remaining declared material bound. Oversize, unknown or corrupt
items remain held with material. This is not a producer admission/financial cap.

There is no held-release operator workflow, automatic material retention, global
queue growth bound or complete process RSS claim in this increment. Authority
TrafficRecord, cumulative cursor, dedup and financial audit remain unarchived.
The existing limited diagnostic cleanup is separate. New source/version-specific
capacity and final validation receipts must be read before accepting this code;
previous681/0f results do not validate the new worker. P4, P6 and P7 remain open.

## Migration, copy and rollback

0060 adds intents, immutable material parts and per-asset source state, without
node/financial foreign keys. The receipt is last, after importing existing old
node positions only as explicit unattributed floors; no old cursor is relabeled
as having applied Stats. Recorded missing tables/columns or incompatible shape
refuse startup/copy before SQLite AutoMigrate. Incomplete empty table creation
can resume; retained source loss is not repaired by silently recreating it.
Metadata query/scan/iteration/close failures propagate. Conflicting ready-index
shape refuses; a missing valid index can be recovered.

Owned copy preserves statuses, leases, tokens, refs, tombstones and exact parts.
Genuinely legacy sources import their copied cursor as an unattributed floor
inside the owned target transaction. Completion failure rolls everything back.
Source business values are preserved; pre-existing task normalization remains.
Tests canonicalize time zone object identity only, without timestamp tolerance,
precision truncation, payload normalization or relaxed source comparisons.

Quiesce old consumers/counter writers before upgrade and back up database/spool
as one recoverable cutover. Old binaries do not maintain0060 state or drain it;
concurrent old/new writers and old-write/roll-forward are unsupported. The down
asset preserves tables, receipt and material. Do not drop these to force rollback
or infer missing source history. Rollback requires a coherent compatible backup
or proven compatible forward recovery. No production migration is performed.

## Administrative entry and verification

Node inventory/detail read counters and provenance in one SQL view. The new
statistics observation item displays recorded/unknown source and receiver time;
it does not claim online, fresh, authorized or configuration-applied status.
Missing/invalid time remains unknown; switching nodes cannot inherit old source
metadata. No new frontend request, financial action or permission is added.

Targeted validation passed in the original cloud environment. Actual SQLite and
MySQL schema/transactions, independent source advancement, owned-copy matrices,
financial append rollback/independent retry and HTTP behavior are recorded in the
[targeted receipt](validation/2026-10-21-node-observation-handoff.json), with50
code/test/SQL/OpenAPI/frontend file hashes. Core focused race ran count3 (48SQLite
and12MySQL top-level executions); after HTTP/writer wiring,9SQLite and3actual
MySQL executions passed count3. No new skips were introduced.

The candidate failures and exact frozen-source differences remain in that receipt:
nil/empty payload, empty composite-key Save, copy time zone/implicit process-time
precision, missing HTTP response metadata, invalid new heartbeat fixtures, and
counter writer misclassification. The pre-fetch budget issue was found statically
before execution; no executed pre-fix regression is claimed. Financial reduction
and151 published SQL remain byte-identical. OpenAPI lint passes with282 warnings.

Frontend full current CI scope passed202files/1124tests, original2files/8skips
and six exclusions unchanged; build passed. One later selected-node integration
test plus the new component pass45focused tests and typecheck; runtime sources
are unchanged from that build. New integrated full backend/MySQL/capacity and CI
remain pending and require their own exact source. No prior CI substitutes.


The first integrateda82e5b6 full backend and exact CI backend failed: final
heartbeat UPDATE lost its credential predicate, and four inventory owners were
missing. These are retained in the [first full failure](validation/2026-10-21-node-observation-first-backend-failure.json),
not dismissed as old test expectations. The follow-up restores the conditional
write before provenance, preserves a credential rotation while rejecting an old
request, and distinguishes valid MySQL zero-change replay. Ownership inventory
and distinct producer/apply transactions are explicit. SQLite/actualMySQL and
focused race3 pass; [follow-up receipt](validation/2026-10-21-node-observation-credential-fence.json).
Fixed integrated full checks/capacity/CI require new evidence; a82 local
vet/build/pureSQLite and originalMySQL/capacity were not executed after its Go
failure. No exclusions, timeout or assertion boundary was relaxed.

Exact follow-up `ae2aadb3d9d5fc50c2fd9706b25815e17578376a` now passes all four
original backend commands in a clean ordinary cloud clone: full Go339.057s,
vet13.212s, build1.946s, pure-Go SQLite39.185s. The
[full backend receipt](validation/2026-10-21-node-observation-final-backend.json)
binds each raw log. Its new CI backend, frontend, contract, compose and
zrs-compiler pass; MySQL and original constrained capacity remain separate checks.
The [first integrated CI](validation/2026-10-21-node-observation-first-ci.json)
is final failed evidence, including both the credential assertion and the
independent MySQL handler900.019s timeout; later CI jobstore/copy did not execute.

The [exact ae2 CI](validation/2026-10-21-node-observation-final-ci.json) is now
complete: all six jobs pass. Actual MySQL datastore144.020s/network143.066s/
handler723.526s, then jobstore92.546s and owned-copy159.289s execute and pass.
Earlier900s failures remain; one successful run is not timing-stability proof.

The [new original capacity run](validation/2026-10-21-node-observation-original-capacity.json)
passes unchanged1CPU/1GiB/20events/s/4readers/60s gates. Hourly158 requests,
zero errors, p95=27ms under250ms;1274 event requests and942 business reads have
zero errors. The first drain has three quota mismatches and20 pending usage facts;
the original5-second retry reaches exact9,833,472B and empty pending/spool.
All1274 observation intents apply,591857B material, no invalid lengths/headers
or missing state refs. This flow-only fixture advances Latest for2 nodes but
correctly leaves Stats/current-counter source unknown. Approximate maximum
header-created-to-ACK3539.993ms is not receive-to-settle or freshness latency.

Four disk preflights stopped before offered load. All five attempts bind the
same source/observer/both binaries; only the last actually offered load. Official
Go/npm rebuildable-cache cleanup and lossless gzip archival of stopped exports'
allocation reserve files recovered space. Every archive's decompressed hash is
verified; databases, event material/checkpoints, source and binaries remain.
The first archival atime comparison failed before unlink, and its verified
recovery is recorded. Earlier hourly157/112503 failures remain separately.
One short SQLite load does not validate MySQL capacity, long-term retention,
real Stats throughput, plugin-busy behavior or standalone-worker autoscaling.

Final [original local MySQL](validation/2026-10-21-node-observation-mysql-local.json)
on exact ae2 retains the handler900.019s timeout, datastore169.240s and
network168.592s pass. Last2s receipt case is still in real RunMigrations fixture
preparation; no assertion failure is reported, and this is not cumulative cause
proof. Independent jobstore120.078s and copy216.211s command wall times pass
sequentially after core failure. The [metadata24bd247 CI](validation/2026-10-21-node-observation-metadata-ci.json)
has its own final result: five jobs pass, MySQL handler900.024s fails;
datastore196.554s/network187.261s pass, later CI jobstore/copy do not execute.
The ae2 six-job success is not substituted for this newer head. P7 timing
stability remains open; all original15m gates and assertions are retained.
