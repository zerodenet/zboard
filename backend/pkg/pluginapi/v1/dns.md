# DNS provider capability v1

An admitted native plugin may declare `zboard.dns.provider.v1` together with a configurable server and one to eight signed `contributions.dns_providers` entries. Provider keys are stable, globally routed identifiers; built-in keys are reserved and duplicate active plugin keys are hidden rather than selected by installation order.

The host exposes two bounded RPCs:

- `VerifyDNSCredential` validates the opaque credential for one provider account.
- `ApplyDNSRecord` applies one already validated A or AAAA desired record and returns only the provider zone/record identity plus observed record facts.

The credential is decrypted immediately before one RPC and is never placed in plugin configuration, logs, provider results or public APIs. A native plugin is trusted executable code rather than an operating-system sandbox, so publishers must still avoid retaining the credential. The host sends no database handle, user object, node object or generic command surface.

Every call is fenced by the active host lease, installation generation, version and configuration revision. Disable, uninstall, replacement or reconfiguration invalidates an in-flight result before core state can be completed. Calls have host deadlines and are not assumed idempotent; the existing provider operation ledger records accepted, running, succeeded and failed outcomes.

Older plugins remain compatible because they do not declare the capability and their embedded unimplemented server supplies the new RPC methods. Older hosts reject an unknown capability at admission. The built-in Cloudflare adapter and existing HTTP routes remain available during migration.
