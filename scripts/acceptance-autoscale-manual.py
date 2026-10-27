#!/usr/bin/env python3
"""Manual/release only: real supervisor, equal total resources, no automatic cleanup.

Inspect is read-only. Preparation builds one exact source receipt. Execution needs
an equal-budget measured-benefit receipt and creates only labeled isolated data.
It never edits a running deployment, changes server variables, or removes data.
"""
import argparse
import datetime as dt
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import resource
import statistics
import subprocess
import sys
import tempfile
import threading
import time
import urllib.error
import urllib.request
import uuid

BASELINE = dict(nanocpus=1_000_000_000, memory=1_073_741_824)
COMPARISON = dict(nanocpus=4_000_000_000, memory=4_294_967_296)
PARAMETERS = dict(nodes=2, subscriptions=20, history_records=2000, seconds=60, rate=20, readers=4)
POST_RESERVE_GATE = 1600 << 20
RUNTIME_DISK_GATE = 1536 << 20
WORKING_SET_MIB = 700
SCENARIOS = ("normal", "backlog", "crash", "dbslow", "pluginbusy")
SUPERVISOR = ["-projection-workers-max=1", "-projection-workers-confirmed-benefit=true",
              "-projection-workers-lock-file=/data/p6-supervisor.lock",
              "-projection-workers-database-path=/data"]  # All policy defaults retained.
sys.dont_write_bytecode = True


def now():
    return dt.datetime.now(dt.timezone.utc).isoformat()


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def save(path, value):
    Path(path).write_text(json.dumps(value, indent=2) + "\n")
    Path(path).chmod(0o600)


def command(args, **kwargs):
    return subprocess.check_output(list(map(str, args)), text=True, stderr=subprocess.PIPE, **kwargs).strip()


def source_manifest(root):
    names = command(["git", "ls-files", "--cached", "--others", "--exclude-standard", "backend", "scripts"], cwd=root).splitlines()
    return {name: sha(root / name) for name in names if "__pycache__" not in Path(name).parts and (root / name).is_file()}


def require_source(root, revision):
    if command(["git", "rev-parse", "HEAD"], cwd=root) != revision:
        raise ValueError("exact revision required; no checkout/reset is performed")
    if command(["git", "status", "--porcelain", "--", "backend", "scripts"], cwd=root):
        raise ValueError("application and harness sources must be committed and clean")
    return source_manifest(root)


def benefit_receipt(path, revision, root):
    """Recompute raw equal-budget gains; declarations/worker counts never suffice."""
    receipt = json.loads(path.read_text())
    report_path = Path(receipt["comparison_report"]).resolve()
    report = json.loads(report_path.read_text())
    if receipt.get("revision") != revision or receipt.get("report_sha256") != sha(report_path):
        raise ValueError("benefit receipt source/report binding differs")
    if report.get("revision") != revision or not report.get("complete") or not report.get("source_unchanged"):
        raise ValueError("completed exact-source comparison required")
    if report.get("budget_profile") != "autoscale-4cpu" or report.get("parameters") != PARAMETERS:
        raise ValueError("separate 4CPU profile and unchanged original load required")
    runs = report.get("runs", [])
    if len(runs) != 6 or any(sum(r.get("profile") == profile for r in runs) != 3 for profile in ("single", "split")):
        raise ValueError("exactly three paired single/split runs required")
    directories = [str(Path(r["directory"]).resolve()) for r in runs]
    if len(set(directories)) != len(directories):
        raise ValueError("paired runs cannot reuse raw evidence directories")
    source_hash = hashlib.sha256(json.dumps(source_manifest(root), sort_keys=True).encode()).hexdigest()
    if report.get("source_manifest_sha256") != source_hash:
        raise ValueError("manual comparison source manifest differs")
    spec = importlib.util.spec_from_file_location("benefit_original_report", root / "scripts/acceptance-report.py")
    evaluator = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(evaluator)
    measured = {"single": [], "split": []}
    for run in report.get("runs", []):
        profile, directory = run.get("profile"), Path(run["directory"]).resolve()
        if profile not in measured:
            raise ValueError("manual comparison must contain only single/split profiles")
        containers = [json.loads((directory / "container.json").read_text())[0]]
        if profile == "split":
            containers.append(json.loads((directory / "worker.json").read_text())[0])
        exact = [(4_000_000_000, 4_294_967_296)] if profile == "single" else [(2_000_000_000, 2_147_483_648)] * 2
        if [(c["HostConfig"]["NanoCpus"], c["HostConfig"]["Memory"]) for c in containers] != exact:
            raise ValueError("actual comparison container resources are not equal to 4CPU/4GiB")
        if "GOMAXPROCS=4" not in containers[0]["Config"].get("Env", []):
            raise ValueError("manual prerequisite must first expose the in-process core to all four CPUs")
        if profile == "split" and "GOMAXPROCS=2" not in containers[1]["Config"].get("Env", []):
            raise ValueError("manual worker must use the declared two-CPU concurrency")
        if any(c["HostConfig"]["MemorySwap"] != c["HostConfig"]["Memory"] or c["State"]["OOMKilled"] or c["State"]["ExitCode"] for c in containers):
            raise ValueError("manual comparison OOM/swap/graceful-exit gate failed")
        result = evaluator.evaluate(directory, expected_nanocpus=containers[0]["HostConfig"]["NanoCpus"], expected_memory=containers[0]["HostConfig"]["Memory"])
        process_rows = [json.loads(line) for line in (directory / "process-samples.jsonl").read_text().splitlines()]
        if not result["passed"] or len(process_rows) < 2 or any(r.get("error") or len(r.get("stats", [])) != len(containers) for r in process_rows):
            raise ValueError("manual comparison raw gates/combined resource coverage failed")
        if max(sum(mib(c["MemUsage"].split("/")[0]) for c in r["stats"]) for r in process_rows) > WORKING_SET_MIB:
            raise ValueError("manual combined working set exceeds unchanged 700MiB")
        load = json.loads((directory / "data/load-result.json").read_text())
        if load.get("rate") != 20 or load.get("readers") != 4 or not 60 <= load.get("elapsed_seconds", 0) <= 95:
            raise ValueError("manual comparison changed offered load or failed to drain promptly")
        core = [r["p95_ms"] for r in result["latencies"] if "/traffic/trends" not in r["path"] and "/traffic/reconciliation" not in r["path"]]
        measured[profile].append(dict(drain_seconds=result["drain_seconds"], core_p95_ms=max(core),
             received_to_settlement_p95_ms=result["runtime_diagnostics"]["last_consumer"].get("p95_upper_ms")))
    if any(len(rows) < 3 for rows in measured.values()):
        raise ValueError("at least three raw paired single/split observations required")
    selected = receipt.get("measured_metric")
    if selected not in ("core_p95_ms", "drain_seconds", "received_to_settlement_p95_ms"):
        raise ValueError("select a real latency/drain metric, never worker count")
    try:
        off = statistics.median(row[selected] for row in measured["single"])
        worker = statistics.median(row[selected] for row in measured["split"])
    except (TypeError, KeyError):
        raise ValueError("selected benefit metric has unknown coverage")
    if not 0 <= worker < off or not receipt.get("confirmed_by") or not receipt.get("confirmed_at"):
        raise ValueError("positive measured gain and attributable declaration required before expansion")
    return dict(receipt=receipt, report_sha256=sha(report_path), recomputed=dict(metric=selected, in_process=off, manual_worker=worker),
                note="observed median difference is not a statistical or production guarantee")


