# P2 recurring agreements: manual checkout and renewal

This increment extends the verified initial-only checkout without rewriting its
V1 evidence. A new recurring purchase stores V2 discount evidence. Manual
renewal inherits the original provider, signer, opaque agreement and selection
from a strictly verified paid series. Omitting browser `discount` does not opt
out of that agreement or enable a full-price fallback. Automatic recurring
debit remains unavailable until the worker and consent contract are integrated.

## Commercial facts and transaction order

The purchased base is resolved before discount calculation and remains separate
from the reduction, payable, actual payment and entitlement-value allocation.
For a purchased base of 120 and an agreed reduction of 20, new purchase and
supported manual renewals each quote and pay 100 even after the catalogue rises.
Provider terms may express a rule rather than a fixed reduction; core compares
the exact agreed identity/version/terms/hash instead of inventing coupon rules.

Only a verified paid V2 recurring new order can begin the supported series.
Renewals must link continuously to the preceding verified paid agreement and
retain currency, purchased base and billing period. Pending orders cannot become
agreement sources. Missing/unverified/unknown discounts, a changed signer,
refund, unsupported series, unavailable provider or changed agreement refuses
the operation with an explanation. A provider's `requires_confirmation` is
rejected; it is never converted into `available`. Full replacement, other
operations, nonuniform historical series and automatic recurring debit remain
separate follow-ups, not a reason to infer a new price agreement.

The source evidence records root/base/preceding order IDs, a fingerprint of the
actual paid series, subscription and entitlement epoch. Source PKs are selected
outside the financial transaction. Inside it, exact source PKs are locked in
ascending order before the owner, matching payment/refund/termination source
lock order. No ordinary consistent read precedes the owner in that transaction.
The first RR snapshot is established only after the owner lock. A changed source
outside the preselected set rejects the transaction rather than locking new
sources in reverse order. Any supplemental exact-PK current read after owner
uses SHARE NOWAIT; only MySQL 3572 becomes a quote-change conflict. Deadlock 1213,
timeout 1205 and text lookalikes are not silently retried or reclassified.

Two short transactions surround the read-only provider RPC, which holds no
financial lock. Creation rechecks business facts, source evidence, agreement,
fingerprint and fresh expiry before the original order writer commits. Existing
matching idempotent orders return before provider evaluation, including after
the provider stops. Receipt/entitlement execution still uses the existing
payment/fulfillment writer; the provider never marks orders paid or grants value.

## API, UI and compatibility

Provider discovery still lists admitted signed active configured native choices
without RPC. It now reports `agreement_scope: quote_required`, because listing
cannot know the actual scope a provider will quote. The actual verified offer
decides initial-only versus recurring. Manual recurring renewal inherits its
provider and agreement; the browser cannot replace either with another choice.
HTTP 409 distinguishes `subscription_quote_changed` and
`discount_confirmation_required`. These are recovery boundaries, not proof that
another concurrent request with the same identity has no order. Creation 409
retains the original request and idempotency key for explicit recovery.

The UI accepts available recurring new quotes and displays the ongoing agreement
and locked base separately from the current reduction/payable. Renewals show
server-inherited amounts/currency and expiry, including when browser `discount`
is absent. Expired or failed quotes block creation. Stored original confirmation
facts retain scope, original price and expiry through an unknown result/reload;
they are not replaced by catalogue prices or automatically replayed writes.

No new schema migration is needed beyond 0055's purchase-snapshot TEXT capacity.
The V1 input-fingerprint function remains byte-identical and has a fixed-hash
regression. V1 initial-only purchases keep their prior renewal/auto-renewal
behaviour. Older strict readers can reject V2 snapshots: pause new recurring
checkout before rolling back, retain all evidence, and reconcile pending orders
with a compatible binary. Never clear evidence or reprice a paid order to make
an old binary accept it. No production migration or external plugin changed.

## Evidence and remaining checks

Pure commerce/store tests and vet pass. SQLite repository tests cover inherited
agreements, changed source/refund/epoch/signer, unavailable/confirmation states,
idempotency, prelock/owner/snapshot order, newly selected source rejection and
exact MySQL-code classification. They do not prove InnoDB locking.

The actual signed-native/HTTP/core-wallet test passed once on SQLite (2.357s):
a trusted top-up credits 500, new purchase consumes 100, two continuous manual
renewals consume 100 each, and wallet ends at 200 with no reserve. It checks the
V2 paid value/entitlement chain, unchanged current usage/reset schedule and exact
term extensions. Disabled provider plus omitted selection returns 503 without
orders/funding; a same-key paid-order replay remains recoverable. External coupon
redemption and remote kernel delivery are not simulated as completed.

Two actual MySQL counterparts subsequently passed on isolated MySQL 8.4: the full
native/wallet chain, and a real payment paused after canonical source locks
while a concurrent preview waits. The latter uses performance_schema plus an
owner NOWAIT probe to detect opposite lock acquisition, then rejects the old
quote without creating an order. The combined P2/P4 run passed datastore in
7.775s and handler in 23.996s (34.133s total). This proves the targeted real
InnoDB scenario, not every concurrency ordering. Final full backend/MySQL/new CI
still apply.

Frontend narrow tests pass (three files, 89 tests, 6.35s), including ongoing
agreement purchase, inherited USD 60/20/40 isolated from a CNY catalogue 100,
expired quotes, lost creation/reload and same-key recovery. The original six-exclusion full frontend suite subsequently passed (199 files,
1069 tests, 8 unchanged test skips, 160.55s); vue-tsc and Vite build passed
(Vite12.00s). Browser/live joint acceptance remains pending. OpenAPI lint is valid with its existing 282 warnings. See the
[validation receipt](validation/2026-10-20-recurring-discount.json).

## Ordinary renewal lock regression and correction

The first full CI for ec55e01 and 7b10068 failed the MySQL job: ordinary
checkout inherited recurring-only source-order locks. This prevented expiry
and termination from reaching their existing owner-wait membership fence.
The local 7b10068 full run also exceeded the original 15-minute MySQL limit;
its subsequent database-copy checks were not executed. These failures remain
recorded, rather than being replaced by earlier green CI.

Preselection now identifies verified V2 recurring roots before the transaction.
Only those roots acquire the additional source locks before owner. Ordinary and
V1 checkout retain their established owner-first path and first consistent read
after owner. A root changing into a recurring agreement after preselection
requires a new quote; it cannot acquire an inverse-order lock later.

The two unchanged historical MySQL race tests and the V2 source-before-owner
test passed together on isolated MySQL 8.4 in 15.973 seconds. No timeout,
deadlock retry or existing assertion was relaxed. A SQLite regression also
checks that ordinary preview/create do not take recurring source locks.
Full checks and CI for the correcting commit remain required. See the
[correction receipt](validation/2026-10-20-ordinary-renewal-lock-correction.json).
