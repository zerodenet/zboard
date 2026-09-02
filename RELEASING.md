# Release Guide

## Version policy

- `v0.0.1`: current internal engineering baseline
- `v0.1.0`: first public release
- Keep all pre-public implementation and hardening work on `v0.0.1`; internal phases are not version increments.
- Keep all v0.0.1 development schema changes squashed into
  `backend/migrations/0001_init.{up,down}.sql`. Do not publish the retired
  development migration chain as installation history.
- Once v0.1.0 is public, treat every released migration as immutable and add
  subsequent schema changes as ordered, append-only migration pairs.
- Identify candidate builds by Git commit and build metadata. Do not create intermediate `v0.0.x` versions merely to mark progress.

## Release checklist

1. API schema lock: `/api/v1` behavior finalized
2. DB migration validation:
   - a clean database applies and rolls back the single v0.0.1 baseline;
   - an existing v0.0.1 development database has reached the terminal
     pre-squash migration and passes the final schema-signature check;
   - post-v0.1.0 changes have new `up` + `down` scripts without modifying a
     released migration.
3. Docker backup, upgrade, and rollback flow rehearsed, including a matching
   database backup and `ZBOARD_MANAGED_RULE_HOST_DIR` snapshot.
4. Backend startup and readiness probes are passable
5. Frontend can consume core APIs for dashboard, nodes, plans, billing
6. Security review passed for auth, payment callback, and SSH operations
7. API contract in `backend/api/openapi.yaml` is frozen for release.
8. Release artifacts include version metadata in `/api/v1/version`
9. Use `docs/release/v0.1.0-launch-checklist.md` for end-to-end release runbook.
10. Confirm production startup rejects missing/weak JWT, database, and bootstrap administrator settings.

## Version lock

- Toolchain baseline: `backend/go.mod`, the Docker builder image, Node.js, and pnpm use reviewed pinned versions.
- Keep `VERSION`, `backend/internal/version/version.go` and
  `frontend/package.json` aligned with the release tag.
- When `v0.1.0`, keep `v0.1.x` backward-compatible as long as business requirements allow.

## Rollback requirements

- Keep last stable image available.
- Keep the database backup, managed-rule directory snapshot, previous
  source/image, encryption key and every released migration rollback script
  available.
- Treat the database and `ZBOARD_MANAGED_RULE_HOST_DIR` as one recovery unit.
  Database rows contain rule metadata and revisions, while canonical IR and
  compiled ZRS artifacts remain on the filesystem.
- During v0.0.1 development, preserve the old database's applied migration
  rows so the immediately previous development binary can still be restored.
  A clean database created from the squashed baseline must not be opened by a
  pre-squash binary.
- Rollback steps:
  - Switch traffic back to previous revision
  - Restore the matching database and managed-rule snapshot when data rollback is required
  - Confirm health and dashboard pages are still readable
  - Confirm orders/subscriptions queries still work
  - Confirm published managed-rule ZRS URLs remain readable

Kubernetes deployment and rollout automation are outside the supported v0.1.0 release path.

## Docker service dependencies

- The supported Compose bundles start only the ZBoard application. MySQL is an
  externally managed service and must already be reachable through
  `ZBOARD_EXTERNAL_NETWORK`.
- Supply the complete application-account DSN in `ZBOARD_DATA_SOURCE`.
  Production startup deliberately rejects the MySQL root account; use root
  only to provision a database-scoped application account.
- Compose does not create, restart, remove or back up the external MySQL
  container.
- Keep `ZBOARD_ZERO_ARTIFACT_HOST_DIR` read-only. It contains trusted Zero
  binaries and checksum files.
- Mount `ZBOARD_MANAGED_RULE_HOST_DIR` read-write at
  `/var/lib/zboard/artifacts/rules`. It contains application-generated IR and
  ZRS files, must persist across container replacement, and must be shared by
  blue/green instances.
- Run `sh deploy/docker/prepare-host-dirs.sh` before the first Compose start.
  Detailed backup, restore and mount verification steps are documented in
  [`deploy/docker/README.md`](deploy/docker/README.md).
The complete schema policy and verification queries are documented in
[`docs/database-migrations.md`](docs/database-migrations.md).

## Tag and release command

```bash
bash scripts/release-tag.sh 0.1.0
```

- On `develop`, pass only numeric SemVer such as `0.1.0`. The script creates
  `v0.1.0-dev`, or `v0.1.0-dev.1` when that local tag already exists.
- On `main`, numeric and prerelease SemVer such as `0.1.0`, `0.1.0-rc` and
  `0.1.0-rc.1` are allowed. Prerelease suffixes are intentionally rejected on
  `develop`.
- The script updates the internal version files, creates a release commit,
  creates an annotated tag and pushes the branch plus tag to every configured
  remote.
- After publishing an `-rc` release, GitHub Actions deletes all historical
  `-dev` GitHub Release records and GHCR package versions. After publishing a
  stable release with no suffix, it deletes all historical `-rc` Release
  records and GHCR package versions. Git tags remain available as immutable
  source-history markers; other prerelease suffixes do not trigger cleanup.
- GHCR cleanup uses the workflow `GITHUB_TOKEN`. The repository must retain
  administrator access to the `ghcr.io/zerodenet/zboard` package; GitHub grants
  this automatically when the repository publishes or is explicitly connected
  to the package.

- GitHub Actions will build:
  - backend binary with `internal/version` ldflags
  - docker image with the same version metadata
  - add OCI source metadata linking the image to `zerodenet/zboard`
  - upload the binary, Docker image archive and checksums as an Actions artifact
  - publish image to `ghcr.io/zerodenet/zboard`
  - attach a compressed Docker image archive to the GitHub Release
  - publish a GitHub Release with the binary archive, checksum file and
    commit-based notes covering commits since the previous tag

GHCR package visibility is managed in the `zerodenet` organization package
settings. Publishing metadata can link the package to this repository, but an
organization owner must make the package public if anonymous `docker pull`
access is required.

## Release notes artifact

- GitHub Actions generates release notes from non-merge commits since the
  previous tag. Do not maintain a second tracked changelog by hand.
- Before publishing, attach `docs/release/v0.1.0-launch-checklist.md` results as the public release check evidence.
- Include:
  - smoke test outputs
  - Docker backup and rollback rehearsal result
  - any known limitations and mitigation