def mib(text):
    match = re.fullmatch(r"([0-9.]+)([A-Za-z]+)", text.strip())
    factors = {"B": 1 / 1048576, "KiB": 1 / 1024, "MiB": 1, "GiB": 1024,
               "kB": 1000 / 1048576, "MB": 1000000 / 1048576, "GB": 1000000000 / 1048576}
    if not match or match[2] not in factors:
        raise ValueError("unknown resource unit, never interpreted as zero")
    return float(match[1]) * factors[match[2]]


def parse_top(text):
    rows = []
    for line in text.splitlines()[1:]:
        fields = line.split(None, 5)
        if len(fields) != 6:
            raise ValueError("incomplete process sample")
        pid, ppid, comm, rss, cpu, args = fields
        role = "worker" if any(arg in ("-projection-worker", "-projection-worker=true") for arg in args.split()) else "plugin" if "/plugins/sockets/run-" in args else "core" if comm == "zboard" else "observer"
        rows.append(dict(pid=int(pid), ppid=int(ppid), comm=comm, rss_kib=int(rss),
                         lifetime_cpu_percent=float(cpu), args=args, role=role))
    return rows


def supervisor_rows(text):
    pattern = r"local projection supervisor: running=(\d+) draining=(\d+) desired=(\d+) reason=([a-z_]+)"
    return [dict(running=int(a), draining=int(b), desired=int(c), reason=d) for a, b, c, d in re.findall(pattern, text)]


