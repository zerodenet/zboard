# One-command development acceptance

This is a user-run, serial acceptance entry for an existing checkout and existing tools. It does not operate another machine. Running it on your own machine later is your choice; no Mac task, remote connection or local operation is created by this PR.

Install the required tools yourself: Python 3.10 or newer (standard library only), Git, Go **1.26.8**, Node.js **22.23.0**, pnpm **11.9.0**, and a disposable **MySQL 8.4** instance. Optional manual upgrade/load/worker stages additionally need an existing Docker installation and their original resource conditions. The entry verifies Go/Node/pnpm versions and the unauthenticated MySQL greeting. It never installs or upgrades a toolchain, starts a MySQL server, creates a container for ordinary tests, changes server/security settings, downloads an external plugin or purchases resources. Normal Go module downloads and the explicitly requested project's frozen pnpm lockfile install are part of the functional checks; neither installs a system toolchain.

Use a fresh private evidence directory **outside the checkout**, and provide the test DSN through your private environment. The DSN must select an explicit numeric loopback TCP port and **no existing database**, for example the shape `TEST_USER:TEST_PASSWORD@tcp(127.0.0.1:3306)/?parseTime=true&loc=UTC`. Do not paste real credentials into commands, issues or reports. `--isolated-mysql` attests this is a disposable test instance, not production or a production tunnel. Loopback alone is not proof of isolation. The test account needs the existing fixtures' random-schema creation/deletion privileges. The runner neither creates that account nor changes its grants.

After setting `ZBOARD_TEST_MYSQL_DSN` privately, run the default functional matrix:

```sh
python3 scripts/acceptance-development.py --isolated-mysql --output-dir /tmp/zboard-development-UNIQUE
```

It runs, in order:

1. Backend `go mod tidy` with unchanged `go.mod`/`go.sum` required (changes preserved), followed by `go test -json -p 2 ./... -count=1`, `go vet -p 2 ./...`, and `go build -p 2` with the binary in the evidence directory. This phase has **no** MySQL test DSN; its MySQL/external skips are listed and never counted as actual MySQL verification.
2. `CGO_ENABLED=0 go test -json -p 2 ./internal/datastore -run SQLite -count=1`. SQLite evidence remains separate from MySQL. This phase requires actual passing test events and no skipped SQLite tests; exit zero with only skips is incomplete.
3. Plugin SDK `go mod tidy`, before/after `go.mod`/`go.sum` equality, `go test -json -p 2 ./... -count=1`, and `go vet -p 2 ./...`. Module changes are preserved and fail the consistency check; the runner does not reset files.
4. Frontend `pnpm install --frozen-lockfile --package-import-method=copy`, the current CI test command with **exactly the existing six exclusions**, then `pnpm build` (typecheck and Vite build). No frontend exclusion is added. Node's existing 4096 MiB test setting is retained. A failed install prevents dependent frontend test/build execution, with explicit reasons.
5. `scripts/mysql_profiles.py --profile full`: actual dynamically inventoried **core plus all 12 extended tests**, including complete generic jobstore and platform-copy coverage across the two profiles. The root-owned MySQL runner retains its `-p 2`, `-count=1`, `-timeout 15m`, grouping, JSONL, stderr, source bindings and fail-closed coverage checks. This entry does not duplicate or replace that implementation, retry failures, raise timeouts or accept a core-only receipt.

Independent functional phases continue after a failure to preserve useful diagnostics; failures remain failures. No phase retries automatically. Missing tools, absent/wrong MySQL, reduced options and missing manual conditions become `not_executed`, not pass. The entry never interprets, for example, 127 SQLite passes and 43 MySQL skips as MySQL success. A reduced SQLite scope is available without a DSN:

```sh
python3 scripts/acceptance-development.py --sqlite-only --output-dir /tmp/zboard-sqlite-UNIQUE
```

SDK and frontend remain enabled by default. `--no-with-sdk` and `--no-with-frontend` explicitly reduce the requested scope and leave their phases `not_executed`. `--skip-project-install` records the missing install and cannot produce a complete functional pass even if already-installed frontend tests run.

## Separate manual execution

Manual phases are never started by default. Select each with its explicit `--execute-*` flag and `--quiet-window` only after other builds/tests/services are stopped and the child tool's original resource and source conditions hold. No resource gate is lowered by this wrapper. Example arguments to append to the functional command:

```text
--execute-p7 --mysql-container YOUR_EXISTING_ISOLATED_CONTAINER --quiet-window
--execute-capacity --quiet-window
--execute-worker-comparison --quiet-window
--execute-autoscale --confirmed-benefit-receipt /ABS/SAME-REVISION-REAL-MEASUREMENT.json --quiet-window
```

