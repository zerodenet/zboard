# Known-partial credential-copy coverage recovery

A source missing a derived principal lookup column could copy version1 into a prepared target with empty digest defaults. The new copier uses its existing Rows.Columns result: only protocol_credentials with any of the three derived columns physically missing writes version0 in every original target INSERT. Complete three-column sources retain their exact versions and legal empty digests. There is no additional query, DDL, migration, marker, source write, or after-copy UPDATE; the existing INSERT explicitly carries the target version.

Baseline: `a9aae1621ce04e303de248747903f3637c1c61b4`. Candidate is one production file and two independent test files; tested hashes are in the [targeted receipt](validation/2026-10-21-principal-copy-recovery.json). The isolated validation left the main worktree untouched; root then backed up and integrated exactly these three files. Integrated-source full backend, original MySQL and new CI are pending.

The old control overlays only the exact original database_copy.go and keeps new tests for compilation. It is not the original parent binary. Its selected public missing_secret_digest leaves failed after successful real owned copies, with exactly `copied lookup coverage=1, want0`: one SQLite leaf and both cross-driver leaves. No compile/setup failure was accepted as the regression proof.

All15 normal owned-copy fixtures passed:5 SQLite→SQLite,5 MySQL→SQLite,5 SQLite→MySQL. These cover complete legalv1/empty digest, missing secret digest, missing version, missing both digests and owned completion failure/retry. There is no newly added MySQL→MySQL coverage. Actualcipher secret lookup depends on rebuilt secret_digest with an empty retired hash; canonical currentmodel fields and rawsource columns/values remain unchanged. The completion callback proves exactly2 zero-version rows visible inside the target transaction before injection, then persisted rollback and retry.

The internal SQLite contract verifies201rows use the original200+1batches, mixed-case physical version keys, v0/v1/v2 preservation, exact BLOB/text and source retention, and ordinary INSERT failure when the target version column is absent. Focused SQLite race count3 passed for its five-case201row contract plus public missing-secret and owned-completion rollback. The full15-fixture matrix was not repeated under race.

| Step | Result | Package time | Wall time |
| --- | --- | --- | --- |
| old-public-sqlite | EXPECTED_ASSERTION_FAIL | 0.442s | 5.760s |
| old-public-cross-driver | EXPECTED_ASSERTION_FAIL | 8.628s | 9.213s |
| fixed-internal-sqlite | PASS | 0.059s | 12.442s |
| fixed-public-sqlite | PASS | 2.236s | 4.441s |
| fixed-public-cross-driver | PASS | 42.578s | 43.189s |
| fixed-focused-race-internal | PASS | 3.196s | 19.973s |
| fixed-focused-race-public | PASS | 74.958s | 77.634s |

Go1.26.8, GOMAXPROCS=1, -p1, isolated MySQL8.4, and the original Go test timeout were used. All148 published migrationSQL assets and all3candidate source hashes remained unchanged. Logs and SHA256 bindings are retained; private logs are credential-redacted. No benchmark/fullsuite/capacity or newCI was run here. These targeted passes do not replace root's integrated-source full backend and original three MySQL commands.

The entire source pipeline is not source-write-free: existing Task.content normalization and task/run bookkeeping remain. This patch adds no source write. Complete but previously wrongv1 cannot be identified safely from legal empty digests and is not repaired. Model snapshots cover the current ProtocolCredential model; actual sourcecolumn/value snapshots preserve the additional source evidence checked by these tests.

Rollback requires no DDL or source restoration: revert the candidate source/tests; copied version0 remains compatible with existing lazy index reconstruction. Reverting the change restores the unsafe future partial-copy behavior. No change to canonical credentials, cipher/backfill rules, permissions, source quiescence, target ownership/transaction/receipt, or FK restoration is claimed.
