# Docker deployment storage

## Required host directories

Zboard uses two different storage trust boundaries under `/var/lib/zboard/artifacts`:

- `ZBOARD_ZERO_ARTIFACT_HOST_DIR` contains trusted Zero binaries and checksum files. It is mounted read-only.
- `ZBOARD_MANAGED_RULE_HOST_DIR` contains managed Zero Rule IR sources and compiled ZRS artifacts. It is mounted read-write and must persist across container replacement.

Prepare both directories before the first deployment:

```bash
cd deploy/docker
sh ./prepare-host-dirs.sh
```

Custom locations can be supplied through the same environment variables used by Compose:

```bash
ZBOARD_ZERO_ARTIFACT_HOST_DIR=/srv/zboard/artifacts \
ZBOARD_MANAGED_RULE_HOST_DIR=/srv/zboard/managed-rules \
sh ./prepare-host-dirs.sh
```

MySQL deployments use only the base Compose file and do not create or mount a
SQLite data directory. For SQLite, set `ZBOARD_DATABASE_DRIVER=sqlite`, set
`ZBOARD_DATA_SOURCE=/var/lib/zboard/data/zboard.db`, optionally set
`ZBOARD_DATABASE_HOST_DIR`, and include the SQLite override:

```bash
set -a
. ./.env.release
set +a
sh ./prepare-host-dirs.sh
docker compose \
  -f docker-compose.release.yml \
  -f docker-compose.sqlite.yml \
  --env-file .env.release \
  up -d
```

The preparation script creates an empty `rules/` mount point under the read-only artifact directory. Compose then overlays that path with the separate writable managed-rule directory.

## Mount layout

```text
/var/lib/zboard/artifacts                 read-only trusted artifacts
└── rules                                writable child bind mount
    └── <tag>
        ├── source.json                  canonical internal Zero Rule IR
        └── artifacts/<source-sha256>/   compiled client artifacts
```

Do not make the complete artifact directory writable. Managed rules are the only application-generated data below this path.

Blue and green application instances must mount the same `ZBOARD_MANAGED_RULE_HOST_DIR`. Using separate directories causes database metadata and rule files to diverge after a traffic switch.

## Backup and restore

The database stores managed-rule metadata and revisions, while the canonical source and compiled ZRS files live in `ZBOARD_MANAGED_RULE_HOST_DIR`. A recoverable backup therefore requires both:

1. a consistent Zboard database backup;
2. an archive or snapshot of `ZBOARD_MANAGED_RULE_HOST_DIR` from the same backup window.

Example filesystem backup:

```bash
managed_rule_dir=${ZBOARD_MANAGED_RULE_HOST_DIR:-./managed-rules}
tar -C "$(dirname "$managed_rule_dir")" \
  -czf "zboard-managed-rules-$(date -u +%Y%m%dT%H%M%SZ).tar.gz" \
  "$(basename "$managed_rule_dir")"
```

Restore the database and managed-rule directory as one recovery unit before starting Zboard. Restoring only the database leaves rule records whose source and ZRS artifacts are missing; restoring only the directory can reintroduce files for revisions that the database no longer references.

## Deployment verification

Render the Compose configuration before applying it:

```bash
docker compose -f docker-compose.release.yml --env-file .env.release config
```

For SQLite, render both files together:

```bash
docker compose \
  -f docker-compose.release.yml \
  -f docker-compose.sqlite.yml \
  --env-file .env.release \
  config
```

After startup, verify that the parent directory is protected and the child directory is writable:

```bash
docker compose -f docker-compose.release.yml --env-file .env.release exec zboard sh -c '
  test ! -w /var/lib/zboard/artifacts || exit 1
  touch /var/lib/zboard/artifacts/rules/.write-test
  rm /var/lib/zboard/artifacts/rules/.write-test
'
```
