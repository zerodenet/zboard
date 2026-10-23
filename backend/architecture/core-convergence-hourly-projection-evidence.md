# P4 hourly usage projection: durable intent and bounded recovery

This incremental batch follows the [0053 daily projection evidence](core-convergence-metering-projection-evidence.md),
integrated at `cf1c8be311b9b6bb6f8d103da3233705208db53c`. It does not complete P4
or P6. All evidence below is from the cloud integration workspace on 2026-10-20;
the integrating commit and complete CI results are recorded separately.

## Business facts and auxiliary state

Previously SQLite's four ledger triggers, and MySQL's accounting writer, updated
`traffic_usage_hourly` inside authoritative settlement. An hourly write failure
therefore rolled back ledger facts, cumulative cursors and quota. Migration 0054
removes that synchronous hourly writer. Accounting now commits its immutable
TrafficRecord, quota/cursors and a durable projection intent in one transaction.
An intent insert failure still rolls back settlement; no in-memory ACK is added.

The existing bounded `UsageProjectionWorker` handles at most 200 exact ledger IDs
per call. It validates source identities, scope and projected UTC hour, applies
hourly and daily increments, and removes exactly those intents in one independent
transaction. Failure of either aggregate or checkpoint removal retains the whole
batch. It copies already settled raw, upload, download and billed deltas and their
multiplier dimension without recomputing billing or rounding. SQLite stored-bucket
integer overflow is rejected instead of silently promoting an integer to REAL.

All new facts enqueue hourly work, including endpoint 0; daily projection skips
endpoint 0. Each intent holds `hourly_pending`, user, subscription, node and the
actual projected UTC hour. Existing 0053 rows default to `hourly_pending=false`:
their synchronous hourly delta has already committed and must not be added again.
The exact pending ID set remains the recovery boundary; there is no maximum-ID
cursor which could omit a late lower committed ID.

The worker uses READ COMMITTED on MySQL. A real lock test found that the previous
default REPEATABLE READ `FOR UPDATE LIMIT 200` could lock the queue's supremum gap
when the batch was short. A worker waiting for an hourly bucket then blocked new
core intent inserts. READ COMMITTED retains exact pending row locks while removing
that gap dependency. The test holds an hourly bucket lock, verifies a second core
batch commits quota while the worker waits, and then verifies exact recovery.
SQLite still has one physical writer; separate transactions do not promise that
an auxiliary write can never delay another SQLite writer.

## Pending reads and time boundaries

Hourly summary, trends, breakdown, node series, dashboard statistics and rankings
probe indexed pending scope/time in the same database snapshot as their aggregate.
Affected reads return `usage_projection_pending` (HTTP 503), rather than a partial
sum or zero. Live quota, dashboard current business health and revenue-only trend
remain independent. Cached results are not returned while their required hourly
inputs are pending; no partial result enters the cache. Daily endpoint inventory
continues its existing per-endpoint nullable pending contract and reset rejection.

The existing aggregate source reads complete UTC hours from the hourly table and
at most two precise partial-hour edges from the ledger. Readiness uses those exact
complete-hour intervals; it does not introduce a fallback ledger scan. A query
starting at 10:30 must not wait on a pending 10:00 bucket that its aggregate never
reads. Calendar trends check each local day's complete UTC hours separately,
including midnight in half-hour and quarter-hour timezones. Adjacent intervals
merge; at most 366 intervals share one indexed `SELECT id ... LIMIT 1` probe.
SQLite and real MySQL tests cover Kolkata and Kathmandu midnight edges, pending
work in an actual full hour, and unchanged results after draining.

## Migration, transfer and rollback

0053 is unchanged. 0054 adds the five intent fields and five indexes with
`hourly_pending`, optional dimension and `record_at` as their leading/filter keys.
SQLite applies cutover DDL, four trigger removals and the version mark atomically.
MySQL resumes individually inspected DDL after interruption and removes legacy
triggers explicitly. Startup preflight runs before SQLite AutoMigrate. A marked
0054 missing queue/field is refused, and true intents with missing coverage/time
are refused; recovery never defaults lost completion evidence to false. Before a
version mark, empty interrupted DDL can complete without deleting existing intents.
The source-protection FK and 0053 lost-queue refusal remain in force.

Hourly rebuilding, including the existing `RebuildMySQLTrafficHourly` copy hook,
excludes precisely the ledger IDs whose `hourly_pending=true`. It retains the
pending queue. Otherwise copying raw history and then applying its intents would
double the newly settled deltas. SQLite and all three MySQL-involved transfer
directions verify confirmed history, legacy daily-only intents and new hourly
intents together. Fixtures use the formal `application.PrepareDatabaseSchema`
boundary, including optional schema reconciliation, and real FK parent rows.

Missing derived tables and an empty replacement with confirmed source facts are
recoverable without changing migration history or pending state. The empty-table
case covers MySQL interruption after a separately committed CREATE and before
atomic backfill. Older 0024 reconciliation and already marked 0054 reconciliation
both preserve the same pending exclusion. These paths never recreate an empty
reliable queue from raw history.

