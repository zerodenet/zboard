# Managed rule import and client compatibility

ZBoard imports independent rule sets, stores their matching conditions, and publishes stable client-specific URLs. Routing actions and ordering belong to subscription templates.

## Importing dler-io/Rules

Use files under `Clash/Provider/` with source format `auto` (recommended) or `clash_classical`. Auto detection accepts Provider YAML, line-oriented Classical rules, domain lists, CIDR lists and canonical JSON. An explicitly selected format is never silently changed.

Supported Classical matchers are `DOMAIN`, `DOMAIN-SUFFIX`, `DOMAIN-KEYWORD`, `IP-CIDR`, `IP-CIDR6`, `PROCESS-NAME` and `PROCESS-PATH`. Process names and paths retain case and spaces. Unknown matchers fail with their position rather than being dropped.

`sing-box/1.12/Head.conf` and `Rule.conf` are configuration fragments, not independent rule sets. They must be adapted in subscription templates; they are not importable as Provider files. Files containing only comments, such as the current `Media/MOO.yaml`, are rejected with an empty-source explanation. A rejected synchronization retains the previous source.

## Storage and publishing

Existing network-only documents retain the Zero Rule IR v1 shape and continue to compile to ZRS. ZBoard stores client-specific matchers in an optional `client_rules` extension, outside the Zero IR `rules` array:

```json
{
  "version": 1,
  "rules": [{ "type": "domain_suffix", "value": "example.com" }],
  "client_rules": [{ "type": "process_name", "value": "Example.exe" }]
}
```

This extended document is internal ZBoard storage, not a new Zero kernel contract. Client-only sources may have `rules: []`, but the combined document must contain at least one matcher. Their database format is `managed_client_rules`.

Clash YAML/text and sing-box source exports preserve both arrays. sing-box uses separate rule objects for different condition types, preserving the imported set's OR semantics. The sing-box artifact cache has a new format revision so previously generated files cannot bypass the corrected encoder.

Zero currently cannot evaluate process rules. A source containing any client rules does not produce ZRS, is excluded from the Zero template picker, and is rejected if explicitly bound to a Zero template. Updates adding client rules to a source already bound by Zero templates are rejected before changing its content. There is no partial ZRS export that silently drops process conditions.

The copied public URL for client-only sources selects sing-box source format. Clash templates automatically select the Classical YAML endpoint. Actual process detection remains a client/platform capability; forwarding a remote device's traffic does not provide its process identity.

## Verification

Normal backend tests cover parsing, canonical round trips, client exports, public downloads, Zero compatibility guards and rejected updates. To validate a checked-out external Provider corpus with a real sing-box binary:

```sh
ZBOARD_RULE_PROVIDER_TEST_DIR=/path/to/Rules/Clash/Provider \
ZBOARD_SING_BOX_VALIDATE_BIN=/path/to/sing-box \
go test ./internal/handler -run TestManagedRuleProviderRepositoryCompatibility -v
```

Run from `backend/`. The optional corpus test performs no network downloads. It imports each nonempty YAML source, round-trips through Clash YAML, and compiles the sing-box output to SRS.
