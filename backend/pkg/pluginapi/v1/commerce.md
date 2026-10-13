# Native commerce and payment contributions (v1)

ZBoard owns products, quotes, business orders and fulfillment. An approved
native plugin owns external payment methods, verification and its private
attempt state. No browser response, return parameter or plugin health result
settles an order. No channel-specific routes or provider SDKs are built into
ZBoard.

## Admission and slots

Account surfaces have two formal slots:

| Slot | Host placement |
| --- | --- |
| `account.checkout.payment` | Existing product/order confirmation page |
| `account.orders.payment` | Existing account order page and payment return |

A contribution declares an `id`, `surface: "account"`, `slot`, `title` and
signed `ui/...` entrypoint. Multiple plugins and multiple methods are supported.
No independent checkout menu is required. Declare a native server, plus:

```json
[
  "zboard.ui.page.v1",
  "zboard.ui.slot.v1",
  "zboard.commerce.orders.read.v1",
  "zboard.commerce.payments.write.v1"
]
```

Storage, configuration and HTTP routes use their existing separate grants.
Native execution also retains the existing configuration read/write admission
requirement; the four grants above describe the payment extension itself.
Payment write requires a native runtime, a payment contribution and order read.
Discovery and slot sessions require all four payment grants. Missing grants,
disabled, revoked or uninstalled packages do not produce usable methods.
Signed manifest/admission validation remains authoritative; declaring a grant
is not a browser permission. Plugin health alone does not establish commerce
availability.

## UI bridge and host-owned windows

The iframe remains `sandbox="allow-scripts"`. It cannot open windows or call
payment writes. Send messages to the parent using the existing
`source: "zboard-plugin-ui"` and fragment `bridge_token`. Origin alone is not
sufficient: the host also validates the exact child window and token.

After readiness, register methods with this one-way message:

```js
parent.postMessage({
  source: 'zboard-plugin-ui', bridge_token: token, type: 'checkout.options',
  options: [{
    id: 'method-one', title: 'Method name', disabled: false,
    prepare_action: 'myplugin.prepare', reconcile_action: 'myplugin.reconcile'
  }]
}, '*')
```

The action names belong to the plugin. ZBoard does not implement them. Up to
16 methods per contribution are allowed. IDs/actions match
`^[a-z0-9][a-z0-9.-]{0,63}$`; titles are nonempty and at most 80 characters.
Duplicate/invalid methods are rejected. Report height via `ui.resize`; the host
bounds it to 0–1000 px for slots. Waiting and confirmation controls also exist
outside the iframe, avoiding clipping and preserving browser user activation.

On the confirmation page, methods appear below the payable amount. When a
payment contribution is available, the host provides one confirm-and-pay action;
it does not offer a separate order-only action. Methods are selected before the
click creates the business order. Failed discovery or an unavailable enabled
contribution never falls back to order-only creation. The order-only action is
available only after successful discovery returns no admitted contributions.

The checkout context additionally includes `presentation: "host"`. In that mode,
the host renders methods, status, retry and confirmation controls. A contribution
can report `checkout.status` with `state` equal to `loading`, `ready`, `empty` or
`error`, and an optional bounded error `code` (`^[a-z0-9_]{1,64}$`). Messages retain
the exact iframe/token checks. Status never settles an order. Report `ui.resize`
with height zero when no additional plugin content is needed; no fixed empty
payment frame is required. Older contributions may keep their own content.
The host bounds an unresponsive method load to 35 seconds and permits local retry.

A `checkout.context` request uses the ordinary request/response bridge. Before
order creation it returns display currency, precision `2`, and null `order_id`.
After creation it returns the **host-selected** owned order's context. A child
cannot substitute an order ID, actor, currency or amount through this message.

