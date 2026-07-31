# Mieru native principal attribution prerequisite

Zboard does not modify the Zero kernel as part of the Mieru endpoint-credential
repair. The current Zero contract accepts Mieru users by password but does not
carry the matched user into the native principal/accounting path. Generating a
different password per subscription in Zboard alone would therefore create
credentials that can authenticate but cannot be billed to the correct
subscription.

## Required Zero contract change

Before Zboard can enable per-subscription Mieru credentials, Zero must:

1. add a stable `principal_key` field to each `MieruUserConfig`;
2. preserve the matched Mieru user identity after password authentication;
3. attach that user's `principal_key` to the authenticated session and every
   emitted flow event, using the same semantics as the existing native managed
   protocols;
4. reject duplicate or empty principal keys and configurations that contain a
   password without an attributable principal;
5. keep username/password client compatibility explicit. If a client requires
   username, Zero or the renderer may use `username=password`; username must
   not become the accounting identity;
6. add kernel tests covering two Mieru users on one listener, successful
   authentication, isolation, reconnects, and correct principal propagation to
   completed flow events.

Managed speed/device policy fields should be added only if Zero can enforce
their semantics consistently with other native managed users. They are not a
precondition for identity attribution.

## Zboard activation gate

After a reviewed Zero release exposes and verifies that contract, Zboard may:

- include Mieru in `protocol_credentials`;
- generate one encrypted password and `principal_key` per active subscription;
- compile those users into the endpoint's Mieru server configuration;
- render only the requesting subscription's password;
- migrate active subscriptions, validate with the installed Zero binary, and
  republish affected nodes.

Until then, Zboard reports Mieru as unavailable through the public protocol
capability contract. The backend rejects new Mieru endpoints, re-enabling and
publication; subscription generation excludes retained Mieru records. The
administrator UI keeps the disabled option visible with the kernel reason so
operators do not mistake absence from the picker for a loading defect.
Existing records are not deleted and may be saved only while being disabled,
allowing the next full node publication to remove them safely.

## Zboard rollout gate

The explicit `native-local-mieru` contract may be selected only with a
reviewed, locally pinned Zero artifact implementing the requirements above.
Only that contract enables Mieru credential creation, subscription delivery
and node runtime compilation.

Under that contract Zboard compiles Mieru users with
`username=password`, `password`, and `principal_key`. Template save and preview
execute `zero validate` from the checksum-pinned artifact. Node publication
then executes the installed Zero validator, atomically activates the
generation, checks process/control health and waits for a Connector event.
Only after that complete publication succeeds is
`protocol_endpoints.mieru_principal_ready` set and subscription delivery
switched from the endpoint credential to the requesting subscription's
credential. Validation or activation failure retains the previous generation
and endpoint credential. The first successful migration generation retains the
fallback user under a bounded `migration:endpoint:<id>` principal so existing
clients are not cut off before the database readiness switch. Zboard
acknowledges but never bills those temporary flows. While holding the same
node publication lock, Zboard then performs a second full publication without
the fallback user. Readiness and subscription delivery switch only after this
fallback-free generation also passes validation, activation, health and
Connector confirmation. If cleanup fails, Zero rolls back to the compatibility
generation and the endpoint remains unready, so the shared credential cannot
remain accepted behind a falsely ready state.
