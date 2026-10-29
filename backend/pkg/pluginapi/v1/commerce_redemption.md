# Durable exclusive discount reservations v1

This optional independent native service requires both signed capabilities
`zboard.commerce.quote.provider.v1` and
`zboard.commerce.discount.redemption.v1`. It grants no browser/page RPC, core-table
access, payment facts, wallet mutation or entitlement mutation permission.
Existing pure quote providers, fingerprints and accepted prices remain unchanged.

EvaluateCommerceQuote remains side-effect-free. A finite offer explicitly adds
`redemption: exclusive_until_core_finalization_v1`; opaque marketing terms cannot
supply or remove this core admission obligation. Unknown/weak requirements are
unsupported. Preview never reserves capacity. An accepted order persists its
immutable binding and one unpredictable idempotency key before native mutation.
The provider sees no authoritative fields chosen by a plugin page/browser.

Implement `CommerceRedemptionProviderServer.ExecuteCommerceRedemption`. Parse
`request.DiscountRedemption()` and return `CommerceRedemptionResult(result)`.
This independent service reuses existing generated bounded byte envelopes. The
request action is Query, Reserve, Commit or Release; names on wire are lowercase.

The capability is a signed operational guarantee:

* Query is authoritative and read-only for the exact key and binding hash.
* Reserve atomically allocates one exclusive durable unit for this order. Repeat
  calls return the same token/outcome; conflicting bindings or token changes fail.
* A confirmed hold survives restarts, config changes and campaign expiry until
  core explicitly Commit/Release. It cannot be independently expired or resold.
* Commit durably finalizes an already-held unit and cannot fail for campaign
  expiry/config changes. Duplicate Commit returns the same committed result.
* Release of absent/rejected/reserved keys persists a tombstone, so a delayed
  Reserve cannot allocate after core canceled. A committed hold is not released.
* Every reply binds key/hash and declares exactly
  `exclusive_until_core_finalization_v1`. Unsupported providers must decline;
  they must not claim a guarantee they cannot persist across native updates.

Core always Query before an unknown mutation and reuses the original key. The
RPC runs outside financial transactions. Unknown/unavailable responses block
new funds/checkout, do not authorize a higher amount, and remain recoverable.
After confirmation core fulfills the locked-price order while a provider is
offline; its payment transaction persists Commit intent. Cancellation closes
payability and persists Release in the same transaction. Real late cash remains
reliably receipted and follows the existing non-resurrection/wallet path.

New Reserve requires the exact accepted quote runtime identity. Recovery
Query/Commit/Release may use the currently admitted same-signer installation
runtime; the old binding is unchanged. Provider upgrades must retain durable
keys, tokens and tombstones. Signer rotation/capability removal leaves explicit
pending reconciliation and does not delete/recreate holds.

Core-owned unpaid expiry uses the existing30-minute funding lifetime; a later
configured funding deadline is respected. Only core cancels and releases. The
provider never implements independent expiry. New finite obligations on automatic
renewal are explicitly unavailable until staged automatic reservation exists;
old pure recurring discounts and already-agreed pure renewal quotes continue.
No coupon-restocking-after-refund rule is invented by core.
