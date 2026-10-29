# P5 isolated browser session acceptance

Completed at **2026-10-20 04:10:02 UTC**. This is a sanitized record; credentials,
database, screenshot, full manifests and diagnostics remain in the ignored private
directory `.codex-local-artifacts/acceptance/session-browser-wp0g9rmm/`.

The run used real headless Chromium **154.0.8037.57**, installed Python Playwright,
a newly seeded SQLite database with three fixture accounts, and a real HTTP
server bound to a dynamically allocated **127.0.0.1** port. No production service,
external identity provider or external payment channel was used.

| Check | Actual result |
| --- | --- |
| Concurrent expiry | Two real HTTP 401 responses; one original-page reauthentication dialog. |
| Real dialog behavior | Background inert blocked focus; eight Tab steps stayed inside the dialog; Escape did not dismiss authentication. |
| Same-account recovery | Original settings input DOM, draft, URL/query/hash and focus retained. Window scroll **350 → 350**. |
| Failed write | One settings PUT attempt, no automatic replay, original SQLite setting unchanged. |
| Financial request | One failed top-up POST; no replay after same-account recovery; **0** new top-up orders and **0** wallet entries. |
| Account change | Same wallet route mounted a fresh input; the previous account's draft and checkout were not applied. |
| Admin revocation | Same account reauthenticated with current non-admin permissions; `/auth/me` returned **200**, administrator endpoint **403**; old editor and draft removed. |
| Isolation / shutdown | All other page-request origins were blocked before forwarding; one unrelated fetch was blocked. Local server shut down with exit code **0**. |

The settings page's document height changed from 2480 to 2560 px after displaying
the failed-write feedback; client height stayed 900 px. The scroll target was
below either maximum (1580 / 1660 px), and the focused element remained the same
settings input. The earlier bundle failed with scroll **350 → 348**. The minimal
`ModalDialog.restoreFocus()` change to `focus({ preventScroll: true })` passed this
real-browser check; the assertion was retained rather than relaxed.

The runner expires only a fixture token, re-signing its original claims with a
past expiry and that fixture's secret. It delays two genuine server responses to
make the concurrent-401 case repeatable, forwarding the actual statuses and
bodies. It does not manufacture an authentication response. Permission revocation
updates only the isolated database's fixture account.

## Source and artifact binding

Backend source is **cf1c8be311b9b6bb6f8d103da3233705208db53c**, built from the
verification checkout with Go 1.26.8, `GOMAXPROCS=1` and `-p 1`. Its only checkout
adjustment was an existing SQLite test fixture repair, with no runtime effect.
This backend includes migration **0053**; the run provides **no 0054 evidence**.

Frontend HEAD recorded at run start was
**b166497f5c3157db360d43d336687d904f1dae77**. The tested build also included current
working-tree changes, including the dialog focus repair. The source and immutable
copied build manifests identify the actual tested worktree/artifacts; HEAD alone
does not describe them. The 188 frontend build files were copied into this run's
private directory and verified against the unchanged input build manifest before
serving.

| Artifact | SHA-256 |
| --- | --- |
| Server executable | `d46374c449f4fce1cf69511f6a5095d8faebe4680d616d62be9210a9c62e7c38` |
| Fixture executable | `2992664a250a3a918b3c824b25712a9eef689899bb7df982dcc160151cf22cf9` |
| Frontend source tree manifest | `e54c389139d08e8f71f2243b65e0dfad8ca938abe21e0d102bfc12b99a3081dc` |
| Frontend build manifest | `284dbada4cc34d905bdb8565b7b1bbbf49ce7fe934479d29bfefcd527dcb4e78` |
| Browser runner | `f3f9fe880f0b5afca86962952010958304fd8c7160ff429eaf7e77d4b31f29b0` |

Reproduction and private-evidence handling are documented in
[`scripts/acceptance-session-browser.md`](../../scripts/acceptance-session-browser.md).

## Limits

This verifies the stated session behavior in one real Chromium/SQLite isolation
run. It does not verify MySQL session races, external OIDC, cross-page draft
restoration, successful payment/refund fulfillment, new quota-sync API behavior,
capacity or production operation. Financial no-replay evidence covers an expired
top-up creation request; it does not claim successful payment processing. Previous
failed harness runs and the pre-fix scroll failure are retained privately and are
not counted as passes.