def runtime_numeric_metrics(output):
    """Decode actual admin observations, never infer SQL from probe timings.

    Overlapping rings are deduplicated by reset/sequence. Totals deltas require
    one Services instance and complete monotone counters; no cross-process sum.
    Full unmodified runtime rows remain in data/runtime-samples.jsonl.
    """
    path = output / "data/runtime-samples.jsonl"
    rows = [json.loads(line) for line in path.read_text().splitlines()] if path.exists() else []
    unknown = []
    stores, native, samples = {"claim": [], "finish": []}, [], {}
    def timestamp(value):
        return dt.datetime.fromisoformat(value.replace("Z", "+00:00"))
    def fresh(row, item, limit):
        try:
            age = (timestamp(row["at"]) - timestamp(item["observed_at"])).total_seconds()
            return 0 <= age <= limit
        except (KeyError, TypeError, ValueError):
            return False
    for row in rows:
        if row.get("status") != 200 or not isinstance(row.get("runtime"), dict):
            unknown.append("admin runtime observation unavailable")
            continue
        runtime = row["runtime"]
        job = runtime.get("job_store", {})
        if job.get("scope") == "this_process" and job.get("timing") == "durable_store_call":
            for operation in stores:
                item = job.get(operation, {})
                if (item.get("known") is True and item.get("sample_count", 0) > 0
                        and fresh(row, item, 30) and all(isinstance(item.get(k), (int, float))
                        and not isinstance(item.get(k), bool) and item[k] >= 0 for k in ("maximum_ms", "mean_ms", "p95_ms"))):
                    stores[operation].append(dict(at=row["at"], **item))
        batch = runtime.get("native_batch_sql", {})
        if (batch.get("scope") != "this_process" or batch.get("phase") != "native_authority_transaction"
                or batch.get("driver_begin_commit_included") is not False or batch.get("physical_bytes_measured") is not False):
            unknown.append("native authority instrumentation missing or incompatible")
            continue
        native.append(batch)
        if not batch.get("known") or not fresh(row, batch, 30) or batch.get("fresh_batch_count", 0) == 0:
            continue
        for item in batch.get("samples", []):
            key = (batch.get("reset_at"), item.get("sequence"))
            if key[0] is None or not isinstance(key[1], int) or key[1] <= 0:
                unknown.append("native batch identity missing")
                continue
            if key in samples and samples[key] != item:
                unknown.append("native batch sequence changed contents")
            samples[key] = item
    store_report = {}
    for operation, values in stores.items():
        if not values:
            unknown.append("no fresh measured durable Store." + operation + " calls")
        store_report[operation] = dict(known=bool(values), observations=len(values),
            maximum_observed_ms=max((v["maximum_ms"] for v in values), default=None),
            maximum_observed_window_p95_ms=max((v["p95_ms"] for v in values), default=None), windows=values)
    complete = [s for s in samples.values() if s.get("statistics_complete") is True]
    if not complete:
        unknown.append("no fresh complete native authority batch samples")
    resets = {b.get("reset_at") for b in native}
    delta = None
    # These are logical authority attempts, not physical IO or entire pipeline.
    if (len(native) >= 2 and len(resets) == 1 and None not in resets
            and all(b.get("totals_complete") is True and b.get("callback_failures") == 0 for b in native)):
        first, last = native[0], native[-1]
        keys = ("statement_count", "read_count", "write_count", "unknown_statement_count", "statement_errors",
                "observed_affected_write_rows", "unknown_write_rows", "text_blob_bound_argument_bytes", "source_payload_bytes")
        try:
            counters = {key: last["totals"][key] - first["totals"][key] for key in keys}
            batches = last["batches"] - first["batches"]
            if batches > 0 and all(v >= 0 for v in counters.values()) and all(
                    b["batches"] <= c["batches"] and all(b["totals"][key] <= c["totals"][key] for key in keys)
                    for b, c in zip(native, native[1:])):
                delta = dict(reset_at=last["reset_at"], batches=batches, counts=counters,
                    mean_statements_per_batch=counters["statement_count"] / batches,
                    affected_write_rows=None if counters["unknown_write_rows"] else counters["observed_affected_write_rows"])
        except (KeyError, TypeError):
            pass
    if delta is None:
        unknown.append("complete same-instance native cumulative delta unavailable")
    return dict(scope="this_process", phase="native_authority_transaction", driver_begin_commit_included=False,
        physical_database_write_bytes=None, argument_bytes_meaning="raw bound text/blob subset, not encoded transport/index/WAL/physical IO",
        child_process_metrics_included=False, job_store=store_report,
        native_batch_sql=dict(known=bool(complete), unique_sample_count=len(samples), complete_sample_count=len(complete),
            commit_confirmed_sample_count=sum(s.get("commit_confirmed") is True for s in complete),
            commit_unknown_sample_count=sum(s.get("commit_confirmed") is not True for s in complete),
            observed_samples=list(samples.values()), cumulative_delta=delta), unknown=sorted(set(unknown)))


