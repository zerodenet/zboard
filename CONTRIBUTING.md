# Contributing

## Development setup

Follow [development guide](https://docs.zerodenet.org/projects/zboard/contributing/development) for the pinned toolchain,
one-command startup, manual service startup and local verification commands.

Keep the root `README.md` focused on product positioning and operator-facing
entry points. Contributor procedures, troubleshooting and script parameters
belong in this guide or the standalone documentation repository.

## Documentation scope

Maintain public guides, installation instructions, plugin documentation, architecture
and API contracts in [zerodenet/docs](https://github.com/zerodenet/docs), under
`docs/projects/zboard/`. This product repository's `/docs/` directory is ignored
and may contain local reference copies; changes there are not published.

Keep temporary implementation plans, audit journals and machine-specific evidence
in the ignored `.codex-local-artifacts/` directory. Move lasting procedures to the
documentation site. Build-owned release notes stay in `.github/release-notes/`.

Keep `README.md` and `README.zh-CN.md` aligned in scope, feature availability,
and getting-started links. Verify feature claims against the release tag before
calling them released; describe development-only features separately. Installation
guides should lead to a working first setup, while release history, migration
procedures, and engineering acceptance targets belong in their focused guides.

## Branch policy

- `main`: stable and releasable
- `feat/*`, `fix/*`: feature and fixes
- `release/vX.Y.Z`: release prep

## Commit format

- Use conventional commit style
- Include verification notes in the commit message body when relevant

## Code conventions

- Follow [the core and hardening baseline](https://docs.zerodenet.org/projects/zboard/reference/core-baseline): preserve
  the existing working flows, fix defects in small verified changes, measure
  performance, and expose narrow internal services for future extensions.
  Keep the core focused on essential panel functions. Implement online payments
  and other capabilities beyond the core through plugins and explicit host
  interfaces; do not describe them as missing built-in features.
- Keep API changes backward-compatible for `v0.1.x`
- New endpoint must be added to `backend/api/openapi.yaml`
- Sensitive operations (SSH, protocol publish, payment callback) should include audit logs and tests where possible
- Database structure is owned by the embedded SQL migrations, not runtime GORM
  `AutoMigrate`; follow [database migration guide](https://docs.zerodenet.org/projects/zboard/reference/database-migrations).
- Run backend tests and vet for backend changes, and frontend tests plus the
  production build for frontend changes. Record any check that could not run.
