# Releasing ZBoard

ZBoard is a basic panel with optional plugins. Keep product introductions and
operator instructions in the README and [documentation site](https://docs.zerodenet.org/projects/zboard/).
Use release notes for the changes and requirements of a particular build.

## Version and branch policy

- `develop` produces development builds: pass a numeric version such as `0.0.2`
  to create `v0.0.2-dev.YYYYMMDDHHmm`.
- `main` produces release candidates and stable releases. Pass `0.0.2-rc` to
  create `v0.0.2-rc.YYYYMMDDHHmm`, or `0.0.2` to create stable `v0.0.2`.
- Timestamps use UTC. An existing tag is an error; do not replace published tags.
- Promote the reviewed, tested source to `main` before creating an RC. Keep
  unrelated uncommitted work out of the release, using a separate worktree if needed.
- `VERSION`, `backend/internal/version/version.go`, and `frontend/package.json`
  must agree with the release tag. The release script updates them together.
- A prerelease must not update the stable Docker `latest` tag.

`v0.0.1` is already public. Its schemas and migrations are published history:
do not squash or rewrite them. Add ordered migrations for subsequent changes,
and validate both fresh installation and upgrades from an existing database.

## Verification before publishing

Use the pinned Go toolchain in `backend/go.mod`, Node.js and pnpm versions from
CI, and the Docker build stages. Follow `.github/workflows/ci.yml` for the exact
commands and documented test exclusions.

1. Run backend tests, `go vet`, formatting and module-tidiness checks, and the
   pure-Go SQLite tests.
2. Run frontend tests, type checking and the production build.
3. Run the ZRS compiler checks, Compose checks and OpenAPI validation in CI.
4. Run `govulncheck` and dependency auditing; distinguish runtime vulnerabilities
   from development-only dependencies and record unresolved findings.
5. Check database migrations and rehearse startup, upgrade and rollback using
   an isolated database. Preserve published migration files.
6. Verify readiness, administrator access, subscriptions, orders and plugin
   loading for the candidate. Check that existing configuration remains intact.

Record results for the exact source revision in CI or ignored
`.codex-local-artifacts/`, including limitations. An RC is not proof of
long-running production stability.

## Create the release

Preview the result first:

```sh
bash scripts/release-tag.sh --dry-run 0.0.2-rc
```

From a clean `main` worktree, create and publish the candidate:

```sh
bash scripts/release-tag.sh 0.0.2-rc
```

The script creates a version commit and annotated tag, then pushes the branch
and tag to every configured remote. GitHub Actions builds the Linux amd64 binary
and Docker image, publishes checksums and an offline image archive, and marks
RC/dev releases as prereleases. Wait for the release workflow to finish.

The release workflow cleans superseded prerelease downloads after publication:
RC releases remove old dev Release records and GHCR package versions; stable
releases remove old RC records and package versions. Source tags remain. Preserve
the deployed image locally before publishing an RC if it currently uses dev.

## Release notes

Write notes for operators: what changed, who benefits, deployment requirements,
and known limitations. Keep development journals out of release descriptions.

The workflow uses `.github/release-notes/<tag>.md` when present. Timestamped RCs
may share `.github/release-notes/vX.Y.Z-rc.md`; otherwise, commit-based notes are
generated. These files belong to release packaging, not the public guide tree.

## Deploy and verify

1. Record the running image ID/digest, version, Compose configuration and mounts.
2. Preserve the previous image and back up the database, configuration, encryption
   keys, plugin directory, managed rules and required event storage.
3. Pull the exact published tag and verify its digest and OCI revision. Do not
   rebuild a different local image under the release tag.
4. Update only the application image, preserving server-local Compose settings,
   credentials, networks and persistent mounts. Do not restart the external database.
5. Recreate the application container and verify health, `/readyz`, the version
   endpoint and the real public domain. Compare the served frontend with the image.
6. Verify administrator reads and existing plugin/login provider configuration.

Compose bundles manage the ZBoard application. MySQL is external; deployment
must not recreate or remove its container. Use a database-scoped account, not
MySQL root, for the application.

## Rollback

Keep the previous image, deployment configuration and matching backups until
candidate verification is complete. Restore the previous image if application
checks fail. A database rollback requires a matching database, rules, plugin data
and configuration snapshot; account for any new writes before restoring it.

After rollback, verify readiness, administrator access, subscriptions, orders and
published rule files. Storage and database-switching procedures are documented in
[storage and backups](https://docs.zerodenet.org/projects/zboard/guides/storage-and-backups)
and [system maintenance](https://docs.zerodenet.org/projects/zboard/guides/maintenance).
