# Schema preparation work and measurement

Repeated isolated MySQL suites exposed significant fresh-schema DDL work. The
existing published migrations and recovery checks remain authoritative. This
increment changes two idempotent projection/index preparation paths, without
changing financial tables, migration identities, column types or index contracts.

Fresh zero-event projection tables now declare their existing timeline/usage
indexes in CREATE TABLE. Existing or partially restored tables still pass through
the original missing-index repair path. Traffic read preparation checks each
required index and groups missing ADD KEY clauses into one ALTER per table,
preserving exact names, column order and error propagation. An already complete
table performs no ALTER. No historical migration is edited.

The real MySQL test uses a random schema and its disposable schema-only account.
It proves fresh projection preparation and repeated reconciliation without ALTER
permission, restores that account's own schema permission, then exercises all
missing and partially missing indexes. Same-connection Com_alter_table deltas
are1 for a six-index ledger repair,0 when repeated,4 for four missing-index
tables,0 when repeated,4 for partial repair,0 when repeated. Rows are compared
field-for-field throughout. Denied DDL returns the real1142 error. The isolated
MySQL8.4 test passes in0.594s; SQLite fresh/repair/repeat and index contracts pass
in0.228s. This does not prove all startup/migration work needs no ALTER permission.

The new opt-in benchmark measures exactly one production PrepareDatabaseSchema
call on a fresh random MySQL schema. Fixture setup/cleanup are outside the timer;
one retained connection and matching CONNECTION_ID before/after make the session
ALTER counter meaningful. Use the existing isolated test DSN and:

```
GOMAXPROCS=1 go test -p 1 ./internal/datastore -run '^$' \
  -bench '^BenchmarkMySQLSchemaPreparation$' -benchtime=1x -count=1 -v
```

A quiet controlled clone based on7018e3e plus this benchmark/preparation patch
compared the old two production files from33b35d3 against the new files. Its
source manifest stayed unchanged during measurement. One pair measured82 to78
session ALTER statements,5.159188957s to4.578557414s inside preparation. Compilation,
fixture and cleanup wall time was24.692s and14.753s respectively and is not the
startup metric. This is one pair, not a throughput or latency guarantee; many
other migration ALTERs remain. The aggregate observed reduction is4; narrower
per-table counter tests must not be summed to invent a different full-startup
result. See the [receipt](validation/2026-10-20-schema-preparation.json).

Rollback is a source revert: same schema/index contracts remain readable, with
more separate DDL operations. No destructive migration or data cleanup is added.
Current full backend, original three-package MySQL15-minute checks and copy
checks must still run at the new commit. Earlier timeout failures and unexecuted
copy checks are retained; this measurement does not mark P7 or that CI gate done.

## 0054 partial column recovery batching

The current0054 owned reconciler now collects only missing MySQL column ADDs
from the unchanged embedded asset into one ALTER. SQLite remains per-column.
Index-only repairs retain CREATE INDEX and their existing permission contract;
explicit trigger drops, authority preflight, legacy false/hourly true meaning,
rebuild and receipt order remain. Partial prefixes, repeated recovery, real1142
refusal, exact definitions and source/hourly/daily preservation pass actual MySQL.
An initial new test fixture parsed a comment semicolon incorrectly; that failure
is retained separately from the corrected old-source batch-count regression.

Three complete fresh Prepare calls per source observe79→75 ALTER while
CREATE_INDEX stays15. Old seconds3.965/3.795/3.549; new4.076/4.007/3.706. This
sample does not show startup time improvement (new median is higher) and cannot
mark the original15-minute suite fixed. Only the operation-count reduction is
verified. The58f source before this change passes full Go286.586s/vet/build/
pure-Go SQLite19.505s and original mixed gates; it does not accept this new DDL.
4e CI's15m MySQL timeout and191 CI's later complete success both remain recorded.
See validation/2026-10-20-hourly-column-batch.json; new-source full checks remain.


## Baseline metadata reads

Pre-release adoption still checks the same table inventory, then all16 required
column identities/strict types in their original order, then all12 index
identities. Only the metadata fetching is batched: one column query and one index
query, local to this invocation. Query/Scan/Rows.Err failures propagate with phase
context; metadata is never reused across DDL. Published migrations are unchanged.

The actual MySQL validator alone performs29→3 SELECTs and zero ALTERs. Missing
column, wrong signedness, missing index, case-insensitive legacy identities,
older compatible shape and unknown/unversioned-chain refusal all pass, retaining
marker data and exact migration history. Fault injection and three race runs pass.
Three complete fresh Prepare calls per source observe2094→2068 SELECTs including
the trailing connection-ID probe; ALTER75 and CREATE_INDEX15 are unchanged.
Median3.628→3.719s shows no startup time improvement and no proof of resolving CI's
15-minute timeout. Source revert restores individual reads with identical schema.

The00f85fb CI also timed out in MySQL handler900.017s; the other five jobs passed,
while subsequent jobstore/copy commands did not run there. That failure and the
4e failure remain evidence despite58f's earlier full success. The current source
requires its own whole backend, original MySQL, capacity and CI checks. See the
[metadata receipt](validation/2026-10-20-baseline-metadata-batch.json).


## Fixed31 acceptance and stage attribution

Source31e72db full Go301.326s/vet3.997s/build2.088s/pure-Go SQLite22.416s pass.
Actual MySQL datastore157.274s and networkstore104.134s pass, while handler hits
900.029s at WalletReservationCompetition (last test2s). Independent jobstore
136.580s and copy117.918s then pass; neither was silently skipped. CI39502621208
also has five successes and a MySQL15m timeout, leaving its later jobstore/copy
commands unexecuted. Priorc722 CI succeeds all six, handler875.129s; that does
not accept31 or establish stable margin. See the
[full result](validation/2026-10-20-records-final-checks.json).

An exact clean31 clone then measures three actual complete Prepare calls using
a private diagnostic overlay with unchanged step order. Versioned migrations
occupy93.03–93.51% of stage time:3.790065/3.505053/3.492524s. Their per-session
raw counts are69ALTER/15CREATE_INDEX/103CREATE_TABLE/1722SELECT, including one
trailing ID probe in the stage SELECT count. These are statement counters, not
physical-table counts or individual SQL latency. The other six preparation
stages each stay below56ms in these samples. Complete tail inventory consumes
0.125/0.115/0.113s and300 production SELECTs; the handler fixture omits this tail.

Whole instrumented calls are4.070512/3.776920/3.746530s, including diagnostic
probes/output, with raw2084SELECT =2067production +16stage-ID probes +1outer probe.
Do not compare this to uninstrumented startup as a regression/improvement. The
versioned phase is still broad: no specific SQL cause or CI fix is proven. Source,
overlay and benchmark hashes remain stable; no DB settings, published migration,
timeout, fixture boundary or permissions changed. See the
[diagnostic receipt](validation/2026-10-20-schema-stage-diagnostic.json).
