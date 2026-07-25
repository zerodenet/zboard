# Database migrations

Zboard embeds its SQL schema in the backend binary and records applied files in
`schema_migrations`. Production startup does not use GORM `AutoMigrate`.

## Current v0.0.1 policy

`v0.0.1` is the development baseline before the first public release. The
repository therefore contains one migration pair:

- `backend/migrations/0001_init.up.sql`
- `backend/migrations/0001_init.down.sql`

The up migration directly creates the complete current business schema and
seeds the 13 system configuration entries and three built-in subscription
templates. It does not retain development-only `ALTER TABLE`, temporary
rewrite tables, data backfills, legacy executable-template archives or
environment-specific auto-increment counters.

Until `v0.1.0` is released, new schema work must update this baseline and its
clean-database verification. Do not add `0002` merely to preserve an
unreleased development step.

## Fresh database

Point the backend at an empty MySQL 8 database and start it normally, or run:

```bash
cd backend
../scripts/migrate.sh
```

```powershell
cd backend
../scripts/migrate.ps1
```

The runner creates `schema_migrations`, applies `0001_init.up.sql`, validates
the final table/column/index signature and records one applied version:
`0001_init.up.sql`.

Do not manually create application tables or insert a migration record.

## Existing pre-squash v0.0.1 database

An existing development database can be retained only when the previous build
already completed the former chain through
`0032_subscription_policy_group_targets.up.sql`.

On first startup with the squashed build, the runner:

1. confirms the original `0001_init.up.sql` record exists;
2. refuses a multi-entry history that never reached the terminal development
   migration;
3. validates all final business tables, selected critical column types and
   cursor/concurrency indexes;
4. removes the empty legacy subscription-template archive table and renames
   the old node-group index to its final resource name;
5. leaves the already-applied development rows in `schema_migrations` so the
   immediately previous development binary remains usable for rollback.

This path does not rerun the baseline and does not rewrite normal business
rows. An archive table containing rows blocks finalization so an operator can
export or deliberately remove that data first.

The retained rows are compatibility metadata only. Their SQL files are not
shipped in the squashed build and a fresh database records only
`0001_init.up.sql`. A database created from the squashed baseline cannot be
opened with an older pre-squash binary.

Before replacing a development build, back up:

- the MySQL database;
- the credential-encryption key;
- the currently deployed source or image needed for rollback.

If the database has a partial migration history, start the previous
development build and finish its migrations before using the squashed build.
If a non-empty schema has no migration history, recreate it or perform an
explicitly reviewed data migration; the runner will not guess its origin.

## Verification

For a fresh database, verify:

```sql
SELECT version FROM schema_migrations ORDER BY version;
SELECT COUNT(*) FROM information_schema.tables
 WHERE table_schema = DATABASE() AND table_name <> 'schema_migrations';
SELECT COUNT(*) FROM system_configs;
SELECT COUNT(*) FROM subscription_templates;
```

Expected baseline results are one migration record, 30 business tables,
13 system configuration rows and three built-in templates.

For a retained development database, also verify that application row counts
and the previous applied-history rows remain unchanged across the upgrade, and
that the obsolete
`subscription_template_legacy_archives` table and
`uk_access_groups_code` index are absent.

## Policy after v0.1.0

Once `v0.1.0` is released:

- `0001_init.up.sql` becomes immutable;
- every released schema change receives a new ordered up/down migration;
- previously published migration content and filenames are never rewritten;
- destructive changes require an explicit backup, compatibility and rollback
  plan;
- clean-install schema and sequential-upgrade schema must be verified as
  equivalent before release.
