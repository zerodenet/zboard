# Read-only native commerce quotes (v1)

The optional `zboard.commerce.quote.provider.v1` capability exposes the separate
`zboard.plugin.v1.CommerceQuoteProvider/EvaluateCommerceQuote` service. Implement
both `PluginControlServer` and `CommerceQuoteProviderServer` (embedding their
unimplemented server types); the normal SDK `Serve` registers the optional
service. Existing plugins retain their current control protocol unchanged.

Declare a signed native server and the separate configuration grant required
for native plugins. No UI page, payment slot, order read or payment write grant
is implied. The host selects exactly one admitted installation ID; it does not
discover a default or combine offers from multiple providers.

The host constructs `CommerceDiscountContext`: current buyer, derived operation,
SKU/plan, authorized target, purchased price-source order, locked base integer
price/currency/period and input fingerprint. `selection` is at most a user-entered
rule reference, never a trusted price or entitlement. `agreement` carries a prior
immutable recurring contract. Plugins must not treat page-supplied copies of
these fields as host authority.

Use `request.DiscountContext()` and `CommerceDiscountResult(offer)` for bounded
JSON encoding. The response contains only a nonnegative integer `discount_minor`
no greater than the host base, the exact currency/input fingerprint, expiration,
and an explicit agreement. The core owns final arithmetic, quote acceptance,
orders, balances, payment facts and fulfillment. Responses cannot set a payable
amount, entitlement, balance contribution or paid status.

`status` is `available`, `unavailable`, or `requires_confirmation`. Unavailable
requires an explanation and has no discount/agreement; it is not an offer of
zero discount. Changed recurring identity/version/scope/terms requires explicit
confirmation; it cannot silently become an available higher charge. Transport
failure, timeout, disable, admission loss, reconfiguration and generation change
produce no accepted offer. The host allows at most 60 evaluations per provider
per minute and a three-second RPC deadline (a shorter caller deadline wins).

Agreement scope is `initial_only` or `recurring`. Initial-only offers cannot
apply to renewals. Base price and actual paid value remain separate: base120,
initial reduction20 and payment100 must not redefine renewal base as100. Core
transaction integration and automatic-renewal agreement persistence are a later
batch; this contract alone does not enable a discounted purchase.

`terms_json` is an opaque UTF-8 JSON **string** (maximum8KiB), not an embedded
object or executable rule. `terms_hash` is lowercase SHA256 of its compact JSON
bytes. Whitespace is removed without interpreting numbers or sorting keys;
numeric spelling and object-key order remain significant. Retain the exact
representation of an agreement. `CommerceDiscountTermsHash` implements this
rule. Encoding the terms as a string preserves these bytes across outer JSON
escaping, including numbers larger than JavaScript's safe integer range.

Evaluation is read-only and may be repeated after a lost response. It must not
consume/reserve a coupon, debit funds, or perform entitlement/payment writes.
This version has no reserve/commit/release protocol and therefore cannot claim
limited-use coupon redemption. Rules requiring such side effects must report
unavailable until a compatible reliable lifecycle exists. No external coupon
plugin implementation or payment-channel integration is delivered by this SDK.

## Regeneration

Generated files are produced from `v1/commerce_quote.proto`, not edited by hand.
The first implementation used official `protoc`3.18.1, `protoc-gen-go`1.36.1 and
`protoc-gen-go-grpc`1.6.1, with no module dependency changes. The Linux x86_64
compiler archive is
`https://github.com/protocolbuffers/protobuf/releases/download/v3.18.1/protoc-3.18.1-linux-x86_64.zip`,
SHA256 `220bd1704c73dbf4d0a91399a2ecf9d19938b5cd80c8a38839a023d8b87bb772`.
With that compiler extracted into `/tmp/zboard-protoc-3.18.1`, run:

```sh
GOBIN=/tmp/zboard-quote-proto-tools GOMAXPROCS=1 go install -p 1 google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.1
GOBIN=/tmp/zboard-quote-proto-tools GOMAXPROCS=1 go install -p 1 google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.1
# From backend/pkg/pluginapi:
PATH=/tmp/zboard-quote-proto-tools:$PATH /tmp/zboard-protoc-3.18.1/bin/protoc \
  --go_out=. --go_opt=paths=source_relative \
  --go-grpc_out=. --go-grpc_opt=paths=source_relative v1/commerce_quote.proto
```
