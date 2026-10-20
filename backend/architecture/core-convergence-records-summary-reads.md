# Explicit traffic record summary pages

The raw diagnostic HTTP page previously loaded full TrafficRecord rows before
converting them to eleven public fields. A200-row limit did not bound the unused
variable-size Meta payload. Records.ReadSummaryPage now explicitly selects those
eleven fields. Records.Read still returns complete evidence in both paged and
unpaged mode; Paged is not a summary or permission selector. A summary requires
pagination and the existing limit/window/cursor validation.

The persistence path shares primary authorization, actor scoping, filters,
repeatable-read/read-only snapshot, count/sums, record_at+id ordering, offset,
limit+1 and newer/older cursor behavior. Only raw+paged HTTP selects the new
method. Member/admin JSON still uses record_at. Ordinary Usage.Read, host usage,
core quota reads and NodeSeries are unchanged. No writes, DDL or migration.
Rollback reverts the source; original facts and full reads remain available.

## Evidence that remains authoritative

The model is TrafficRecord. NodeID+ReportID and NodeID+Nonce identify replay;
EntitlementEpoch, CreatedAt and UsedBytes rebuild actual received/current-cycle
usage. record_at cannot substitute for CreatedAt. FlowUsage retains cumulative
runtime identities. Meta accounting_samples, first_sequence and rounding_numerator
are real per-sample rounding evidence:1000 samples×3 bytes at1500milli settle5000,
where combining first would produce4500. Other source provenance has not been
proved disposable either. This change does not archive/delete Meta, source facts,
identity or checkpoints, and does not claim bounded lifetime authority storage.

## Validation and measured scope

A deliberately wide-column overlay retains the new API for compilation and fails
the actual selected-column/evidence-loading assertions. The fixed seven SQLite/
capability regressions, their race run and one original real MySQL fixture pass.
Both HTTP routes, summary/full JSON, source retention, scope/time edges, ties,
offset/cursors, primary fallback, admin revocation, cancellation and real query
errors are covered. Initial SQL time.Location and MySQL missing-node FK fixture
failures are preserved; corrected tests compare identical time instants and
create real parents without weakening FK or preparation.

The frozen c72254b+five source files+benchmark overlay uses203 facts,201 in scope,
200 returned,32KiB Meta per fact. Five samples×100 pages per path compare actual
full Read plus original HTTP model-copy/summary conversion with SummaryPage.
Both perform3 SELECT/page including authorization and0 DML. Median allocations
7,296,596→517,263B and elapsed52.253081→1.942201ms; all source facts/Meta and output
summary JSON match. Old samples precede new samples; no statistical confidence,
MySQL, HTTP authentication/serialization, core settlement or capacity claim.

The measured benchmark is now tracked; only its explanatory first comment changed.
From backend, with the existing Go toolchain/cache:

```sh
GOMAXPROCS=1 go test -p 1 ./internal/handler -run '^$' \
  -bench '^BenchmarkTrafficRecordsRawSummaryLargeMeta$' \
  -benchtime=100x -count=5 -benchmem -cpu=1 -timeout 4m
```

See the [receipt](validation/2026-10-20-records-summary-reads.json) for every raw
sample, source/binary hashes, commands, historical failures and pending final-source
whole backend/original MySQL/copy/CI checks. No P4/P6/P7 completion is inferred.
