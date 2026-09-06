#!/usr/bin/env python3
"""Evaluate preserved mixed-load evidence; failing budgets are never warnings."""
import argparse
import json
import pathlib
import re


def evaluate(directory):
    load = json.loads((directory / "data/load-result.json").read_text())
    ledger = json.loads((directory / "data/verification.json").read_text())
    spool = json.loads((directory / "data/spool-verification.json").read_text())
    container = json.loads((directory / "container.json").read_text())[0]
    samples = [json.loads(line) for line in (directory / "container-stats.jsonl").read_text().splitlines()]
    failures = []
    latencies = []
    for path, metric in load["metrics"].items():
        if path.endswith("/auth/login"):
            continue  # One setup request is not a percentile population.
        budget = 1000 if "/traffic/trends" in path or "/traffic/reconciliation" in path else 250
        passed = metric["count"] >= 20 and metric["errors"] == 0 and metric["p95_ms"] <= budget
        latencies.append({"path": path, "budget_ms": budget, **metric, "passed": passed})
        if not passed:
            failures.append(f'{path}: count={metric["count"]}, errors={metric["errors"]}, p95={metric["p95_ms"]}ms, budget={budget}ms')
    def mib(value):
        match = re.fullmatch(r"([0-9.]+)([A-Za-z]+)", value.strip())
        if not match:
            raise ValueError(f"unknown Docker memory unit: {value}")
        factors = {"B": 1 / 1048576, "KiB": 1 / 1024, "MiB": 1, "GiB": 1024,
                   "kB": 1000 / 1048576, "MB": 1000000 / 1048576, "GB": 1000000000 / 1048576}
        return float(match[1]) * factors[match[2]]
    peak = max((mib(sample["MemUsage"].split("/")[0]) for sample in samples), default=0)
    if not samples or peak > 700:
        failures.append(f"sampled peak working set {peak:.2f}MiB; budget=700MiB")
    settings, state = container["HostConfig"], container["State"]
    if settings["NanoCpus"] != 1000000000 or settings["Memory"] != 1073741824 or settings["MemorySwap"] != 1073741824:
        failures.append("container did not use the exact 1CPU/1GiB/no-extra-swap profile")
    if state["OOMKilled"] or state["ExitCode"] != 0:
        failures.append(f'container exit={state["ExitCode"]}, OOM={state["OOMKilled"]}')
    if not ledger["passed"] or not spool["passed"]:
        failures.append("ledger mismatch or unconsumed durable events")
    if load["generator_dropped"] or load["undelivered_jobs"] or any(row["errors"] for row in load["metrics"].values()):
        failures.append("load generator dropped jobs or HTTP requests failed")
    storage = [json.loads(line) for line in (directory / "storage.jsonl").read_text().splitlines()]
    free = [row.get("disk_free_bytes") for row in storage]
    if not free or any(value is None or value < 1536 * 1024 * 1024 for value in free):
        failures.append("missing Linux disk samples or insufficient normal-pressure disk headroom")
    pressures = sorted(set(re.findall(r"spool pressure changed: level=(\w+)", (directory / "server.log").read_text())))
    if any(level != "normal" for level in pressures):
        failures.append("spool entered disk/storage pressure; this is not a normal-pressure capacity run")
    runtime_path = directory / "data/runtime-samples.jsonl"
    runtime_rows = [json.loads(line) for line in runtime_path.read_text().splitlines()] if runtime_path.exists() else []
    runtime = [row["runtime"] for row in runtime_rows if row.get("status") == 200 and isinstance(row.get("runtime"), dict)]
    diagnostics = {}
    if any(row["errors"] for row in load.get("diagnostic_metrics", {}).values()):
        failures.append("runtime diagnostic HTTP requests failed")
    if len(runtime) < 2 or len(runtime) != len(runtime_rows):
        failures.append("runtime observation missing or failed; connection wait and processing latency are unverified")
    elif any(not isinstance(row.get("event_consumer"), dict) or not isinstance(row.get("event_spool"), dict) for row in runtime):
        failures.append("event consumer/spool diagnostics unavailable")
    else:
        diagnostics = {"samples": len(runtime), "last_consumer": runtime[-1]["event_consumer"],
                       "maximum_sampled_pending_events": max(row["event_spool"]["pending_events"] for row in runtime),
                       "database_wait_deltas": {}}
        for pool in ("accounting_db", "traffic_read_db"):
            diagnostics["database_wait_deltas"][pool] = {
                key: runtime[-1][pool][key] - runtime[0][pool][key]
                for key in ("wait_count", "wait_seconds")
            }
    return {"passed": not failures, "failures": failures, "latencies": latencies,
            "sampled_peak_working_set_mib": peak, "resource_samples": len(samples),
            "minimum_disk_free_bytes": min((value for value in free if value is not None), default=None),
            "spool_pressure_levels": pressures,
            "runtime_diagnostics": diagnostics,
            "drain_seconds": int((directory / "drain-seconds.txt").read_text()),
            "note": "Client-observed HTTP latency includes Docker loopback transport; this short run does not prove 24h stability."}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("directory", type=pathlib.Path)
    args = parser.parse_args()
    result = evaluate(args.directory)
    (args.directory / "acceptance-result.json").write_text(json.dumps(result, indent=2) + "\n")
    if not result["passed"]:
        raise SystemExit("acceptance failed:\n" + "\n".join(result["failures"]))


if __name__ == "__main__":
    main()
