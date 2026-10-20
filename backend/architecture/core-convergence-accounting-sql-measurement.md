# P6 accounting SQL measurement contract

The existing `BenchmarkZeroAccounting` now records statement count, SELECT/DML
count, driver-reported affected DML rows, unknown affected-row counts, errors,
mean GORM client duration and a bounded-histogram SQL p95 upper bound. It stores
numeric totals only, never SQL text/parameters. Observer classification has a
regression for selected rows versus writes, unknown rows and error outcomes.
Production accounting and runtime diagnostics are unchanged.

One FullBatch operation uses 32 distinct cumulative flows for one subscription
and commits the existing authority/coverage path. Fixture creation and final
assertions are outside the timer; parsing and envelope generation are inside.
CoverageOnly is reported independently, not subtracted as an inferred core
latency. The final replay must leave the subscription and ledger total exact.
Reproduce on a clean fixed checkout:

```sh
GOMAXPROCS=1 go test -p 1 ./internal/handler -run '^$' \
  -bench '^BenchmarkZeroAccounting$' -benchtime=100x -count=5 -timeout=5m
```

Reserve a quiet window and record the exact commit, source/binary hashes,
toolchain, CPU/memory limits, commands, output and failures before drawing a
comparison. Client SQL durations include driver/callback work; they are not
server-only execution latency. Driver BEGIN/COMMIT calls are outside Trace.
Affected rows are logical DML attempt counts, not bytes, WAL growth or unique
business facts. A failing benchmark must not be reported as committed work.

The new observer invokes GORM's SQL formatter to classify statements, so its
wall time includes that measurement cost. Earlier count-only benchmark timings
are not a matched performance comparison. This local single-subscription
microbenchmark also does not replace the original 60-second constrained HTTP
capacity profile, prove a capacity ceiling, demonstrate independent-worker
benefit, or measure quota-to-stop delay/overuse. Results will be appended only
after actual execution; P6/P7 remain incomplete.

## Fixed 7b10068 execution

The clean standard checkout ran at 2026-10-20 18:45:54–18:46:03 UTC,
Go1.26.8/GOMAXPROCS1. All ten repetitions and final replay/ledger assertions
passed. The measured binary hash and unchanged before/after source binding
are in [the receipt](validation/2026-10-20-accounting-sql-baseline.json). This
cloud cgroup allows four CPU cores and16GiB; it is not the constrained1CPU/1GiB
HTTP container. The direct test process, including untimed fixture/assertions,
used8.384240 user CPU seconds/0.751648 system seconds over9.15s and peaked
at58,056KiB RSS. These are whole-test resource costs, not timed-batch-only values.

| Metric | FullBatch, five repetitions | CoverageOnly, five repetitions |
| --- | --- | --- |
| SQL/batch | 87 | 2 |
| SELECT / DML per batch | 48 / 39 | 1 / 1 |
| Logical affected DML rows/batch | 101 | 1 |
| Unknown affected rows/batch | 0 | 0 |
| Mean GORM client SQL time | 91.39–101.60us | 36.22–73.43us |
| SQL p95 upper bound | 200us | 100–200us |
| Batch p95 | 12.54–17.15ms | 0.2465–0.3808ms |
| Batch p99 | 13.06–18.75ms | 0.3634–2.236ms |
| Timed events/second | 2,908–3,243 | 143,348–227,301 |

FullBatch creates3,200 accounting facts per repetition from32 cumulative flow
identities, checking exactly96,000 billed bytes after replay. Its allocation
measurements are1,648,597–1,649,705B/batch and16,505–16,514 allocations/batch.
CoverageOnly does no billable settlement; its events/s must not be presented as
accounting throughput. None of these microbenchmark rates are sustained HTTP
capacity claims.

The87 statements close as48 SELECTs (Prepare2, report batch1, principal lookup4,
owner/parent/credential revalidation5, subscription/endpoint2, per-flow cursor32,
node cursor1, coverage1) and39 DML (SQLite write-boundary placeholder1, per-flow
cursor32, ledger batch1, projection intent batch1, subscription/touch2, node
cursor1, coverage1). Logical affected rows are1+32+32+32+1+1+1+1=101. The SQLite
write-boundary placeholder preserves ownership locking and is not removable
measurement noise. No cursor, quota, ledger, or projection intent is replaced
by a statistical approximation.

The original observer counted one normal first node-cursor ErrRecordNotFound
per100 batches as SQL-errors/batch0.01. That original output is preserved. A
subsequent test-only observer correction distinguishes expected read misses
from actual errors; one classification-only run reproduced87 SQL/101 rows,
0 actual errors and0.01 expected misses. Its1.408s run was concurrent with full
checks and is not a performance comparison.

## SQLite cursor cohorts

The next production slice loads runtime and legacy cursor candidates together
only after the existing credential, subscription and quota ownership locks.
Exact input node/principal cohorts and at most200 keys per SELECT bound each
query. Processing remains in input order, with the same continuation, principal,
sequence, regression and per-sample charge validation. The transaction-local
cache changes only after each individual cursor write succeeds and removes a
migrated legacy alias. It never survives rollback or commit.

Eight repository tests with seven subcases passed in0.417s; nine existing
accounting/window/closed-epoch integration tests passed in3.494s. The integrated
main source repeated the eight tests in0.275s. A real scalar control makes32
cursor SELECTs versus the cohort's1, with the same37 DML/99 logical affected
rows at the accounting-only boundary. Including unchanged node/coverage writes
adds2 DML/2 rows.205 samples/410 candidate keys use3 SELECTs, each at most200.
Completed/stale events, rounding, runtime changes, legacy migration, principal
conflicts, exhaustion, closed epochs, failed final writes and replay are covered.

The MySQL path remains the original scalar database lookup: its collation and
missing-row lock semantics cannot be replaced by Go string equality. No schema,
queue ownership, cursor write batching, retention or financial writer changes.
Reverting this slice requires no data conversion. The initial failed test-file
path produced no tests and is retained as a failed attempt, not validation.
See [the cohort receipt](validation/2026-10-20-sqlite-cursor-cohort.json).

The full-path87→56 statement count and any wall-time/HTTP gain still require a
new fixed-source quiet measurement. No MySQL throughput, extra-worker benefit
or autoscaling success is claimed.
