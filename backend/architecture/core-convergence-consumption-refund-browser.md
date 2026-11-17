# Closed subscription consumption-refund browser acceptance

The isolated Chromium acceptance exercises an existing historical-order refund
through the built UI, actual handlers and SQLite. It introduces no production
refund rule, migration or external payment integration. The approved scope is a
source order whose subscription has already ended; active partial refunds still
require the missing business decision described in the convergence decisions.

## Actual sequence and invariants

A configured node/endpoint/group precedes the actual purchase. The existing
funding/payment-fact test path fulfills a 1000-minor CNY order and creates a real
credential. SubscriptionAccess.Read issues a persisted token. Both sets must be
nonempty. CredentialExpiry receives an explicit future clock after the actual
recovery grace period; it ends the subscription and revokes those same identities.
No paid-order, wallet, usage, approval or credential row is fabricated. Endpoint
delivery remains pending: this test proves neither native delivery nor a remote
connection being closed.

The owner logs in, opens the historical order and submits a linked support ticket.
An owner approval is refused with403. A separate administrator browser context
submits a foreign owner's ticket and is refused with400, then explicitly approves
300 minor against the correct ticket. Wallet0 becomes300, reserved remains0.
Two explicit HTTP replays of the successful key preserve the refund/entry IDs;
refund, wallet entry and audit each occur once. Reloads and verification buttons
perform reads without replaying financial writes. The owner sees the entry, while
terminal access remains unconfigured and the subscription stays out of active
results.

At every financial checkpoint the fixture hashes full persisted row maps,
including fields excluded from model JSON. Payment facts, rights and metering
remain identical. The source order may change only refund_amount, refunded_at
and updated_at. A separate budget assertion checks1000 funded/300 refunded/0
converted. Browser coordination signals acknowledge database checks only; no API
response is mocked. All non-loopback requests are aborted before network access;
the administrator layout's known public release GET attempts are recorded.

## Reproduction

Use the repository's declared Go/Node/pnpm versions and already installed Python
Playwright/Chromium. Do not install or change permissions as part of this runner.
Build frontend with `pnpm build` after its frozen dependency install. Create a new
private0700 evidence directory, copy that exact dist into its `web` child, and
record source, dist and test-binary hashes before and after. The script expects
Chromium at `/usr/bin/chromium`. The actual Go test is opt-in:

```sh
# backend directory; use an ordinary checkout rather than a linked worktree.
go test -c -p 1 -o /absolute/private-evidence/handler.test ./internal/handler
# Run from backend/internal/handler, with absolute fresh evidence/web/script paths.
ZBOARD_CONSUMPTION_REFUND_BROWSER=1 \
ZBOARD_CONSUMPTION_REFUND_BROWSER_EVIDENCE_DIR=/absolute/private-evidence \
ZBOARD_CONSUMPTION_REFUND_BROWSER_WEB_DIR=/absolute/private-evidence/web \
ZBOARD_CONSUMPTION_REFUND_BROWSER_SCRIPT=/absolute/checkout/scripts/acceptance-consumption-refund-browser.py \
GOMAXPROCS=1 /absolute/private-evidence/handler.test \
  -test.run '^TestConsumptionRefundChromiumAcceptance$' -test.timeout=5m -test.v
```

The directory may contain the just-built binary and copied web files, but must
contain no previous fixture, screenshots, signals or result files. Outputs use
exclusive creation. Keep artifacts private: they include test authentication and
persisted evidence. The public receipt contains hashes and results only.

## Measured result and limits

On2026-10-20, exact production31e72db plus the two acceptance files passed in9.61s:
32 browser checks, five screenshots, two explicit identical-key replays and all
financial/terminal-state assertions.529 frontend source files,188 dist files,
1903 backend source files, owned acceptance files and binary were hash-bound
before/after. The actual frontend build passed. Three earlier failed attempts
are retained in the receipt: missing endpoint prerequisites; unencoded redirect
and absent fixture plugin-manager dependency; and an overbroad assertion against
already-blocked release requests. All fixes affect the harness only.

This is selected-handler HTTP acceptance, not full production routing/middleware,
natural-term passage, live payment, MySQL, capacity, unknown-outcome browser retry
or native delivery acceptance. It adds no new dependency, migration or data
rollback requirement. The ordinary suite skips this opt-in browser test. Newer
commits still require their own CI; this receipt does not turn the outstanding
MySQL900s timeout green. See the [receipt](validation/2026-10-20-consumption-refund-browser.json).
