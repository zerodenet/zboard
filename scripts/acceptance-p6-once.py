#!/usr/bin/env python3
"""One temporary VM, existing P6 gates, private raw evidence and safe public metrics."""
import argparse
import hashlib
import importlib.util
import json
import math
import os
from pathlib import Path
import platform
import re
import subprocess
import sys
import time

sys.dont_write_bytecode = True
SCENARIOS = ("normal", "backlog", "crash", "dbslow", "pluginbusy")
PARAMETERS = dict(nodes=2, subscriptions=20, history_records=2000, seconds=60, rate=20, readers=4)
STAGES = ("prepare-autoscale", "prepare-comparison", "baseline", "comparison", "autoscale")
GATE = 1600 << 20


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def load(path):
    if path.is_symlink() or not path.is_file() or not 1 <= path.stat().st_size <= 32 * 1024 * 1024:
        raise ValueError("invalid_private_evidence")
    return json.loads(path.read_text())


def save(path, data):
    with path.open("x") as stream:
        json.dump(data, stream, indent=2, allow_nan=False); stream.write("\n")


def numeric(value):
    if type(value) is int:
        return value if 0 <= value <= 2**64 - 1 else None
    return value if type(value) is float and math.isfinite(value) and value >= 0 else None


def module(root, filename, name):
    spec = importlib.util.spec_from_file_location(name, root / "scripts" / filename)
    result = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(result)
    return result


def resource_values(nproc, affinity, mem_available, cpu_limits, memory_limits, host_free, docker_free, v2):
    cpu = min([nproc, affinity] + [quota / period for quota, period in cpu_limits])
    memory = min([mem_available] + [max(0, limit - current) for limit, current in memory_limits])
    return dict(cgroup_v2=v2 is True, nproc=nproc, affinity_cpus=affinity, effective_cpu_capacity=cpu,
                available_memory_bytes=memory, checkout_free_bytes=host_free, docker_free_bytes=docker_free,
                admitted=v2 is True and cpu >= 4 and memory >= 4 * 1024**3 and min(host_free, docker_free) >= GATE)


def cgroup_limits(mount, current):
    cpu_limits, memory_limits = [], []
    while True:
        cpu = current / "cpu.max"
        memory = current / "memory.max"
        # The real cgroup-v2 root is unbounded and need not expose max files;
        # missing limits below that root remain unknown, never assumed free.
        if cpu.exists():
            quota, period = cpu.read_text().split()
            if quota != "max": cpu_limits.append((int(quota), int(period)))
        elif current != mount:
            raise ValueError("cpu_limit_unknown")
        if memory.exists():
            maximum = memory.read_text().strip()
            if maximum != "max": memory_limits.append((int(maximum), int((current / "memory.current").read_text())))
        elif current != mount:
            raise ValueError("memory_limit_unknown")
        if current == mount: return cpu_limits, memory_limits
        current = current.parent


def read_resources(root):
    # Read-only local probes. No network, sudo, mount, resource acquisition or
    # configuration mutation; unknown cgroup/Docker observations refuse entry.
    nproc = int(subprocess.check_output(["nproc"], text=True, stderr=subprocess.PIPE).strip())
    affinity = len(os.sched_getaffinity(0))
    mem = {row.split(":", 1)[0]: int(row.split()[1]) * 1024 for row in Path("/proc/meminfo").read_text().splitlines()}
    group = next(row.split("::", 1)[1] for row in Path("/proc/self/cgroup").read_text().splitlines() if row.startswith("0::"))
    mount = Path("/sys/fs/cgroup")
    current = mount / group.lstrip("/")
    if not (mount / "cgroup.controllers").is_file() or not current.resolve().is_relative_to(mount.resolve()) or not current.is_dir():
        raise ValueError("cgroup_v2_unavailable")
    cpu_limits, memory_limits = cgroup_limits(mount, current)
    docker_root = json.loads(subprocess.check_output(["docker", "info", "--format", "{{json .DockerRootDir}}"], text=True, stderr=subprocess.PIPE))
    def free(path):
        value = os.statvfs(path)
        return value.f_bavail * value.f_frsize
    result = resource_values(nproc, affinity, mem["MemAvailable"], cpu_limits, memory_limits, free(root), free(docker_root), True)
    result["linux_amd64"] = sys.platform == "linux" and platform.machine() in ("x86_64", "amd64")
    result["admitted"] = result["admitted"] and result["linux_amd64"]
    return result


