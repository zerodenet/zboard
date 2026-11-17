# P7 database migration copy: owned preparation and safe retry

This is an incremental recovery fix, not P7 completion. Evidence was collected
in the shared cloud integration workspace on 2026-10-20. The integrating commit
and full CI remain the parent integration's responsibility. The final defensive same-server MySQL rejection passed its dedicated real-database
test, followed by the final SQLite focused suite and vet. The earlier full
cross-driver MySQL passing run predates that defensive change; it was not repeated
and is not represented as an exact final-commit full-suite run.
Machine-readable results are in
[the validation record](validation/2026-10-20-migration-retry.json).

## Reproduced failure and resulting behavior

A real SQLite INSERT failure during copy rolled back destination business rows,
but schema initialization had committed its default data. Retrying then failed
with `target database is not empty: system_configs contains 1 rows`. The source
user remained intact. The initial reproducer passed in 0.358s by asserting that
failure; the retained tests now assert the corrected recovery behavior.

The target may now be retried only when a durable destination-local receipt
proves that this source and its original randomly identified task owned the
initially empty database, and the complete prepared schema/data manifest still
matches. A newly constructed copier and a new migration task on that same source
can reuse that proof without re-running schema initialization. Arbitrary seeded
databases and older failed destinations without a receipt remain rejected.

The only authoritative preparation write path is
`platformstore.prepareMigrationDestination`. The actual copy transaction remains
owned by `platformstore/database_copy.go` and the existing datastore session
helpers. No payment, entitlement, billing or kernel behavior changes.

## Receipt, identity and transaction boundaries

The target-local table is `zboard_migration_preparation`, with one row, ID 1:

- A random 256-bit `token`, source/target identity hashes, original source task ID,
  random task idempotency key and job run ID identify ownership.
- `state`, `schema_fingerprint`, `seed_fingerprint`, `created_at` and `updated_at`
  record preparation and completion. No DSN or password is stored.
- SQLite identity uses the actual persistent file's device/inode on Unix, or
  Windows volume/file index. Paths and DSNs are not database identities. MySQL
  identity uses actual server UUID plus database name.

The state transitions are deliberately conservative:

| State | Evidence and allowed action |
| --- | --- |
| `preparing` | Ownership is persisted before schema DDL. DDL may have partially committed; refuse automatic reuse and retain all data for inspection. |
| `prepared` | Actual initialization content and schema were verified and sealed. Matching source and exact unchanged fingerprints permit copy/retry. |
| `completed` | Business rows, copied task/item/run completion and receipt completion committed together. Refuse destructive replay. |

Creation of the table without a row, missing fields, multiple rows, wrong token,
wrong source task nonce, wrong physical target, changed schema/data or missing
receipt never becomes an empty-target exemption. Source authority is rechecked
through the existing claimed maintenance job and quiescence boundary. The
original source preparation task/run must remain verifiable when a new task
resumes a prepared target.

Immediately before any destination row clearing, the destination transaction
locks and rechecks receipt ownership and the entire manifest. MySQL locks existing
rows and empty-table ranges and requires REPEATABLE READ or SERIALIZABLE; READ
COMMITTED is refused because it cannot supply the required gap protection.
Every MySQL table must use InnoDB. Generated AUTO_INCREMENT counters, which may
advance on rolled-back INSERTs, are excluded from schema comparison; row IDs and
all stored row contents remain fingerprinted. The transaction then clears only
the existing application transfer inventory, copies the source snapshot and
commits task/run completion plus `completed` atomically. Failure at receipt
completion rolls the copied data and all completion states back to `prepared`.

The source snapshot/lock behavior and constraint restoration are unchanged.
Source business data is not deleted. If the destination commit succeeds but the
source loses completion confirmation, the target's completed receipt and copied
successful task/run provide evidence for reconciliation. The implementation does
not automatically declare the source successful, switch the active datasource or
re-copy a completed destination. Operators must reconcile that outcome; a new
migration must not overwrite the completed target.

## Strict initialization proof

A receipt alone cannot label arbitrary writes made during the DDL interval as
initialization. Before sealing, bounded reads compare actual seed contents:

- Both engines require the genuine embedded menu nodes and initial revisions,
  default execution budget/group and untouched automatic-renewal scan.
- SQLite requires the exact recovery-window config, empty template tables and
  its initial traffic ledger revision 0 with the generated instance identity.
- MySQL requires its 18 baseline configs, recovery-window config, two email
  templates and three subscription templates. A read-only helper derives expected
  SELECT projections from immutable published migration INSERT literals. It
  compares every logical field, including revision, and canonicalizes JSON.
  Generated IDs/timestamps are additionally pinned by the final manifest.
- The literal reader rejects unsupported tables, columns, functions, subqueries,
  unexpected upserts, malformed tuples and additional statements. It does not
  execute INSERTs or require temporary-table privileges.
- Non-initialization tables must have no rows. Unknown populated tables are not
  exempt; MySQL names beginning `sqlite_` are not mistaken for SQLite internals.

MySQL fresh-baseline protection normally rejects any unversioned existing table.
The composition-root hook `datastore.WithOwnedMigrationPreparation` carries only
an in-process verifier, not request JSON. It can run only when history is entirely
empty and the receipt is the sole non-history table. The verifier checks the
current random token, `preparing`, actual source/target identity and claimed source
task/run. Wrong proof, ordinary RunMigrations without the callback, an unknown
additional table or an expired source claim cannot adopt the baseline. No
baseline history record is fabricated.