- **P7** invokes `scripts/acceptance-upgrade/run.py --driver both --execute` against the exact clean committed HEAD, after the current authority probe is integrated. It passes an external artifact root when supported. The child owns its preflight budgets, old/current build, SQLite copies, isolated MySQL logical backup and independent restore, authority fixtures and immutable backup checks. An old narrow probe cannot silently stand in for the new authority matrix. Docker/container 3306 mapping and test DSN must match. A child's passing execution receipt is required. This remains an isolated saved-provider/probe exercise, not a real external coupon durability, payment sandbox, remote kernel or production acceptance.
- **Original capacity** invokes the existing fixed-revision baseline controller once and preserves all its original 1 CPU/1 GiB, 700 MiB working-set, HTTP/accounting/spool/drain/disk gates. For a reviewed old pin it uses that controller unchanged. At a new HEAD it uses only an integrated `acceptance-autoscale-manual.py --execute-baseline BUILD_RECEIPT` mode, which must preserve the original profile/gates and return `baseline_passed`; until this mode is integrated the stage is `not_executed`. It prepares a source-bound build once and never inserts an old-controller pin, substitutes an old checkout/result, starts a load at a rejected revision, or treats the historical 112/157 HTTP 503 failure as passed.
- **Worker comparison** prepares once, then uses the child's printed build receipt for the unchanged three-pair equal-budget single/split comparison. It requires all source bindings and a completed measurement, retains `benefit_verified=false` when the child does, and cannot turn merely starting workers into demonstrated benefit.
- **Autoscaling** invokes the separate `scripts/acceptance-autoscale-manual.py` only after it is integrated and a same-revision real measurement receipt is supplied. It prepares once and passes that build receipt to the explicit execution mode. The child's Linux cgroup-v2, Docker, plugin/source/receipt and original disk/resource checks remain authoritative. Missing integration/receipt is `not_executed`. The current harness's `measurements_preserved` with `benefit_verified=false` is recorded **inconclusive**, not pass, even with exit zero; no growth/shrink observation or inconclusive load must be reported as proven autoscaling. Quota stop without actual kernel confirmation remains an external requirement.

The child tools may create their own labelled test containers/volumes or randomized schemas and perform **only their documented owned-fixture cleanup**. Ordinary Go/MySQL fixtures create random schemas and normally drop those exact schemas; some failure paths deliberately preserve evidence. P7 creates independent backup/restore fixtures and preserves backups. Worker/load fixtures export their test database before their documented owned-resource cleanup. This wrapper itself never removes any database, volume, container, image, source, backup, acceptance log, cache or credential; it never runs cache-cleaning or Docker-prune commands. Existing resources remain untouched. If an interrupted child still owns a process/resource, inspect its exact receipt/PID before deciding on another invocation.

## Evidence and result meaning

The new evidence directory is private (`umask 077`) and must not already exist. It contains per-command stdout/JSONL and separate stderr, start/end/exit/duration records, nested MySQL inventory/partition/commands/results, and `source-before.json` / `source-after.json`. The bindings include exact HEAD plus every tracked and nonignored untracked file's content SHA-256, executable bit, deleted-file state and symlink target identity. Ignored installed dependencies, generated builds, caches and private test artifacts are excluded; no credential contents or environment dump is placed in the public summary. Treat raw child evidence as private, including generated test credentials; share a sanitized report only.

`summary.json` distinguishes three results:

- `requested_scope_status`: whether every **explicitly requested** stage passed with unchanged HEAD/source. The process exits zero only for this scope. Thus a successful `--sqlite-only` command is a scoped SQLite result, never a full-MySQL result.
- `functional_status`: all default backend/SQLite/SDK/frontend/**full MySQL** checks passed, with unchanged source. Missing DSN/tools/install cannot pass this result.
- `full_development_acceptance`: all functional **and** P7/original-capacity/worker/autoscale stages passed with unchanged source. Unrequested or resource-blocked manual stages leave this incomplete even if functional checks passed. External provider/kernel/sandbox/production acceptance remains separately `not_executed` regardless.

No started process, prepare-only receipt, old-head CI or source-changing command can produce a full pass. A controller error or interruption prevents requested-scope success. No real user refund, external payment, deployment, release or production migration is performed. Rust/zrs-compiler, OpenAPI/Redocly, full Compose deployment checks, browser automation and external native/payment integration remain separately required according to the current CI/acceptance plan; this functional runner does not install their extra tools or replace their records. Existing cloud `errno 28` failures, failed CI and unexecuted final-head matrices must remain in the handoff; creating this runner is not evidence they have passed. The full runner has not yet been executed. Its nine isolated Python orchestration-policy tests passed on 2026-10-27; this is not a Go, frontend, database, upgrade or capacity acceptance result.

The pure orchestration-policy tests are runnable without Go/Node/MySQL/Docker:

```sh
python3 -m unittest discover -s scripts -p 'test_acceptance_development.py'
```

## Same-budget autoscale prerequisite (manual only)

`--execute-worker-comparison --worker-budget-profile autoscale-4cpu
--worker-benefit-metric core_p95_ms --execute-autoscale --quiet-window`
selects the additional 4CPU/4GiB three-pair comparison before autoscaling. Single
uses 4CPU/4GiB/GOMAXPROCS4; split uses main2CPU/2GiB/GOMAXPROCS4 plus
worker2CPU/2GiB/GOMAXPROCS2. The default original-1.5cpu profile, original
1CPU/1GiB capacity baseline and every existing HTTP/accounting/drain/disk/700MiB
combined gate remain unchanged. This extra budget is not the original baseline.

The explicit metric selects a real median to compare, not worker-count success.
Only three complete raw source-bound pairs with a positive selected difference
produce a hashed benefit receipt. No gain or unknown data produces no receipt,
so autoscale remains not_executed. The autoscale child independently rechecks raw
artifacts and source/budget before admission; receipt creation is no overall P6
pass or statistical/production guarantee. All newly created comparison resources
and DB/log exports are retained; this mode performs no container/volume cleanup.
No capacity/load execution is implied by adding this interface.