def stage(root, private, name, arguments):
    evidence_parent = root / ".codex-local-artifacts/acceptance"
    before = set(evidence_parent.iterdir()) if evidence_parent.exists() else set()
    started = time.monotonic()
    with (private / (name + ".stdout")).open("xb") as stdout, (private / (name + ".stderr")).open("xb") as stderr:
        code = subprocess.run(arguments, cwd=root, stdout=stdout, stderr=stderr).returncode
    created = sorted(set(evidence_parent.iterdir()) - before)
    try: response = load(private / (name + ".stdout"))
    except (OSError, ValueError, TypeError): response = {}
    result = dict(name=name, returncode=code, elapsed_seconds=time.monotonic()-started,
                  created=[str(p.resolve()) for p in created], response=response)
    save(private / (name + ".json"), result)
    return result


def owned(stage_result, path):
    path = Path(path)
    if not path.is_absolute() or path.is_symlink() or not path.is_file() or len(stage_result["created"]) != 1:
        raise ValueError("unowned_stage_evidence")
    if path.resolve().parent != Path(stage_result["created"][0]):
        raise ValueError("unowned_stage_evidence")
    return path.resolve()


def build_receipt(stage_result, expected, revision, sources):
    path = owned(stage_result, stage_result["response"]["build_receipt"])
    record = load(path)
    if record.get("revision") != revision or record.get("prepared") is not True or record.get("sources") != sources or set(record.get("binaries", {})) != set(expected):
        raise ValueError("build_source_binding_failed")
    if any(sha(path.parent / name) != digest for name, digest in record["binaries"].items()):
        raise ValueError("build_binary_binding_failed")
    return path, record


def reason_class(text):
    # Finite classifications only, never arbitrary exception/log/SQL/path text.
    for pattern, name in (("403", "dependency_or_network_access_denied"), ("Permission denied", "permission_denied"),
                          ("Operation not permitted", "permission_denied"), ("Forbidden", "permission_denied"),
                          ("count=", "http_population_latency_or_errors"), ("working set", "working_set_gate"),
                          ("disk", "disk_gate"), ("OOM", "process_exit_or_oom"), ("ledger", "ledger_or_spool"),
                          ("spool", "ledger_or_spool"), ("natural expansion", "expansion_unobserved"),
                          ("shrink", "shrink_unobserved"), ("crash", "crash_unobserved"),
                          ("DB-pressure", "database_pressure_unconfirmed"), ("plugin", "plugin_pressure_unconfirmed"),
                          ("coverage", "observation_coverage_incomplete"), ("unknown", "observation_unknown")):
        if pattern in text: return name
    return "original_assertion_failed_or_incomplete"


ENDPOINTS = {
    "/api/v1/admin/users?paged=true&limit=50&offset=0": "users",
    "/api/v1/admin/subscriptions?paged=true&limit=50&offset=0": "subscriptions",
    "/api/v1/admin/subscriptions/1": "detail",
    "/api/v1/admin/traffic/records?paged=true&bucket=hour&limit=50&offset=0": "hour",
    "/api/v1/admin/traffic/trends": "trends",
    "/api/v1/admin/traffic/reconciliation?paged=true&limit=50&offset=0": "reconciliation",
    "/api/zero/events": "event_ingest",
}


def json_lines(path):
    try:
        if path.is_symlink() or not path.is_file() or path.stat().st_size > 32 * 1024 * 1024:
            return [], False
        rows = [json.loads(line) for line in path.read_text().splitlines()]
        return (rows, True) if rows and all(isinstance(row, dict) for row in rows) else ([], False)
    except (OSError, ValueError): return [], False


def series(values, covered):
    valid = [value for value in values if numeric(value) is not None]
    known = covered and len(values) >= 2 and len(valid) == len(values)
    return dict(known=known, observed_samples=len(valid), expected_samples=len(values) if covered else None,
                first=values[0] if known else None, last=values[-1] if known else None,
                peak=max(values) if known else None, growth=values[-1]-values[0] if known else None)


