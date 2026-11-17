# Docker storage and backups

For a first installation, start with the [installation guide (Chinese)](https://docs.zerodenet.org/projects/zboard/guides/installation). This page covers persistent directories, database storage options, and backup/restore.

## Required host directories

`ZBOARD_FILE_HOST_DIR` (default `./files`, relative to the Compose directory) stores uploaded site logos, icons, and ticket attachments. Every application container must mount this directory read-write at `/var/lib/zboard/files`, matching `ZBOARD_FILE_STORAGE_DIR`. `prepare-host-dirs.sh` creates it with mode `0700`. Keep this host path stable when changing release directories, Compose projects, or images; an absolute path such as `/srv/zboard/files` makes this explicit. Blue and green instances must share the same upload directory.

Zboard uses two different storage trust boundaries under `/var/lib/zboard/artifacts`:

- `ZBOARD_ZERO_ARTIFACT_HOST_DIR` contains trusted Zero binaries and checksum files. It is mounted read-only.
- `ZBOARD_MANAGED_RULE_HOST_DIR` contains managed Zero Rule IR sources and compiled ZRS artifacts. It is mounted read-write and must persist across container replacement.

Prepare the persistent directories before the first deployment:

```bash
cd deploy/docker
sh ./prepare-host-dirs.sh
```

Custom locations can be supplied through the same environment variables used by Compose:

```bash
ZBOARD_FILE_HOST_DIR=/srv/zboard/files \
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

## Upgrading deployments that lack upload persistence

Changing only the image tag keeps the old Compose mount layout. Add the upload mount to the existing application service before its next replacement:

```yaml
services:
  zboard:
    environment:
      ZBOARD_FILE_STORAGE_DIR: /var/lib/zboard/files
    volumes:
      - type: bind
        source: /srv/zboard/files
        target: /var/lib/zboard/files
        read_only: false
```

Retain all existing environment settings and other mounts. Before replacing an unmounted container, pause new uploads and copy its `/var/lib/zboard/files` contents into the chosen host directory with `docker cp`. If the old container was already deleted, its writable-layer files cannot be recovered from the application image or database; use a file backup or upload the originals again. Do not replace the container until the copy is complete. Applying a new mount to an empty host directory hides any files left in the old container.

After applying the updated Compose configuration, check the running container rather than only the YAML:

```bash
container_id=$(docker compose -f docker-compose.release.yml --env-file .env.release ps -q zboard)
docker inspect --format '{{range .Mounts}}{{if eq .Destination "/var/lib/zboard/files"}}{{.Type}} {{.Source}} -> {{.Destination}} writable={{.RW}}{{end}}{{end}}' "$container_id"
```

The output must identify the intended durable host directory and `writable=true`. Upload a small file, replace the container using the same configuration, then verify that its original URL still returns the same content.

## Backup and restore

The database stores managed-rule metadata and revisions, while the canonical source and compiled ZRS files live in `ZBOARD_MANAGED_RULE_HOST_DIR`. A recoverable backup therefore requires both:

1. a consistent Zboard database backup;
2. an archive or snapshot of `ZBOARD_MANAGED_RULE_HOST_DIR` from the same backup window.

Uploaded files are also split between database metadata and filesystem blobs. Back up `ZBOARD_FILE_HOST_DIR` in the same recovery window and restore it with the database, keeping UUID filenames unchanged. A database backup alone restores `/media/<id>` and ticket references but cannot restore their contents. Include the upload directory when backing up or moving the application host.

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

## Plugin persistence

The service mounts `ZBOARD_PLUGIN_HOST_DIR` (default `./plugins`) at `/var/lib/zboard/plugins`. `prepare-host-dirs.sh` creates this private directory. Back it up together with the database and credential encryption key. Add publisher public keys and the optional signed catalog URL to a custom ZBoard YAML file and mount it read-only at `/app/etc/zboard.yaml`; see [plugin setup](https://docs.zerodenet.org/projects/zboard/plugins/development).

Only one instance per database owns plugin execution. Standby instances keep core APIs available but do not issue plugin page sessions or execute plugin operations. Stop the owner and restart the replacement to transfer plugin execution; an ungraceful exit requires the one-minute lease to expire. This does not implement active-active plugin routing.
