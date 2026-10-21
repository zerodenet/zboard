# Principal lookup recovery must invalidate before DDL

Status: the reviewed four-file patch is integrated after isolated targeted
validation. Exact a9aae16 full backend and all six new CI jobs pass; the original
local MySQL core fails at its original900s alarm; independent follow-ups pass.
Parent `c61df99` contains the earlier source-bound acceptance records; its new
CI fails MySQL at the original900s limit and does not accept this implementation.

## Concrete failure and behavior

Principal/secret digests and their lookup version are derived from canonical
credentials. Previously, missing columns were added before the global coverage
invalidation. If ADD committed and the next UPDATE failed or never ran, a retry
could see all columns present and keep version1 with a new empty digest. The
indexed lookup could then miss a real credential while reporting preparation
complete. This is a derived lookup recovery bug, not permission to invent or
rewrite canonical identity or financial facts.

The candidate consumes and closes a complete metadata snapshot before any write.
When a column is missing and the version column already exists, the existing
global UPDATE to version0 must succeed before ADD. When the version column itself
is missing, its unchanged DEFAULT0 supplies unknown coverage. MySQL adds only the
missing three explicit derived fields in one ALTER; SQLite keeps the original
per-column ADD definitions and order after the same pre-invalidation boundary.
The existing index shape repair and0046 receipt remain last. Complete structures
do not reset valid version1, including a legitimate empty secret digest.

The audit/phase helper's metadata query was extracted without changing its SQL,
field validation, full type expressions, quoting or error/Rows-close behavior.
The principal fields remain excluded from model migration and use their explicit
varchar64/tinyint definitions. No published SQL, numbered migration, backfill,
financial writer, authorization rule, public API or native-core contract changes.

## Evidence under validation

The original principal reconciler plus new behavior tests reproduces exactly two
critical leaf assertion failures on SQLite and actual isolated MySQL: rejected
invalidation follows an already committed ADD, and an error after committed ADD
leaves persisted coverage1. This is the original function with compatible new
test/helper code, not a byte-identical parent executable. No compile/setup failure
is counted as the regression.

The fixed five-scenario matrix passes on both databases. Two separately opened
pools verify persisted failure/re-entry boundaries. Actual credential encryption
and a secret different from PrincipalKey, with no retired digest, require real
secret-index rebuilding before Lookup succeeds. Canonical comparison covers the
complete current ProtocolCredential model except its three derived fields;
unknown extra SQL columns are not covered by that comparison. Ciphertext, stable
identity, status, scope, epoch, timestamps and retired evidence are unchanged.

Cases include complete repeat/no writes, UPDATE rejection/no ADD, error after a
successful actual ADD, missing version/default0, all three missing columns and
wrong-shape index repair. Query/Scan/Rows.Err/Close faults stop before UPDATE/DDL
and release the only connection. Existing model type/Vars/quoting and5001-row
bounded-index regressions pass. Race-count3 passes (datastore1.089s,
networkstore363.370s; wall389.547s, no changed timeout). Exactly one original
uninstrumented fresh Prepare passes with69ALTER/15CREATE_INDEX/2031SELECT,
same session24554. The prior same-SQL diagnostic had71ALTER/15INDEX/2047SELECT.
These are count observations, not a performance A/B; the new single3.609s sample
does not establish timing improvement or900s causality. Full commands, source
and log hashes are in the [targeted receipt](validation/2026-10-21-principal-lookup-recovery.json).

## Compatibility, rollback and limits

There is no new schema or format. A binary rollback keeps the derived version0
as pending and can use the existing bounded backfill; it also reintroduces the
old ordering bug for future damaged schemas. No business row, ciphertext, receipt
history or dedup state is deleted. All database executions are disposable test
instances, not production migrations.

This prevents the new ADD-before-invalidation window. It cannot diagnose a
previously damaged but structurally complete version1 state: empty digests can
be legitimate. The database-copy path can also copy a partial source's version1
into a complete target; source-preparation protection for that case is unproved
and is not fixed here. No new marker or guessed target reset is introduced.

Error injection after actual committed DDL and another-pool re-entry is not an
OS-crash test. No cross-host migration lock or online-repair availability guarantee
was added; competing initializers may explicitly fail duplicate ADD. The single
global credential UPDATE already existed and only its order changes. Three-to-one
ADD arithmetic alone is not proof that the original900s regression gate is fixed.
Independent Stats ordering/asset identity, durable auxiliary handoff, authoritative
retention and P6 worker/scaling remain separate incomplete work.


## Exact a9aae16 full backend follow-up

The clean ordinary clone passes Go288.313s, vet3.568s, build1.845s and
pure-Go SQLite21.967s, GOMAXPROCS2/-p2. [Exact command/log-hash receipt](validation/2026-10-21-principal-recovery-backend.json).
Original3-command local MySQL has finished separately: datastore152.038s and
network112.003s pass, handler900.017s times out; independent jobstore120.268s and
copy108.349s run afterward and pass. The active2s test remained in fixture formal
migration Exec; the statement ID/cause is unknown. [Local MySQL receipt](validation/2026-10-21-principal-recovery-mysql-local.json). Exact-source new
[CI39522585804](https://github.com/zerodenet/zboard/actions/runs/39522585804)
passes all six jobs, with MySQL datastore158.643s/handler809.717s/network113.348s/
jobstore104.071s/copy94.612s. [CI receipt](validation/2026-10-21-principal-recovery-ci.json).
Original timeout/exclusions remain unchanged. One pass with90.283s handler margin
does not prove stable startup improvement; earlier timeouts remain failed evidence.
