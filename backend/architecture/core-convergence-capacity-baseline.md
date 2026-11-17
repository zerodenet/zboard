# P6 fixed-source SQLite baseline evidence

## Attempt 2: short baseline passed

The second explicitly reserved window completed at 2026-10-20
04:33:09–04:34:31 UTC. The only environment correction was
`DOCKER_CONFIG=/workspace/toolchains/docker-config`, an existing writable cloud
directory. The first failed attempt remains preserved below. The source, binary,
runner hashes, parameters, and original thresholds were unchanged. Both the
original report and all nine outer hash bindings passed; no automatic retry or
extra worker was used.

The exact source scope is `7ca9f63d0cf12a03713f3a242c508dcbf82e4856`, including
0053 and excluding the pending 0054/P3/migration-copy changes. This is a
single-process SQLite baseline, not evidence for those later changes or P6
completion. The cloud host ran Docker 28.4.0; the server container was verified
at 1 CPU, 1 GiB, no extra swap, with `GOMAXPROCS=1`. The generator and readers
ran outside the constrained server. Core/payment services and real nodes were
not contacted; fixture events used the existing legacy Zero event HTTP contract.

Reproduction requires a clean standard checkout at that exact commit and a
reserved quiet window. From the main worktree, run once:

```sh
DOCKER_CONFIG=/workspace/toolchains/docker-config \
GOCACHE=/workspace/toolchains/cache/go-build \
GOPATH=/workspace/toolchains/gopath \
python3 scripts/acceptance-capacity-baseline.py \
  --checkout /workspace/zboard-capacity-7ca9f63 \
  --go-bin /workspace/toolchains/go/bin/go --execute
```

### Workload and correctness

- Two nodes/endpoints, 20 independent subscription subjects, 20 simulated active
  flows, and 2,000 completed historical flows/ledger records distributed over six
  days. Final persisted flow counts were 20 active and 2,000 completed; these are
  simulated metering identities, not real proxy connections.
- The 60-second open-loop schedule offered 1,201 unique cumulative events;
  observed client elapsed time was 60.539 seconds, approximately 19.84 unique
  events/second including completion overhead. Duplicate and older cumulative
  replay probes brought event HTTP requests to 1,274 (approximately 21.04/second).
  Schedule lag peaked at 1.895 ms; dropped and undelivered jobs were both zero.
- Four readers, 250 ms think time, made 945 reads across the six endpoints below.
  Seven runtime diagnostic requests also succeeded. Every HTTP metric, including
  the single setup login, reported zero errors. The one setup login is excluded
  from percentile population gates by the unchanged original runner.
- The final exact expected, ledger, and subscription totals all equaled
  **9,833,472 bytes**: 6,144,000 seeded bytes plus 3,689,472 newly accounted bytes.
  Each live cumulative increment was 3,072 raw bytes at multiplier 1000; no
  duplicate or older replay added value. All subject checks passed. Final
  `pending_usage_projections=0`, accounting and statistics checks both passed.
- Drain wall time was 5 seconds. After the service stopped, the independent
  durable-spool verifier found no unconsumed event, pending event, or pending
  byte. The service exited 0 without OOM.

### HTTP measurements

Client-observed milliseconds include Docker loopback transport. Percentiles use
the original load runner's integer measurement and percentile rules.

| Endpoint | Requests | p50 | p95 | p99 | Original p95 budget |
| --- | ---: | ---: | ---: | ---: | ---: |
| Users list | 157 | 1 | 3 | 17 | 250 |
| Subscriptions list | 158 | 2 | 7 | 16 | 250 |
| Subscription detail | 159 | 2 | 8 | 15 | 250 |
| Hourly traffic records | 158 | 9 | 16 | 33 | 250 |
| Event ingestion | 1,274 | 12 | 18 | 33 | 250 |
| Traffic trends | 157 | 1 | 6 | 32 | 1,000 |
| Traffic reconciliation | 156 | 3 | 6 | 23 | 1,000 |

### Processing, resource, and storage observations

The seven runtime samples recorded at most 29 pending events. The last sample
covered 1,266 envelopes in 35 batches, 796 projected samples and 470 coalesced
updates, with zero failures and 0.948 cumulative processing seconds. Its
receive-to-projection/checkpoint histogram had 100 ms buckets, p95 upper bound
2,000 ms, and maximum 2,124 ms. This last runtime sample precedes the final drain;
it is not a claim that all 1,274 HTTP deliveries were captured by that sample.

Accounting pool waits increased by 218 waits / 0.642405 seconds between the first
and last runtime samples; traffic-read pool waits increased by zero. These are
connection-pool waits, not SQL execution latency. SQL execution p95, statements
per batch, and logical write volume per batch remain unmeasured.

