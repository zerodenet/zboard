# P6 short hourly-projection comparison: failed availability gate

The explicitly reserved comparison window at 2026-10-20 04:50:51–04:52:28 UTC
ran the unchanged mixed-load profile once against the clean standard checkout
`4dfb6642c87d1900882afdc90d4467369e98fce1`. The original acceptance report
**failed**: the traffic trends endpoint returned errors in **112 of 157 requests**
(71.34%). Its low observed p95 does not override the unchanged zero-HTTP-error
gate. All nine source/runner/binary bindings passed. No threshold or exclusion
was changed, and no second comparison was started.

The runner retains at most ten response error samples. All ten preserved samples
were HTTP 503 from `/api/v1/admin/traffic/trends` with version 1 code
`usage_projection_pending` and `retryable=false`. The full metric counts 112
errors; the bounded sample does not preserve a response body for every error.
The original report's second failure string, "load generator dropped jobs or HTTP
requests failed", refers to these HTTP errors: dropped and undelivered jobs were
both zero.

## Scope, load, and unchanged gates

The target includes the 0054 hourly projection and subsequent fixture corrections;
it excludes the still-in-progress P3 plan-group and migration-copy work. The
[earlier 7ca9f63 baseline](core-convergence-capacity-baseline.md) includes 0053 and
passed the identical original runner. The three runner files and all
`backend/tools/acceptance` source files were byte-identical between the two
checkouts. Go 1.26.8 built Linux/amd64 static binaries with
`GOMAXPROCS=1 GOFLAGS=-p=1`; Docker 28.4.0 ran one SQLite server at exactly 1 CPU,
1 GiB memory, no extra swap. The existing writable Docker CLI configuration was
selected explicitly, without exposing its contents. Host-side clients ran
outside the constrained server.

Both runs seeded two node/endpoints, twenty independent subscription subjects,
and 2,000 historical completed flows/ledger records over six days. Each offered
1,201 unique cumulative events on a 20/second schedule, plus duplicate/older
cumulative replay probes for 1,274 event HTTP requests. This target's elapsed
client time was 60.537 seconds; maximum schedule lag was 3.183 ms. Four readers
with 250 ms think time made 946 reads. Seven runtime diagnostic requests returned
200 with zero errors. The simulated active flows were twenty, not twenty real
proxy connections. No production or real kernel endpoint was contacted.

Gates remained: zero HTTP errors and at least twenty observations per non-login
endpoint; p95 at most 250 ms for core/event/hourly-record endpoints and 1,000 ms
for trends/reconciliation; sampled Docker working set at most 700 MiB; exact
resource profile; no OOM and exit 0; exact quota/ledger verification, statistics
and durable-spool drain; no dropped/undelivered jobs; required runtime samples;
normal spool pressure; and original 1,600 MiB preflight / 1,536 MiB sampled free
disk thresholds. The bounded setup login remains excluded only from percentile
population by the original runner, with zero errors required.

## HTTP comparison

These are client-observed integer milliseconds, including Docker loopback.
The target trend p95 includes failed responses and is **not** a successful-query
latency comparison. Each version is a single short sample, not a statistically
established performance trend.

| Endpoint | Baseline count/errors | Target count/errors | Baseline p95 | Target p95 | Budget |
| --- | ---: | ---: | ---: | ---: | ---: |
| Users list | 157 / 0 | 157 / 0 | 3 | 3 | 250 |
| Subscriptions list | 158 / 0 | 158 / 0 | 7 | 5 | 250 |
| Subscription detail | 159 / 0 | 159 / 0 | 8 | 10 | 250 |
| Hourly traffic records | 158 / 0 | 158 / 0 | 16 | 15 | 250 |
| Event ingestion | 1,274 / 0 | 1,274 / 0 | 18 | 17 | 250 |
| Traffic trends | 157 / 0 | **157 / 112** | 6 | **9, failed responses included** | 1,000 |
| Traffic reconciliation | 156 / 0 | 157 / 0 | 6 | 5 | 1,000 |

## Correctness and resource observations

Final accounting and statistics verification passed for the target: expected,
ledger, and subscription totals all equaled **9,833,472 bytes**, comprising
6,144,000 seeded bytes and 3,689,472 newly accounted bytes. The raw increment was
3,072 bytes at multiplier 1000; duplicate and older probes did not add value.
Final pending projection count was zero. Drain took 5 seconds; after service
stop, the separate spool verifier found zero pending events/bytes and no
unconsumed durable event. The service exited 0 without OOM. These final checks
do not erase the observed statistics-query failures during load.

| Observation | Passing 7ca baseline | Failed 4df comparison |
| --- | ---: | ---: |
| Runtime samples | 7 | 7 |
| Maximum sampled pending events | 29 | 45 |
| Last sampled processed envelopes / batches | 1,266 / 35 | 1,254 / 31 |
| Last sampled projected / coalesced flow updates | 796 / 470 | 722 / 532 |
| Consumer failures in last runtime sample | 0 | 0 |
| Receive-to-projection/checkpoint p95 upper bound | 2,000 ms | 2,000 ms |
| Receive-to-projection/checkpoint sampled max | 2,124 ms | 2,656 ms |
| Accounting-pool wait count / seconds delta | 218 / 0.642405 | 314 / 0.569311 |
| Traffic-read-pool wait count / seconds delta | 0 / 0 | 0 / 0 |
| Docker resource samples | 15 | 15 |
| Sampled CPU min / mean / max, % of one core | 2.35 / 9.67 / 15.33 | 1.38 / 9.41 / 18.47 |
| Sampled peak Docker working set | 37.46 MiB | 36.11 MiB |
| Supplemental RSS samples | 22 | 22 |
| Sampled peak process RSS | 63.46 MiB | 60.04 MiB |
| Minimum sampled free disk | 3,251.61 MiB | 2,346.27 MiB |

