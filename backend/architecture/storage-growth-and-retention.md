# Metering storage growth and retention

This policy supersedes the original shared fifteen-day raw/point/minute lifetime
in `core-convergence-observation-retention.md`. It does not expire historical
financial reports or replace accounting authority with graphs.

## Production findings, 2026-11-04

Read-only inspection of `sg-server`, schema `763106.xyz`, found:

| Table | Data and indexes, GiB | Cause |
| --- | ---: | --- |
| traffic_records | 11.50 | Historical per-report ledger plus newer minute settlements; indexes exceed row data |
| principal_flow_observations | 4.69 | Legacy diagnostics no longer written, but native retention did not own this table |
| flow_usages | 3.71 | Full indexed records retained for completed connections indefinitely |
| subscription_flow_start_events | 2.38 | Per-connection diagnostics retained fifteen days |
| job_runs | 1.54 | Existing operational history; native compact periodic history and configured TTL are separate policies |
| flow_start_statistics_points | 0.95 | A second per-connection representation with the same fifteen-day lifetime |

A second indexed sample, 04:06–04:56 UTC, found 8,890 actual newly inserted
flow rows, all completed, versus 2,521 new ledger rows holding 8,846
contributions. The legacy principal observation maximum ID did not move.

These are physical allocation estimates, not exact logical row counts. New
ledger writes already use `settlement.minute`: the latest 2,000 actual rows
represented 6,998 source contributions over about 43.5 minutes. The latest 1,000
actual cursor rows were all completed connections over about 347 seconds. The
problem is not just historic free pages: full connection records still grow
with each completed connection.

The external `/usr/local/sbin/zboard-db-retention.py` aborts its entire cycle
below 4 GiB free. Production had about 3.34 GiB free; the hourly cycle exited
before its observation cleanup. Removing files or lowering that threshold alone
would leave the ownership and representation problems unresolved.

## Ownership and lifetimes

| Data | Lifetime | Permanent authority? |
| --- | --- | --- |
| Financial report identity receipts | No age cutoff | Yes: original database identity equality |
| Cold cumulative accounting floors | No age cutoff on reportable nodes | Yes: compact node/flow key, credential, revision, completed bit and four counters |
| Full completed-flow records | 24 hours after completion, then compacted | Transfer in the same transaction; never discard the floor |
| Active-flow full records | While recently updated; after 24 idle hours transfer to a nonterminal checkpoint | Preserve late updates and legacy-flow continuation |
| Flow-start raw records and exact boundary points | 24 hours after receipt | No: diagnostic dedup only |
| Flow-start minute counts | Fifteen days by each bucket's own time basis | No: derived charts, independent expiry |
| Legacy principal observation rows | 24 hours after creation | No: current/generation floors and scope statistics live elsewhere |
| Principal minute metrics | Thirty days | No: existing minute-to-hour policy |
| Runtime host metrics | 1,440 minute slots | No: existing bounded persistent ring |
| Task, periodic execution and audit history | Existing configured independent TTLs | No change to unresolved tasks or live pointers |

Finite diagnostic retention does not imply constant total financial storage.
Unlimited historical admission with exact report-ID deduplication requires
retained identity information. Checkpoints remove activity indexes, duplicate
strings and event metadata from completed-flow authority; they still grow with
distinct completed flow identities. No fixed-byte accounting bound is claimed.

Recoverable internal core scanners and retention jobs reuse only clean successful/yielded
periodic execution rows. Domain results (tasks, orders, DNS/certificate operations
and audit) remain independent. Failed, retried and unknown attempts are not reused
and continue through configured history retention. This prevents a one-second
cleanup poller from generating a new permanent execution row for each batch.
External execution groups, non-recoverable scans, plugin-defined jobs and explicit
submitted jobs do not inherit this internal-only policy.

Minute financial settlements continue to sum already calculated charges. Pricing,
rounding, entitlement epochs, reset boundaries and historical ownership do not
change. Two additional ledger indexes (`flow_id`, `event_type`) have no core read
owner and are removed by the existing exact-name/shape index budget. Report and
nonce uniqueness and all supported scope/time access paths remain.

## Compact cold connection accounting

