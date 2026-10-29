# P4 daily usage projection: recovery boundary and evidence

This is a verified incremental boundary, not completion of P4 or P6. The source
is the current code and tests on `codex/core-convergence`, based on `665578d`.
The overall decisions and remaining phases remain in
[core-convergence-decisions.md](core-convergence-decisions.md) and
[core-convergence-plan.md](core-convergence-plan.md).

## Actual change

Previously `CompletionAccounting.Complete`, `BatchAccounting.Apply` and
`NodeReports.Record` wrote `protocol_endpoint_usage_daily` in the same transaction
as TrafficRecord, quota and cumulative flow cursors. A daily chart upsert failure
therefore rolled back authoritative settlement, despite reliable spool receipt.

They now insert `metering_usage_projection_pending` with the ledger/quota commit.
Migration 0053 adds this reliable recovery table. Each intent has a ledger ID,
endpoint ID and creation time; its primary key is the exact outstanding fact,
not a global maximum ledger ID. Concurrent transactions can commit IDs out of
order, so a global high-water mark would lose a late lower ID.

`meteringstore.UsageProjectionWorker.Drain` handles 1–200 pending facts per call.
It locks pending rows in ID order, reads those immutable ledger deltas, writes
daily buckets in endpoint/date order, and deletes exactly those pending rows in
one separate transaction. Errors return zero committed progress and retain all
intents. It sums already settled raw/upload/download/billed bytes; it does not
reapply multipliers, recompute charges, or change rounding. The existing
application job runtime calls this worker, independently of event settlement.

Inventory uses an indexed pending `EXISTS` for each selected endpoint in the
same SQL snapshot as its daily sums. Pending traffic totals are JSON null with
`traffic_projection_pending=true`; operational observations remain independent.
The protocol page explains the pending state. A statistics reset rejects an
incomplete baseline without writing reset/audit records. Other endpoints remain
independent.

## Migration, compatibility and rollback

Existing daily buckets retain their exact values. Historical ledger entries are
not replayed into the additive projection. Stop old writers during the cutover;
running old synchronous and new asynchronous writers concurrently is unsupported.
The queue and its TrafficRecord sources must participate in a consistent backup
or logical transfer. The FK restricts deletion of a pending source fact.

Preflight runs before SQLite AutoMigrate. If 0053 is recorded but its queue or a
required column is missing, startup refuses to fabricate an empty recovery state.
Before the version mark, interrupted complete DDL can recover its index/FK while
preserving existing intents; an incomplete table containing intents is rejected.
SQLite additionally refuses a complete queue lacking its source-protection FK.

For rollback, stop settlement, drain all pending daily work with this version,
verify no pending row remains, and then start an older writer. The down migration
preserves the queue. Restoring a whole mutually consistent database backup is an
alternative; deleting pending intents is not a rollback strategy.

## Verification

2026-10-20, cloud workspace only. These narrow tests passed with Go 1.26.8:

```sh
GOMAXPROCS=1 go test -p 1 \
  ./internal/adapters/persistence/meteringstore ./internal/datastore \
  ./internal/adapters/persistence/networkstore ./internal/handler \
  -run 'TestUsageProjection|TestSQLiteUsageProjection|TestProtocolEndpointPending|TestProtocolUsageProjection|TestAccountingDailyProjection|TestCompletionCapabilityRollsBack|TestNodeReportCapabilityTransaction|TestAccountingBatchReplayOutOfOrderAndExhaustion' \
  -count=1 -timeout 5m

GOMAXPROCS=1 go test -p 1 ./internal/datastore \
  -run TestSQLiteUsageProjection -count=1 -timeout 2m

GOMAXPROCS=1 go test -p 1 ./internal/handler \
  -run '^TestAccountingUsageProjectionIntentFailure' -count=1 -timeout 2m

pnpm test src/views/Protocols.test.ts --no-file-parallelism
```

The four backend packages passed (0.063s, 0.023s, 0.230s and 0.979s in one run).
The expanded schema tests passed in 0.022s; durable-intent failure/replay passed
in 0.266s. Protocols passed all 22 tests.
These timings are test runner results, not request latency measurements.

