# Batch two additive MySQL recovery paths

The plugin-operation audit reconciler formerly added four missing columns with
four ALTER statements, and publication phase recovery added two with two ALTERs.
Each HasColumn also repeated metadata discovery. On MySQL these two paths now
read the selected database's table columns once, consume/close that result, and
issue one ALTER containing only the missing model-defined additions per table.
SQLite retains its original paths.

The helper is deliberately limited to these non-primary/non-auto-increment
fields. It preserves FullDataTypeOf expressions and bound values, identifier
quoting, naming, widths, defaults and nullability. Metadata query/scan/iteration/
close errors stop before DDL; DDL errors propagate without retry. No target-table
SELECT permission, global migrator override, cross-call cache or new dependency
is introduced. Published migrations, schema-account permissions and database
settings are unchanged.

The publication connections-phase correction still requires a nonempty applied
configuration SHA. The existing reset/update and0045 receipt order are unchanged:
a failed ADD cannot record completion. Empty audit/SHA defaults remain missing
evidence, never invented publisher identity or confirmation. The separate
already-recorded0045 compatibility path is not claimed to batch every ADD.

## Validation

A fixed ordinary3c44aa0 clone plus the exact five candidate files passed actual
SQLite and isolated MySQL fresh/partial/repeated recovery. Tests compare complete
column contracts and retained non-target row fields, committed audit prefixes,
empty-SHA phase reset, absent receipt after injected DDL-call failure and retry.
Fake-driver tests additionally cover query/scan/Rows.Err/Close faults and custom
FullDataTypeOf Vars; they are not represented as real MySQL evidence. Three race
repetitions pass103.724s (wall121.314s).

The controlled old-callsite overlay retains the new helper/tests for compilation;
it is not a byte-identical parent binary. Old SQLite passes. Old real MySQL fails
only expected ALTER-count assertions, while preservation/error checks still run.
Actual session totals are fresh RunMigrations69→65, owned missing recovery6→2,
partial public recovery3→2, complete repeat0 and one-column retry1. Raw callback
counts do not include direct sql.DB.Exec migrations; complete session counters do.
Earlier test-draft failures and corrections remain in the receipt/raw logs.

The unchanged original BenchmarkMySQLSchemaPreparation was then run once per
fresh schema in three alternating old/fixed pairs, GOMAXPROCS1/-p1. No additional
SQL, stage probe or driver wrapper was used. Every old sample records75 ALTER,
15 CREATE_INDEX and2068 SELECT; every fixed sample records71/15/2047. The original
SELECT metric includes its unchanged trailing connection-ID probe.

| Pair | Old s/prepare | Fixed s/prepare |
|---|---:|---:|
| 1 | 3.769 | 3.628 |
| 2 | 3.754 | 3.803 |
| 3 | 3.866 | 3.554 |

These observations prove four fewer ALTERs and21 fewer SELECTs for full Prepare.
Ranges overlap and pair2 is slower; three pairs do not establish stable latency
improvement, explain prior CI variation or prove the unchanged15-minute gate.
All source/fixture/embedded migration hashes stayed identical during measurement.
See the [source-bound receipt](validation/2026-10-20-audit-phase-column-batches.json)
for commands, all raw samples, session IDs, UTC order and hashes. Candidate full
backend/current CI/original capacity checks remain separate requirements.

## Upgrade and rollback

There is no new migration number, historical SQL edit or business-data rewrite.
Fresh/partial tables obtain the same additive fields; existing complete tables
perform no ALTER. Old binaries accept the resulting schema and retain their
per-column reconciliation; binary rollback reintroduces the redundant DDL only.
Do not drop the added audit/publication columns for rollback. Existing migration
receipts and retained publisher/configuration evidence remain in place. No
production migration has been executed. P7 stable headroom and P4/P6 convergence
remain open.
