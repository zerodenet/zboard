# Plan synchronization: paged authority and base-only perpetual quota

This record describes the 2026-10-27 implementation candidate based on
`8fbcb1ddbe4e0f825419a7938a3e632b11674dcc`, followed by the stage-1 paging patch.
The parent integrated stage 1 before this second patch. No Go compilation,
SQLite/MySQL test or production action was executed by the candidate authors.
`gofmt` and patch application checks are source checks only. CI and exact-commit
acceptance remain pending; earlier green runs cannot validate this new code.

## Original problem and atomic paging

Quota and group synchronization previously rejected more than 1000 matching
subscriptions. The same owning PlanUpdate transaction wrote plan revision,
subscription quota/group, revocation and publication intents, so that rejection
rolled the whole operation back. Removing LIMIT without another bound would
materialize every subscription and change the resource contract.

The new page size is 128; it limits SQL result materialization, not the total
number of subscribers that a plan can affect. One fixed now/recovery grace feeds
both selections. With the plan authority still locked, an ascending primary-key
pass locks only IDs for the union of group/quota targets. All subscription
authorities are acquired before endpoint, credential and publication effects.
A second bounded current-read pass processes the same predicates and high-water
ID. There is no OFFSET, accumulated array of every subscription ID, page commit,
new durable job or alternate writer. Group changes retain no subscription opt-out.

The original transaction still owns all effects and final audit. A later-page
failure rolls back every earlier page, including plan revision, credentials and
both old/new node publication intents. Total SQL work and database lock retention
remain proportional to the target cohort. This is bounded application memory,
not constant transaction duration or a database lock-memory benchmark. Nothing
claims that remote delivery is already applied; the existing verified publication
worker owns execution/reconciliation after the durable business commit.

## Approved base-only rule and actual source identity

The parent relayed the approved base-only rule: base 100 plus separately bought
100, with plan base changed to 200, becomes total 300. Used 60 remains 60, giving
240 remaining. Neither the add-on nor any original actual payment is repriced.

For mixed perpetual subscriptions, ResetQuotaBytes may describe a later once
renewal. It cannot identify the original base. The adapter reads existing
immutable paid snapshots and their real quota-event identities; the new pure
projector resolves the original new/recovery/replacement root for the current
term, then follows paid additions and prior base-sync events in real event-ID
order. Unknown snapshots, unverifiable payment ancestry or inconsistent service
counters fail closed. Current catalog data and the latest order are not substitute
historical purchase facts.

Each lot preserves its actual paid minor amount and historical consumed bytes.
Only the base quota changes. Past add-on use is not reassigned to an expanded
base; future use resumes from the oldest available lot. For example, original
base100 consumed100 plus add-on100 consumed50 leaves add-on50. Shrinking base to
50 still leaves that add-on50. The business counter is `FlowUsed + sum(remaining)`
for this mixed path: used150/total200, rather than used150/total150 which would
swallow the independently purchased add-on. Expanding base to200 then adds
base100 remaining without reversing the add-on's earlier consumed50.

Ordinary single-cycle subscriptions retain the previous SynchronizedPlanQuota
rule, including preserving used, effective dates and reset dates. The new strict
mixed path supports perpetual paid-lot histories. Timed mixed quota resizing is
not generalized by this patch and remains refused; it is not marked complete.
Old overrun histories whose business counters and paid-lot remainder disagree
remain refused for new synchronization. The existing no-sync projection and
its golden fingerprints are delegated unchanged.

## Non-payment evidence, consistency and budgets

New strict evidence uses the existing QuotaEvent row and v1 Detail. Its fields
bind subscription/epoch, plan/revision, true base order ID, old/new base quota,
used-at-sync, base consumed/remainder, complete before/after value states and a
source fingerprint. It creates no paid lot, payment fact, wallet credit, refund
or new actual payment. Actor identity stays in the existing plan-update audit.

Real QuotaEvent.ID merges grants and syncs, including same-millisecond records.
The row supplies actual CreatedAt, epoch, type and plan-revision reference; the
adapter verifies delta and before/after balances against the strict Detail.
Timestamp precision is only a weak millisecond consistency check. Source hashes
bind the original sources, prior events, state, lot consumption and epoch;
rolling source digests avoid reserializing the entire prefix for each event.

Plan edits lock Plan then Subscription. Financial writers lock Order, Account,
Plan then Subscription. Under MySQL the mixed-plan reader therefore uses current
`FOR SHARE NOWAIT` reads for bounded source metadata and exact body IDs. It must
never wait for orders in the reverse direction. A concurrent financial-order
lock yields a retryable, explanatory validation failure and rolls the entire
edit back. Ordinary RR consistent reads are not used to attach stale order
history to a newly locked subscription. SQLite ignores MySQL locking syntax and
uses the already-owned SQLite write transaction's serialized snapshot.

