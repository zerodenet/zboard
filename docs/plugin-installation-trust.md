# Plugin installation trust

**English** · [简体中文](plugin-installation-trust.zh-CN.md)

ZBoard verifies package contents before accepting a signing identity. A package may include an Ed25519 public key in `signature.json.public_key`; this proves that its manifest and file hashes match the signature, but does not identify the publisher by itself.

## Offline import

The administrator selects a package, reviews its name, publisher, capabilities, UI surfaces and compatibility, and confirms installation. An unknown source additionally requires explicit confirmation of the signing-key fingerprint. The confirmation request binds the SHA-256 of the entire archive and the fingerprint of the verified public key. Inspection performs no installation, runtime execution or trust write.

The host stores the public key and per-plugin trust together with the installation transaction. A failed installation grants no trust. Later versions must retain both plugin ID and publisher key; trusting one plugin does not trust other plugins from that publisher. Uninstall retains the identity pin along with the installation history, preventing another key from claiming the same plugin ID. Key rotation is deliberately rejected until a separate reviewed rotation workflow is available.

Legacy archives without an embedded key can supply their public key through the dialog's compatibility option. Previously configured publisher keys remain supported; removing a configured key still revokes installations whose trust originated from that configuration. Explicit per-plugin trust is stored in the database and retained across restarts.

## Market installation

Without `plugins.catalog_url`, the market reads the [ZeroDeNet ZBoard registry](https://github.com/zerodenet/plugins/blob/main/catalogs/zboard.json). Each plugin has a detail page with platform downloads, online installation and a link to its installed management page. The detail page resolves `releases/download/v<registered-version>/marketplace-entry.json` from the registered GitHub repository. The release must match the registry's plugin ID, repository, publisher and version, and list platform-specific `.zbplugin` assets with SHA-256, size and the publisher's public key. No arbitrary download URL is accepted from the browser.

Online installation selects the server's platform (or a platform-independent `any` artifact). Inspection downloads the package, checks its size, digest, identity, signature and compatibility, and displays the same confirmation used by offline import. A public listing or publisher metadata never grants trust: an unknown key requires explicit fingerprint confirmation scoped to that plugin. Confirmation re-fetches and re-validates the package against the inspected archive digest; changed packages require inspection again. Existing signing-key pins, capability admission, migrations and activation policy continue to apply. A missing release or unsupported server platform remains visible without presenting an installable package.

The host first validates the configured catalog signature and expiry. Each signed entry may supply `public_key`, attesting that publisher key only for the entry's plugin. Before recording trust, the host checks the package digest, signature, plugin ID, publisher and version. Entry keys do not enter the global publisher configuration and cannot sign future catalogs.

`plugins.catalog_url` must reference a signed host catalog, not the source registry JSON. The catalog signer remains a host-configured trust root. This implementation does not publish a production catalog or invent an official market key.

Downloads allow at most four redirects. Every destination must use HTTPS on port 443, and every DNS resolution is checked against private addresses before dialing. Redirect targets may contain expiring signed queries used by release hosting services. Authorization and cookie headers are removed on redirects; package signatures and digests remain mandatory.

## Packaging and compatibility

`pluginpackager -source <directory> -out <package.zbplugin>` automatically creates and reuses a private local signing key beneath the user's configuration directory. Explicit `-key` and `-key-id` remain available and are required for catalog signing. OAuth's build wrapper also creates and reuses its ignored development key by default.

The updated packager adds the optional public-key field to package signatures. Earlier hosts with strict signature decoding must be upgraded to read these new packages. Existing released archives are not rewritten; the compatibility input supports them without changing their checksums.

Installation remains disabled initially. Core capability admission, registration policy, account ownership, configuration validation and data migrations apply independently of publisher trust.

Plugin admission depends on the host's plugin protocol, UI bridge, supported capabilities and runtime platform, together with host authorization and data lifecycle checks. ZBoard product release numbers, including dev and RC suffixes, do not block installation, activation or active upgrades. `requires.zboard` is optional advisory metadata; `tested_zboard_versions` records publisher test coverage. An out-of-range or unrecognized host version produces a notice, not a refusal or extra activation consent. Existing signed packages need no rebuilding to adopt this host policy. Unsupported APIs, capabilities and runtime platforms remain rejected.
