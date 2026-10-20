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
