# CI40471848896 fixture followups

The four failures reported for head2974bb3 remain part of the failure history. These changes correct test setup and retain the original invariants; they are not evidence that the database paths now pass.

- A pre0067 copy fixture now removes its future0067 receipt and authorities together. A separate negative keeps the receipt and removes an authority table; copy must fail without changing source material/budget or target initialization. A copied legacy source is replayed after two reopen cycles without changing identity/budget.
- Principal-runtime upgrade comparisons first require exact timestamp instants and raw identities/digests, then compare every historical column after normalizing only the time location representation.
- The later-renewal base-lot test now obtains its first2000 discount through real Preview/Create/wallet payment with an accepted initial_only agreement. Its provider transport alone is simulated. It asserts base100+addon100 becomes300 with Used60, later renewal remains a separate lot, and final residual/unchanged cash facts remain correct.
- The1200-subscription rollback fixture reloads the whole persisted baseline after its deliberate unknown-history mutation, including GORM UpdatedAt. Full subscription/plan/credential/publication/money, zero quota events and zero audit writes remain checked; diagnostics distinguish the failing domain.

No production rules, published migration, transaction, retry or refund guard changed. No data migration or rollback is needed for this test-only increment. gofmt and git diff --check passed. New SQLite/MySQL execution is unverified locally: required modernc SQLite/compression module downloads returned HTTP403; no alternate source was used. Exact-head CI must establish compilation and behavior. Earlier failures and local receipts are retained; this does not claim P2/P4/P7 completion.
