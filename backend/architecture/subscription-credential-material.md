# Subscription credential convergence

## Storage and wire identity

An entitlement generation owns one `protocol_credentials` root (`material_version=1`). Its physical node/endpoint parents are NULL. Existing membership tables produce transient bindings; adding another endpoint writes no new credential or confirmation relation. A unique `(subscription_id, entitlement_epoch, material_scope)` prevents duplicate current roots without rewriting legacy IDs or accounting foreign keys.

The encrypted 32-byte root seed derives domain-separated connection profiles: UUID for VLESS/VMess, a password for ordinary password protocols, and Base64-encoded 16/32-byte keys for SS2022. The seed never leaves the panel. Per-inbound authentication still needs a user entry, but these entries reuse the connection profile instead of creating independent persistent secrets.

The internal `sp1:` principal signs the root ID, authenticated node ID and endpoint ID. Nodes receive this attribution token separately from the connection key. A retained public verifier validates historical reports after usable secrets are erased. It cannot authenticate a new client. Explicit inbound tags must agree with the signed endpoint. Runtime-scoped flow cursors and compacted floors retain endpoint identity as well as the root ID, preventing cross-endpoint reuse from charging another multiplier. Existing seven-number cold floors remain readable; new endpoint-bearing floors use an eighth number.

## Rollout and compatibility

- `subscription_credential_material_enabled` defaults to **false**. Enabling applies to fresh subscriptions; reconciliation does not silently migrate existing subscriptions.
- An administrator starts a stored subscription's migration in its detail view. The same root is reused on retries. Node publication remains asynchronous; a queued publication, heartbeat or matching version is not confirmation.
- During rollout nodes receive both old and new users. Public subscription output keeps the old key until the exact frozen configuration containing that root/endpoint/material is confirmed. Subscription tokens and URLs are unchanged.
- `subscription_credential_compatibility_days` defaults to **30**, configurable from **1–365**. Each old binding starts its clock only after exact confirmation on its node. Repeated confirmation does not extend it; setting changes affect windows not yet started.
- Before a deadline, rollback restores legacy output and disables automatic re-enablement for that subscription. Once old material has expired or been erased, rollback is rejected.
- On expiry, rendering excludes the old key immediately. A bounded existing expiry worker queues its removal. Only a newer successful exact manifest excluding that old identity permits erasing the usable secret and allocated ports. Offline nodes retain recoverable material while publication retries; no unbounded per-node polling table is added. Retired rows leave the due scan.
- Legacy random credentials cannot be reconstructed from a new seed. Their finite old attribution aliases remain, without usable secrets, to preserve late reporting and replay correctness. New subscriptions do not create more endpoint-sized aliases.

V1 deployment manifests retain their exact canonical format. V2 permits one root ID on several distinct endpoints; duplicate root/endpoint pairs and mixed identity types remain invalid. Root delivery uses the latest frozen node manifest rather than a new subscription × endpoint confirmation table. Each projection reads and decodes that source once per node, retaining proofs only for requested bindings. History cleanup protects that one current source per node while pruning older deployments.

## Kernel boundary and unsupported transitions

Only ZBoard changes. The existing publisher validates and activates the generated configuration before confirmation. Zero's user schemas reject unknown fields, so an old kernel without the managed principal field cannot silently acknowledge a differently attributed user. Readiness is committed after successful activation for each supported managed protocol.

SS2022 ChaCha cannot safely hold two users on one listener, so migration is blocked. A single-endpoint subscription with hard speed/device limits is also blocked while the kernel cannot share that limit across old/new principals; migration must not double the allowance. Neither block rewrites the subscription or removes its existing key.

## Local validation

Tests cover deterministic profiles and SS2022 key lengths, signed binding tampering, node isolation, one root across multiple endpoints, exact confirmation and repeated receipts, client output before/after confirmation, finite compatibility and rollback, late old/new reports after secret erasure, per-endpoint multipliers and replay deduplication, new endpoint membership without new credential rows, fresh-only enablement, and MySQL upgrade/restart with unchanged historical credentials. MySQL tests use independent random local schemas. Native/production connection acceptance remains a separate rollout step; no kernel repository change or release build is part of this work.