Stop all old writers during upgrade. Do not run old synchronous and new
asynchronous writers together. Rollback requires stopping intake and all workers,
draining the entire reliable queue with the current version, and then invoking
the explicit `PrepareHourlyProjectionRollback` maintenance entry. It refuses an
unmarked 0054, missing recovery coverage, pending work or missing/empty derived
state requiring repair. SQLite restores the original four triggers only after
these checks; MySQL uses the older binary's synchronous writer. The integrating
CLI provides the explicit maintenance flag. Re-upgrade removes restored triggers
without replaying already confirmed buckets. The down SQL preserves reliable
intent state; it does not silently enable an old writer. No production migration
or rollback was executed.

## Verification

Environment: Linux x86_64 cloud workspace, Go 1.26.8, isolated MySQL 8.4 random
test databases, repository SQLite WAL/immediate-transaction driver. Commands use
`PATH=/workspace/toolchains/go/bin:$PATH`,
`GOCACHE=/workspace/toolchains/cache/go-build`,
`GOPATH=/workspace/toolchains/gopath`, and `GOMAXPROCS=1`.

Final time-boundary SQLite run:

```sh
go test -p 1 ./internal/adapters/persistence/meteringstore \
  ./internal/datastore ./internal/handler \
  -run 'TestHourlyProjection|TestSQLiteHourly|TestUsageProjection|TestAccountingHourlyProjection|TestHourlyRebuild' \
  -count=1 -timeout 4m
```

Passed: meteringstore 0.763s, datastore 0.857s, handler 0.424s.
Log: `/tmp/zboard-hourly-final-sqlite-tests.log`.

Final real MySQL run:

```sh
ZBOARD_TEST_MYSQL_DSN='<isolated test server DSN>' go test -p 1 \
  ./internal/datastore ./internal/handler \
  -run '^TestMySQL(HourlyProjection|SlowHourlyProjection|UsageProjection|HourlyRetirement|TrafficHourlyDatabaseTransfer|TrafficSummaryReads|TrafficBreakdownReads)' \
  -count=1 -timeout 5m
```

Passed: datastore 16.127s and handler 48.444s.
Log: `/tmp/zboard-hourly-final-mysql-tests.log`. This includes 0053 competing
workers/checkpoint recovery, 0054 failure and replay, actual gap-lock isolation,
partial DDL, missing/empty derived recovery, refused lost scope, three transfer
directions, reporting and real half/quarter-hour calendar scopes. The latest
rollback precondition hardening is additionally verified by the SQLite regression
below; exact-commit global validation is the integration step's responsibility.

Existing MySQL hourly migration/upgrade fault regressions also passed separately:

```sh
ZBOARD_TEST_MYSQL_DSN='<isolated test server DSN>' go test -p 1 \
  ./internal/datastore ./internal/handler \
  -run '^TestMySQLHourly(Migration|Upgrade|ProjectionSchema)' -count=1 -timeout 5m
```

Passed: datastore 32.167s, handler 6.044s, including partial-DDL fault stages and
legacy trigger visibility handling. These runner durations are not API latency.

Final SQLite reporting/rollback regression after the maintenance preconditions:

```sh
go test -p 1 ./internal/adapters/persistence/meteringstore \
  ./internal/adapters/persistence/observabilitystore ./internal/datastore ./internal/handler \
  -run 'TestUsageProjection|TestHourlyProjection|TestSQLiteHourly|TestAccountingHourlyProjection|TestHourlyRebuild|TestTrafficSummary|TestTrafficBreakdown|TestTrafficTrends|TestTrafficNode|TestTrafficCalendar|TestTrafficReporting|TestDashboard' \
  -count=1 -timeout 5m
```

Passed: meteringstore 2.543s, observabilitystore 0.634s, datastore 1.552s and
handler 3.787s. Log: `/tmp/zboard-hourly-final-regression.log`. Tests include
refused incomplete rollback, repeat rollback/re-upgrade, reporting snapshots,
cache invalidation checks and bounded dashboard queries. MySQL-only cases are
covered by the separate real-DB runs above, not by this unset-DSN SQLite run.

## Reproducible storage and cost boundary

```sh
go test -p 1 ./internal/datastore \
  -run '^TestSQLiteHourlyProjectionPendingStorageSample$' -v -count=1
```

Passed in 0.133s. The sample creates 1,000 reliable facts across 64 users, 128
subscriptions, 16 nodes and 8 endpoints, then measures SQLite `dbstat` allocated
pages for the pending table and its indexes only. New rows use actual projected
UTC-hour metadata and normal GORM timestamp encoding.

