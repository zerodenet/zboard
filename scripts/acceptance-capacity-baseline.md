# Fixed SQLite short baseline

`acceptance-capacity-baseline.py` observes exactly one execution of the unchanged
mixed-load runner at an explicitly fixed revision. The default is
`7ca9f63d0cf12a03713f3a242c508dcbf82e4856`; the first comparison revision
is `53a056d32633dcbda9b243553d85a88fe9fc2090`; subsequent exact pins are listed
in the controller and recorded below. It refuses a different or dirty
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
has 100 ms buckets and ends after the event accounting/network projection
transaction and durable-spool checkpoint. It does not include the independently
drained usage-statistics queue or prove chart freshness. Database pool waits
are not SQL execution latency.

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


The current narrow-metadata/explicit-record-summary comparison source is
`31e72db8c6fbbbedddbd006cd5887c39c3d6cd44`. It is an explicit additional pin;
the default and all original mixed/load/report thresholds and sources remain.
A new fixed-source run is required; earlier58f/e5 passes and112/157 historical
503 failures are preserved. Selection alone is not a passing load or final CI.


Fixed31e72db has now passed the original profile and all nine bindings. The
[receipt](../backend/architecture/validation/2026-10-20-records-original-capacity.json)
keeps actual metrics and the one supplemental RSS probe error; prior failures
and earlier passing runs remain separate. MySQL15m acceptance is still failed
locally and in its CI, so this capacity result does not close P7.

The heartbeat/source-material comparison pin is
`01ebcde72befca5f491e96c9b4c8b03fe125118a`. Its original60s profile and nine
bindings pass; [receipt](../backend/architecture/validation/2026-10-20-heartbeat-original-capacity.json).
Trends156/0/p95=10ms and final exact accounting do not prove complete charts
throughout load: pending/complete response proportions remain unknown. The
first drain verification's21pending facts and original5s retry, and one failed
supplemental RSS probe, remain recorded. The explicit e902 pin was preparation
only; no local e902 capacity run was performed. Defaults, three original runners
and every original gate remain unchanged.

The next reviewed exact comparison pin is
`0f491e151079ca88b224d86baaeeae6a9211b120` (known-partial principal copy
recovery on top of invalidate-before-DDL). The default pin, original three
runners, resource budget, workload and gates are unchanged. Execution/result
are separate from adding the pin; no load result is inferred here.

The asset admission foundation source is pinned separately at
`68176675bd3b4f7c759ec266f563d350d44bfd3b`. This adds only the explicit allowed
source to the observer; the default pin, three original runners, load profile and
gates stay unchanged. Its targeted dual-database/race results and preceding0f
capacity do not replace a new exact-source run. The quiet window follows complete
backend checks; execution and all failures must be recorded separately.


The durable node observation handoff comparison pin is
`a82e5b60c74d58821884bb703eaa97c2532682f3`. It adds no runner or threshold
change. Its targeted dual-database/race/HTTP checks are separate from a new
source-specific capacity run. All old disk-preflight/hourly503/drain failures
remain evidence; this pin alone is not capacity or P6/P7 acceptance.
