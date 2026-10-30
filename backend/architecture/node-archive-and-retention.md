# Node deletion, disable and history retention

## Product contract

The user has two operations: **disable** and **delete**. Disable retains configuration, supplier resources and management relationships and permits re-enabling. Delete closes management permanently, removes current delivery relationships and deletes system-managed provider DNS. Neither the UI nor a successful deletion presents internal archival as a third user operation.

Deletion uses a durable plan. Provider DNS cleanup is independent of SSH. Each tracked DNS record must be deleted or confirmed absent before node deletion completes; denied provider access, unknown ownership and shared dependencies stay explicit blockers with retryable evidence. Ownership follows the recorded zone/record identity and the panel marker; an owned record's changed IP/TTL is not a reason to keep it. Shared references must first be transferred or removed.

Offline/missing/unverified SSH does not block provider cleanup. Host cleanup is attempted only for a currently observed node with verified SSH credentials and an exclusive host. Host command failures and unavailable access are reported as unconfirmed, never as proof that native processes or files closed. Custom certificate paths and shared hosts are not removed. The remote event directory remains protected from deletion.

Successful deletion removes local DNS management rows, stops certificate management and detaches its provider account while retaining certificate execution history. It also removes topology/group membership, disables endpoint delivery, revokes node authentication, clears node/SSH/pool secrets, suspends publication and stops automatic renewal/synchronization. A provider error prevents this final transaction and preserves progress for retry. Completed mandatory steps are not replayed.

## Internal identity boundary

Stable node and endpoint rows remain solely to preserve historical foreign keys and billing attribution. Internally this terminal state is stored as `archived` plus `archived_at`; the UI displays **deleted** and a deletion timestamp. It is not reactivation-capable. Current writes, publication and accounting reject `deleting` and `archived` under parent locks. Buffered accounting uses a preliminary snapshot to omit already closed sources, then takes the definitive current parent locks after subscription locks. Closure racing that snapshot rolls back settlement for spool replay; waiting on a subscription never retains an early node lock that suppresses heartbeats. The deletion transaction never scans or rewrites TrafficRecord/FlowUsage. Late event sources remain explicitly held, while healthy nodes continue settlement.

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

Prove ledger/cursor rows and historical foreign keys byte-for-byte unchanged by deletion; compare SQL statement counts at small and large history sizes. Verify offline provider deletion, denied-provider retry, explicit unconfirmed SSH results, preserved certificate execution history, reversible disable, legacy-mode compatibility, permission/blocker/lease checks, atomic failure rollback, terminal mutation/accounting rejection, deleted-node history access, bounded/resumable GC and delayed replay refusal. Rehearse SQLite and real MySQL migration/copy and query plans. Build and test the coherent change before a single deployment candidate; production remains on its current verified image until release validation.

## API compatibility

`DELETE /api/v1/nodes/:id` and `POST /api/v1/admin/nodes/:id/deletion` submit the same `mode=delete` command and return 202 plus durable progress. `GET .../deletion` remains readable after removal. The UI offers one delete action and separate disable controls.

The old `/retirement` URL, persisted job handler/table identities, and explicit `retire`/`detach` modes remain compatibility contracts for historical requests. They are not shown as new user choices and are never silently reinterpreted into destructive provider deletion. An explicit delete can upgrade a failed or completed legacy detach with a new run/attempt. A canonical delete cannot downgrade to detach to bypass required DNS cleanup. Legacy lifecycle value `retired` is displayed as disabled; the new batch action is `disable`.
