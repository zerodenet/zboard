# Node archive and retention contract

## Lifecycle

`active/maintenance/retired -> deleting -> archived` is irreversible once management retirement starts. `retired` remains a reversible service setting; `archived` is the terminal management boundary. Remote cleanup receipts belong to the existing durable retirement plan. Detach preserves external resources. Neither mode deletes historical identity rows or rewrites traffic references.

The archive transaction locks the actual retirement lease and node, rechecks unresolved work, disables current endpoints, removes live topology/group membership, clears current observations, revokes node authentication and erases SSH/control secrets. Node and endpoint IDs, immutable report receipts, traffic ledger and cumulative cursors remain unchanged. Its work depends on current configuration, never the number of historical flows or traffic records. Names remain reserved by archived identities; reusing a host creates a new identity and credentials.

All writes and publication/accounting admission reject deleting and archived nodes under the existing parent lock. Archive cannot be reverted through single or bulk updates. History remains readable by ID and in an explicit archived inventory filter. Default inventories exclude archives.

## Retention matrix

| Data | Eligibility | Default | Authority retained |
| --- | --- | --- | --- |
| Node/endpoint identity | archived | permanent | historical FK and asset attribution |
| Financial ledger, report receipts, cumulative cursors on live nodes | no time-only deletion | permanent | quota and replay protection |
| Flow cursors on archived nodes | immutable terminal fence plus archive grace | 7 days | ledger and report receipts; node permanently rejects accounting |
| Periodic successful runs | terminal, finished, not current schedule run | 7 days | explicit submission keys are excluded |
| Periodic unsuccessful runs | known terminal, finished, not current | 30 days | unknown/pending evidence is excluded |
| Operator tasks and operations | finished | 90 days | independent of periodic job traces |
| Audit logs | received | 180 days | independent policy |
| Diagnostic and native proof material | existing verified projection/retirement rules | existing 15/7 days | existing proof and immutable archive contracts |

Every retention invocation performs at most one bounded batch per policy and at most 1,000 child rows of each kind, with context cancellation between policies. Periodic history has separate success/failure policies. No drain-until-empty loop, global job-admission mutex or financial-row mutation belongs to retention. Indexed candidate discovery occurs outside the write transaction; point locks recheck eligibility before deletion. A full batch means work may remain; the next scheduled invocation resumes from committed state. NULL timestamps, uncertain runs, pending mail acceptance and current schedule pointers are retained. Each policy has an independent native job, read-only eligibility probe, durable lease and 20-second invocation deadline. Cursor/periodic policies run at most once per minute and operational policies at most once per five minutes. Empty policies suppress dispatch rather than appending successful empty runs; completed batches survive cancellation. A full-batch observation is a lower-bound backlog hint, never a claim that cleanup is complete.

TTL bounds the time window, not absolute financial storage. Live cursor reclamation requires an authenticated producer replay horizon plus sealed settlement and cannot be inferred from completed status or age. Fixed-byte financial retention and physical InnoDB space reclamation are explicitly outside this change. Do not promise bounded financial storage or run OPTIMIZE as part of retirement.

## Migration and rollout

Migration 74 installs seek indexes on operational history (including existing job history); schedule this index build separately from activation and measure its duration. It does not touch financial tables. Migration 73 adds only nullable `nodes.archived_at` and its seek index; no ledger scan or startup backfill. Recorded archive metadata loss must fail startup and database copy before automatic schema repair can erase evidence. Legacy sources without the migration copy with NULL archive timestamps. Copies preserve archive identity, tombstones, financial receipts and all original FK values.

Deploy with old writers stopped. After the first archive, older binaries lacking the archived lifecycle fence are unsafe; rollback must retain the new admission fence (or restore the entire pre-activation database and matching binary). Never clear archived status to roll back. The archive down migration is a deliberate non-destructive no-op; terminal metadata and identity fences survive. A failed archive transaction retains all identities and credentials; remote receipts already committed stay resumable.

## Acceptance

Prove ledger/cursor rows and historical foreign keys byte-for-byte unchanged by retirement; compare SQL statement counts at small and large history sizes. Verify no remote calls on detach, actual remote receipts on retire, permission/blocker/lease checks, atomic failure rollback, terminal mutation/accounting rejection, archived inventory/history access, bounded/resumable GC and delayed replay refusal. Rehearse SQLite and real MySQL migration/copy and query plans. Build and test the coherent change before a single deployment candidate; production remains on its current verified image until release validation.

## API compatibility

Legacy `DELETE /nodes/:id` now closes management without remote commands and returns `archived=true`, `history_preserved=true`, `deleted=false`. The deprecated per-node traffic count is no longer computed or emitted. Use the durable retirement endpoint for explicit remote cleanup. Node inventory omits archives unless requested by lifecycle or ID; historical reference APIs retain node names and IDs.
