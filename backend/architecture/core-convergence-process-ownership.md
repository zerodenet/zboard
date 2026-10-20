# P6 process ownership prerequisites

The existing runtime already shares database admission, execution budgets,
leases, completion fences and durable schedules. This alone is not evidence
that starting or stopping additional server processes is safe or beneficial.
The default remains one complete server; no independent-worker CLI or autoscaler
is introduced by this prerequisite batch.

## Local server shutdown versus global handler revocation

Normal `CloseBackgroundJobs` previously called `RemoveJob` for each local
registration. That operation deliberately calls `CancelHandler`, which cancels
all durable queued work and requests cancellation of active leases for that
handler, including another process's work. It belongs to explicit domain/plugin
deactivation, not shutdown of an execution process.

Shutdown now closes only the local Services/Runtime, waits for its executions
using existing cancellation/completion rules, and updates local observations.
Explicit `closeScheduledJob`/RemoveJob retain global revocation semantics.
This change does not claim graceful completion of every local task: existing
uncertain-result/reconciliation rules still apply to interrupted local work.
No new transaction, financial writer, schema, timeout or lease duration changes.
Rollback is a source revert but restores the old multi-process shutdown risk.

A deterministic regression with the old implementation produces a foreign
lease `cancel_requested` and queued intent `canceled`. The correction preserves
the foreign lease and token, accepts its successful completion, and allows a
surviving process to claim the queued intent. SQLite correction/explicit-stop/
publish-reopen tests pass three times in2.267s; the actual isolated MySQL8.4
counterpart passes in4.114s (21.199s including compilation). The original global
revocation test remains unchanged. See
[validation](validation/2026-10-20-local-background-shutdown.json).

## Remaining prerequisites

The following spool batch now enforces directory ownership; it does not make
recovery/checkpoint/compaction a shared multi-process queue. A future DB-only projection
worker must not open the event spool. It needs bounded stop-claim/drain and
crash/reclaim proofs in both databases before process scaling. Plugin admission,
resource reservations, CPU/RSS/DB-pressure sampling, cooldown and a measured
benefit remain required. The short20events/s profile demonstrates no need for
additional processes and must not be presented as an autoscaling result.

## File-spool directory ownership

Start now acquires an exclusive nonblocking kernel file lock before emergency
reserve, checkpoint or segment recovery. Same-process objects and cooperating
processes sharing the directory fail with `ErrSpoolInUse`; aliases resolve to
the same lock inode. Failed Start releases its descriptor. The permanent
`.owner.lock` file is never unlinked, so a second inode cannot be created by
normal teardown while an old owner still uses the queue.

Close waits for writer, compression, storage and in-flight ReadBatch/Commit/
Compact operations before unlocking. Concurrent Close calls wait for the same
completion and return the same result. Parent context cancellation alone keeps
ownership: the existing object may still commit its durable read horizon until
explicit Close. Process death releases the kernel lock without stale-PID repair.
No buffered event, checkpoint, dedup or financial record is deleted or changed
by ownership acquisition.

Linux local-filesystem tests cover duplicate objects, aliases, startup failure,
checkpoint continuation, two real competing subprocesses, SIGKILL release,
same-inode takeover and concurrent Close blocked by actual reader/commit/
compaction operations. Complete zeroevent tests pass1.466s and race14.206s.
Darwin/BSD flock code is present but unexecuted; other platforms, including
Windows, explicitly refuse file-spool startup before recovery. There is no
claim for network filesystems, legacy binaries that ignore the lock or external
lock-file replacement. No new dependency, worker, runtime scaling or migration.
See [the receipt](validation/2026-10-20-spool-ownership.json).

Rollback retains the lock file, all segments and checkpoint. Stop the new owner
fully before an older binary starts; an old binary does not cooperate with this
fence. Copying an active directory or sharing it with an older process remains
unsafe. Current full backend, transport integration and new CI remain separate
checks; package race tests do not prove independent-worker correctness.

## Bounded runtime drain and remote cancellation

Runtime.Drain stops new local scheduling and claims, rejects registration, and
waits for admitted work without canceling it. Its context limits the wait;
expiry retains the local handler and durable outcome. Close remains the explicit
local interruption fallback. A Claim already inside persistence is in flight,
and existing uncertain-result and completion fences remain authoritative.

Lifecycle mutexes no longer cover waits for a Claim, schedule/global cancellation
or a maintenance database read. A canceled registration stays as a same-name
fence until explicit global cancellation completes; cached handler snapshots
must still match that registration object at claim admission. Storage errors
remain visible. The runtime's Store wrapper now forwards the optional durable
cancellation observer, so another process's cancel_requested actually reaches
the executor poll; canceled execution still becomes unknown rather than a false
safe-cancellation claim.

Five deterministic old-source latch cases fail (2.255s); corrected whole jobs
package passes0.580s and ten focused race repetitions pass6.800s. An additional
real SQLite two-Store cancellation regression fails against the old wrapper
(3.921s including compilation) and passes with the correction. Integrated focused
jobs/jobstore race tests pass three repetitions,2.760s/10.602s respectively
(20.181s wall). The earlier unrestricted package race run remains failed because
the existing dispatch p95 guard measured802.696ms over its unchanged500ms gate;
no data race was reported, and that guard has not been weakened or excluded.

Earlier actual MySQL drain repetitions passed before the lifecycle corrections;
they are not acceptance of this final source. New-source whole jobstore MySQL
and full backend remain required. The actual route/plugin shutdown path still
has explicit global-disable calls to replace; this API alone does not complete
process shutdown integration, an independent worker, or autoscaling. See
[receipt](validation/2026-10-20-runtime-drain.json).