def runtime_cost(raw):
    job, native = raw.get("job_store", {}), raw.get("native_batch_sql", {})
    delta = native.get("cumulative_delta")
    delta = delta if isinstance(delta, dict) else {}
    counts = delta.get("counts", {})
    compatible = raw.get("scope") == "this_process" and raw.get("phase") == "native_authority_transaction" and raw.get("driver_begin_commit_included") is False
    known = native.get("known") is True and compatible
    keys = ("statement_count", "read_count", "write_count", "unknown_statement_count", "statement_errors",
            "observed_affected_write_rows", "unknown_write_rows", "text_blob_bound_argument_bytes", "source_payload_bytes")
    return dict(scope="this_process_only_store_call_not_sql_lock_latency_logical_bytes_not_physical_writes",
        physical_database_write_bytes=None,
        job_store={operation: dict(known=compatible and job.get(operation, {}).get("known") is True,
            **{key: numeric(job.get(operation, {}).get(key)) if compatible and job.get(operation, {}).get("known") is True else None
               for key in ("observations", "maximum_observed_ms", "maximum_observed_window_p95_ms")}) for operation in ("claim", "finish")},
        native_batch_sql=dict(known=known,
            **{key: numeric(native.get(key)) if compatible else None for key in ("unique_sample_count", "complete_sample_count", "commit_confirmed_sample_count", "commit_unknown_sample_count")},
            cumulative_delta=dict(batches=numeric(delta.get("batches")) if known else None,
                mean_statements_per_batch=numeric(delta.get("mean_statements_per_batch")) if known else None,
                affected_write_rows=numeric(delta.get("affected_write_rows")) if known else None,
                counts={key: numeric(counts.get(key)) if known else None for key in keys})))


def _case_cost(directory, auto=None):
    storage, storage_ok = json_lines(directory / "storage.jsonl")
    preflight = directory / "storage-preflight.json"
    if preflight.is_file():
        try: storage.insert(0, load(preflight))
        except (OSError, ValueError, TypeError): storage_ok = False
    def file_value(row, name):
        files = row.get("files", {})
        if not isinstance(files, dict): return None
        if name != "spool": return numeric(files.get(name))
        values = [numeric(value) for key, value in files.items() if isinstance(key, str) and key.startswith("events/")]
        return sum(values) if values and all(value is not None for value in values) else None
    samples, process_ok = json_lines(directory / "process-samples.jsonl")
    cpu, role_rss, role_cpu = [], {k: [] for k in ("core", "worker", "plugin", "observer")}, {k: [] for k in ("core", "worker", "plugin", "observer")}
    for row in samples:
        healthy = row.get("known") is True if "known" in row else ("stats" in row and row.get("error") is None)
        stats = row.get("stats", [])
        stats = [stats] if isinstance(stats, dict) else stats
        percents = []
        for value in stats if isinstance(stats, list) else []:
            raw = value.get("CPUPerc", "")
            percents.append(float(raw[:-1]) if isinstance(raw, str) and re.fullmatch(r"[0-9]+(?:\.[0-9]+)?%", raw) else None)
        cpu.append(sum(percents) if healthy and percents and all(v is not None for v in percents) else None)
        processes = row.get("processes")
        if isinstance(processes, list):
            for role in role_rss:
                values = [p for p in processes if p.get("role") == role]
                rss = [numeric(p.get("rss_kib")) for p in values]
                lifetime = [numeric(p.get("lifetime_cpu_percent")) for p in values]
                role_rss[role].append(sum(rss) if healthy and rss and all(v is not None for v in rss) else None)
                role_cpu[role].append(sum(lifetime) if healthy and lifetime and all(v is not None for v in lifetime) else None)
        else:
            rss = row.get("rss_kib", [])
            for index, role in enumerate(role_rss):
                role_rss[role].append(numeric(rss[index]) if healthy and role in ("core", "worker") and len(rss) in (1, 2) and index < len(rss) else None)
                role_cpu[role].append(None)
    sql = {}
    reader = getattr(auto, "runtime_numeric_metrics", None)
    if callable(reader):
        try: sql = reader(directory)
        except (OSError, ValueError, TypeError, KeyError, AttributeError): pass
    return dict(storage_scope="sampled_logical_file_lengths_not_physical_writes",
        storage={name: series([file_value(row, filename) for row in storage], storage_ok) for name, filename in
                 (("database_bytes", "zboard.db"), ("wal_bytes", "zboard.db-wal"), ("shm_bytes", "zboard.db-shm"), ("spool_bytes", "spool"))},
        process_scope="sampled_docker_cpu_percent_and_process_rss_lifetime_cpu_not_instantaneous",
        process=dict(docker_cpu_percent=series(cpu, process_ok),
            roles={role: dict(rss_kib=series(role_rss[role], process_ok), lifetime_cpu_percent=series(role_cpu[role], process_ok)) for role in role_rss}),
        sql=runtime_cost(sql), quota_exhaustion_to_kernel_stop="not_exercised")


