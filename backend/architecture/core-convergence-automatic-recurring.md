# P2/P5 verified recurring wallet renewal

Automatic renewal now accepts explicitly approved, verified V2 recurring
agreements from the same paid series and purchased billing period. It defaults
off per subscription and spends only available site balance. V1 full-price and
verified initial-only agreements retain their existing rules. Unavailable
discounts never fall back to a higher automatic debit.

## Authority and transaction order

The private canonical approval snapshot enumerates stable owner/subscription,
root/base order, SKU/period/currency, locked base, reduction, actual per-period
debit, entitlement terms and full provider identity/agreement. Its hash is the
repeat-debit authority. Moving paid heads, expiry, source fingerprints and
provider response clocks are checked per order rather than included in consent.

Read/Set prepare paid sources under existing source-before-owner locks, release
the transaction, then call the admitted native provider. Enabling repeats core
validation under current financial locks and checks expiry after acquiring the
consent lock. Browser input remains SKU, fingerprint and expected revision.
Closing consent needs no provider RPC, including when approval evidence is bad.

The worker selects stable `(user_id, end_at, subscription_id)` order before RPC,
then takes the existing singleton scan guard, related orders, owner, catalogue,
subscription, consent and wallet locks. Its first consistent read after owner
reselects the queue head and checks prepared revision, sequence, next-attempt
time, source and approval. Manual renewal, withdrawal, termination or another
worker advancing that preparation causes a bounded rescan. RPC never holds the
scan guard or financial transaction. Deterministic order identity, reservation,
payment, savepoint, fulfillment and publication intent reuse existing writers.

Amount, terms, provider package/generation/configuration or billing-period
changes disable consent as `conditions_changed`; fresh approval is required.
Temporary inability to verify produces `agreement_unavailable` with no debit
and a five-minute retry inside the recovery window. Unknown versions or damaged
authority isolate that subscription and remain revocable. Expiry after wallet
acquisition but before insertion rolls back the savepoint without financial
effects and persists an explanatory retry state.

Provider admission is checked around RPC. As with existing manual checkout, a
valid short-lived quote and approved stable terms can be committed after RPC
returns; stopping a plugin between that return and DB commit is not atomically
fenced. No immediate-plugin-revocation-through-commit guarantee is claimed.

## API, UI and migration

Offers report `agreement_version`, exact decimal-string base/reduction/payable,
and a verified V2 discount quote. The UI checks arithmetic with BigInt, displays
the per-period debit and expiry, and requires explicit save. Expiry clears a
proposal without replaying writes or changing consent. Failed proposal checks
leave existing consent revocable. Provider unavailable/invalid/confirmation
remain distinct HTTP error classes. Private approval evidence stays server-side.

0057 adds `agreement_version DEFAULT 1` and `approval_snapshot TEXT`. Existing
V1 rows are not fabricated into recurring approvals. Preflight precedes SQLite
AutoMigrate: recorded missing authority fields refuse startup. Unrecorded
partial DDL completes only when remaining fields contain no new authority.
Complete structure needs no global per-row JSON audit; the worker isolates bad
rows. Down preserves evidence. Before rollback stop automatic execution, retain
approvals/orders/wallet/audit/intents, and verify the target binary safely
refuses V2; do not rewrite approvals as V1 or mix incompatible workers. No
production migration or external plugin/kernel change was executed.

## Evidence and boundaries

SQLite V1/V2 tests cover two renewals, stable wallet competition, changed terms,
unavailability, withdrawal, source/consent advancement during RPC, damaged
authority and fresh expiry. Store/application/handler passed in0.008s/0.008s/
10.085s; schema recovery in3.868s. Later expiry/savepoint and V1 outbox regression
passed in0.621s; four-package vet passed.

Actual isolated MySQL8.4 migration/recovery and continuous-renewal/competition
counterparts passed together in26.872s (datastore11.436s/handler12.316s).
The signed native fixture then passed the actual HTTP new purchase, trusted
wallet payment, explicit HTTP approval, two native-quoted automatic100 debits,
unchanged approval and final200 balance. Stopping the provider leaves status
readable, rejects enabling, creates no order/debit and still allows withdrawal.
SQLite passed in2.335s; the MySQL native counterpart and HTTP error contract
passed in7.264s (8.845s wall). Only due dates are compressed by the fixture;
this is not proof of two natural calendar months passing or a live coupon
provider. Existing paid and approval evidence is not rewritten.

Frontend quote/component narrow tests passed23 cases. The unchanged six-
exclusion full suite passed200 files/1086 tests, with8 existing skips, in
158.992s wall. Typecheck/build passed in31.897s wall (Vite13.53s). OpenAPI lint
is valid with282 existing warnings. An initial narrow run failed on a test
matcher and hidden local validation message; both were corrected, retaining
the failure log. Full backend, new CI and live browser joint acceptance remain
separate requirements. See validation/2026-10-20-automatic-recurring.json;
P2–P7 remain incomplete beyond this slice.
