# P2 initial-purchase discount checkout

This follows the read-only provider boundary in
[the provider evidence](core-convergence-discount-provider-evidence.md). It adds
checkout acceptance for a derived `new` purchase with an `available`,
`initial_only` offer. It does not implement recurring offers, limited-use coupon
consumption or discounted replacement/renewal checkout.

## Request, preview and stored evidence

`OrderCreateRequest.discount` contains only `provider_id` and optional `selection`.
The provider is one explicit admitted installation; selection is a rule reference.
The existing authenticated buyer, current SKU/plan and derived operation remain
host authority. The provider never receives payment credentials or sets payment,
balance, funding or entitlement status.

Preview adds `base_price_minor`, `discount_amount` and `discount` evidence to the
existing payable/currency/confirmation fields. A discounted create requires the
preview's exact `quote_fingerprint`. Unavailable, confirmation-required and
unsupported recurring offers do not create a payable order or become a
zero-discount approval.

The purchase snapshot adds:

```text
orders.purchase_snapshot
  base_price_minor                     12000
  discount.context                    host facts and input fingerprint
  discount.quote.provider             installation/signer/package/generation/config
  discount.quote.offer.discount_minor   2000
  discount.quote.offer.agreement       initial_only identity/version/terms/hash
  discount.final_price_minor           10000
  discount.accepted_at                 host acceptance clock
```

The order keeps `amount_cents=12000`, `discount_amount=2000` and
`payable_amount=10000`. Existing settlement owns actual paid value and fulfillment;
base is not redefined by the reduction or actual cash. Existing renewal-price
queries continue to use the purchased base amount, even if the catalog changes.

Core validation checks the complete host context fingerprint against the stored
order and original policy, exact provider identity shape, agreement scope/status,
terms hash, acceptance before expiry and `base - reduction = final = payable`.
Discount evidence uses strict single-object UTF-8 JSON decoding, including nested
unknown-field rejection. The snapshot is bounded to `60 << 10` bytes (61440).
Root integration owns migration 0055, which expands MySQL's old VARCHAR(2048) to
TEXT and retains SQLite TEXT. Evidence is never truncated or backfilled.

## Transaction ordering

`commercestore.OrderCreation` accepts a `commerce.DiscountQuoter` dependency.
The create sequence is:

1. A short transaction checks the buyer and idempotency key first. An identical
   committed order returns immediately, even when the provider is unavailable.
2. The same short transaction runs existing order preparation, captures the
   undiscounted policy and constructs the host's complete baseline fingerprint.
3. The transaction ends. Only then is the native provider evaluated. Existing
   native admission, timeout, generation/configuration and post-RPC fences apply.
4. A fresh transaction checks idempotency again, repeats complete preparation
   and verifies the baseline. The reduction is applied before target/residual
   quote allocation. This version admits only a new purchase at that point.
5. The host binds the original policy, complete material offer and provider
   identity into the final confirmation fingerprint and persists the order.
   A fresh trusted-clock check after financial locks rejects offers which expired
   while waiting and records the actual acceptance clock immediately before insert.

Repeated RPCs can return different expiry timestamps. The approval token removes
only expiry, acceptance time and explanatory message. It retains the input,
base/reduction/final values, policy, provider ID, signing fingerprint, package,
generation, configuration revision and complete agreement identity/scope/terms.
Every create still obtains a fresh offer and validates its expiry. A later provider
shutdown cannot rewrite an already accepted order's locked price.

`Services.ConfigureCommerceDiscountProvider` exposes composition to the startup
adapter. Root integration owns its startup/HTTP wiring and the frontend/OpenAPI
entry points.

## Automatic renewal

The historical discount guard now recognizes only complete, internally consistent
new-purchase `initial_only` snapshots. It also reuses fulfillment's original policy
presence and validity checks. A missing/unknown/null/malformed snapshot, unknown
field, changed money/context/terms or recurring scope keeps automatic renewal
closed. Existing verified-payment evidence and explicit automatic-renewal consent
checks remain in force. No new RPC runs within the automatic-renewal worker.