Migration 0083 creates a small independent authority table. It inherits the hot
table's actual identity collation, has one composite primary key and no secondary
indexes or source foreign keys. Recorded missing/changed authority stops startup
and copying rather than being recreated as empty.

The native worker examines at most 200 hot rows per run using a durable primary
key seek. Old completed rows and active rows without updates for 24 hours transfer atomically.
The checkpoint preserves whether the flow completed. Recently updated active rows
remain hot; a lost completion after a node restart cannot retain full history forever.
A completed pass sleeps one hour, then revisits skipped rows. There is no large
startup history scan or hot-table rebuild. A crash rolls back both the floor
transfer and its progress checkpoint.

Single and batch accounting look in both representations. Window intersection
checks also consult compact revisions. Duplicate authority fails closed using
database equality, including collation-equivalent identities. A valid later
completion or update restores a hot row only when it actually writes a new cumulative
floor. A completed floor stays terminal; an abandoned active floor still permits
normal continuation. Stale/duplicate reports do not recreate activity rows. Original receipts
still protect report identity independently of the connection cursor.

Only an already archived, disabled node with both reporting credentials revoked
can eventually retire these floors under the existing terminal-node policy.
Ordinary disabling, deleting a protocol or deleting an account is not such proof.
Copy/reset inventory includes both authority and operational progress tables.

## Separate diagnostic detail from minute history

The new dedicated job retires verified source/point pairs after 24 hours without
subtracting their minute counts. It independently expires minute buckets after
fifteen days, with an indexed time seek. Old or corrupt diagnostics do not block
minute expiry; unprojected/changed source evidence is retained and deferred.
Captured legacy projection must finish before minute expiry is allowed.

Migration 0084 adds a small deadline-conversion checkpoint and an online expiry
index on the minute table. The captured pre-upgrade point range is converted in
200-row primary-key batches, preserving explicit failure deferrals. New points
are written with the 24-hour deadline directly. There is no startup bulk UPDATE.
Each cycle can convert, retire detail, and expire minutes in separately bounded
phases; retirement starts with the first converted batch rather than waiting for
the whole historic table. The three native maintenance jobs are independently
leased and cannot starve each other through a shared application history loop.
The MySQL index preparation requests INPLACE/LOCK=NONE and bounded metadata lock
waiting; it must fail instead of falling back to a table-copy algorithm.
The syntax follows the [MySQL CREATE INDEX contract](https://dev.mysql.com/doc/refman/8.4/en/create-index.html);
documentation verification is not a substitute for the pending MySQL execution tests.

Historical charts use whole minutes at the old left boundary, and return that
rounded `since`. Current Fair Use windows (at most one hour) keep the original
exact inclusive point/minute union. Very old incoming events are still admitted:
their received-time observation is current. Node occurrence clocks more than five
minutes beyond receipt cannot create future minute keys; existing out-of-policy
future buckets also expire. Occurrence-time diagnostics
follow the same finite history policy. Their financial admission has no time
limit and remains independent of this diagnostic policy.

## Release and acceptance boundaries

Implementation-phase snapshot, before release authorization: these were local
source changes. No production cleanup,
migration, timer modification, image build, release or deployment was performed.
The production allocation above therefore has not been reclaimed by this work.
Logical retirement makes InnoDB pages reusable; it does not promise smaller
tablespace files or enough workspace for a physical rebuild.

Before rollout, run the reference MySQL migration/collation/concurrency tests on
an isolated database. Local SQLite tests cover twenty-year-old completion replay,
per-report fractional rounding, transfer rollback, bounded/resumable scans,
copying floors, legacy deadline conversion and forty days of diagnostic turnover.
Those tests are not evidence of MySQL physical capacity or production acceptance.

Deploy all reporting writers/workers quiescently. After any floor is compacted,
an older binary that only reads `flow_usages` is unsafe: do not use image rollback
as accounting recovery. Retain the new reader while repairing or rehydrate every
floor under a separately verified recovery procedure. The 0083 down migration
deliberately refuses destructive downgrade.

Hand off the legacy external timer only after the new native jobs are registered
and progressing. Do not run two owners indefinitely or disable production
maintenance before replacement readiness. Historic financial ledger conversion
and tablespace reclamation are separate work: no date-based ledger DELETE,
blind OPTIMIZE, or backup requirement is introduced by this policy.