def case_cost(directory, auto=None):
    # Cost coverage is observational only: a missing/malformed optional sample
    # never changes any original pass/fail gate or creates a zero measurement.
    try:
        return _case_cost(directory, auto)
    except (OSError, ValueError, KeyError, TypeError, AttributeError, OverflowError):
        return dict(known=False, reason="cost_observation_unavailable", storage=None, process=None,
                    sql=runtime_cost({}), quota_exhaustion_to_kernel_stop="not_exercised")


def safe_runtime_diagnostics(raw):
    raw = raw if isinstance(raw, dict) else {}
    consumer = raw.get("last_consumer", {})
    consumer = consumer if isinstance(consumer, dict) else {}
    count = numeric(consumer.get("latency_samples"))
    any_latency = count is not None and count > 0
    pools = raw.get("database_wait_deltas", {})
    return dict(samples=numeric(raw.get("samples")), maximum_sampled_pending_events=numeric(raw.get("maximum_sampled_pending_events")),
        receive_to_settlement_scope="received_to_accounting_and_durable_spool_checkpoint_coarse_upper_bucket",
        last_consumer={key: numeric(consumer.get(key)) for key in ("batches", "processed_envelopes", "projected_flow_samples", "coalesced_flow_updates",
            "failures", "processing_seconds", "missing_receive_time", "latency_samples", "latency_bucket_ms")} |
            dict(p95_upper_ms=numeric(consumer.get("p95_upper_ms")) if any_latency else None,
                 maximum_ms=numeric(consumer.get("maximum_ms")) if any_latency else None),
        database_wait_scope="connection_pool_wait_not_sql_or_lock_latency",
        database_wait_deltas={pool: {key: numeric(pools.get(pool, {}).get(key)) for key in ("wait_count", "wait_seconds")} for pool in ("accounting_db", "traffic_read_db")})


def safe_gate(gate):
    return dict(passed=gate.get("passed") is True,
        reasons=sorted({reason_class(x) for x in gate.get("failures", []) if isinstance(x, str)}),
        **{key: numeric(gate.get(key)) for key in ("sampled_peak_working_set_mib", "minimum_disk_free_bytes", "drain_seconds", "resource_samples")},
        latency_populations=[{key: numeric(row.get(key)) for key in ("count", "errors", "p95_ms", "budget_ms")} |
            {"passed": row.get("passed") is True, "endpoint": ENDPOINTS.get(row.get("path"), "unknown")} for row in gate.get("latencies", [])],
        runtime_diagnostics=safe_runtime_diagnostics(gate.get("runtime_diagnostics", {})))