def analyze(output, auto, scenario):
    """Only preserved observations decide status; unavailable coverage cannot pass."""
    failures, incomplete = [], []
    result = dict(status="inconclusive", failures=failures, incomplete=incomplete,
                  quota_stop=dict(status="external_required", reason="fixture has 1<<50 quota and no real kernel application/connection confirmation"))
    stats = [json.loads(line) for line in (output / "process-samples.jsonl").read_text().splitlines()]
    observations = [json.loads(line) for line in (output / "resources.jsonl").read_text().splitlines()]
    good = [row for row in stats if row.get("known")]
    if len(good) != len(stats) or len(good) < 2:
        incomplete.append("whole-cgroup/process coverage missing; unknown is not zero")
    else:
        peak = max(mib(row["stats"]["MemUsage"].split("/")[0]) for row in good)
        result["combined_sampled_peak_working_set_mib"] = peak
        if peak > WORKING_SET_MIB:
            failures.append("combined core+workers+native+observer working set exceeds unchanged 700MiB gate")
    known_resources = [r for r in observations if r.get("resources", {}).get("Known")]
    if len(known_resources) < 2 or len(known_resources) < len(observations) - 1:
        incomplete.append("production HostResources coverage is unknown/incomplete; no fabricated capacity admission")
    if known_resources and any(r["resources"].get("CPUCapacity", 0) < 4 for r in known_resources):
        incomplete.append("effective CPU capacity is below the separately declared 4CPU comparison budget")
    decisions = supervisor_rows((output / "server.log").read_text())
    worker_pids = {p["pid"] for row in good for p in row["processes"] if p["role"] == "worker"}
    result.update(supervisor_decisions=decisions, observed_worker_pids=sorted(worker_pids))
    if not auto and worker_pids:
        failures.append("autooff unexpectedly ran projection workers")
    if auto and scenario in ("backlog", "crash"):
        if not worker_pids or not any(d["reason"] == "healthy_sustained_backlog" for d in decisions):
            incomplete.append("no verified natural expansion under the unchanged production policy")
        if scenario == "backlog" and not (output / "shrink-result.json").exists():
            incomplete.append("idle shrink/drain and actual child exit not jointly observed")
        if scenario == "crash":
            crash = json.loads((output / "crash-result.json").read_text()) if (output / "crash-result.json").exists() else {}
            if not crash.get("signaled"):
                incomplete.append("no owned worker existed; crash was not executed")
            result["crash"] = crash
    if scenario == "dbslow":
        writes = [json.loads(line) for line in (output / "slowwrite.jsonl").read_text().splitlines()]
        if not writes or any(not w.get("committed") or w.get("held_seconds", 0) < .25 for w in writes):
            incomplete.append("actual committed SQLite write-lock pressure not established")
        reasons = {d["reason"] for d in decisions}
        if auto and not reasons.intersection({"database_slow_admission_or_completion", "database_error", "database_connection_reserve"}):
            incomplete.append("actual production DB-pressure response not observed")
        # Starting more than one worker is impossible by configured SQLite cap,
        # but repeated starts are checked separately instead of calling cap a gain.
        lines = (output / "server.log").read_text().splitlines()
        starts = [i for i, line in enumerate(lines) if "starting projection worker (" in line]
        pressure = [i for i, line in enumerate(lines) if any("reason="+reason in line for reason in ("database_slow_admission_or_completion", "database_error", "database_connection_reserve"))]
        result["all_worker_starts_in_dbslow_run"] = len(starts)
        if auto and pressure and writes:
            finish = dt.datetime.fromisoformat(writes[-1]["finished_at"].replace("Z", "+00:00"))
            late_starts = []
            for index in starts:
                if index <= pressure[0]:
                    continue
                match = re.search(r"(\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2})", lines[index])
                if not match:
                    incomplete.append("worker startup timestamp unavailable; pressure-window storm check unknown")
                    continue
                at = dt.datetime.strptime(match[1], "%Y/%m/%d %H:%M:%S").replace(tzinfo=dt.timezone.utc)
                if at <= finish:
                    late_starts.append(lines[index])
            result["worker_starts_after_confirmed_pressure_before_last_slow_commit"] = late_starts
            if late_starts:
                failures.append("worker expansion occurred after actual DB-pressure observation while slow-write experiment continued")
    if scenario == "pluginbusy":
        plugins = [p for row in good for p in row["processes"] if p["role"] == "plugin"]
        if not plugins or max(p["rss_kib"] for p in plugins) < 64 * 1024 or max(p["lifetime_cpu_percent"] for p in plugins) < 50:
            incomplete.append("configured busy plugin did not produce measured native CPU/RSS pressure")
        else:
            result["plugin_sampled_peak_rss_kib"] = max(p["rss_kib"] for p in plugins)
            result["plugin_peak_lifetime_average_cpu_percent"] = max(p["lifetime_cpu_percent"] for p in plugins)
    # The unchanged report provides HTTP p95, disk/spool gates, runtime receive
    # latency and exact ledger/projection reconciliation. Do not replace it.
    if json.loads((output / "load-exit.json").read_text()).get("returncode") != 0 or not json.loads((output / "reconciliation.json").read_text()).get("passed"):
        failures.append("load process or complete durable reconciliation failed")
    acceptance = json.loads((output / "acceptance-result.json").read_text())
    if not acceptance["passed"]:
        failures.extend(acceptance["failures"])
    result["original_gate_report"] = acceptance
    if auto and scenario == "crash":
        result["crash_work_reconciled"] = bool(result.get("crash", {}).get("signaled") and acceptance["passed"])
        result["distinct_replacement_worker_observed"] = len(worker_pids) > 1
    result["runtime_numeric_metrics"] = runtime_numeric_metrics(output)
    incomplete.extend(result["runtime_numeric_metrics"]["unknown"])
    result["unmeasured"] = {
        "physical_database_write_bytes": "not measured by logical text/blob argument counters",
        "worker_process_sql_and_store_metrics": "admin diagnostics cover the core process only; child counters are not exposed",
        "quota_exhaustion_to_confirmed_stop": "baseline has 1<<50 quota and no real kernel; not_exercised/external_required",
    }
    result["status"] = "failed" if failures else "inconclusive" if incomplete else "passed_internal_scope"
    return result


