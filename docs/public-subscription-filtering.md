# Public subscription filtering

Every public subscription URL is bound to exactly one subscription. Query parameters can derive read-only client views from that subscription without changing its authorization boundary:

```text
/api/v1/client/subscription/{subscription-token}
  ?template=clash
  &protocol=vless,hysteria2
  &region=jp,hk
  &tag=premium,streaming
  &exclude_tag=maintenance
  &plan=pro
  &sku=pro-annual
  &node_group=jp-premium
  &q=日本
```

## Authorization boundary

Filtering is an output projection, not an authorization mechanism:

1. Zboard resolves the token to one `subscription_id` and verifies that the same user owns both records.
2. Zboard verifies that exact subscription is active, unexpired, and has remaining traffic.
3. `plan`, `sku`, and `node_group` may remove that source, but they cannot select a different subscription owned by the same account.
4. Zboard resolves protocol endpoints and credentials only from the token-bound subscription.
5. Endpoint filters reduce that authorized endpoint set again.
6. The result is ordered by the configured protocol delivery order and sent to the selected renderer.

A filter can only remove endpoints authorized by the token-bound subscription. It cannot add a node group, endpoint, credential, plan, SKU, or another subscription.

The `Subscription-Userinfo` response header and manifest quota metadata always describe the token-bound subscription only. Traffic totals and expiry are never accumulated across the account.

## Account access API

Authenticated users manage credentials from the target subscription:

| Method | Path | Meaning |
| --- | --- | --- |
| `GET` | `/api/v1/account/subscriptions/{id}/access` | Read or lazily provision the link for one active subscription |
| `POST` | `/api/v1/account/subscriptions/{id}/access/rotate` | Replace only that subscription's token |
| `DELETE` | `/api/v1/account/subscriptions/{id}/access` | Revoke only that subscription's token |

The legacy account-level `/api/v1/subscription/access` routes are removed. Existing aggregate tokens without a `subscription_id` are invalidated during schema reconciliation, and one independent token is provisioned for each usable subscription.

## Parameters

| Parameter | Meaning | Matching |
| --- | --- | --- |
| `template` | Existing renderer slug or `native` | Exact |
| `plan` | Stable plan slug | OR within the parameter |
| `sku` | Stable plan SKU code | OR within the parameter |
| `node_group` | Stable node-group code | OR within the parameter |
| `protocol` | Supported protocol code | OR within the parameter |
| `region` | Node region | OR within the parameter |
| `tag` | Structured protocol-service tag | Any requested tag |
| `exclude_tag` | Structured protocol-service tag to remove | Any match excludes |
| `q` | Protocol-service name keyword | Case-insensitive substring |

Different dimensions use AND semantics. Values can be supplied as comma-separated items or repeated query parameters. Values are normalized, deduplicated, and bounded in count and length.

Malformed stable codes, unsupported protocol values, overlong values, and control characters return HTTP 400. A valid filter that removes the token-bound source or matches no endpoint returns a valid empty subscription while keeping that subscription's quota metadata and `Cache-Control: no-store`.

## Delivery order and service state

All native renderers consume the same ordered manifest. The administrator's protocol delivery order remains authoritative within the token-bound subscription. Endpoint identity is only a deterministic tie-breaker.

`ProtocolEndpoint.is_active` is the protocol-service delivery switch. Disabled services are removed from public subscription output and from node runtime publication; re-enabling the service restores both through the existing runtime publish flow.
