# Fixed SQLite short baseline

`acceptance-capacity-baseline.py` observes exactly one execution of the unchanged
mixed-load runner at an explicitly fixed revision. The default is
`7ca9f63d0cf12a03713f3a242c508dcbf82e4856`; the only allowed comparison revision
is `53a056d32633dcbda9b243553d85a88fe9fc2090`. It refuses a different or dirty
checkout. It does not reset a checkout, retry a load, change
acceptance thresholds, start extra workers, or remove evidence. The original
runner owns its isolated containers, volume, and normal cleanup.

Default execution is a read-only preflight:

```sh
python3 scripts/acceptance-capacity-baseline.py \
  --checkout /workspace/zboard-capacity-7ca9f63 \
  --go-bin /workspace/toolchains/go/bin/go
```

For read-only comparison preparation, supply `--revision` with the complete
allowed comparison SHA and its existing clean checkout. Selecting that revision
does not authorize a load: omit `--execute` until a separate quiet window is
reserved. The selected revision is recorded in preflight and final bindings.
Any future repaired source revision requires a separately reviewed exact pin;
branch names and arbitrary revisions are refused.

After the shared environment has an explicitly reserved quiet window, append
`--execute` to run one profile: 2 nodes, 20 independent subscription subjects,
2,000 historical traffic records, 60 seconds, 20 offered events/second, and 4
readers. The original runner applies 1 CPU, 1 GiB memory, and no extra swap to
the SQLite server container. Client load and compilation run on the host. Use
the environment's writable `GOCACHE` and `GOPATH`; compilation uses
`GOMAXPROCS=1 GOFLAGS=-p=1`. If the default Docker CLI configuration directory is
read-only, explicitly set `DOCKER_CONFIG` to the environment's existing private
writable directory before starting; do not read or print its authentication
contents. A failed build is preserved as a failed attempt, not retried by the
observer.

Private artifacts remain under the fixed checkout's ignored
`.codex-local-artifacts/acceptance/`. This includes fixture credentials, raw
logs, original results, binaries, source manifests, and the observer's
`baseline-binding.json`. Share only a sanitized report. The observer binds
source files before build and after the run, the original runner copies, and
the built binary hashes recorded by the runner. Any binding mismatch or original
gate failure fails the outer result. An interruption with an unconfirmed runner
exit is recorded as uncertain; inspect the recorded owned resources before
retrying, rather than starting another load automatically.

Supplemental Docker process RSS is sampled only for the new constrained server
container with the expected image and `/zboard` entrypoint. It is a sampled
process measurement, distinct from the original Docker working-set memory gate.
SQLite table/index page allocation is read from the stopped exported database
using a read-only connection. Either supplemental observation can be unavailable;
unknown is never converted to zero and neither replaces an original gate.

The original gates cover HTTP error/count/p95, constrained container resources,
working set, disk headroom and spool pressure, exact usage/ledger reconciliation,
projection and durable-spool drain, and runtime observations. Preserve their
actual values and failures in the report. The runtime consumer latency histogram
has 100 ms buckets and ends after projection/checkpoint commit; database pool
waits are not SQL execution latency.

This is a short single-process SQLite comparison point. It does not establish a
capacity ceiling, MySQL behavior, long-term growth, SQL statements or logical
writes per batch, SQL execution p95, financial-write concurrency, quota-exhaustion
stop latency/overuse, plugin contention, or worker/scaling safety. Do not label it
P6 complete or report improvement without a comparable second measurement.

Observer and unchanged report-gate checks require no load:

```sh
python3 -m unittest discover -s scripts -p 'test_acceptance_capacity_baseline.py'
python3 -m unittest discover -s scripts -p 'test_acceptance_report.py'
```

The corrected 0054 comparison source is `4dfb6642c87d1900882afdc90d4467369e98fce1`. It retains the 53a056d runtime and only materializes two directly inserted historical test fixtures, with CI failure/fix evidence. The observer accepts this exact revision explicitly; its inclusion is preparation, not a passing capacity or complete-CI result. Keep the old 7ca baseline selected by default.

The repaired hourly/pending read contract is pinned separately at
`308689e38cff794865b633cde7193656ca2ecd4c`. Its default remains the old baseline;
this pin does not change any original mixed/load/report runner or gate. Pending
HTTP200 explicitly means unknown statistics. The original client does not count
pending/complete responses, so those proportions are unknown for this comparison;
final drained verification is recorded separately and does not establish graph
availability throughout load. Selecting this source is preparation, not proof of
a passing run. No extra observer HTTP requests are added to the original load.
