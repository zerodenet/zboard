# Public HTTP route request contract

A native plugin declares `zboard.http.route.v1` and exact method/path contributions
in its signed manifest. ZBoard dispatches only to the current enabled, active,
admitted route owner. Registration conflicts, disablement, upgrades and uninstall
follow the existing registry and process lifecycle. Request headers, query fields
and supplied route IDs cannot select another plugin or grant host capabilities.

The host reads the bounded body to EOF, captures the request, assigns the matched
manifest `route_id`, and invokes `PluginControl.HandleHTTP`. The plugin owns query
and payload parsing, route-specific authentication, signature verification and the
business response. No provider, payment or notification dialect is interpreted
by this transport.

## Request fields

Existing protobuf field numbers 1–5 retain their meaning. Fields 6–18 are additive;
the handshake protocol and capability remain v1. Existing plugins can ignore
unknown fields, and plugins consuming the new information must rebuild with the
updated SDK.

| Field | Meaning |
| --- | --- |
| `route_id` | Host-selected ID from the admitted signed route contribution. |
| `method`, `path` | Observed method and Go HTTP parser's decoded URL path used for registry matching. |
| `content_type` | Compatibility field containing the first Content-Type value when it is UTF-8; otherwise empty. The original value always remains in `headers`. |
| `body` | Application body bytes as delivered by Go's HTTP parser, with no JSON/form parsing, query merging, decompression or re-encoding by the route dispatcher. HTTP chunk framing has already been removed by the HTTP transport. |
| `raw_query`, `force_query` | Unparsed query string and presence of an empty trailing `?`. Duplicate keys, empty values, order, percent escape case and `+` are retained; even malformed query escapes are left for the plugin to handle. |
| `request_uri`, `raw_path` | Original server request target and URL.RawPath. Use `request_uri` for signatures over the request target; `raw_path` may be empty for canonical paths. |
| `headers` | Header-name map to `HTTPHeaderValues.values`, retaining every value as bytes, including non-UTF-8 octets, Authorization, Cookie and signature headers. No allowlist, merging or redaction. |
| `host` | HTTP Host/authority, which Go stores separately from Header. |
| `scheme`, `proto`, `remote_addr` | Actual HTTP/TLS transport scheme, HTTP protocol and socket peer observed by ZBoard. |
| `content_length`, `transfer_encoding` | Parsed transport metadata; a length of -1 means unknown. |
| `trailers` | All trailer names and values after the body has reached EOF. |
| `tls` | Optional observed TLS version, cipher suite, SNI, ALPN, handshake/resumption flags, DER peer certificates and verified certificate chains. Absent on an HTTP connection. |

Headers retain Go `net/http`'s parsed representation. Original header-name casing,
wire whitespace and ordering across different header names are not available;
ordering within a name's value list is preserved. The dispatcher never rebuilds
the body or query from parsed parameters.

Forwarded/X-Forwarded-* headers are passed as caller-supplied values and do not
override transport metadata. A plugin that accepts traffic through a proxy owns
its proxy trust policy. TLS metadata describes the connection to ZBoard, not an
earlier connection terminated by a reverse proxy. Public route invocation does
not authenticate a ZBoard account or attach a host account/session assertion.

## Plugin parsing and verification

For example, a plugin can parse a query itself and reject invalid syntax:

```go
query, err := url.ParseQuery(request.RawQuery)
if err != nil {
    return &pluginv1.HTTPResponse{
        Status: 400, ContentType: "text/plain", Body: []byte("invalid query"),
    }, nil
}
nonces := query["nonce"] // preserves duplicate values after plugin-owned parsing
signatures := request.Headers["X-Webhook-Signature"].GetValues() // [][]byte
// Verify against request.Body and/or request.RequestUri according to the
// plugin's own protocol before applying a business transition.
```

`NewHTTPRequest` is the SDK's transport snapshot helper; body, header/trailer
lists and TLS certificate bytes are copied. It does not assign a route ID.

## Transport bounds and responses

The application body limit is 8 MiB and the serialized request metadata budget is
256 KiB, including the protobuf field framing. Oversized requests return HTTP 413
and are not truncated or sent to the plugin. The SDK server and host client use a
9 MiB gRPC message ceiling, allowing the complete accepted body and metadata to
cross the process boundary. Dispatch retains its 15-second deadline.

Responses retain `HTTPResponse`: status, content_type, body and retry_after.
The existing transport accepts statuses 200, 400, 401, 403, 404, 409, 429 and 503,
JSON/plain text content types and bodies up to 8 MiB. Retry-After is emitted only
for positive values up to 3600 seconds.
ZBoard adds no-store and nosniff response headers. Plugin invocation failures are
503; missing registrations are 404; invalid plugin responses are 502.

The signed test plugin demonstrates an ordinary notification protocol that
parses its own query and verifies an HMAC over the original request target and
body. It is a development fixture, not a built-in provider integration.
