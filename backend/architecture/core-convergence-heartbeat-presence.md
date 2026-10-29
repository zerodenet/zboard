# Preserve missing legacy heartbeat observations

The legacy heartbeat decoder used scalar integers, so omitted/null uptime,
active-flow and byte counters became zero and overwrote known node values. Four
internal pointers now retain presence and pass directly to the existing
NodeActivity nil-skipping write path. Partial/null requests keep their existing
200 response and liveness update, provided values update only their own columns,
and explicit zero remains a real zero. Empty/missing build ID retains its existing
behavior. No credential, permission or billing rule changes.

OpenAPI still describes the canonical six required fields with unchanged numeric
types. A description explicitly explains the existing permissive intake and
missing/null preservation; partial requests are not claimed to satisfy that
canonical schema. Current Zero uses generic events; real older-core senders were
not tested or modified. Receipt/build/counters do not prove configuration,
credential delivery, installed runtime or authorization renewal.

## Evidence

The selected tests execute actual HTTP handlers, service and repository against
one fresh SQLite fixture and one isolated actual MySQL fixture per matrix. Full
persisted node maps include private columns. They cover per-field missing/null/
zero, mixed updates, liveness/version, invalid type/negative/fraction/overflow,
malformed/trailing/oversized bodies, bad authentication, actual credential SQL
rotation between authentication and conditional update, and injected write
failure. Rejected/failed requests leave the row unchanged; only the existing
activity columns and explicitly provided counters can change. Heartbeat creates
no event cursor. No artificial sleep or RowsAffected rule change was introduced.

The controlled old decoder coalesces nil pointers to zero while retaining the
new types/tests for compilation. It is a controlled semantic baseline rather
than the byte-identical parent binary. Exactly17 leaf preservation assertions
fail; fixed parser/SQLite66 and actual MySQL25 pass. Race-count3 passes198 leaves.
Actual execution is0.219s/4.368s/16.301s respectively; compile wall time is separate
(19.314s fixed warm and160.022s initial race after cache cleanup).

Two fixture failures remain: duplicate node names caused setup failures after
the first correct old-behavior reproduction; then a14-character test prefix
exceeded MySQL's existing12-character column before heartbeat requests ran.
Only unique short fixture names and the actual secret[:12] prefix convention
were corrected. Final source was rerun through old/fixed/MySQL/race; no schema,
assertion or timeout was relaxed. The original143.537s cold compile is retained.
See the [complete source-bound receipt](validation/2026-10-20-heartbeat-numeric-presence.json).

The existing pinned Redocly image validates the exact candidate API with282
warnings and no errors, network disabled/read-only mount. No image/tool install
was needed. Observer/report Python checks each pass6; they are not a load result.
Combined-source backend, original full MySQL15m, current CI and original capacity
remain separate acceptance requirements. Earlier e902 CI failure is preserved.

## Compatibility and rollback

There is no migration, persisted shape, new dependency, external protocol field,
node queue or cursor change. Existing valid full heartbeats and zero values keep
the same response. Missing/null numbers preserve last known observations instead
of inventing zero. Binary rollback requires no DDL but reintroduces the overwrite.
Missing samples still need separate freshness/state handling; no async projection,
Stats ordering, asset incarnation or P4/P6 completion is claimed.

## Exact01ebcde acceptance follow-up

The clean ordinary clone passed full Go294.950s and vet13.007s. A cloud restart
ended the outer runner before build/SQLite logs existed; the original process
was defunct. Its persisted receipt/logs were protected, and only the two pending
checks resumed: build2.381s and pure-Go SQLite37.698s pass. The
[backend receipt](validation/2026-10-20-heartbeat-backend-checks.json) records that
interruption and each actual command. No successful Go/vet command was repeated.

The exact [new CI](https://github.com/zerodenet/zboard/actions/runs/39515537970)
passes all six jobs, including original MySQL handler759.718s and actual
jobstore/copy commands; [receipt](validation/2026-10-20-heartbeat-ci.json).
Earlier e902 handler900s failure remains and no stable timing margin is claimed.

The original constrained profile also passes all nine bindings:1201unique,
1274event requests and942business reads, zero errors. Trends156/0/p95=10ms,
hourly157/0/p95=22ms; final expected/ledger/quota9,833,472B and both queues0.
The first drain check saw21 pending facts and three subscription mismatches;
its original5s retry then reconciled. SampledCPU18.98%, working set35.88MiB;
22 valid RSS probes peak62,116KiB plus one failed probe retained. Stopped SQLite
allocation6,094,848B, indexes3,989,504B. Runtime p95 upper2s/max2069ms covers the
core transaction/checkpoint only. Pending/complete chart proportions remain
unknown. Full scope/raw hashes and limits are in the
[capacity receipt](validation/2026-10-20-heartbeat-original-capacity.json).
This is one60s SQLite simulation, with no MySQL-capacity/plugin-busy/scaling proof.
Local original mysql-core fails at handler900.019s; datastore156.828s and
networkstore103.014s pass. The active receipt test had run only1s and was still
inside fixture versioned-migration Exec, so it is not assigned causality.
Independent jobstore131.560s/copy111.709s then pass;
[local MySQL receipt](validation/2026-10-21-heartbeat-mysql-local.json).
This local failure remains separate from the exact successful CI and does not
permit a timeout/exclusion change.