The original 15 Docker resource samples observed CPU 2.35–15.33% of one core
(sample mean 9.67%) and peak working set **37.46 MiB**, below its unchanged
700 MiB gate. Supplemental `docker top` observed 22 RSS samples: first 55.44 MiB,
last 61.68 MiB, sampled peak **63.46 MiB**. RSS and working set have different
definitions and are not interchangeable; neither is a continuous peak. Docker
BlockIO writes grew from 4.58 MB to 74.6 MB between samples; these physical-I/O
counters do not establish per-batch SQL write amplification.

The original live-storage samples covered 04:33:24.673–04:34:24.483 UTC, after
load began and before final drain. Their measured file growth was:

| File | First bytes | Last bytes | Difference |
| --- | ---: | ---: | ---: |
| SQLite database | 5,337,088 | 5,976,064 | 638,976 |
| SQLite WAL | 4,338,392 | 4,808,072 | 469,680 |
| Active durable event segment | 14,051 | 566,132 | 552,081 |

The spool's fixed emergency reserve was 268,435,456 bytes throughout; it is
distinct from event growth. Minimum sampled free disk was **3,251.61 MiB**, above
the original 1,536 MiB gate; preflight free space was 3,253.38 MiB, above its
1,600 MiB gate. Spool pressure remained normal. Stopped-export dbstat showed
6,053,888 allocated bytes / 1,478 pages: tables 1,970,176 bytes, indexes 3,956,736
bytes, internal pages 126,976 bytes, zero freelist pages. Index growth was not
sampled separately and must not be inferred from this final allocation. Final
ledger rows were 2,731, hourly usage rows 2,013, and pending projection rows zero.
No retention or compaction occurred in this short profile.

### Binding and handoff

Private artifacts remain ignored under the fixed checkout:
`baseline-controller-rq7qugu2/` and `mixed-20261020T043309Z-Xxy5wt/`.
All source/runner/binary hashes equal the table in Attempt 1. Additional report
hashes from this successful attempt are:

| Evidence | SHA-256 |
| --- | --- |
| Outer binding report | `59d6623533fc7d3276e56f242d3a48836ffcba2ae721fd944196c35d93cdcccc` |
| Original acceptance report | `c952b042451fd4cc09925b0a231f1f7d416a4f2cec3d1a244d2ac67072fa65cd` |
| Load result | `285b9ed428ea13855692dc8316164c29a4ec3d405abff2c4489f6f977339e202` |
| Accounting/statistics verification | `a618d2a4cb2c4751563290472e1e3389fdb151f9b989bc1b99c5a6bcfaade5d8` |
| Stopped-spool verification | `11a29e9ce404f680661ea9b771ba2d74a96804c65910442aec565d593870b2ec` |

After exit, Docker listed only the pre-existing isolated MySQL service; the
observer, original runner, load client, server, compiler, and linker had no live
process. Free filesystem space was approximately 3.4 GiB. The window was released
immediately to the next database verification task. No production migration,
deployment, release, or merge occurred.

This provides a reproducible short comparison point. It does not measure a
capacity ceiling or prove improvement, MySQL capacity, prolonged growth,
financial-write concurrency, coverage-loss behavior, quota exhaustion to
confirmed stop/overuse, plugin contention, or worker crashes/scaling. Those
remain explicit P6/P7 follow-ups; no throughput or zero-overuse promise follows
from this run.

## Prepared comparison target: not executed

Read-only inspection confirmed the existing standard checkout
`/workspace/zboard-verify-clone` is clean at
`53a056d32633dcbda9b243553d85a88fe9fc2090`. It is one hourly-projection commit
after the successful baseline. The three original runner files and all
`backend/tools/acceptance` files are byte-identical to 7ca9f63; the target source
manifest hash is
`bb1fc8853d98805dcbb27db2d567c89e6f02adf11716a0397916927a10a4d3a4`.
No target compilation or second workload has started.

The outer observer now accepts explicit `--revision` selection only for these
two complete SHAs, defaulting to 7ca9f63; clean-checkout, before/after source,
binary, and runner bindings use that selected revision. The observer's six
checks and both versions' read-only preflight passed after this addition; the
successful run above remains bound to its original observer hash. The comparison
must retain the same fixture sizes, resource
limits, read mix, zero-HTTP-error requirement, and all other original gates.
In particular, an hourly projection backlog producing HTTP 503 during a read is
a comparison failure, even if core accounting and the later drain succeed.
It must not be excluded, assigned zero, or treated as a successful statistics
response. A future target run needs its own explicitly reserved quiet window;
the current preparation is not an A/B improvement result. Separate CI has found
statistics regressions at 53a056d under investigation; no capacity result is
claimed for it. A fixed successor requires a new explicit approved hash.

