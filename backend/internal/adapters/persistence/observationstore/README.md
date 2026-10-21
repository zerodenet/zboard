# Node observation handoff (0060)

Targeted SQLite/MySQL transaction, migration, owned-copy and focused race
validation is recorded in `backend/architecture/core-convergence-node-observation-handoff.md`
and its linked receipt. New integrated full checks, capacity and CI are separate
acceptance gates; previous source results do not validate this increment.

The authority transaction settles the original financial samples and appends
an immutable source intent. It never updates `nodes` or observation state. A
parts/header insertion error rolls back authority and leaves the original spool
checkpoint replayable. Projection failure happens after authority commits and
cannot roll back settlement. No financial report key, source dedup, first
sequence, checkpoint, nonce, sample rounding or merge window changes.

Material is a versioned length-delimited binary stream, preserving both original
positions and the original envelope routing fields/payload. SHA256 identifies
that complete material; it is not a new financial identity. Sixty-KiB parts keep
long routing identifiers out of VARCHAR columns and bound each fetched blob.
Even a forged declared size cannot fetch an unbounded blob: SQL fetches at most
PartBytes+1 after verifying the actual length also fits the remaining declared
material budget. There is no
truncation or billing admission limit derived from auxiliary storage size.

A drain claims one item in a short READ COMMITTED transaction, then releases the
queue lock. Apply takes node, asset state and final exact-token/lease ACK in that
order. No node, credential or financial FK is installed on queue tables. Busy
nodes defer their item. Item-specific failures defer and remain visible as real
errors while other sources in this bounded round can still advance. Lost/expired
ownership rolls back counters/state. ACK never advances a MAX-ID cursor.

Drain caps: 200 claimed items, 2MiB per decoded source, 8MiB raw retained material
per round including prior floor/Latest/Stats references. The normal periodic job
requests 32 items. Above-limit or unattributed sources are HELD with all parts;
there is no automatic held replay API, retention cleanup, global queue-growth
limit, total process RSS guarantee, pressure benchmark or throughput claim.
Producer streams parts within the EXISTING authority transaction and existing
2000-envelope window; one previously accepted large compacted record may still
make that transaction larger. Decoder allocations/driver metadata are additional
to the raw material byte counter and are separately bounded by count/item caps.

Independent Stats and Latest refs retain their own captured asset and runtime.
An old cursor is imported only once as an UNATTRIBUTED floor (empty asset), never
as applied Stats/Latest. New flows cannot advance it. Fresh flow100 followed by
stats99 can update Stats independently. Cross-runtime Latest/Stats do not make
current counters known. Missing/zero original positions or labels stay unknown;
replay never looks up the current label or substitutes Now as a source time.

Administrative node deletion writes exact-asset and unattributed-floor
retirement under the node lock; pending/history/parts are not deleted. Numeric
ID reuse has a different0059 tag and cannot adopt an old intent. Heartbeat and
legacy explicit counter updates share node→state ordering and mark their current
values unattributed to buffered Stats. Liveness-only updates preserve evidence.
Kernel/config credential rollback production logic is unchanged.

API `observation` fields on node administration/inventory:

- `known`: current numeric counters have attributed buffered Stats consistent
  with the most recently observed runtime. This is NOT freshness, online status,
  installed-runtime/configuration proof or permission evidence.
- `source`: current counter writer/disposition (`unknown`, `buffered_stats`,
  `historical_stats`, `heartbeat`, `legacy_connector`, `invalid_reference`,
  `deleted`).
- `latest_source_known`, `stats_source_known`: independent recorded source state,
  not a claim that a historical Stats value is the current numeric counter.
- `counter_received_at`: recorded receiver time of the current counter writer;
  buffered Stats.ReceivedAt or heartbeat server update.At, never replay/apply Now.
- `latest_occurred_at`, `latest_received_at`, `stats_occurred_at`,
  `stats_received_at`: each original position's own time, absent when unknown.
- `latest_reference`, `stats_reference`: opaque audit refs; UI need not display.

Counters and evidence are selected in one SQL statement to avoid attributing an
old counter snapshot using a later state read. Node responses not explicitly
joined report conservative unknown rather than invent evidence. Source times in
read-state caches use portable millisecond precision on both drivers; immutable material
and receiver-time comparison stamp retain original nanoseconds.

0060 receipt is recorded after schema and all legacy floor imports succeed.
Recorded missing tables/columns, wrong types/PKs/identity collations or an added
FK reject before SQLite AutoMigrate; missing ready index can be recovered and a
conflicting shape fails. Interrupted floor import resumes without overwriting an
already imported floor. A changed floor during incomplete initialization fails.

Owned copy retains pending/claimed/applied/held material, refs, lease and deletion
state. Recorded source loss rejects, rather than reconstructing authority. A
truly legacy source imports its copied cursor as an unattributed floor inside the
existing owned destination transaction. Completion failure rolls back copied
sources and floor together. Source normalization of Task.content retains its
existing behavior; this is not a whole-pipeline no-source-write claim.

Deployment/rollback limits:

- Schema upgrade requires quiescent old observation writers/consumers. There is
  no new distributed migration/rolling-version fencing framework in this slice.
- Older binaries neither maintain independent state nor drain these intents.
  Concurrent old/new counter writers, or writing with an old binary followed by
  rolling forward, can invalidate the provenance contract and are unsupported.
- The down asset deliberately retains tables/receipt/material. Dropping them
  loses pending and identity evidence. Retention is not a claim that active old
  binaries can safely resume0060 work. Rollback needs a coherent pre-cutover
  recovery plan/quiescence; no history can be fabricated from current labels.
- Queue corruption discovered on replay is a durable-storage failure: producer
  refuses to ACK a source whose existing retained material no longer verifies.
  The spool remains available for recovery; no repair/delete-bad-head endpoint
  silently replaces authoritative evidence.