def measurement_summary(record, auto=None):
    """Positive allowlist of complete or partial observations, never a verdict."""
    if len(record.get("created", [])) != 1 or record["name"] not in ("baseline", "comparison", "autoscale"):
        return None
    directory = Path(record["created"][0])
    path = directory / ("baseline.json" if record["name"] == "baseline" else "comparison.json")
    if not path.is_file(): return None
    try:
        raw = load(path)
        if record["name"] == "baseline":
            return dict(report_sha256=sha(path), gate=safe_gate(raw.get("original_gate_report", {})), cost=case_cost(directory / "baseline", auto))
        rows = []
        for row in raw.get("runs", []):
            value = row.get("result", {})
            item = dict(gate=safe_gate(value.get("original_gate_report", value)),
                core_p95_ms=numeric(value.get("core_p95_ms")), drain_seconds=numeric(value.get("drain_seconds")),
                combined_peak_working_set_mib=numeric(value.get("combined_peak_working_set_mib")))
            if record["name"] == "comparison":
                item["profile"] = row.get("profile") if row.get("profile") in ("single", "split") else "unknown"
            else:
                messages = value.get("failures", []) + value.get("incomplete", [])
                if isinstance(value.get("error"), str): messages.append(value["error"])
                item["reasons"] = sorted({reason_class(x) for x in messages if isinstance(x, str)})
                item.update(scenario=row.get("scenario") if row.get("scenario") in SCENARIOS else "unknown",
                    pair=row.get("pair") if type(row.get("pair")) is int and 0 <= row["pair"] < 3 else None,
                    auto=row.get("auto") if type(row.get("auto")) is bool else None,
                    status=value.get("status") if value.get("status") in ("passed_internal_scope", "failed", "inconclusive", "failed_or_not_executed") else "unknown")
            evidence = Path(row.get("directory" if record["name"] == "comparison" else "evidence", ""))
            if evidence.is_absolute() and not evidence.is_symlink() and evidence.resolve().parent == directory.resolve():
                item["cost"] = case_cost(evidence, auto)
            rows.append(item)
        return dict(report_sha256=sha(path), complete=raw.get("complete") is True, runs=rows)
    except (OSError, ValueError, TypeError, KeyError, AttributeError):
        return None


def retained_fixture(directory):
    # Inspect/hash no configuration values. Actual raw material remains private.
    for relative in ("data/zboard.db", "data/zboard.json", "data/fixture.json", "server.log"):
        path = directory / relative
        if path.is_symlink() or not path.is_file() or path.stat().st_size == 0:
            raise ValueError("private_raw_fixture_missing")


