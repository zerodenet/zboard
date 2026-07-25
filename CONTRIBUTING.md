# Contributing

## Development setup

Follow [docs/development.md](docs/development.md) for the pinned toolchain,
one-command startup, manual service startup and local verification commands.

Keep the root `README.md` focused on product positioning and operator-facing
entry points. Contributor procedures, troubleshooting and script parameters
belong in this guide or a focused document under `docs/`.

## Branch policy

- `main`: stable and releasable
- `feat/*`, `fix/*`: feature and fixes
- `release/vX.Y.Z`: release prep

## Commit format

- Use conventional commit style
- Include verification notes in the commit message body when relevant

## Code conventions

- Keep API changes backward-compatible for `v0.1.x`
- New endpoint must be added to `backend/api/openapi.yaml`
- Sensitive operations (SSH, protocol publish, payment callback) should include audit logs and tests where possible
- Database structure is owned by the embedded SQL migrations, not runtime GORM
  `AutoMigrate`; follow [docs/database-migrations.md](docs/database-migrations.md).
- Run backend tests and vet for backend changes, and frontend tests plus the
  production build for frontend changes. Record any check that could not run.
