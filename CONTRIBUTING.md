# Contributing

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
