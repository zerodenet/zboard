# Contributing

## Development setup

Follow [docs/development.md](docs/development.md) for the pinned toolchain,
one-command startup, manual service startup and local verification commands.

Keep the root `README.md` focused on product positioning and operator-facing
entry points. Contributor procedures, troubleshooting and script parameters
belong in this guide or a focused document under `docs/`.

## Documentation scope

Version architecture, API and configuration contracts, operator/developer guides,
reusable verification procedures, roadmaps and release/upgrade notes under `docs/`.
Store temporary implementation plans, audit journals, per-run test reports and
machine-specific evidence in the ignored `.codex-local-artifacts/` directory.
When a working note contains a lasting decision or procedure, move that content
into the relevant guide; keep the execution history local. Published documents
must not depend on ignored notes. Dated release notes remain versioned.

## Branch policy

- `main`: stable and releasable
- `feat/*`, `fix/*`: feature and fixes
- `release/vX.Y.Z`: release prep

## Commit format

- Use conventional commit style
- Include verification notes in the commit message body when relevant

## Code conventions

- Follow [the core and hardening baseline](docs/core-baseline.md): preserve
  the existing working flows, fix defects in small verified changes, measure
  performance, and expose narrow internal services for future extensions.
  Online payment and a plugin runtime are not core-release prerequisites.
- Keep API changes backward-compatible for `v0.1.x`
- New endpoint must be added to `backend/api/openapi.yaml`
- Sensitive operations (SSH, protocol publish, payment callback) should include audit logs and tests where possible
- Database structure is owned by the embedded SQL migrations, not runtime GORM
  `AutoMigrate`; follow [docs/database-migrations.md](docs/database-migrations.md).
- Run backend tests and vet for backend changes, and frontend tests plus the
  production build for frontend changes. Record any check that could not run.