Runtime samples precede final drain, use 100 ms latency buckets, and do not cover
every final delivery. Connection-pool waits are not SQL execution latency. CPU
and memory samples are not continuous peaks or proof of improvement. The target
RSS observer recorded one `CalledProcessError` in addition to its 22 valid
samples; it did not synthesize a zero or replace the original working-set gate.
The error has no captured Docker stderr, so its exact timing/cause is unknown.

The target's live storage samples covered 04:51:22.671–04:52:22.446 UTC, after
load started and before final drain:

| File | First bytes | Last bytes | Difference |
| --- | ---: | ---: | ---: |
| SQLite database | 4,923,392 | 5,959,680 | 1,036,288 |
| SQLite WAL | 4,383,712 | 4,808,072 | 424,360 |
| Active event segment | 14,046 | 566,112 | 552,066 |

The fixed emergency reserve remained 268,435,456 bytes. Preflight free disk was
2,348.42 MiB and pressure stayed normal. Docker BlockIO sampled writes grew from
4.62 MB to 69.2 MB; these are physical I/O counters, not logical SQL writes.
Stopped-export dbstat reported 5,992,448 allocated bytes (1,463 pages): tables
1,933,312 bytes, indexes 3,936,256 bytes, internal pages 122,880 bytes, zero
freelist pages. Final counts were 2,669 traffic ledger rows, 2,009 hourly rows,
20 active flows, 2,000 completed flows and zero pending projection rows. No
retention or compaction occurred. Different batch coalescing and sample alignment
mean these single-run differences do not establish lower storage growth.

## Reproduction, evidence, and handoff

The executed command, from the main worktree, was:

```sh
DOCKER_CONFIG=/workspace/toolchains/docker-config \
GOCACHE=/workspace/toolchains/cache/go-build \
GOPATH=/workspace/toolchains/gopath \
python3 scripts/acceptance-capacity-baseline.py \
  --checkout /workspace/zboard-verify-clone \
  --revision 4dfb6642c87d1900882afdc90d4467369e98fce1 \
  --go-bin /workspace/toolchains/go/bin/go --execute
```

Private artifacts remain under the checkout's ignored acceptance directory:
`baseline-controller-p4pr1sez/` and `mixed-20261020T045051Z-VpYSh7/`. Fixture
secrets, raw logs, database, and binaries are not committed. The source remained
clean and unchanged; original source-before/build-after manifests, final source
manifest, binary hashes, and copied Python runners all matched their bindings.

| Evidence | SHA-256 |
| --- | --- |
| Source manifest | `c34019cb8b3a6927107096516481e804f9da1c6edc1940b26f3740b11deb0c87` |
| Outer observer | `3cac8a37b3bb4896169fd0bb07e23fda6b7b0fed038f72ed1ee8ead37e9cedac` |
| Server binary | `827d2bda751cd4950690a994db459d5e21a04d377d7c6e085fe64d473c098a76` |
| Acceptance binary | `3d413f7c08b04bb3919d393c05ed5f3c958cde6ef6d97828b0d9f7df3824d7fe` |
| Outer binding report | `0cd01ca7466da2e4f423de4e84e7332d2f68726c8a533b2d58ae510f7719a000` |
| Original acceptance report | `414e0dabaf9ac87caa5cf24d66d733cacf892c3e168704fb07eacd0bbf427e00` |
| Load result | `20631ec0263243a732a62e5e8d84dd1ead7e3a321d3c661c3f0c81f9273caffc` |
| Final accounting/statistics verification | `f35de118c9e189fcaee13af01687d1e2ebfba33eb21baf487ec411b5046c8fd4` |
| Stopped-spool verification | `013b72d5ba9278f8def16b19601e6320a6c8f729e4f87c3e6425c71278f490bb` |

Original mixed/load/report runner hashes were respectively
`e526b44d5a81e306f34a4b069e40fdd29b908c55f4bc45a070ef558722a259f6`,
`3a8d946d14e96b3262f7ee831c4da0dcd96f31d31a1fe2ce2b8525a90e00af12`, and
`d8c9e0ab163a29a62fb296ddb30e5c22b6efe68cab3d1f94bfc7395082d06021`.

After execution, only the pre-existing isolated MySQL container remained. No
observer, original runner, load client, server, compiler, or linker was alive.
Filesystem free space was approximately 2.5 GiB. The reserved window was released
immediately; no retry, production migration, release, deployment or merge occurred.

Next work is to resolve statistics availability under normal continuous ingestion
without hiding pending work, serving an incomplete projection as complete, or
relaxing zero-error gates. A repaired exact source revision needs its own reviewed
tests and an explicitly reserved comparison window. Until then, the capacity
comparison remains failed and P6/P7 capacity acceptance remains incomplete.
SQL execution latency/statements/writes per batch, quota-stop delay/overuse,
plugins, independent workers/scaling, MySQL capacity, and long-duration resource
growth are still unverified.
