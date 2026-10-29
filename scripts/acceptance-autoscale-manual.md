# Manual local-worker and autoscaling acceptance

Use [the complete development entry](acceptance-development.md) for functional checks and explicit manual stages. These tools operate only the selected isolated checkout/fixtures. They do not deploy, migrate production, connect to a user's computer or purchase resources.

`acceptance-autoscale-manual.py` has separate inspect, prepare, original-baseline and autoscale execution modes. The original baseline remains 1 CPU / 1 GiB, 20 events/s, four readers and 60 seconds, with all original HTTP, accounting, drain, 700 MiB combined working-set and disk gates. It does not start a helper, plugin or worker. Original failures remain failures; a new executable alone is not a passed benchmark.

The explicit `autoscale-4cpu` worker-comparison profile is an additional equal-budget experiment: single 4 CPU / 4 GiB / GOMAXPROCS 4; split main 2 CPU / 2 GiB / GOMAXPROCS 4 plus worker 2 CPU / 2 GiB / GOMAXPROCS 2. The default older 1.5 CPU profile is unchanged. Three independent complete raw pairs, exact source/binary bindings and a positive explicitly selected measured metric are required to produce a benefit receipt. Its consumer independently checks raw directories, every container limit and no-swap/exit condition, source, load and concurrency. Repeated directories cannot substitute for independent pairs. No positive gain or incomplete data produces no usable receipt.

After this prerequisite, the separate 4 CPU / 4 GiB autoscaling experiment uses the production supervisor and its original resource reserves, thresholds and cooldowns. Normal/backlog/crash/slow-write/plugin-busy scenarios use real queued seed facts, observed managed children, a bounded actual SQLite writer and a signed config-only busy native fixture. The crash action uses a pidfd only after checking fixture owner, parent, executable and managed-worker arguments. No guessed PID, lock takeover, mocked pressure source or manual worker start can count as automatic expansion. Missing expansion/shrink or missing observations remain inconclusive.

The report preserves actual runtime job-store/native-authority SQL observations, cgroup/process CPU and RSS, request distributions, backlog/drain, source/binary/fixture identities, logical database/spool growth and errors. Fixed-ring samples are deduplicated by metrics reset/sequence. Commit-unknown and unknown affected rows stay unknown. Store-call time is not server-only SQL time; argument bytes are not physical writes. Other-process metrics, Docker-daemon costs, actual server write bytes and long-term stability are not inferred. The seed's huge quota and absent real kernel cannot demonstrate exhaustion-to-stop confirmation or zero overuse; this remains external-required.

Every newly created container, volume, database export and log is retained. Graceful stops release processes, but these tools do not prune resources or delete old evidence. Account for this disk cost before running; original resource gates remain authoritative. Ordinary isolated functional MySQL fixtures retain their separate documented random-schema cleanup contract.

Actual checks for this implementation: all 44 offline acceptance-policy tests passed with unchanged backend/scripts sources; the native busy fixture binary compiled in 9.752 seconds and was never started or installed. Earlier patch-format and handcrafted-receipt fixture failures were preserved and corrected without weakening profile/binary checks. The default old receipt format already contains its resource layout; its changed-binary assertion still runs. The Linux experiment helper, complete application build, full dual-database matrix and actual load/comparison/autoscale scenarios remain unexecuted locally because required dependency downloads return HTTP403. No capacity gain or overall P6/P7 completion is claimed.

```sh
python3 -m unittest discover -s scripts -p 'test_acceptance*.py'
python3 scripts/acceptance-autoscale-manual.py --help
python3 scripts/acceptance-development.py --help
```

For actual execution use a clean committed revision and the explicit commands, toolchain, isolated database attestation and quiet-window requirements in the complete-entry README. A same-source measured benefit permits this controlled experiment, not production rollout or an automatic claim of statistical significance.