## Attempt 1: environment failure before load

At 2026-10-20 04:32:04–04:32:15 UTC, the explicitly reserved capacity window
ran the unmodified mixed-load runner once from the clean standard checkout
`7ca9f63d0cf12a03713f3a242c508dcbf82e4856`. This scope contains migration 0053;
it excludes the in-progress 0054 hourly projection, P3 authorization changes,
and migration-copy receipt work. No production service or database was used.

The requested profile was 2 nodes, 20 subscriptions, 2,000 historical traffic
records, 60 seconds, 20 offered events/second, and 4 readers. The unchanged runner
would constrain its SQLite server to 1 CPU, 1 GiB memory, and no extra swap.
Compilation used Go 1.26.8, `CGO_ENABLED=0 GOOS=linux GOARCH=amd64`,
`GOMAXPROCS=1 GOFLAGS=-p=1`, and the cloud environment's writable Go caches.

Both binaries built successfully, but `docker build` exited 1:

```text
ERROR: mkdir /home/agent/.docker/buildx: read-only file system
```

The default Docker CLI configuration directory is read-only in this execution
environment. No image ID was produced, no fixture was seeded, and no server or
load container started. There are no HTTP, settlement, latency, RSS, storage
growth, or throughput results. The original acceptance report was not reached;
this is an unsuccessful preparation attempt, not a failed performance threshold
and not a passing baseline. No threshold, gate, exclusion, or original runner
was changed. No second execution was started automatically.

The outer observer confirmed all nine available bindings: checkout HEAD and
clean status, before/after source equality, both original build manifests,
both binary hashes, and both copied Python runners. Private evidence remains in
the fixed checkout's ignored acceptance directory:

- `baseline-controller-btn2sadw/`: preflight, source/binary bindings, runner log.
- `mixed-20261020T043204Z-0RAyYP/`: the original runner's manifests and binaries.

Only sanitized evidence is recorded here. Hashes from this attempt:

| Item | SHA-256 |
| --- | --- |
| Source manifest | `04412fe930f802ced271d5c156935dfa0d3e2d1d955b100807c41db0e4153e3c` |
| Original mixed runner | `e526b44d5a81e306f34a4b069e40fdd29b908c55f4bc45a070ef558722a259f6` |
| Original load runner | `3a8d946d14e96b3262f7ee831c4da0dcd96f31d31a1fe2ce2b8525a90e00af12` |
| Original report runner | `d8c9e0ab163a29a62fb296ddb30e5c22b6efe68cab3d1f94bfc7395082d06021` |
| Outer observer | `a3edca31cf441540be47fabeb21eb492e8e21c396c27f3ce8abc9d9281208915` |
| Server binary | `8de726df03bd5acc4a1fb0df345fc6a9e4f9cc84a4347a33fb3d9c6d1b5218a8` |
| Acceptance binary | `8ea416aa44ba183f847e58e1262457891b96fe4da36c7c9ca4f0c4bd5650136e` |
| Private binding report | `77f9c2cbbbdc82207be3fa939c7c8a86a0a36752b2d4e3aca6f9ccf3c792f1e8` |

Post-exit checks found no live observer, original runner, load client, server,
Go compiler, or linker. Docker listed only the pre-existing isolated MySQL 8.4
test service; it was not modified. The filesystem had approximately 4.0 GiB free.
The reserved window was released immediately.

## Verification and next condition

The observer's four checks passed: source manifest matches the original
`sha256sum` loop including untracked sources, a missing SQLite export remains
unknown and is not created, read-only dbstat leaves database bytes unchanged,
and wrong/dirty checkouts refuse execution before Docker. The original six
report-gate checks also passed. Commands:

```sh
python3 -m unittest discover -s scripts -p 'test_acceptance_capacity_baseline.py'
# In the fixed 7ca9f63 checkout:
python3 -m unittest discover -s scripts -p 'test_acceptance_report.py'
```

The second explicitly reserved window above corrected only `DOCKER_CONFIG`,
while preserving this failed attempt and all original gates. See
[the runner contract](../../scripts/acceptance-capacity-baseline.md).
The short SQLite profile remains only a comparison prerequisite: P6 scaling,
MySQL capacity, SQL-per-batch/write amplification, quota exhaustion to confirmed
stop, plugin contention, and worker failure/scaling validation are unverified.