## Validation, 2026-10-20

Go 1.26.8, `GOMAXPROCS=1`, `-p 1`, workspace toolchain/cache/GOPATH. The following
were executed on the shared working tree; they are not final exact-commit checks.
Logs are in the ignored persistent `.codex-local-artifacts/recovery-validation/`.

- `discount-checkout-tests.log`: targeted commerce, commercestore and application
  tests passed in 0.004s, 0.262s and 0.009s. Tests use temporary SQLite and a mock
  quoter; they do not start a native subprocess or use MySQL.
- `discount-checkout-package-tests.log`: complete commerce and commercestore
  package tests passed in 0.007s and 0.218s.
- `discount-checkout-vet.log`: vet of commerce, commercestore and application
  exited 0 with no diagnostics.
- `git diff --check` passed. No go.mod/go.sum changes were introduced.

SQLite coverage proves that the quoter can acquire the only database connection
and execute a write, so the earlier financial transaction has ended. It covers
12000-2000=10000 acceptance, idempotent retry with an unavailable provider,
changed-idempotency rejection before RPC, current buyer/SKU/operation/capacity
revalidation, changed provider signer/configuration/terms/amount, provider failure,
expiry, required confirmation and rollback after insertion. The renewal price
query is checked with an explicitly simulated paid row; it is not a full payment
or fulfillment acceptance test.

The minimal pure serialized fixture is 1239 bytes; an actual short-terms SQLite
order snapshot is 1259 bytes. A 7900-character terms fixture persists 9156 bytes and
retains the exact original terms, exceeding the old MySQL schema without loss.
Pure tests cover all material-versus-transient fingerprint fields and corrupt
snapshot rejection. SQLite tests also confirm that unknown historical discount
rows cannot authorize restoration to the base renewal price.

Remaining integration evidence: migration recovery/MySQL, startup/HTTP/native
provider checkout, real payment and fulfillment through to subscription renewal
base, explicit automatic-renewal consent/worker behavior, frontend/OpenAPI and
final frozen-source global checks. No phase-wide P2 completion is claimed here.

## Root integration and discovered payment boundary

Startup now composes the provider into the ordinary OrderCreation capability.
Authenticated provider choices expose only presentation names, descriptions and
explicit new/initial_only scope from admitted active configured live natives.
They never expose configuration or signing material, discover rules, choose a
provider for the buyer or create a charge. No native host returns an empty list;
real discovery failure is independent from ordinary undiscounted purchases.
Quote/create distinguish unavailable503 and invalid502 with stable public codes.
The frontend integration and frozen checks are recorded after completion.

A real signed-native + HTTP + SQLite + wallet/fulfillment acceptance test exposed
a flaw the mock-price-source test could not: core payment allocation legitimately
changes Order.Channel to wallet. Including that mutable channel in the discount
input hash made valid paid initial-only history fail later renewal validation.
The commercial hash now excludes payment channel; request idempotency still binds
the complete original creation request. Buyer, SKU, purchased policy, provider
identity/configuration/terms and all monetary values remain bound. The original
failing acceptance is preserved in discount-native-http-fourth.log. The repaired
joint test passed2.317s in discount-native-http-fifth.log: first12000/2000/10000,
actual wallet capture and entitlement, disabled-provider idempotent recovery,
new stopped-provider request503, raised catalog15000 retaining renewal12000,
explicit automatic consent and one wallet-only12000 recovery; repeated worker
runs add no order and the22000 paid topup ends at zero balance/reservation.

This is a local signed fixture, not a real external coupon/redemption integration
or remote kernel delivery proof. Original compile failures (wrong fixture return
type and process-client assertion), missing explicit wallet allocation and the
actual post-payment hash failure remain in separate initial–fourth logs.

