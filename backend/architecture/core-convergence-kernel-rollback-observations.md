# Preserve received observations across an installation rollback

An installation may receive node events between credential activation and a
failed verification. RestoreKernelConnectorCredential previously restored nine
runtime fields from the earlier snapshot while leaving zero_event_node_cursors
at the later event. In that state an equal/older event is correctly skipped, but
the displayed counters have already been rewound. Heartbeat timestamps, status,
version and uptime could likewise regress to the snapshot.

The rollback now restores exactly the existing three credential identity fields
inside the existing fenced transaction. Their values, encryption, authorization,
activation and rollback rules are unchanged. Independently received observations
and their cursor remain intact. This matches the existing configuration rollback
boundary; the historical unused restoreConnectorSnapshot helper is not a new
runtime path. No external kernel, event queue ownership or publication result
verification is changed.

A received observation describes its source and observation time. Preserving it
does not prove which process remains after rollback, that a target configuration
was applied, or that connections were terminated. A later actual observation is
needed to update the last known runtime. Kernel operation/delivery state retains
its separate verification/failure behavior.

## Evidence

The regression uses production migration/reconciliation and real persistence.
It captures an operation snapshot, activates the existing test credential, then
receives a real ProjectObservation stats event and NodeActivity heartbeat before
rollback. Full persisted node maps include private JSON-hidden columns. Only the
three credential columns and updated_at may differ after a successful restore;
all cursor columns remain identical. Equal and older event replays preserve the
new observations. Injected persistence failure changes neither node nor cursor,
and an operation that loses its fence cannot restore anything.

The old production path fails the new regression at the observation-preservation
assertion. The first real MySQL attempt also exposed an existing SQLite-only
fixture's empty JSON nodes.config; the shared fixture now supplies valid `{}`,
without loosening MySQL or migration checks. Both failures remain recorded.
SQLite kernel/configuration checks, three race repetitions and the isolated real
MySQL regression pass. See the [receipt](validation/2026-10-20-kernel-rollback-observations.json)
for exact source hashes and commands. Complete backend checks and the new
commit's CI remain separate acceptance requirements.

## Compatibility and remaining gaps

There is no schema migration, new dependency, data rewrite or destructive
rollback. Existing databases need no repair step. Returning to an older binary
would reintroduce this race. Already overwritten observations are not fabricated
or restored from unrelated evidence; subsequent real reports remain authoritative.

This is one prerequisite for a later durable observation handoff. Accounting
and node observation writes still share a transaction, and Latest/Stats still
share a cursor. Legacy activity writers, install-host ownership, per-node fault
isolation, persistent byte bounds and migration/recovery proofs must be resolved
before asynchronous projection can be claimed. P4/P6 are not completed here.

The exact clean cabce7c ordinary clone subsequently passed complete Go299.750s,
vet3.723s, build2.009s and pure-Go SQLite21.544s with GOMAXPROCS2/-p2 and no
concurrent local load. The [whole-source receipt](validation/2026-10-20-kernel-rollback-final-checks.json)
binds every raw log. This is not the original full MySQL or new CI result;
prior handler900s failures and the outstanding P4/P6 gaps remain.

The exact cabce7c [CI run](https://github.com/zerodenet/zboard/actions/runs/39508446918)
subsequently passed all six jobs, including original full MySQL datastore152.619s,
handler871.468s, network97.870s, jobstore119.129s and platform-copy104.375s. The
[CI receipt](validation/2026-10-20-kernel-rollback-ci.json) binds the raw log. Only
28.532s remain below the unchanged handler900s limit in this run; earlier timeouts
remain and this does not establish stable headroom or validate later source.
