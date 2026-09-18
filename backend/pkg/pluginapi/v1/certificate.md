# Certificate provider capability v1

An admitted native plugin may declare `zboard.certificate.provider.v1` together with a configurable server and one to eight signed `contributions.certificate_providers` entries. A provider key may also declare the DNS provider capability; the host exposes one combined provider account while preserving both capabilities. Built-in keys are reserved, and duplicate active plugin keys are hidden instead of being selected by installation order.

The host exposes two bounded RPCs:

- `VerifyCertificateCredential` validates the opaque credential for one provider account.
- `IssueCertificate` receives an already validated CSR, domains, ACME contact, environment, renewal flag and operation identity, then returns a PEM certificate chain.

The target node generates the private key and signed CSR. The private key never enters the host process, plugin RPC, provider account, log or database. The host verifies the CSR signature and exact domain set before calling the plugin, then verifies that the returned leaf certificate matches the CSR public key, covers every requested domain, is currently usable and has more than 24 hours remaining. Certificate bytes are streamed to the node over SSH stdin and are never interpolated into a shell command. The node repeats the public-key match before atomically switching the current generation.

Credentials are decrypted only for a single verification or issuance call. Every call is fenced by the active host lease, installation generation, version and configuration revision; disable, uninstall, replacement or reconfiguration invalidates an in-flight result before it can update certificate state. Issuance has a six-minute host deadline and a 96 KiB chain limit.

The built-in Cloudflare Certbot path remains compatible during migration. Older plugins remain compatible through the generated unimplemented RPC methods, while older hosts reject the unknown capability at admission.