Behavioral evidence covers actual flow-batch settlement while the daily write
fails, replay without double settlement/projection, persisted recovery after
closing/reopening SQLite, failed checkpoint deletion with aggregate rollback,
late lower ledger IDs, batch limits, original settled deltas, pending nulls,
endpoint isolation, reset rejection, interrupted migration and refused lost
recovery state. Two production-driver SQLite workers serialize through the
existing `BEGIN IMMEDIATE`; the second cannot read a stale pending snapshot
while the first owns the writer transaction.

The real isolated MySQL 8.4 run also passed (10.633s):

```sh
ZBOARD_TEST_MYSQL_DSN='<isolated test server DSN>' GOMAXPROCS=1 \
  go test -p 1 ./internal/handler -run '^TestMySQLUsageProjection' \
  -count=1 -timeout 4m
```

The runner provisions random databases and removes them after the tests. The
MySQL-specific tests are
`TestMySQLUsageProjectionCompetingWorkersAndCheckpointRecovery` and
`TestMySQLUsageProjectionSchemaPreservesInterruptedIntentsAndRefusesLostQueue`.
They cover concurrent workers, failing checkpoint deletion, replay, real
inventory SQL with pending and recovered states, repeated/interrupted migration,
preserved pending rows, FK source protection and refused lost recovery state.

## Reproducible limited cost sample

2026-10-20 environment: Linux x86_64; AMD EPYC 9V74; 5 visible logical CPUs;
cgroup CPU budget `400000 100000` (4 CPUs), memory limit 16 GiB; Go 1.26.8;
`GOMAXPROCS=1`; SQLite persistent temporary file using the repository's WAL,
immediate transactions and normal synchronous setting. At the environment check,
the 32 GiB workspace filesystem had approximately 1.5 GiB free. No other scheduled
MySQL test was running during this sample.

```sh
GOMAXPROCS=1 go test -p 1 ./internal/adapters/persistence/meteringstore \
  -run '^$' -bench '^BenchmarkUsageProjectionRecovery200$' \
  -benchtime=1x -count=5 -timeout 2m
```

Each sample starts with 200 newly settled synthetic facts across 8 endpoints,
raw delta 7 and settled billed delta 11 per fact. Fixture insertion is outside
the timer. Each timed batch executes 4 ORM SQL statements: locked pending
selection, ledger selection by those IDs, one daily bucket upsert, and pending
deletion. Driver BEGIN/COMMIT calls are not included in that SQL count.

| Sample | ns per 200-fact batch | ORM SQL per batch | Derived facts/s |
| --- | ---: | ---: | ---: |
| 1 | 2,625,026 | 4 | 76,190 |
| 2 | 2,590,943 | 4 | 77,192 |
| 3 | 2,360,499 | 4 | 84,728 |
| 4 | 5,258,991 | 4 | 38,030 |
| 5 | 3,051,563 | 4 | 65,540 |

This measures a single auxiliary recovery batch only. It is not event ingestion,
quota settlement, HTTP capacity, p95, sustained throughput or autoscaling proof.
There is no same-condition old synchronous implementation measurement, so no
improvement ratio is claimed.

## Remaining boundaries

Hourly usage remains synchronous: SQLite ledger triggers and the MySQL accounting
writer can still fail settlement. Online/coverage projections and their spool
consumer ordering have not been fully isolated by this patch. TrafficRecord and
cumulative/dedup/recovery state are retained; no archive/delete policy is added.

Full backend/CI, complete MySQL regressions and all other phase checks remain
the integrating stage's responsibility. The cost sample does not measure core
HTTP p95, acceptance-to-settlement lag, queue growth, CPU/RSS, database/index/file
growth, slow-database behavior, quota-to-revocation latency or observed overuse.
Those require a separately scheduled sustained workload with fixed resource
budgets before P6 or bounded retention can be accepted.

### Integration regression found by the complete backend run

The first complete run at cf1c8be found one failed pre-existing networkstore test:
its synthetic daily totals referenced endpoint 9 without creating that endpoint,
while the new bounded pending probe starts from selected actual endpoint rows.
The fixture now creates its physical node and endpoint, retaining raw ledger
9999 versus daily 31+47 assertions. The targeted test passed in 0.216s. No
production read was changed and no assertion/exclusion was removed. All other
packages passed in that run, including handler188.071s and plugins70.988s; the
complete regression and exact-head CI remain separately tracked.
