# Release Guide

## Version policy

- `v0.0.1`: current internal engineering baseline
- `v0.1.0`: first public release

## Release checklist

1. API schema lock: `/api/v1` behavior finalized
2. DB migration validation (`up` + `down` scripts prepared)
3. Docker backup, upgrade, and rollback flow rehearsed
4. Backend startup and readiness probes are passable
5. Frontend can consume core APIs for dashboard, nodes, plans, billing
6. Security review passed for auth, payment callback, and SSH operations
7. API contract in `backend/api/openapi.yaml` is frozen for release.
8. Release artifacts include version metadata in `/api/v1/version`
9. Use `docs/release/v0.1.0-launch-checklist.md` for end-to-end release runbook.

## Version lock

- Toolchain baseline: `backend/go.mod`, the Docker builder image, Node.js, and pnpm use reviewed pinned versions.
- Keep `backend/internal/version/version.go` aligned with release tag.
- When `v0.1.0`, keep `v0.1.x` backward-compatible as long as business requirements allow.

## Rollback requirements

- Keep last stable image available.
- Keep migration rollback scripts.
- Rollback steps:
  - Switch traffic back to previous revision
  - Confirm health and dashboard pages are still readable
  - Confirm orders/subscriptions queries still work

Kubernetes deployment and rollout automation are outside the supported v0.1.0 release path.

## Tag and release command

```bash
git tag -a v0.1.0 -m "release v0.1.0"
git push origin v0.1.0
```

- GitHub Actions will build:
  - backend binary with `internal/version` ldflags
  - docker image with same version metadata
  - publish image to `ghcr.io/zerodenet/zboard`

## Release notes artifact

- Before publishing, attach `docs/release/v0.1.0-launch-checklist.md` results as the public release check evidence.
- Include:
  - smoke test outputs
  - Docker backup and rollback rehearsal result
  - any known limitations and mitigation