class Experiment:
    def __init__(self, root, output, image, scenario, auto, history, baseline=False):
        self.root, self.output, self.image = root, output, image
        self.scenario, self.auto, self.history, self.baseline = scenario, auto, history, baseline
        self.owner, self.volume, self.server, self.run_id = uuid.uuid4().hex, None, None, None
        self.containers, self.threads, self.children = [], [], []
        self.stop = threading.Event()
        self.load = None
        self.label = "zboard.acceptance.p6=" + self.owner

    def docker(self, *args):
        return command(["docker", *args])

    def persist_ownership(self):
        save(self.output / "owned-resources.json", dict(label=self.label, owner=self.owner,
             volume=self.volume, containers=self.containers, image=self.image, cleanup="none; all resources/evidence retained"))

    def verify_owned(self, identity):
        meta = json.loads(self.docker("inspect", identity))[0]
        if meta["Config"]["Labels"].get("zboard.acceptance.p6") != self.owner or meta["Image"] != self.image:
            raise ValueError("container ownership/image changed; no mutation admitted")
        return meta

    def verify_volume(self):
        if self.volume:
            meta = json.loads(self.docker("volume", "inspect", self.volume))[0]
            if (meta.get("Labels") or {}).get("zboard.acceptance.p6") != self.owner:
                raise ValueError("volume ownership changed; no fixture operation admitted")

    def logs(self):
        return subprocess.check_output(["docker", "logs", self.server], text=True, stderr=subprocess.STDOUT)

    def create(self, *args):
        self.verify_volume()
        identity = self.docker("create", "--label", self.label, *args)
        self.containers.append(identity)
        self.persist_ownership()
        return identity

    def helper(self, mode, *args):
        self.verify_owned(self.server)
        return self.docker("exec", self.server, "/acceptance-p6", "-mode", mode, "-dir", "/data",
                           "-owner", self.owner, "-run-id", self.run_id, *args)

    def request(self, method, path, body=None, raw=None):
        headers = {"Content-Type": "application/json"}
        if getattr(self, "token", None):
            headers["Authorization"] = "Bearer " + self.token
        if raw is not None:
            headers["Content-Type"] = "application/octet-stream"
        data = raw if raw is not None else json.dumps(body).encode() if body is not None else None
        with urllib.request.urlopen(urllib.request.Request(self.url + path, data=data, method=method, headers=headers), timeout=15) as response:
            return json.load(response)["data"]

    def sample(self):
        while not self.stop.is_set():
            row = dict(at=now(), known=False)
            try:
                self.verify_owned(self.server)
                row["stats"] = json.loads(self.docker("stats", "--no-stream", "--format", "{{json .}}", self.server))
                row["processes"] = parse_top(self.docker("top", self.server, "-eo", "pid,ppid,comm,rss,pcpu,args"))
                storage = self.docker("exec", self.server, "/acceptance", "-mode", "sample-storage", "-dir", "/data")
                with (self.output / "container-stats.jsonl").open("a") as f:
                    f.write(json.dumps(row["stats"]) + "\n")
                with (self.output / "storage.jsonl").open("a") as f:
                    f.write(storage + "\n")
                row["known"] = True
            except (OSError, ValueError, KeyError, subprocess.CalledProcessError) as error:
                row["error"] = str(error)
            with (self.output / "process-samples.jsonl").open("a") as f:
                f.write(json.dumps(row) + "\n")
            self.stop.wait(3)

    def start_exec(self, mode, filename, seconds):
        self.verify_owned(self.server)
        stream = (self.output / filename).open("x")
        process = subprocess.Popen(["docker", "exec", self.server, "/acceptance-p6", "-mode", mode,
             "-dir", "/data", "-owner", self.owner, "-run-id", self.run_id, "-seconds", str(seconds)],
             stdout=stream, stderr=(self.output / (filename + ".stderr")).open("x"))
        self.children.append((process, stream, mode))

    def run(self):
        self.output.mkdir(mode=0o700)
        data = self.output / "data"
        data.mkdir(mode=0o700)
        try:
            self.volume = self.docker("volume", "create", "--label", self.label)
            self.persist_ownership()
            seed = self.create("--network", "none", "--entrypoint", "/acceptance", "-v", self.volume + ":/data", self.image,
                "-mode", "seed", "-dir", "/data", "-nodes", "2", "-subscriptions", "20", "-records", str(self.history))
            with (self.output / "seed.log").open("x") as log:
                subprocess.run(["docker", "start", "-a", seed], stdout=log, stderr=subprocess.STDOUT, check=True)
            if self.verify_owned(seed)["State"]["ExitCode"] != 0:
                raise RuntimeError("isolated seed failed")
            self.docker("cp", seed + ":/data/fixture.json", data / "fixture.json")
            self.run_id = json.loads((data / "fixture.json").read_text())["run_id"]
            preparation = None
            if not self.baseline:
                preparation = self.create("--network", "none", "--entrypoint", "/acceptance-p6", "-v", self.volume + ":/data", self.image,
                    "-mode", "prepare", "-dir", "/data", "-owner", self.owner, "-run-id", self.run_id,
                    "-backlog=" + str(self.scenario != "normal").lower())
                with (self.output / "fixture-preparation.log").open("x") as log:
                    subprocess.run(["docker", "start", "-a", preparation], stdout=log, stderr=subprocess.STDOUT, check=True)
                if self.verify_owned(preparation)["State"]["ExitCode"] != 0:
                    raise RuntimeError("isolated source/fixture preparation failed")
                self.docker("cp", preparation + ":/data/p6busy.zip", self.output / "p6busy.zip")
            budget = BASELINE if self.baseline else COMPARISON
            args = ["--cpus", str(budget["nanocpus"] / 1e9), "--memory", str(budget["memory"]), "--memory-swap", str(budget["memory"]), "--network", "bridge", "-p", "127.0.0.1::8080",
                    "-e", "GOMAXPROCS=" + ("1" if self.baseline else "4"), "-v", self.volume + ":/data", self.image, "-f", "/data/zboard.json"]
            self.server = self.create(*(args + (SUPERVISOR if self.auto else [])))
            self.docker("start", self.server)
            self.url = "http://127.0.0.1:" + self.docker("port", self.server, "8080/tcp").split(":")[-1]
            for _ in range(120):
                try:
                    with urllib.request.urlopen(self.url + "/readyz", timeout=2) as response:
                        if response.status == 200:
                            break
                except OSError:
                    pass
                if not self.verify_owned(self.server)["State"]["Running"]:
                    raise RuntimeError("server exited before readiness")
                time.sleep(1)
            else:
                raise RuntimeError("readiness unavailable")
            storage = json.loads(self.docker("exec", self.server, "/acceptance", "-mode", "sample-storage", "-dir", "/data"))
            save(self.output / "storage-preflight.json", storage)
            if storage.get("disk_free_bytes", 0) < POST_RESERVE_GATE:
                raise RuntimeError("unchanged 1600MiB post-reserve gate failed; no load executed")
            fixture = json.loads((data / "fixture.json").read_text())
            if not self.baseline:
                self.token = self.request("POST", "/api/v1/auth/login", dict(email=fixture["admin_email"], password=fixture["admin_password"]))["auth"]["token"]
                installation = self.request("POST", "/api/v1/admin/plugins", raw=(self.output / "p6busy.zip").read_bytes())
                installation = self.request("POST", "/api/v1/admin/plugins/acceptance.p6busy/actions/enable", dict(generation=installation["generation"]))
                config = dict(cpu_burners=2, memory_mib=128) if self.scenario == "pluginbusy" else dict(cpu_burners=0, memory_mib=0)
                self.request("PUT", "/api/v1/admin/plugins/acceptance.p6busy/config", dict(revision=0, config=config))
                save(self.output / "plugin-configuration.json", config)
                self.start_exec("observe", "resources.jsonl", 900)
                if self.scenario == "dbslow":
                    self.start_exec("slowwrite", "slowwrite.jsonl", 60)
            sampler = threading.Thread(target=self.sample, daemon=True)
            sampler.start()
            self.threads.append(sampler)
            load_log = (self.output / "client.log").open("x")
            load = subprocess.Popen([sys.executable, str(self.root / "scripts/acceptance-load.py"), "--dir", str(data), "--url", self.url,
                 "--seconds", "60", "--rate", "20", "--readers", "4"], stdout=load_log, stderr=subprocess.STDOUT)
            self.load = load
            crashed = False
            while load.poll() is None:
                if self.auto and self.scenario == "crash" and not crashed:
                    try:
                        raw = self.helper("crash-worker")
                        save(self.output / "crash-result.json", json.loads(raw))
                        crashed = True
                    except subprocess.CalledProcessError as error:
                        # Only absence may defer the fault; permission/ownership
                        # or an uncertain signal result stops retries.
                        text = error.stderr or ""
                        if "no owned managed worker is running" not in text:
                            raise
                time.sleep(1)
            load_log.close()
            save(self.output / "load-exit.json", dict(returncode=load.returncode))
            self.docker("cp", data / "expected.json", self.server + ":/data/expected.json")
            drained, started = False, time.monotonic()
            # Extra observation is idle/drain time, never more offered load.
            deadline = time.monotonic() + 240
            with (self.output / "verification.log").open("x") as stream:
                while time.monotonic() < deadline:
                    check = subprocess.run(["docker", "exec", self.server, "/acceptance", "-mode", "verify", "-dir", "/data"], stdout=stream, stderr=subprocess.STDOUT)
                    if check.returncode == 0:
                        drained = True
                        if not self.auto or self.scenario not in ("backlog", "crash"):
                            break
                        logs = self.logs()
                        decisions = supervisor_rows(logs)
                        if decisions and decisions[-1]["reason"] == "idle_queue" and decisions[-1]["desired"] == 0 and decisions[-1]["running"] == 0 and decisions[-1]["draining"] == 0:
                            processes = parse_top(self.docker("top", self.server, "-eo", "pid,ppid,comm,rss,pcpu,args"))
                            if not any(p["role"] == "worker" for p in processes):
                                save(self.output / "shrink-result.json", dict(at=now(), supervisor=decisions[-1], actual_worker_pids=[], owner_verified=True))
                                break
                    time.sleep(5)
            (self.output / "drain-seconds.txt").write_text(str(int(time.monotonic() - started)))
            save(self.output / "reconciliation.json", dict(passed=drained))
        finally:
            self.stop.set()
            final_errors = []
            for thread in self.threads:
                thread.join(timeout=10)
            def retain(name, action):
                try:
                    return action()
                except (OSError, ValueError, subprocess.CalledProcessError, subprocess.TimeoutExpired) as error:
                    final_errors.append(dict(action=name, error=str(error), at=now()))
                    return None
            if self.load and self.load.poll() is None:
                # Only this runner's concrete load process is signaled. Never
                # locate/kill arbitrary Python, Docker, database or test PIDs.
                self.load.terminate()
                retain("stop own load generator", lambda: self.load.wait(timeout=20))
            if self.server:
                # A failed owner check halts dependent mutations, never changes
                # labels, steals a lock, or switches to another container.
                own = retain("verify container ownership", lambda: self.verify_owned(self.server))
                if own:
                    if self.children and own["State"]["Running"]:
                        retain("stop own observer", lambda: self.helper("stop-observer"))
                    retain("graceful own server stop", lambda: self.docker("stop", "-t", "35", self.server))
                    retain("preserve server log", lambda: (self.output / "server.log").write_text(self.logs()))
                    stopped = retain("inspect stopped server", lambda: self.verify_owned(self.server))
                    if stopped:
                        save(self.output / "container.json", [stopped])
                    if stopped and not stopped["State"]["Running"]:
                        retain("export own stopped DB/WAL/spool", lambda: self.docker("cp", self.server + ":/data/.", self.output / "data"))
                        def verify_spool():
                            spool = self.create("--network", "none", "--entrypoint", "/acceptance", "-v", self.volume + ":/data", self.image, "-mode", "verify-spool", "-dir", "/data")
                            with (self.output / "spool-verification.log").open("x") as stream:
                                subprocess.run(["docker", "start", "-a", spool], stdout=stream, stderr=subprocess.STDOUT, check=True)
                            if self.verify_owned(spool)["State"]["ExitCode"] != 0:
                                raise ValueError("isolated spool verification failed")
                            self.docker("cp", spool + ":/data/spool-verification.json", self.output / "data/spool-verification.json")
                        retain("verify preserved owned spool", verify_spool)
            elif self.containers:
                source = self.containers[-1]
                if retain("verify early fixture ownership", lambda: self.verify_owned(source)):
                    retain("export early partial DB", lambda: self.docker("cp", source + ":/data/.", self.output / "data"))
            for process, stream, role in self.children:
                code = retain("collect " + role + " terminal state", lambda: process.wait(timeout=15))
                stream.close()
                save(self.output / (role + "-exit.json"), dict(returncode=code, note="raw coverage is assessed separately; unknown exit is never success"))
            if (self.output / "data").exists():
                retain("hash preserved exports", lambda: save(self.output / "export-sha256.json", {
                    str(path.relative_to(self.output / "data")): sha(path)
                    for path in (self.output / "data").rglob("*") if path.is_file() and not path.is_symlink()
                }))
            self.persist_ownership()
            save(self.output / "retention.json", dict(errors=final_errors, removed_resources=[], note="all self-created images, containers, volumes, raw evidence and DB exports retained"))
            if final_errors:
                raise RuntimeError("finalization/ownership/export incomplete; inspect retention.json, no cleanup/retry performed")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--checkout", type=Path, required=True)
    parser.add_argument("--revision", required=True)
    parser.add_argument("--go-bin", default="go")
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument("--inspect", action="store_true")
    mode.add_argument("--prepare", action="store_true")
    mode.add_argument("--execute", type=Path, metavar="BUILD_RECEIPT")
    mode.add_argument("--execute-baseline", type=Path, metavar="BUILD_RECEIPT")
    parser.add_argument("--confirmed-benefit-receipt", type=Path)
    parser.add_argument("--history-records", type=int, default=2000, choices=(2000, 100000))
    parser.add_argument("--pairs", type=int, default=1, choices=(1, 2, 3))
    parser.add_argument("--scenarios", nargs="+", choices=SCENARIOS, default=list(SCENARIOS))
    args = parser.parse_args()
    os.umask(0o077)
    root = args.checkout.resolve()
    sources = require_source(root, args.revision)
    prepared = dict(revision=args.revision, sources=sources, baseline=BASELINE, additional_equal_total_budget=COMPARISON,
                    parameters=PARAMETERS, original_gate_bytes=POST_RESERVE_GATE, prepared=False)
    if not args.prepare and not args.execute and not args.execute_baseline:
        print(json.dumps({key: value for key, value in prepared.items() if key != "sources"} |
                         dict(status="not_executed", missing_prerequisites=["source-bound measured benefit at additional budget", "quiet manual/release window", "verifiable Linux cgroup v2 and sufficient disk"],
                              policy="all production defaults retained; baseline not replaced", quota_stop="real kernel fixture required")))
        return
    if sys.platform != "linux":
        raise ValueError("execution/preparation requires the selected Linux cloud environment")
    for row in command(["ps", "-eo", "state=,comm="]).splitlines():
        state, name = row.split(None, 1)
        if state != "Z" and (name in ("go", "compile", "link", "node", "pnpm", "mysql") or name.endswith((".test", ".tes", ".te", ".t"))):
            raise ValueError("another build/test workload is active; manual quiet window required")
    if os.statvfs(root).f_bavail * os.statvfs(root).f_frsize < POST_RESERVE_GATE:
        raise ValueError("host disk gate failed before build/experiment; no cleanup attempted")
    parent = root / ".codex-local-artifacts/acceptance"
    parent.mkdir(exist_ok=True, parents=True)
    output = Path(tempfile.mkdtemp(prefix="p6-manual-", dir=parent))
    if args.prepare:
        env = os.environ.copy()
        env.update(CGO_ENABLED="0", GOOS="linux", GOARCH="amd64", GOMAXPROCS="2")
        for name, package in (("zboard", "./cmd/zboard"), ("acceptance", "./tools/acceptance"), ("acceptance-p6", "./tools/acceptance-p6"), ("p6busy", "./internal/plugins/testdata/p6busy")):
            with (output / (name + "-build.log")).open("x") as stream:
                subprocess.run([args.go_bin, "build", "-p", "1", "-o", str(output / name), package], cwd=root / "backend", env=env, stdout=stream, stderr=subprocess.STDOUT, check=True)
        if source_manifest(root) != sources:
            raise ValueError("source changed while preparing; no experiment admitted")
        prepared.update(prepared=True, binaries={name: sha(output / name) for name in ("zboard", "acceptance", "acceptance-p6", "p6busy")})
        save(output / "build.json", prepared)
        print(json.dumps(dict(status="prepared_not_executed", build_receipt=str(output / "build.json"))))
        return
    docker_arch = command(["docker", "info", "--format", "{{.OSType}}/{{.Architecture}}"])
    if docker_arch not in ("linux/amd64", "linux/x86_64"):
        raise ValueError("this prepared native profile requires local Linux amd64 Docker")
    benefit = None
    if not args.execute_baseline:
        if not args.confirmed_benefit_receipt:
            raise ValueError("actual equal-budget benefit proof required; no experimental expansion admitted")
        benefit = benefit_receipt(args.confirmed_benefit_receipt.resolve(), args.revision, root)
    receipt_path = (args.execute or args.execute_baseline).resolve()
    built = json.loads(receipt_path.read_text())
    if not built.get("prepared") or built["revision"] != args.revision or built["sources"] != sources:
        raise ValueError("build receipt/source differs")
    for name, digest in built["binaries"].items():
        source = receipt_path.parent / name
        if sha(source) != digest:
            raise ValueError("binary changed")
        os.link(source, output / name)
    save(output / "admission.json", dict(benefit=benefit, parameters=PARAMETERS, additional_history_records=args.history_records,
                                        baseline_kept_separate=True, total_resource_budget=COMPARISON))
    (output / "Dockerfile").write_text('FROM scratch\nWORKDIR /tmp\nCOPY zboard /zboard\nCOPY acceptance /acceptance\nCOPY acceptance-p6 /acceptance-p6\nCOPY p6busy /p6busy\nENTRYPOINT ["/zboard"]\n')
    with (output / "image-build.log").open("x") as stream:
        subprocess.run(["docker", "build", "--label", "zboard.acceptance.p6-build=true", "--iidfile", str(output / "image.txt"), str(output)], stdout=stream, stderr=subprocess.STDOUT, check=True)
    image = (output / "image.txt").read_text().strip()
    report_spec = importlib.util.spec_from_file_location("original_acceptance_report", root / "scripts/acceptance-report.py")
    original_report = importlib.util.module_from_spec(report_spec)
    report_spec.loader.exec_module(original_report)
    if args.execute_baseline:
        directory = output / "baseline"
        Experiment(root, directory, image, "normal", False, 2000, baseline=True).run()
        result = original_report.evaluate(directory)
        if json.loads((directory / "load-exit.json").read_text()).get("returncode") != 0 or not json.loads((directory / "reconciliation.json").read_text()).get("passed"):
            result["failures"].append("load process or complete durable reconciliation failed")
            result["passed"] = False
        save(directory / "acceptance-result.json", result)
        save(output / "baseline.json", dict(revision=args.revision, parameters=PARAMETERS, budget=BASELINE,
             source_unchanged=source_manifest(root) == sources, original_gate_report=result, baseline_fixture_unmodified=True))
        print(json.dumps(dict(status="baseline_passed" if result["passed"] else "baseline_failed", evidence=str(output), benefit_verified=False)))
        raise SystemExit(0 if result["passed"] and source_manifest(root) == sources else 1)
    results = []
    for pair in range(args.pairs):
        for scenario in args.scenarios:
            for auto in ((False, True) if pair % 2 == 0 else (True, False)):
                directory = output / f'{pair}-{scenario}-auto{int(auto)}'
                started = resource.getrusage(resource.RUSAGE_SELF)
                children_started = resource.getrusage(resource.RUSAGE_CHILDREN)
                try:
                    Experiment(root, directory, image, scenario, auto, args.history_records).run()
                    original = original_report.evaluate(directory, expected_nanocpus=COMPARISON["nanocpus"], expected_memory=COMPARISON["memory"])
                    save(directory / "acceptance-result.json", original)
                    result = analyze(directory, auto, scenario)
                except (OSError, subprocess.CalledProcessError, KeyError, ValueError, RuntimeError) as error:
                    result = dict(status="failed_or_not_executed", error=str(error), benefit_verified=False)
                finished = resource.getrusage(resource.RUSAGE_SELF)
                result["driver_extra_cpu_seconds"] = (finished.ru_utime + finished.ru_stime) - (started.ru_utime + started.ru_stime)
                result["driver_peak_rss_kib"] = finished.ru_maxrss
                children_finished = resource.getrusage(resource.RUSAGE_CHILDREN)
                result["driver_child_extra_cpu_seconds"] = (children_finished.ru_utime + children_finished.ru_stime) - (children_started.ru_utime + children_started.ru_stime)
                result["driver_child_peak_rss_kib"] = children_finished.ru_maxrss
                result["cost_scope"] = "container budget includes core/workers/native/probes; external Python/Docker clients separate, Docker daemon cost not attributed"
                results.append(dict(pair=pair, scenario=scenario, auto=auto, evidence=str(directory), result=result))
                save(output / "comparison.json", dict(revision=args.revision, parameters=PARAMETERS, additional_total_resource_budget=COMPARISON,
                     runs=results, complete=False, benefit_verified=False, external_quota_stop="not_executed"))
                if result["status"] in ("failed", "failed_or_not_executed"):
                    raise SystemExit("experiment failed; no retries/cleanup, evidence: " + str(output))
    save(output / "comparison.json", dict(revision=args.revision, parameters=PARAMETERS, additional_total_resource_budget=COMPARISON, runs=results,
         complete=True, source_unchanged=source_manifest(root) == sources, benefit_verified=False,
         note="Compare observed p95/drain/receive-settlement latency and combined RSS/CPU. Worker starts are not a benefit verdict. Kernel stop acceptance remains external."))
    unchanged = source_manifest(root) == sources
    print(json.dumps(dict(status="measurements_preserved" if unchanged else "source_changed", evidence=str(output), benefit_verified=False)))
    if not unchanged:
        raise SystemExit(1)


if __name__ == "__main__":
    main()
