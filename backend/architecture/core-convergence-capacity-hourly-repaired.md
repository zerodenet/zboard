# Repaired hourly statistics: fixed-source original-gate comparison

Source308689e38cff794865b633cde7193656ca2ecd4c passed every original mixed-load
acceptance gate and all nine source/binary/runner bindings. The earlier4dfb664
comparison remains failed with112/157 trend503 responses in
[its immutable historical report](core-convergence-capacity-hourly-comparison.md).
The repaired API's explicit unknown/pending read state is documented in
[the pending evidence](core-convergence-pending-statistics-evidence.md).

The same original runner/load/report hashes,1CPU/1GiB/no extra swap,
GOMAXPROCS1,2nodes/20subscriptions/2000historical rows,20unique events/s,
4paced readers and60seconds were used. No workload/gate was changed and no extra
HTTP observer was added. All other compilation/tests/load processes had exited.
Window2026-10-20 17:59:16–18:00:48 UTC includes build/seed/drain/verification.

1201unique events over60.537s produced1274event HTTP requests and948business
reads, all with zero errors. All six read endpoints had157–159samples. Trends
had158requests/0errors/p95=5ms; eventp95=17ms and other business readp95=5–14ms.
The original thresholds remained250ms(core/events),1000ms(trends/reconciliation),
zero errors and700MiB working set. Sampled working set peaked36.78MiB and process
RSS61.14MiB (23samples, no observer errors). CPU and exact resource values are in
[the structured result](validation/2026-10-20-hourly-repaired-capacity.json).

Expected, ledger and subscription totals all equal9,833,472bytes. Final statistics
verification passed with zero pending projection intents. Drain took5seconds;
after shutdown, spool pending events/bytes were zero, pressure stayed normal and
564,893bytes of retained spool storage were preserved. The stopped SQLite export
had6,045,696allocated bytes, including3,973,120index and1,949,696table bytes.
These are stopped observations, not a production growth or archive guarantee.

The original HTTP client does not count pending and complete response bodies.
Their proportions during load are unknown: successful HTTP transport must not be
reported as continuously complete graphs. The final drained statistics check is
a separate fact. Last runtime diagnostics cover1250envelopes/32batches,
receive-to-projection/checkpointp95 upper bound2000ms/max2041ms; this sampled
counter does not include every final event. SQL execution latency, SQL statements
and write bytes per batch remain unmeasured; DB-pool wait is not SQL latency.
Quota-to-stop delay/overuse, busy plugins, independent workers, MySQL capacity and
long-duration resource bounds remain separate unverified work.

## Reproduction and source binding

```sh
PATH=/workspace/toolchains/go/bin:/usr/local/bin:/usr/bin:/bin \
DOCKER_CONFIG=/workspace/toolchains/docker-config \
GOCACHE=/workspace/toolchains/cache/go-build \
GOPATH=/workspace/toolchains/gopath \
python3 /workspace/zboard/scripts/acceptance-capacity-baseline.py \
  --checkout /workspace/zboard-verify-clone \
  --revision 308689e38cff794865b633cde7193656ca2ecd4c \
  --go-bin /workspace/toolchains/go/bin/go --execute
```

The first repaired attempt stopped before the original runner/load because PATH
omitted the existing docker executable in/usr/local/bin; its failed controller
and log were retained. The second changed only PATH. Private evidence remains
under the verify clone's ignored acceptance/baseline-controller-zq0vqyjw and
mixed-20261020T175916Z-u8SWxZ directories. Credentials, fixture files, raw logs,
SQLite exports and binaries are not committed. Clean source, before/build-after
manifests, both binary hashes and copied Python runner bindings all matched.
Normal original-runner cleanup stopped/removed only its isolated resources.

This precise source also passed local fullGo/vet/build/pureGoSQLite and the
original realMySQL suite(823.311s), followed sequentially by realMySQL copy suite
(113.401s). All six CI jobs passed for this source in
https://github.com/zerodenet/zboard/actions/runs/39467975060 . Later commits require
their own validation; no production rollout, migration, release or merge occurred.