The host button reserves a blank window synchronously on the current click,
sets `opener = null`, then runs existing business order creation and calls the
selected native prepare action. Payload contains only `{ "option_id": "..." }`.
Return `{ "checkout_url": "https://..." }`. The host rejects non-HTTPS URLs,
credentials, control characters, missing hosts and addresses over 4096 bytes.
A blocked popup prevents order creation; preparation failure closes the blank
window. Plugins must also validate their provider URL. Completed host orders
are not sent to another payment preparation.

Creation is coalesced per checkout intent and persisted **before** sending.
`POST /orders` accepts an optional 8–80 character `idempotency_key` with
letters, digits, `_` and `-`. The host hashes it together with the authenticated
buyer and saves a business-request fingerprint. Identical retries return the
original order even after SKU/pricing changes; changed content conflicts with
that intent. Keys are not payment method IDs. The browser retains the original
request/key and recovered order across reloads; payment retries reuse that
order. Existing callers without a key retain their prior semantics.

## Native action context

Authorized business and account/admin slot sessions can call their own
`page.call`/`HandlePageAction`. Additive protobuf fields identify slots:
`purpose`, `slot_id`, `slot`; `page_id` is empty for a slot. Actor and surface
come from the host session. Administrative actor status is re-read from the
current account. Cross-account sessions and forged top-level identity fields
are rejected; plugin-owned payload data is not authority.

`checkout.call` requires a nonzero owned order selector; it never falls back to
an ordinary account-scoped action. Host checkout actions additionally receive `checkout_json`, decoded with
`request.Checkout()`, containing:

- `order_id`, stable host `trade_no`, owning `account_id`;
- final `payable_amount` in minor units, `currency`, `currency_precision: 2`;
- current host `status` and deployment-configured `return_url`.

The return URL comes from trusted installation `SiteURL` (HTTPS, no credentials,
query or fragment), including any deployment path prefix. It targets
`/account/orders?payment_order=...&payment_plugin=...`. Return parameters select
navigation/reconciliation only. The owning plugin's reconciliation action is
called, followed by a fresh owned host order read through `GET /api/v1/orders`
with `id`, `paged=true`, and `limit=1`; only host status `paid` reports success.
This read uses the main account session and remains available after plugin
disable/revocation. Even if a reconciliation response is lost, the host receipt
is checked; a plugin error cannot hide a committed host payment. Saved disabled
methods can reconcile historical attempts but cannot start new payments. No return success flag changes business state.

`ActorId` on a checkout action is an opaque **order-bound** principal. It is
provided only to the native process, not the checkout UI bridge. Persist it in
existing private plugin storage together with the host order/attempt. It is
signed by the host, bound to the plugin's signing identity, account and exact
order, and cannot be minted from public notification account/order numbers.

## Native calls and background authority

```go
host, err := pluginv1.HostClientFromEnvironment()
// Handle err and close host when finished.
context, err := request.Checkout()
var page pluginv1.CommerceOrderPage
err = host.ListOrders(ctx, pluginv1.CommerceOrdersRequest{
    PrincipalID: request.ActorId, ID: context.OrderID, Limit: 1,
}, &page)
// Verify external truth against page.Items[0].PayableAmount and Currency.
result, err := host.RecordPayment(ctx, pluginv1.PaymentFactRequest{
    PrincipalID: request.ActorId, OrderID: context.OrderID,
    ProviderEventID: verifiedEventID, ProviderTradeNo: verifiedTradeNo,
    Status: "paid", AmountMinor: finalMinorAmount, Currency: currency,
    OccurredAt: verifiedEventTime,
})
```

These invoke existing `commerce.orders.list` and `commerce.payments.record`
semantics and transactions. `id` is an additive optional order filter. An
account-bound principal may list its account's orders; an order-bound principal
can only query its exact order (omitted ID is constrained automatically).
Cross-account/other-order selection is rejected. Payload `user_id`, provider
identity and host principal fields cannot override scope. Payment records
require the order-bound native principal, never an ordinary account principal,
configuration session or browser capability grant. Integration credentials
retain their existing behavior; consumers need no integration credential or
main-site login token.

