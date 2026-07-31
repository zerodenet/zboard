# Mieru native principal attribution prerequisite

Zboard does not modify the Zero kernel as part of Mieru endpoint-credential
support. Zero `0.0.15-rc.4` implements the required matched-user
`principal_key` propagation. Older releases accept Mieru users by password but
cannot attribute their traffic to the correct subscription, so Zboard keeps
Mieru disabled on those nodes.

## Required Zero contract

The rc.4-or-newer contract used by Zboard must:

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

For a target node running Zero rc.4 or newer, Zboard:

- include Mieru in `protocol_credentials`;
- generate one encrypted password and `principal_key` per active subscription;
- compile those users into the endpoint's Mieru server configuration;
- render only the requesting subscription's password;
- migrate active subscriptions, validate with the installed Zero binary, and
  republish affected nodes.

The public protocol capability contract advertises `0.0.15-rc.4` as the
minimum Zero version. The backend checks the selected or actually installed
version rather than a panel-wide flag. On older nodes it rejects new Mieru
endpoints, re-enabling and publication; subscription generation excludes
retained Mieru records. Existing records remain visible and can be disabled or
deleted safely.

## Zboard rollout gate

`native-local-mieru` remains a backwards-compatible contract name. A reviewed,
locally pinned rc.4-or-newer artifact under the normal `native-local` contract
enables the same Mieru behavior automatically. GitHub-managed nodes are gated
by the selected or probed installed version.

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

`credential_id` is deliberately absent from every emitted Zero user object. It
is a stable panel/database identifier, not part of the Zero runtime schema.
