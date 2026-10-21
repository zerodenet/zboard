# Node asset admission foundation (P3/P4, partial)

This increment gives future buffered admissions a server-generated panel asset
label. It prepares the source evidence needed before observations can be split
from accounting. It does **not** enable an asset admission/deletion fence or
independent statistics cursor, and does not resolve the existing transaction in
which failed node statistics can roll back accounting. P4 remains incomplete.

## Behavior and authority

`model.Node.AssetTag` is 32 random bytes encoded as 64 hex characters, generated
on ORM creation, omitted from public JSON and protected from ordinary ORM
updates. Credential rotation, SSH changes and renaming keep the label. Deleting
and explicitly reusing a numeric node ID creates a new label. Privileged raw SQL
can still change it; this is not database-enforced immutability or authorization.

The existing authenticated event credential lookup captures the label together
with the credential. The private file-spool envelope receives that captured
label; neither the outer Zero wire JSON nor its payload can supply it. An
existing cache entry keeps its captured value. This changes no external Zero
wire contract, permission predicate, accepted CoreInstanceID length, accounting
report identity, rounding, or checkpoint rule. No core, sink, plugin or Agent is
modified or required.

Latest and StatsEvent each carry their own captured source label. Missing old
material stays unknown; a later current database row is not used to fill it.
Version-one framing/CRC stays unchanged and the JSON member is optional. Raw,
zstd and lz4 representations retain it. In-memory flow reduction and persistent
semantic compaction preserve every original record, in order, when a node/core
lineage within that already loaded segment/window contains different labels,
including a known/unknown transition. This is not a cross-spool proof. A/B/A
must not merge backwards across B, even when B describes another flow. Uniform
labels and wholly legacy lineages retain the existing optimization.

The label is not yet used as a billing key or observation gate. Old queued
events from a deleted/recreated node still require a later current-asset and
deletion policy, independent Stats position, and consistent lock ordering.
The earlier [source-material audit](core-convergence-event-source-material.md)
and transaction-coupling findings remain applicable; no safety fence is claimed
from carrying a label alone.

## Migration, copy and recovery

New migration 0059 adds `nodes.asset_tag` on SQLite and MySQL. Existing published
SQL files are unchanged. Before AutoMigrate, full metadata reads propagate query,
scan, iteration and close failures instead of treating them as absence. A saved
0059 receipt with a missing nodes table/column is refused before automatic repair.
An incompatible column shape/default is also refused. A receipt and column both
lost cannot be distinguished from a genuine legacy schema; restore evidence
rather than pretending this detects every destructive operator change.

Without a receipt, initialization fills empty current rows in batches of at most
200 IDs with conditional writes. Saved labels survive interrupted initialization;
the receipt is recorded only after initialization finishes. This establishes
future capture identity and does not reconstruct historical events. Once the
receipt exists, an individual empty row remains explicitly unknown on restart.
Concurrent initializers do not overwrite an already saved label; simultaneous
missing-column DDL may fail and require normal recovery, with no new distributed
migration coordinator.

The existing owned database copier classifies the source before opening streaming
Rows, preserving its single-connection boundary. Complete sources preserve every
label, including unknown empties. A genuinely legacy source receives future-only
target labels in the existing row insert/target transaction. Recorded structural
loss is rejected. On completion failure, copied labels, rows, task/run and
completion roll back together; the owned, prepared empty target remains available
for retry. Existing source task normalization/bookkeeping remain: the change adds
no source writes, but the complete migration pipeline is not globally read-only.
The prior principal-index version-zero copy fix remains in place.

The down migration deliberately preserves labels and the receipt. A binary
rollback requires stopping/fencing consumers, a database and spool backup, and
source-version verification. Old binaries can ignore or discard the optional
member during re-encoding/compaction; cross-version identity safety is not
promised. Do not drop the column, erase the receipt, relabel queued records or
run production migration as part of this PR. Returning to this version preserves
unknown rows created by an older binary rather than inventing their history.

## Validation status

Targeted execution passed in the original cloud environment against 0f491e1 plus
the frozen candidate. Actual SQLite schema, management, owned copy, HTTP append,
replay/rounding and all51 original spool tests pass. Actual MySQL schema, the
three-body network wrapper and all8 cross-engine copy fixtures pass. Focused
`-race -count=3` covers ten pure tests, four handler tests, three network tests and
the public SQLite owned-copy completion rollback leaf. The original subprocess
helper is skipped in the parent suite as designed;
the actual cross-process ownership parent test executes. Two invalid new test
fixtures omitted mandatory stats counters. Migration and cross-engine copy
fixtures also compared transient JSON text formatting instead of stored/semantic
values. All failures and test-only corrections are preserved, and are not counted
as proof of production defects. Production sources remained identical throughout.

Controlled old production-file overlays retain the new test/type compatibility;
they are not the old binary. They reproduce mixed-lineage compaction loss, flow
merge/reordering, missing authenticated stamp after actual HTTP append, and
missing legacy target labels after successful owned copying. These are9 actual
assertion leaves; the invalid HTTP400 attempt is separate. Actual MySQL and race
are separate fixed-source executions, not inferred from controls. The
[targeted receipt](validation/2026-10-21-node-asset-foundation.json) records all23
final source hashes, commands, outcomes, earlier failures and source-version
differences. New integrated full checks, original MySQL, capacity and CI remain
pending; previous0f results belong only to0f.

No benchmark, long-term growth bound, independent auxiliary handoff, authority
retention policy, worker or autoscaler is delivered by this increment. Existing
financial-policy questions and external capability gaps remain unchanged.


Integrated6817667 full backend passes: Go301.855s, vet3.926s, build2.078s,
CGO-disabled SQLite22.346s. [Exact-source backend receipt](validation/2026-10-21-node-asset-backend.json).
The first original capacity attempt was blocked before offered load by unchanged
normal-pressure disk headroom (759451648B). All9bindings pass; latency/counts are
unmeasured. The [failed preflight](validation/2026-10-21-node-asset-capacity-preflight-failure.json)
is retained. With no active Go tools, official build-cache cleanup recovered
4.1GiB; a separate same-source/observer attempt is in progress with unchanged gates.
NewCI39531386103 currently has five successful jobs, MySQL still pending. Original
local MySQL has not yet run. No earlier source acceptance substitutes these results.

The new original capacity result now passes:1274event requests and940business
reads/zeroHTTPerrors; hourly157/p95=20ms. First10drain mismatches remain, original
5s retry then exact9,833,472B; queues empty. Source, observer and both binaries
are identical across the disk-blocked preflight and successful load.
[Capacity receipt](validation/2026-10-21-node-asset-original-capacity.json).
NewCI39531386103 is final:five jobs pass, MySQLhandler900.024s timeout;
datastore184.150s/network143.669s pass. CIjobstore/copy do not run.
[Failed newCI receipt](validation/2026-10-21-node-asset-ci.json). Original local
MySQL remains pending; no source acceptance or P7 completion is inferred.

The local original run is final:handler900.025s fails;datastore155.586s/
network130.686s and independentjobstore125.359s/copy187.273s pass.
[Exact-source local receipt](validation/2026-10-21-node-asset-mysql-local.json).
No fullMySQL or stableP7 claim is made.
