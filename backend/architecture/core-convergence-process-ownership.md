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

File-spool recovery/checkpoint/compaction are currently single-process state;
the next isolated batch must enforce directory ownership before recovery and
wait for all local file operations before release. A future DB-only projection
worker must not open the event spool. It needs bounded stop-claim/drain and
crash/reclaim proofs in both databases before process scaling. Plugin admission,
resource reservations, CPU/RSS/DB-pressure sampling, cooldown and a measured
benefit remain required. The short20events/s profile demonstrates no need for
additional processes and must not be presented as an autoscaling result.