An order principal outlives browser sessions. Every callback nevertheless checks
current process identity, signed package, enabled/active state, host lease,
admission and active account. The current process's private socket/token is
required. Revocation/disable/uninstall closes this path; a saved order association
by itself grants nothing. Re-enabling the same admitted signing identity can
reconcile prior authorized attempts. Host secret or publisher key replacement
invalidates old principals. Plugins should keep authorities private.

The stable payment provider namespace is `plugin:` plus the first 24 hex digits
of SHA-256(plugin ID, NUL, stored signing key). Process generation, configuration
revision, package version and browser session are excluded. Event uniqueness is
`(provider, provider_event_id)` across that signing identity's accounts/orders.
Ordinary restart/upgrade retains the namespace; publisher replacement does not.
Freeze event ID, external reference, amount, currency and provider event time
before first submission, and use identical values from page, return and callback
paths. Identical retries are successful with `fulfilled: false`; changed content
for the same event conflicts. Fulfillment uses the existing locked order,
payment-event, subscription/credential and publication transaction. Existing
business state, amount/currency and external-reference constraints remain in
force, including uniqueness of `provider_trade_no`; plugins must use a stable,
unambiguous external reference. Refunds and reverse fulfillment are not added.

## Limits, errors and compatibility

| Surface | Limits |
| --- | --- |
| UI/native page actions | 64 KiB JSON each direction, 30 s, 60/account/plugin/minute |
| UI bridge | 8 concurrent requests/frame; existing session 10 min, checked every 15 s |
| Native order query | 64 KiB input, maximum 200 rows, 10 s, 60/plugin/minute |
| Native payment fact | 64 KiB input, one fact, 30 s, 120/plugin/minute |
| Native transport | Private Unix socket; 35 s server/client response timeout; 8 MiB outer bound |

Native `HostCallError.Code` distinguishes `forbidden`, `invalid_request`,
`capability_unavailable`, `conflict`, `rate_limited`, `temporary_unavailable`
and `deadline_exceeded`. HTTP 403/503 callback responses can also carry this
structured envelope; SDK callers can use `errors.As`. Browser bridge errors use
`error.code` (`permission_denied`, `session_expired`, `invalid_argument`,
`conflict`, `rate_limited`, `unavailable`, `deadline_exceeded`) and expose an additive `code` field in
iframe responses. A busy lifecycle/native transaction may return temporary
unavailability; use bounded retries. Never record local paid state until the
host acknowledges the fact. A lost response is recovered by retrying the exact
same fact, not by generating a new event identity.

Proto fields and SDK helpers are additive within protocol/bridge v1. Older
plugins ignore new fields. Older hosts reject the new payment grant/slots;
plugins should publish a suitable host version range and use host discovery.
Configuration/storage/registered HTTP request passthrough are unchanged.

## Repeatable local acceptance

From `backend`:

```sh
GOMAXPROCS=2 go test -p 1 ./internal/handler \
  -run 'TestSignedPaymentPlugins|TestOwnedOrderCreationRecovers' -count=1 -v
GOMAXPROCS=2 go test -p 1 ./internal/plugins \
  -run 'TestSlotActionsExpire|TestPaymentIdentityAndAdmission' -count=1 -v
```

The signed native fixture in `internal/plugins/testdata/payment` uses public
SDK host/storage calls and an HTTP simulated provider. It is an acceptance
fixture, not a channel integration template. Assertions cover two discoveries,
no checkout menu, trusted slot context, actor/identity spoof rejection,
unauthorized browser writes, cross-account/order rejection, amount/currency and
event conflicts, pending browser claims, concurrent page/return/notification
settlement, one payment event/subscription/credential, closed sessions,
restart replay, disable/revocation/uninstall and stable provider identity.
Frontend tests cover local discovery retry, disabled methods, popup ordering,
creation coalescing/recovery, same-order retries, sandbox/context boundaries and
host-only success. SQLite uses real commerce transactions. MySQL-specific tests
follow the repository's existing optional database fixture configuration; real
provider payments are not a development precondition.
