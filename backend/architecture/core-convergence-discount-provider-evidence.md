# P2-15 read-only discount provider boundary

This batch implements the provider boundary for decision D08; it does not enable
discounted order creation, coupon redemption or automatic renewal. The existing
purchase/renewal/payment/fulfillment path is unchanged. No database migration,
new frontend, external plugin repository or production action is included.

## Contract and authority

Core `commerce.DiscountQuoter` accepts exactly one explicitly selected installation
ID and a host-constructed `DiscountQuoteContext`. It contains the current buyer,
derived operation, plan/SKU, authorized target, actual purchased price-source
order, locked base/currency/period, input fingerprint and optional prior agreement.
No browser or page result can supply these fields as authoritative business facts.

The independent native service is
`zboard.plugin.v1.CommerceQuoteProvider/EvaluateCommerceQuote`; its separate
manifest grant is `zboard.commerce.quote.provider.v1`. There is no provider
discovery order, stacking rule, page operation or plugin-to-core payment command.
The normal SDK registers this optional service only when its server implements
the corresponding interface. Existing control protobuf/generated files are
unchanged, and the composite client retains the existing control interface.

The host checks signed-package admission, native execution, enabled/active state,
configured revision, valid signing public key, package identity and current
runtime before dispatch. Each provider has a 60-per-minute admission limit and
a three-second RPC deadline; a shorter caller deadline wins. Input and accepted
output JSON are bounded to16KiB and terms to8KiB. After RPC, the host checks the
same process, admission, signing identity, package, generation and configuration
revision before accepting the offer. It supplies provider identity itself.

An offer contains only an integer reduction, identical currency/input fingerprint,
expiry, explicit `initial_only` or `recurring` agreement and a bounded explanation.
Negative reductions, amounts above base, unknown fields, scope/currency changes,
expired offers and invalid terms hashes are rejected. A changed prior agreement
cannot return `available`; it must require confirmation. `unavailable` remains an
explicit state and cannot be converted to a zero-discount approval. An
initial-only offer is rejected for a renewal or other non-new operation.

Opaque `terms_json` is encoded as a string to preserve exact JSON bytes through
outer JSON escaping. The hash is SHA256 of compact UTF-8 JSON, without converting
numbers or sorting keys. Core/SDK tests cover values above JavaScript's safe
integer range and HTML-sensitive characters. This defines an agreement identity,
not a pricing/rounding engine in core.

## Verification, 2026-10-20

Cloud workspace, Go1.26.8; `GOMAXPROCS=1`, `-p 1`. The following commands also used
`PATH=/workspace/toolchains/go/bin:$PATH`,
`GOCACHE=/workspace/toolchains/cache/go-build` and
`GOPATH=/workspace/toolchains/gopath`.

```sh
# From backend/pkg/pluginapi: complete independent SDK module.
go test -p 1 ./... -count=1 -timeout 3m
go vet -p 1 ./...
go mod tidy -diff

# From backend: no database and no native subprocess.
go test -p 1 ./internal/capabilities/commerce ./internal/plugins ./internal/application \
  -run '^Test(DiscountQuote|CommerceQuote(Signed|Runtime|NativeResponse)|CommerceDiscountAdapter)' \
  -count=1 -timeout 3m

# From backend: static checks for all three touched host packages.
go vet -p 1 ./internal/capabilities/commerce ./internal/plugins ./internal/application

# Separately scheduled SQLite plus one signed native subprocess.
go test -p 1 ./internal/plugins \
  -run '^TestCommerceQuoteRealNativeAdmissionAndReconfiguration$' -count=1 -timeout 3m
```

Recovery validation reran every command above on2026-10-20, after the
previous ephemeral logs had disappeared. The complete SDK module passed in0.005s;
SDK vet and `go mod tidy -diff` both exited0 with no output. Host pure tests
passed: commerce0.003s, plugins0.016s and application0.006s. Host vet exited0.
The real native test passed in3.545s. Its executable is compiled from
in-repository testdata with `go build -p 1`. Temporary SQLite and process resources
are closed by test cleanup. No MySQL was used.

Current logs are under the ignored persistent repository directory
`.codex-local-artifacts/recovery-validation/`: `discount-sdk-test.log`,
`discount-sdk-vet.log`, `discount-sdk-tidy.log`, `discount-pure-test.log`,
`discount-host-vet.log` and `discount-native-test.log`. Empty vet/tidy logs record
successful commands with no diagnostics; structured results are retained in
[the validation record](validation/2026-10-20-discount-provider.json).

The real signed/native test covers disabled and unconfigured rejection, valid
host-owned context/offer, a20ms caller deadline and changed configuration,
generation and authorization while a slow native call is in flight. The fixture's
Health counter proves evaluation has started before state changes; it does not
guess this from a sleep or use health as proof of an offer. Revoked authority is
also rejected before another native evaluation. Pure tests cover amount,
currency, scope, fingerprints, terms hash, unknown/oversized responses, recurring
confirmation, caller cancellation, explicit provider selection and rate limits.
An ordinary page authority remains unable to record a payment fact. SDK tests
also prove an old control-only server acquires no quote service.

Generated protobufs use official protoc3.18.1, protoc-gen-go1.36.1 and
protoc-gen-go-grpc1.6.1. Repeating the pinned generation command produced identical
hashes for both generated files. Recovery validation downloaded the pinned
compiler archive, verified its SHA256, generated into the ignored artifact
directory and compared both files byte for byte; both were identical
(`discount-proto-reproducibility.log` in the same persistent log directory).
The fixed archive hash and exact commands are
in [the SDK contract](../pkg/pluginapi/v1/commerce_quote.md). No module dependency
or existing control protobuf/generated file changed.

## Next transaction batch and remaining limits

The commerce adapter validates and returns evidence only. It is deliberately
not wired into `prepareOrder`, PurchaseSnapshot, automatic renewal, funding or
payment facts yet. Those paths must explicitly handle offer status, preserve
locked base versus discount/final/actual-paid values, revalidate business facts
after lock-free native evaluation and bind the accepted quote before an order
commit. Existing discounted historical orders continue their current explanatory
automatic-renewal pause; this batch does not relax it.

Preview evaluation must be repeatable and read-only. There is no reserve/commit/
release protocol, so finite-use coupon consumption is not supported. A provider
whose rule requires those side effects must report unavailable. No actual
commercial coupon plugin, discounted purchase UI, sustained plugin resource
benchmark or full-phase P2/P5 completion is claimed. SDK full test/vet/module
consistency is now verified. Exact-commit global checks and capability inventory
integration remain the integrating step's work.

## Contract inventory integration

The ownership scanner now enumerates every v1 protobuf source and actual service
name, ignoring commented/string/nested-option RPC text. Independent new services
cannot bypass the unassigned-contract check. The ledger contains 17 RPCs and 31
capabilities; host operations remain 16 and cross-domain transactions 19.
`go test -p 2 ./tools/capabilityinventory -count=1` passed on this integration.
No order/renewal wiring is implied by this contract-only batch.