0055 expands MySQL purchase_snapshot VARCHAR(2048) to TEXT with an empty default;
SQLite is already TEXT. No published migration is edited. Capture stays bounded
to60KiB and does not truncate terms. Existing wider TEXT variants never shrink.
A completed0055 with missing purchase evidence fails before AutoMigrate, requiring
restoration rather than invented empty facts. Formal down is intentionally a
non-destructive no-op; retaining the wider column supports binary rollback.
Old binaries may reject new discount-bearing snapshots, so pause new discount
checkout, drain/reconcile pending discounted orders with a compatible binary and
preserve snapshots/financial facts before rolling back. Never remove terms or
reprice old orders to make a rollback appear successful. SQLite schema/repeated
upgrade/lost-evidence checks passed1.623s; real MySQL0055 and final frozen checks
remain pending at this entry.

Final root scoped integration after adding independent discount evidence version1:
commerce0.004s, commercestore0.233s, handler2.254s passed. Version1 preserves the
initial-only evidence/input contract for later compatible extension. The actual
MySQL schema package passed11.037s and signed-native/HTTP/wallet/automatic-renewal
handler9.204s in separate random test databases. HTTP rejection contract0.217s
and ownership inventory0.415s passed; OpenAPI valid with282 existing warnings.
The initial error-contract test incorrectly expected a null data property and a
generic internal_error; corrected assertions now preserve omitted data and the
established commerce_persistence_failed uncertain-outcome classification. The
failed test log remains. New exact-source full backend/dual-database/CI checks
follow this commit; frontend integration/review is a separate append batch.

## P5 purchase and recovery entry

The actual account purchase view now offers named providers and optional codes,
defaulting to no discount. Preview/create validate provider, operation, currency,
amounts, expiry and final fingerprint; plugin JSON/hashes/signatures are not UI
copy. Unavailable providers cannot silently create a full-price order. Other
operations retain their current sale rules and do not advertise this initial-only
entry. Ordinary purchases remain usable if the optional catalog fails.

Review found another real financial display boundary: an unknown creation result
could retain its original request but display a newly fetched renewal/change
quote. All operations now persist versioned original confirmation facts, strip
them from the API request and recover only the original key/request. During an
unknown attempt, current quote/residual displays and automatic requoting stop.
Legacy intents lacking valid confirmation facts show unknown original amounts;
a stored JSON null is corruption, not absence. Only exact versioned no-commit
rejections release the failed intent; idempotency-key conflicts remain uncertain.
Nine regression cases failed before the fix and their log is preserved.

Final original CI frontend command:1058tests passed/8existing skipped,199files
passed/2existing skipped,168.01s. Eight-file narrow suite123/123 passed. Vue-tsc
and Vite13.99s passed. The earlier TS2532 in a test helper and its repair remain
in separate logs. No exclusion or skip was added. Browser/live-server joint
acceptance and exact combined commit CI still require their own evidence.

## Full-check recovery failure and compatibility fix

de0f2ab fullGo failed after288.369s: the legacy0037 old-schema/missing-column
fixture retained the newer0055 receipt, correctly triggering the new lost-evidence
guard. The fixture now removes only0055's receipt to represent its actual legacy
upgrade target; all original paid-history and missing-column assertions remain.
The first realMySQL rerun then found an actual ordering gap: unrecorded0055 ran
before the outer0037 reconciliation for a recorded-but-missing legacy column.
0055 now explicitly invokes that existing recovery path before expansion, while
already-recorded0055 still refuses invented lost evidence. The real failing
MySQL27.195s log and first fullGo failure are retained. A later shared-source
attempt could not compile in-progress V2 work and did not execute database tests;
subsequent validation uses the isolated committed checkout. These failures are
not marked as passes, and exact fixed-source validation follows separately.

## Subsequent manual recurring increment

The initial-only scope and discovery statement above describe the original MVP.
The [V2 manual recurring increment](core-convergence-recurring-discount.md) now
adds strictly verified recurring new/manual renewal paths and uses
`agreement_scope: quote_required` in discovery. It retains the V1 evidence/hash
and the earlier failure record; it does not enable automatic recurring debit or
claim historical/unsupported series are safe to reprice.
