# Actual MySQL startup DDL attribution

The original local full regression on exact `01ebcde` still fails at the
unchanged handler package 900s limit. Datastore and networkstore pass; independent
jobstore and copy commands also run and pass after that failure. The same source
passes all six CI jobs, including handler759.718s. These are separate outcomes,
not stable acceptance margin. See the [local receipt](validation/2026-10-21-heartbeat-mysql-local.json)
and [CI receipt](validation/2026-10-20-heartbeat-ci.json).

A single separate fresh-schema diagnostic now identifies actual successful DDL
by table/action/name and original SQL hash. It replaces only the ZBoard-owned
benchmark file in a clean ordinary01eb clone, using the previous native-driver
wrapper. Original SQL, arguments, fixture, seven production preparation stages,
session counters and trailing inventory stay unchanged. Parser-only tests run
first with the DSN absent; the one actual MySQL sample follows. No external
module overlay, source edit, new SQL query or privilege is involved.

The sample has71ALTER,117CREATE_TABLE and15CREATE_INDEX,2047SELECT including the
original trailing connection-ID probe. Detail/category/table sums and original
same-session ALTER/INDEX counters match; open Rows is0. Successful Exec spans
are counted once, including prepared fallback, with no Prepare/Close duplication.
The [sanitized receipt](validation/2026-10-21-ddl-attribution.json) contains all71
ALTER structures, table totals, frozen source/overlay/runner/log hashes and actual
commands. No raw SQL, arguments, default values or credentials are published.

Examples of actual attribution:

| Table | ALTER statements | Implication |
|---|---:|---|
| orders | 12 | Includes published additions and widening; no general batch substitution |
| subscriptions | 8 | Several separate versioned lifecycle/policy/epoch transitions |
| protocol_credentials | 7 | Three derived principal-lookup additions are in one existing recovery owner |
| subscription_tokens | 5 | Includes uniqueness removal, NOT NULL and new uniqueness; not interchangeable additions |
| node_config_publishes | 4 | Deferred/reconciling/suspended published steps plus the separately batched phase columns |
| subscription_automatic_renewals | 2 | Approval authority preflights must survive any later optimization |

The three principal-lookup fields are a possible narrow follow-up: fresh3ALTER
could become1, while retaining any-addition invalidation, existing digests,
index repair, receipt order and SQLite behavior. It is only a candidate here,
not an implemented improvement. Earlier FK batching was parked because those
constraints already exist inline on fresh schemas; missing-constraint recovery
counts cannot be presented as fresh-start savings.

The instrumented whole3.871s includes wrapper parsing/hash/record overhead and a
separately initialized diagnostic pool. It is not unmodified performance, Scan
timing, an improvement A/B or proof of CI causality. A multi-action ALTER has one
shared Exec duration; do not multiply it by its action count. CREATE TABLE IF NOT
EXISTS is syntax, not proof of physical creation. One fresh sample says nothing
about long-term capacity or P4/P6 completion. No migration or rollback change is
introduced by the diagnostic; published migration files remain untouched.
