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