def verify_final(root, revision, tools, sources, auto_build, baseline_stage, comparison_stage, auto_stage):
    auto, worker, report = tools
    baseline_path = owned(baseline_stage, Path(baseline_stage["response"]["evidence"]) / "baseline.json")
    baseline = load(baseline_path)
    retained_fixture(baseline_path.parent / "baseline")
    baseline_gate = report.evaluate(baseline_path.parent / "baseline")
    if baseline.get("revision") != revision or baseline.get("source_unchanged") is not True or baseline.get("original_gate_report") != baseline_gate or not baseline_gate["passed"]:
        raise ValueError("baseline_final_revalidation_failed")
    benefit_path = owned(comparison_stage, comparison_stage["response"]["benefit_receipt"])
    comparison_report_path = Path(load(benefit_path)["comparison_report"])
    if comparison_report_path.resolve().parent != benefit_path.parent:
        raise ValueError("unowned_comparison_evidence")
    for row in load(comparison_report_path).get("runs", []):
        directory = Path(row["directory"])
        if directory.is_symlink() or directory.resolve().parent != benefit_path.parent:
            raise ValueError("unowned_comparison_evidence")
        retained_fixture(directory)
    benefit = auto.benefit_receipt(benefit_path, revision, root)
    if benefit["recomputed"]["metric"] != "core_p95_ms":
        raise ValueError("prespecified_metric_changed")
    path = owned(auto_stage, auto_stage["response"]["comparison_report"])
    recorded = load(path)
    if recorded.get("revision") != revision or sha(path) != auto_stage["response"].get("comparison_sha256"):
        raise ValueError("autoscale_report_binding_failed")
    derived = []
    for row in recorded.get("runs", []):
        if type(row.get("pair")) is not int or not 0 <= row["pair"] < 3 or row.get("scenario") not in SCENARIOS or type(row.get("auto")) is not bool:
            raise ValueError("invalid_scenario_identity")
        directory = Path(row["evidence"])
        if directory.is_symlink() or directory.resolve().parent != path.parent:
            raise ValueError("unowned_scenario_evidence")
        retained_fixture(directory)
        gate = report.evaluate(directory, expected_nanocpus=auto.COMPARISON["nanocpus"], expected_memory=auto.COMPARISON["memory"])
        value = auto.analyze(directory, row["auto"], row["scenario"])
        if value.get("original_gate_report") != gate:
            raise ValueError("scenario_gate_revalidation_failed")
        retention = load(directory / "retention.json")
        if retention.get("errors") or retention.get("removed_resources") != []:
            raise ValueError("private_retention_failed")
        derived.append(dict(pair=row["pair"], scenario=row["scenario"], auto=row["auto"], evidence=str(directory), result=value))
    binaries = all(sha(path.parent / name) == digest for name, digest in auto_build["binaries"].items())
    unchanged = auto.require_source(root, revision) == sources
    verdict = auto.internal_scope_verdict(derived, unchanged, binaries, benefit == auto.benefit_receipt(benefit_path, revision, root))
    cases = []
    for row in derived:
        value = row["result"]
        cases.append(dict(pair=row["pair"], scenario=row["scenario"], auto=row["auto"],
            status=value["status"] if value["status"] in ("passed_internal_scope", "failed", "inconclusive") else "failed",
            reasons=sorted({reason_class(x) for x in value.get("failures", []) + value.get("incomplete", [])}),
            gate=safe_gate(value["original_gate_report"]), cost=case_cost(Path(row["evidence"]), auto),
            **{key: numeric(value.get(key)) for key in ("combined_sampled_peak_working_set_mib", "plugin_sampled_peak_rss_kib", "plugin_peak_lifetime_average_cpu_percent")}))
    return dict(status=verdict["status"], complete=verdict["complete"], run_count=verdict["run_count"],
        all_scenarios_passed=verdict["all_scenarios_passed"], source_unchanged=unchanged, binaries_unchanged=binaries,
        benefit_prerequisite_revalidated=verdict["benefit_prerequisite_revalidated"], benefit_verified=verdict["benefit_verified"],
        baseline_report_sha256=sha(baseline_path), comparison_report_sha256=sha(Path(load(benefit_path)["comparison_report"])),
        benefit_receipt_sha256=sha(benefit_path), autoscale_report_sha256=sha(path), baseline=safe_gate(baseline_gate),
        baseline_cost=case_cost(baseline_path.parent / "baseline", auto),
        metric="core_p95_ms", in_process_median_ms=numeric(benefit["recomputed"]["in_process"]),
        independent_worker_median_ms=numeric(benefit["recomputed"]["manual_worker"]), cases=cases)