Schema review: MySQL baseline QuotaEvent.Detail is native JSON, not VARCHAR;
CreatedAt is datetime(3). EventType is varchar(24), ReferenceType varchar(32),
ReferenceID varchar(128); the existing names and `planID:revision` fit. The existing
entitlement_epoch field is reused. SQLite stores the same JSON detail under its
existing model. No new field/table/index, numbered migration or recovery bypass
is introduced; in particular there is no new 0067 ownership or schema change.

Source orders retain the existing 200-row metadata/body paging and evidence
budgets (1 MiB per evidence row, 32 MiB body total and the existing ID budget).
Sync events also page 200 IDs and check bytes before loading JSON: MySQL uses
OCTET_LENGTH, SQLite LENGTH(CAST(detail AS BLOB)). The body must match its exact
metadata ID/width. Strict event JSON rejects duplicate/missing/unknown fields.
Before commit, the newly stored JSON width is checked against the row and total
history budgets, because MySQL's normalized JSON can be wider than Go's compact
input. An over-budget write rolls everything back; it cannot report success then
leave an unreadable proof history. Order and event histories have separate
bounded input budgets; this is not an RSS guarantee for their decoded graphs.

User/admin value reads, full replacement and unsubscribe share the same updated
projection adapter. The source allocation is still capped at each real order's
PaidAmount; the existing cumulative refund/conversion budget and settlement
idempotency remain mandatory. No PaidAmount, original purchase/value snapshot,
actual receipt, funding split, wallet entry or discount value is rewritten by
synchronization. No new browser or plugin payment-fact entry is added.

## Validation and remaining acceptance

Root ran the complete pure commerce package after integration: 231 passing test events, no skips or failures, 4.591 seconds, with unchanged backend sources and dependency downloads disabled. This validates pure pricing/source projection only; it does not validate the adapter or real database transactions.

Stage 1 prepares real SQLite/MySQL tests with 1200 sparse-PK subscriptions,
quota-only/group-only/combined changes, page read bounds, actual publication
intents, old credential revocation, stale revision replay and financial facts
unchanged. It also covers downward quota, actual SQL failure at subscription1001,
unknown later source rollback, and real metering of subscription1200 blocked
before subscription1's first write. Those tests are still unexecuted here.

Stage 2 prepares funded wallet/real add-on behavior tests for the exact 100+100
example, multiple add-ons, consumed base/add-on preservation, repeated resize,
a real later once renewal with a different RQB, original per-lot paid residuals,
final-audit SQL rollback, missing historical root, wrong persisted epoch,
MySQL inverse order-lock refusal/retry, original metering concurrency/replay,
and whole unsubscribe/replay with original source refund caps. Pure tests add
strict JSON, duplicate/reordered/unknown sources, same-millisecond identities,
overflow/cancellation, long event chains and old no-sync golden identities.

Suggested narrow commands from backend with the pinned toolchain and GOMAXPROCS=1:

```sh
go test -p 1 ./internal/capabilities/commerce -run 'TestPerpetualBaseSync|TestPerpetualFIFO|TestValueProjectionLarge' -count=1
go test -p 1 ./internal/handler -run '^Test(PlanSyncPages|PlanQuotaSyncSupportsMore|PlanGroupSyncSupportsMore|PlanBaseQuotaSync)' -count=1 -timeout 15m
ZBOARD_TEST_MYSQL_DSN='<isolated test DSN>' go test -p 1 ./internal/handler -run '^TestMySQL(PlanSyncPages|PlanQuotaSyncSupportsMore|PlanGroupSyncSupportsMore|PlanBaseQuotaSync)' -count=1 -timeout 15m
go vet -p 1 ./internal/capabilities/commerce ./internal/adapters/persistence/commercestore ./internal/handler
```

Run the original plan/group, value/settlement and relevant endpoint/publication
regressions, then the required complete checks. Local dependency download HTTP403
is currently reported by the parent; do not replace new-code evidence with an
older green CI. No MySQL DryRun, SQLite run or static read is a real MySQL result.

## Compatibility and rollback

v1 QuotaEvents become authoritative provenance. Preserve them with paid source
snapshots and settlement budgets; they are not disposable diagnostic material.
Known eight-field older single-source audit events remain historical compatibility
input and are not forged into paid facts. An older-format event after strict new
evidence is refused. Do not fill absent legacy sources from today's plan.

Before code rollback, stop these edits and retain all v1 event/source records.
An old projector does not understand changed mixed histories; preserve a reader
that understands them or disable automatic mixed settlement until re-upgrade.
Do not delete events or rewrite old snapshots to make an older binary quote.
Committed rights are not silently reversed on code rollback; an authorized later
plan revision uses the same auditable business operation. No production change,
external payout, datasource switch or migration was performed by this batch.