| Object | 0053 bytes | 0054 bytes |
| --- | ---: | ---: |
| Pending table | 49,152 | 86,016 |
| Existing endpoint index | 16,384 | 16,384 |
| Hour pending/time index | — | 40,960 |
| Hour pending/user/time index | — | 45,056 |
| Hour pending/subscription/time index | — | 45,056 |
| Hour pending/node/time index | — | 40,960 |
| Hour pending/endpoint/time index | — | 45,056 |
| Total | 65,536 | 319,488 |

This is 65.536 versus 319.488 allocated bytes per pending fact at this small page
layout, not a universal per-row bound. It excludes raw facts, WAL, hourly/daily
buckets and MySQL index storage. The extra storage buys bounded scope/time reads;
the queue must still be monitored and drained. Log:
`/tmp/zboard-hourly-storage-sample.log`.

The existing reproducible microbenchmark now covers both hourly and daily writes:

```sh
go test -p 1 ./internal/adapters/persistence/meteringstore -run '^$' \
  -bench '^BenchmarkUsageProjectionRecovery200$' -benchtime=1x -count=5 -timeout 2m
```

Its fixture is outside the timer, both aggregates are verified, and the emitted
SQL/batch metric excludes driver BEGIN/COMMIT. It has not been rerun for this
hourly batch; the previous document's daily-only numbers remain historical,
not evidence for this workload. The cgroup inspection reported CPU budget
`400000 100000` and memory limit 17,179,869,184 bytes (16 GiB). No improvement
ratio, sustained throughput, HTTP p95 or autoscaling result is claimed.

## Remaining work

Online/coverage auxiliary projections and their spool-consumer ordering remain
unisolated. Raw TrafficRecord, cursor, epoch and dedup recovery state are retained;
no archival or destructive retention policy is introduced. No Zero/core/client
repository changed. Native delivery failure rules and financial behavior are
outside this batch. Complete backend/vet/build, full real-DB CI, frontend and
contract checks must run on the final integrated commit. Sustained growth, queue
lag, CPU/RSS, core p95, quota-to-revocation delay, overuse and controlled scaling
remain P6 measurements, not conclusions from these unit and fault tests.

## Exact-commit CI failure and fixture follow-up

The first exact-commit backend CI for
`53a056d32633dcbda9b243553d85a88fe9fc2090` failed two existing handler tests
(handler runner 224.517s). The original
[CI run 39369974199, job 118455402109](https://github.com/zerodenet/zboard/actions/runs/39369974199/job/118455402109)
and its failures are retained; the targeted successes above did not cover these
two test names.

* `TestTrafficProductionTrendRouteReusesFactsAndSeparatesTimezoneChanges`
  directly inserted an extra 77-byte historical fact after `seedUsage` had
  already rebuilt its seven historical facts. With 0054 there is no synchronous
  insert trigger. The UTC result correctly excluded this earlier-day fact; the
  freshly computed Shanghai result also lacked its absent hourly bucket, giving
  1,209 bytes/7 records instead of 1,286/8. This was not reuse of the UTC cache.
* `TestTrafficUsagePageReferencesAreBoundedIndependentAndAccountScoped` directly
  seeded ten settled historical facts, without projection intents or hourly
  buckets. Its hourly node ranking was consequently empty before its raw-page
  scope/reference assertions could run.

A clean independent checkout of exact 53a056d reproduced both failures without
other changes, handler 0.348s. Log:
`/tmp/zboard-53a-hourly-ci-repro.log`. The minimal fix explicitly calls the existing
`rebuildFixtureHourly` after each historical seed is complete. It changes no
production code, cache logic, permission rule or raw fallback. Every original
timezone/AsOf/cache assertion, node limit/truncation assertion, foreign/deleted
subscription label assertion and exact five-query boundary is unchanged.

```sh
GOMAXPROCS=1 go test -p 1 ./internal/handler \
  -run '^TestTraffic(ProductionTrendRouteReusesFactsAndSeparatesTimezoneChanges|UsagePageReferencesAreBoundedIndependentAndAccountScoped)$' \
  -count=1 -timeout 3m
```

Exact 53a056d plus only these two fixture edits passed, handler 0.359s. Log:
`/tmp/zboard-53a-hourly-ci-fixture-fixed.log`. Both modified test files matched
the main integration workspace byte-for-byte:

* `traffic_calendar_snapshot_test.go`: SHA256
  `28bdf583d52379e2287a624248803dafea99f75e67a0364f915031aace3837aa`.
* `traffic_usage_query_test.go`: SHA256
  `d731dd62a8a0f251ab26f8117a9a9793f8b35fb2960bea18c8b6f94fec86d7db`.

The temporary patch is retained as
`/tmp/zboard-53a-hourly-ci-fixture-fix.patch`; the verification checkout was
restored to clean 53a056d after the comparison. An initial shared-workspace run
was blocked at compilation by a separate concurrent P3 import-cycle intermediate
state, not a test assertion. Its owner has removed that cycle; rerunning in the
shared workspace awaits the scheduled P3 test slot. No shared-workspace or full
CI pass is claimed from the independent two-test result.
