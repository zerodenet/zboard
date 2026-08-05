# Public subscription filtering

The public subscription endpoint can derive multiple read-only client views from one existing user token:

```text
/api/v1/client/subscription/{token}
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

1. Zboard loads the token owner's active subscriptions.
2. `plan`, `sku`, and `node_group` reduce those subscription sources.
3. Zboard resolves protocol endpoints and credentials only from the remaining authorized sources.
4. Endpoint filters reduce that authorized endpoint set again.
5. The result is deduplicated, ordered by the global protocol delivery order, and sent to the selected renderer.

A filter can only remove authorized endpoints. It cannot add a node group, endpoint, credential, plan, or SKU that the token owner does not already have.

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

Malformed stable codes, unsupported protocol values, overlong values, and control characters return HTTP 400. A valid filter that matches no authorized source or endpoint returns a valid empty subscription with unchanged quota metadata and `Cache-Control: no-store`.

## Delivery order and service state

All native renderers consume the same ordered manifest. The administrator's global protocol delivery order is authoritative across node groups and aggregated subscriptions. Endpoint identity is only a deterministic tie-breaker.

`ProtocolEndpoint.is_active` is the protocol-service delivery switch. Disabled services are removed from public subscription output and from node runtime publication; re-enabling the service restores both through the existing runtime publish flow. No schema migration is required.

## Current delivery phase

This phase implements the backend projection contract and renderer consistency required by Issue #13. The account-page visual filter builder and OpenAPI parameter presentation remain separate follow-up work; clients can already construct filtered links directly from the documented query parameters.