def execute(root, revision, private, resource_probe=read_resources, runner=stage, final_verifier=verify_final):
    result = dict(version=1, scope="p6_internal_one_temporary_vm", source_head=revision, status="not_executed",
        parameters=PARAMETERS, selected_metric="core_p95_ms", stages=[], raw_evidence_uploaded=False,
        final_raw_consumer_completed=False, raw_deletion_performed=False, raw_lifetime="temporary_vm_only", new_loopback_isolated_fixtures=True,
        external_quota_stop="not_verified", external_payment_kernel_production="not_executed")
    if not re.fullmatch(r"[0-9a-f]{40}", revision) or private.exists() or private.resolve().is_relative_to(root.resolve()):
        raise ValueError("fresh_outside_checkout_evidence_required")
    private.mkdir(mode=0o700, parents=False)
    try:
        try:
            resources = resource_probe(root)
        except (OSError, ValueError, TypeError, AttributeError, StopIteration, subprocess.SubprocessError):
            result.update(status="admission_failed", reason="resource_observation_unavailable"); return result
        result["resources"] = resources
        if resources.get("admitted") is not True:
            result.update(status="admission_failed", reason="original_resource_prerequisites_unmet"); return result
        tools = (module(root, "acceptance-autoscale-manual.py", "p6_original_auto"),
                 module(root, "acceptance-worker-comparison.py", "p6_original_comparison"),
                 module(root, "acceptance-report.py", "p6_original_report"))
        auto, worker, _ = tools
        sources = auto.require_source(root, revision)
        result["source_manifest_sha256"] = hashlib.sha256(json.dumps(sources, sort_keys=True).encode()).hexdigest()
        common = ["--checkout", str(root), "--revision", revision]
        def run(name, filename, arguments):
            record = runner(root, private, name, [sys.executable, str(root / "scripts" / filename), *common, *arguments])
            result["stages"].append(dict(name=name, returncode=record["returncode"], elapsed_seconds=numeric(record["elapsed_seconds"]),
                status="completed" if record["returncode"] == 0 else "failed"))
            measured = measurement_summary(record, auto)
            if measured is not None: result[name + "_measurements"] = measured
            if record["returncode"] != 0:
                error = private / (name + ".stderr")
                if error.is_file(): result["stages"][-1]["reason"] = reason_class(error.read_text(errors="replace")[:65536])
                raise ValueError("original_stage_failed")
            return record
        prepared = run("prepare-autoscale", "acceptance-autoscale-manual.py", ["--prepare"])
        auto_path, auto_build = build_receipt(prepared, ("zboard", "acceptance", "acceptance-p6", "p6busy"), revision, sources)
        prepared_worker = run("prepare-comparison", "acceptance-worker-comparison.py", ["--budget-profile", "autoscale-4cpu", "--prepare"])
        worker_path, worker_build = build_receipt(prepared_worker, ("zboard", "acceptance"), revision, sources)
        if any(auto_build["binaries"][key] != worker_build["binaries"][key] for key in worker_build["binaries"]):
            raise ValueError("cross_stage_binary_identity_failed")
        result["binaries"] = auto_build["binaries"]
        baseline = run("baseline", "acceptance-autoscale-manual.py", ["--execute-baseline", str(auto_path)])
        comparison = run("comparison", "acceptance-worker-comparison.py", ["--budget-profile", "autoscale-4cpu", "--pairs", "3", "--confirm-benefit-metric", "core_p95_ms", "--execute", str(worker_path)])
        receipt = comparison["response"].get("benefit_receipt")
        if comparison["response"].get("benefit_verdict") != "positive_measurement_receipt_created" or not receipt:
            result.update(status="inconclusive", reason="no_positive_prespecified_metric_gain"); return result
        if auto.benefit_receipt(owned(comparison, receipt), revision, root)["recomputed"]["metric"] != "core_p95_ms":
            raise ValueError("prespecified_metric_changed")
        scaled = run("autoscale", "acceptance-autoscale-manual.py", ["--execute", str(auto_path), "--confirmed-benefit-receipt", receipt,
            "--pairs", "3", "--history-records", "2000", "--scenarios", *SCENARIOS])
        result["verification"] = final_verifier(root, revision, tools, sources, auto_build, baseline, comparison, scaled)
        result["status"] = result["verification"]["status"]
        result["final_raw_consumer_completed"] = True
    except (OSError, ValueError, KeyError, TypeError, AttributeError, StopIteration, subprocess.SubprocessError):
        result.update(status="failed", reason="original_gate_or_source_or_raw_evidence_verification_failed")
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--checkout", type=Path, required=True)
    parser.add_argument("--revision", required=True)
    parser.add_argument("--private-root", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--execute", action="store_true")
    args = parser.parse_args()
    os.umask(0o077)
    root = args.checkout.resolve()
    if (not args.output.is_absolute() or args.output.resolve().is_relative_to(root)
            or args.output.exists() or args.output.is_symlink()):
        print(json.dumps(dict(status="admission_failed", scope="p6_internal_one_temporary_vm",
                              reason="fresh_outside_checkout_summary_required")), flush=True)
        raise SystemExit(1)
    try:
        result = execute(root, args.revision, args.private_root) if args.execute else dict(status="not_executed", scope="p6_internal_one_temporary_vm")
    except (OSError, ValueError, TypeError):
        result = dict(status="admission_failed", scope="p6_internal_one_temporary_vm", reason="fresh_evidence_or_source_admission_failed")
    try:
        args.output.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
        save(args.output, result)
    except OSError:
        result = dict(status="failed", scope="p6_internal_one_temporary_vm", reason="safe_summary_write_failed")
    print(json.dumps(result, allow_nan=False), flush=True)
    raise SystemExit(0 if result["status"] == "passed_internal_scope" else 1)


if __name__ == "__main__": main()
