# Zero v0.0.3 native integration

The reproducible test and default artifact reference is `v0.0.3-dev.202610102233`,
commit `71b3db1` in `zerodenet/core`. It is not a minimum version or a node
admission rule. Release resolution pins this reference when no version is
selected. Configuration adaptation uses the actual binary's declarations and
native candidate validation. Hot publication needs a working control interface;
Connector is checked when event sinks are configured, and protocol checks concern
only the requested configuration. Missing declarations defer to native validation.
An unavailable operation leaves the existing kernel and configuration running,
retains the desired setting for correction/reconciliation, and never triggers a
restart, forced upgrade or silent removal of an egress constraint. Persistent
Connector state is inspected before an installation or upgrade changes the
running service.
This reference is a **prerelease**, not the final `v0.0.3`. Version metadata
retains the complete `dev`/`rc` suffix but is not used to infer feature support.
Release metadata preserves the registry's prerelease flag and also recognizes
semantic prerelease suffixes, including for locally hosted artifacts. Selecting
a version does not replace inspection of the actual binary and validation of
the candidate configuration.

The renderer selects a named wire contract, not a product-version range.
The native desired configuration uses `zero.event.v1`; an unsupported candidate
stays unapplied rather than being rewritten into a weaker configuration.
Known capability mismatches finish as `deferred` publication/operation records.
The node's applied digest, health and readiness are preserved. Durable publication
intent is retained without incrementing failure attempts; reevaluation is spaced
15 minutes apart, while a new settings generation or an explicit successful
kernel operation resumes it immediately. Migration `0035` stores this distinction.

Regular configuration publication never starts, stops or restarts a service.
It validates the candidate and uses the native CLI `reload`, which invokes the
acknowledged `config.apply` transaction. That transaction owns source persistence,
listener reconciliation and last-known-good rollback. Binary installation and
upgrade remain separate explicit operator operations.

Each node keeps a receipt containing the desired input digest, canonical source
file digest, kernel instance and committed configuration revision. A repeated
publication skips apply only while all four still match. Source formatting is
owned by the kernel; its physical file digest must not be confused with the
panel's desired-input digest. An acknowledgement lost after commit preserves
the credential and durable intent for reconciliation. A later controller
verification/storage failure does not reverse an acknowledged native apply.
Kernel-operation and protocol-deployment archives use separate namespaces.

Startup repairs unfinished credential migrations, not already-ready nodes.
The host's artifact selection mode does not reverse an installed Mieru
readiness fact. Subscription kernel policy revisions are prepared under a
subscription lock and advance only when the policy fingerprint changes;
traffic settlement timestamps are excluded. Migration `0033` adds this state.

Endpoint egress stores a backwards-compatible protocol object. Constrained
Direct adds `dial` to that object; compilation moves it to the native outbound
level and references the named outbound only from the selected inbound.
Default Direct/auto remains implicit. Non-default dial constraints on proxy
protocols fail validation. Local interface/address ownership is checked by the
kernel; no failed constraint may become an unrestricted socket.

For an endpoint that must exit through IPv6 on a selected interface, the stored
egress object is:

```json
{
  "type": "direct",
  "dial": {
    "address_family": "only_ipv6",
    "interface": "eth1",
    "source_ip": "2001:db8::10"
  }
}
```

The interface and source address are optional and must belong to the node that
executes Direct (the landing node). `only_ipv6` forbids IPv4 fallback; it does not
provide IPv4-to-IPv6 translation or make IPv4-only destinations reachable. A
dual-stack relay between an IPv4 entry and IPv6 landing remains an independent
forwarding path.

Protocol and managed forwarding listeners have independent local listen and
public subscription addresses. Migration `0034` adds forwarding listen addresses;
old records default to `0.0.0.0`, and omitted update fields preserve existing
values. IPv6 wildcard listeners follow OS defaults and do not promise portable
dual-stack acceptance.

## Verification

```sh
cd backend
GOMAXPROCS=2 go test -p 2 ./...
ZERO_NATIVE_TEST_BINARY=/absolute/path/to/verified/zero \
  GOMAXPROCS=2 go test -p 2 ./internal/adapters/zero -run '^TestNative' -count=1 -v
```

The native tests launch only their own local kernel processes and isolated
directories. They exercise existing TCP/UDP across publisher-process restarts,
lost acknowledgements, duplicate publications, occupied-listener rollback,
IPv4 ingress to IPv6 egress, IPv6 listening, loopback source/interface binding,
and failure without address-family/interface fallback. Supply a checksum-verified
release binary. An absent binary or unavailable host IPv6 is an explicit skip;
unit tests alone are not production or cross-platform connection evidence.