No 0055 or other formal schema migration is added. The local recovery table is
not in `MigrationTables`, is not copied from the source, and does not weaken the
normal empty-target or central migration-history rules. Existing numbered
migrations are unchanged. Full destination preparation uses the composition
root's migration and reconciliation boundary; tests mirror all seven steps and
check the complete transfer table inventory.

## Verification and discovered limitation

Commands ran in `backend`, with Go 1.26.8, `GOMAXPROCS=1`,
`GOCACHE=/workspace/toolchains/cache/go-build`,
`GOPATH=/workspace/toolchains/gopath`, and `go test -p 1`.

| Check | Observed result |
| --- | --- |
| Final SQLite copier, physical identity, embedded literals and cancellation classification | PASS 8.215s |
| Real MySQL copier suite, isolated random DBs | PASS 108.804s before that final change; source MySQL to SQLite and source SQLite to MySQL |
| Existing application single-connection, task completion, failure/panic, cancellation and transfer/missing-table regressions | PASS 0.592s |
| Final platformstore and menustore vet | PASS |
| Windows file identity helper cross-compilation | PASS; no Windows runtime acceptance performed |
| Additional same-server MySQL-to-MySQL retry experiment | BLOCKED/FAIL: global source read lock blocked target; terminated at 107.429s |
| Final real MySQL adapter same-server preflight refusal test | PASS 4.132s |

The MySQL passing suite includes genuine SQL failure/rollback and retry,
new-task recovery, receipt-completion SQL failure/rollback, preinitialized or
modified targets, DDL-window config/menu/template changes, wrong source/target,
invalid baseline proof, unknown tables, empty-range write locking and READ
COMMITTED refusal. All preservation cases check retained source and/or target
values. This is test fault injection on real SQLite/MySQL, not production fault
acceptance or a capacity measurement.

The extra same-server experiment found that the existing
`FLUSH TABLES WITH READ LOCK` source snapshot is server-wide: a different database
on the same MySQL server cannot accept target locking/writes while that lock is
held. The public capability already rejects same-driver migrations; this
experiment exercised a direct adapter call, outside the existing HTTP contract.
It is not a passing retry case. Its failure log is retained at
`/tmp/zboard-migration-preparation-mysql-same-server.log`; the observed processlist
and cleanup transcript are retained in
`/tmp/zboard-migration-preparation-mysql-same-server-diagnosis.log`. The process was stopped;
only its two specifically identified random databases were removed. Successful
DDL cleanup and zero owned sessions confirmed global lock release.

The added defense compares actual `@@server_uuid` values in both target Check and
Copy, before any receipt/DDL writes, and returns an explanatory invalid-target
error for that topology. It does not guess from host strings, weaken the source
lock or change the existing cross-driver product rule. The latest dedicated test
asserts prompt rejection, an untouched empty target, no initializer call and
source data retention; the final dedicated isolated-MySQL run passed in 4.132s.
The final SQLite focused suite then passed in 8.215s and both adapter packages
passed vet. No test or compiler processes remained at handoff.

Reproduction commands and final logs:

```sh
# Original cross-driver MySQL batch, before final same-server guard:
ZBOARD_TEST_MYSQL_DSN='<isolated test DSN>' go test -p 1 \
  ./internal/adapters/persistence/platformstore \
  -run '^TestMySQLMigrationCopier' -count=1 -timeout 10m
# Final focused MySQL defense:
ZBOARD_TEST_MYSQL_DSN='<isolated test DSN>' go test -p 1 \
  ./internal/adapters/persistence/platformstore \
  -run '^TestMySQLMigrationCopierRejectsSameServerBeforeAnyTargetWrites$' \
  -count=1 -timeout 2m
# Final SQLite and parser/identity/cancellation checks:
go test -p 1 ./internal/adapters/persistence/platformstore \
  -run '^(TestMigrationCopier|TestMigrationFileIdentity|TestMigrationEmbedded|TestMigrationTarget)' \
  -count=1 -timeout 4m
go vet -p 1 ./internal/adapters/persistence/platformstore \
  ./internal/adapters/persistence/menustore
```

The original full MySQL pass log is
`/tmp/zboard-migration-preparation-mysql.log`. The final narrow pass logs are
`/tmp/zboard-migration-preparation-mysql-topology-guard.log`,
`/tmp/zboard-migration-preparation-final-sqlite.log` and
`/tmp/zboard-migration-preparation-final-vet.log`. The last is empty with exit 0.
No DSN or password is included in committed evidence. CI runs the platformstore
MySQL package after the other MySQL packages, because source FTWRL otherwise
interferes with their server-level lock timing.

## Compatibility, rollback and remaining limits

There is no automatic recovery for an unowned older failed target, an interrupted
DDL receipt, or a completed destination with source confirmation lost. Preserve
the source and target for explicit reconciliation; do not clear tables, remove
receipts, fabricate migration history or relax errors to force adoption.
Persistent SQLite file identity is required; in-memory databases and platforms or
filesystems without a stable physical identity are refused. Windows runtime,
different physical MySQL servers and production transfer acceptance remain
unverified. Public same-driver migration remains unsupported.

For code rollback, stop migration execution and retain the original authoritative
source. Do not run an older copier against a partially prepared target or delete
its local evidence to bypass admission. No down migration or data deletion is
needed for this batch. Completed-target datasource cutover and manual resolution
remain the existing controlled operator workflow. No production migration,
deployment, datasource switch or rollback was performed. Exact integrating-commit
full backend/MySQL/CI checks remain outstanding.
