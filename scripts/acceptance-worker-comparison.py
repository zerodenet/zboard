#!/usr/bin/env python3
"""Compare the same binary with equal total resources; never enable autoscaling.

--prepare builds once and preserves a source/binary receipt. --execute consumes
that receipt, using fresh isolated SQLite fixtures for each profile. Neither
mode cleans build caches, existing containers, images, volumes or evidence.
"""
import argparse
import datetime
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import statistics
import subprocess
import sys
import tempfile
import threading
import time
import urllib.request
import uuid

PROFILES = {
    "single": [(1500000000, 1610612736)],
    "split": [(1000000000, 1073741824), (500000000, 536870912)],
}
GATE = 1677721600  # Same post-reserve gate as acceptance-mixed.sh.
PARAMETERS = {"nodes": 2, "subscriptions": 20, "history_records": 2000,
              "seconds": 60, "rate": 20, "readers": 4}
sys.dont_write_bytecode = True


def now():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def save(path, value):
    path.write_text(json.dumps(value, indent=2) + "\n")


def command(args, **kwargs):
    return subprocess.check_output(args, text=True, stderr=subprocess.PIPE, **kwargs).strip()


def sources(root):
    names = command(["git", "ls-files", "--cached", "--others", "--exclude-standard", "backend", "scripts"], cwd=root).splitlines()
    return {name: sha(root / name) for name in names
            if "__pycache__" not in Path(name).parts and (root / name).is_file()}


def validate_profiles():
    expected = (1500000000, 1610612736)
    if any(tuple(map(sum, zip(*rows))) != expected for rows in PROFILES.values()):
        raise ValueError("comparison profiles must have identical total CPU and memory")


def prepared(root, receipt):
    record = json.loads(receipt.read_text())
    if not record.get("prepared") or record["sources"] != sources(root):
        raise ValueError("prepared binaries do not match the current source tree")
    if record["revision"] != command(["git", "rev-parse", "HEAD"], cwd=root):
        raise ValueError("prepared revision changed")
    for name, digest in record["binaries"].items():
        if sha(receipt.parent / name) != digest:
            raise ValueError("prepared binary changed")
    return record


def resources(meta, expected):
    settings, state = meta["HostConfig"], meta["State"]
    return (settings["NanoCpus"] == expected[0]
            and settings["Memory"] == expected[1]
            and settings["MemorySwap"] == expected[1]
            and not state["OOMKilled"] and state["ExitCode"] == 0)


def mib(value):
    import re
    match = re.fullmatch(r"([0-9.]+)([A-Za-z]+)", value.strip())
    factors = {"B": 1 / 1048576, "KiB": 1 / 1024, "MiB": 1, "GiB": 1024,
               "kB": 1000 / 1048576, "MB": 1000000 / 1048576,
               "GB": 1000000000 / 1048576}
    if not match or match[2] not in factors:
        raise ValueError("unknown Docker memory unit")
    return float(match[1]) * factors[match[2]]


def evaluate(root, output, profile):
    spec = importlib.util.spec_from_file_location("comparison_report", root / "scripts/acceptance-report.py")
    report = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(report)
    expected = PROFILES[profile]
    result = report.evaluate(output, expected_nanocpus=expected[0][0], expected_memory=expected[0][1])
    failures = result["failures"]
    for index, budget in enumerate(expected):
        name = "container.json" if index == 0 else "worker.json"
        if not resources(json.loads((output / name).read_text())[0], budget):
            failures.append("process resource budget, OOM or graceful exit check failed")
    observations = [json.loads(line) for line in (output / "process-samples.jsonl").read_text().splitlines()]
    if not observations or any(row["error"] or len(row["stats"]) != len(expected) for row in observations):
        failures.append("process resource coverage incomplete; unknown is not zero")
    else:
        result["combined_peak_working_set_mib"] = max(sum(mib(s["MemUsage"].split("/")[0]) for s in row["stats"]) for row in observations)
        if result["combined_peak_working_set_mib"] > 700:
            failures.append("combined working set exceeded the unchanged 700MiB budget")
    result["profile"] = profile
    result["total_resource_budget"] = {"nanocpus": 1500000000, "memory_bytes": 1610612736}
    result["passed"] = not failures
    return result


def run_profile(root, parent, image, profile):
    output = Path(tempfile.mkdtemp(prefix=profile + "-", dir=parent))
    (output / "data").mkdir()
    label = "zboard.worker-comparison=" + uuid.uuid4().hex
    created, volume, main, worker = [], None, None, None
    stop = threading.Event()
    sampler = None
    exported = False
    result = {"profile": profile, "started_at": now(), "passed": False}

    def docker(*args):
        return command(["docker", *map(str, args)])

    def create(*args):
        identity = docker("create", "--label", label, *args)
        created.append(identity)
        save(output / "owned-resources.json", {"label": label, "volume": volume, "containers": created})
        return identity

    def attached(identity, log):
        with (output / log).open("x") as stream:
            code = subprocess.run(["docker", "start", "-a", identity], stdout=stream, stderr=subprocess.STDOUT).returncode
        if code or docker("inspect", "--format", "{{.State.ExitCode}}", identity) != "0":
            raise RuntimeError("isolated acceptance command failed: " + log)

    def stop_process(identity, log_name):
        docker("stop", "-t", "35", identity)
        log_file = "server.log" if log_name == "container" else log_name + ".log"
        with (output / log_file).open("x") as stream:
            subprocess.run(["docker", "logs", identity], stdout=stream, stderr=subprocess.STDOUT, check=True)
        (output / (log_name + ".json")).write_text(docker("inspect", identity))

    try:
        volume = docker("volume", "create", "--label", label)
        seed = create("--network", "none", "--entrypoint", "/acceptance", "-v", volume + ":/data", image,
                      "-mode", "seed", "-dir", "/data", "-nodes", PARAMETERS["nodes"],
                      "-subscriptions", PARAMETERS["subscriptions"], "-records", PARAMETERS["history_records"])
        attached(seed, "seed.log")
        docker("cp", seed + ":/data/fixture.json", output / "data/fixture.json")
        for index, (cpu, memory) in enumerate(PROFILES[profile]):
            args = ["--cpus", str(cpu / 1000000000), "--memory", str(memory), "--memory-swap", str(memory),
                    "-e", "GOMAXPROCS=" + ("2" if index == 0 else "1"), "-v", volume + ":/data"]
            args += ["--network", "bridge", "-p", "127.0.0.1::8080"] if index == 0 else ["--network", "none"]
            args += [image, "-f", "/data/zboard.json"]
            if index:
                args += ["-projection-worker", "-projection-worker-drain-timeout=30s"]
            identity = create(*args)
            docker("start", identity)
            if index == 0:
                main = identity
                port = docker("port", main, "8080/tcp").split(":")[-1]
                url = "http://127.0.0.1:" + port
                for attempt in range(120):
                    try:
                        with urllib.request.urlopen(url + "/readyz", timeout=2) as response:
                            if response.status == 200:
                                break
                    except OSError:
                        pass
                    if docker("inspect", "--format", "{{.State.Running}}", main) != "true":
                        raise RuntimeError("server exited before readiness")
                    time.sleep(1)
                else:
                    raise RuntimeError("server readiness not reached")
            else:
                worker = identity
        storage = json.loads(docker("exec", main, "/acceptance", "-mode", "sample-storage", "-dir", "/data"))
        save(output / "storage-preflight.json", storage)
        if storage.get("disk_free_bytes", 0) < GATE:
            raise RuntimeError("original post-reserve disk safety gate failed; no load started")
        processes = [main] + ([worker] if worker else [])

        def sample():
            while not stop.is_set():
                row = {"at": now(), "stats": [], "rss_kib": [], "error": None}
                try:
                    stats = [json.loads(line) for line in docker("stats", "--no-stream", "--format", "{{json .}}", *processes).splitlines()]
                    for identity in processes:
                        values = [s for s in stats if identity.startswith(s["ID"])]
                        if len(values) != 1:
                            raise RuntimeError("process resource identity unavailable")
                        row["stats"].append(values[0])
                        top = docker("top", identity, "-eo", "pid,comm,rss").splitlines()[1:]
                        rss = [s.split()[2] for s in top if len(s.split()) == 3 and s.split()[1] == "zboard"]
                        if len(rss) != 1:
                            raise RuntimeError("process RSS unavailable")
                        row["rss_kib"].append(int(rss[0]))
                    with (output / "container-stats.jsonl").open("a") as stream:
                        stream.write(json.dumps(row["stats"][0]) + "\n")
                    with (output / "storage.jsonl").open("a") as stream:
                        stream.write(docker("exec", main, "/acceptance", "-mode", "sample-storage", "-dir", "/data") + "\n")
                except (OSError, subprocess.CalledProcessError, ValueError, KeyError, RuntimeError) as error:
                    row["error"] = type(error).__name__
                with (output / "process-samples.jsonl").open("a") as stream:
                    stream.write(json.dumps(row) + "\n")
                stop.wait(3)

        sampler = threading.Thread(target=sample, daemon=True)
        sampler.start()
        with (output / "client.log").open("x") as stream:
            load = subprocess.run(["python3", str(root / "scripts/acceptance-load.py"), "--dir", str(output / "data"), "--url", url,
                                   "--seconds", str(PARAMETERS["seconds"]), "--rate", str(PARAMETERS["rate"]), "--readers", str(PARAMETERS["readers"])], stdout=stream, stderr=subprocess.STDOUT)
        docker("cp", output / "data/expected.json", main + ":/data/expected.json")
        started = time.monotonic()
        verified = False
        with (output / "verification.log").open("x") as stream:
            for attempt in range(24):
                if subprocess.run(["docker", "exec", main, "/acceptance", "-mode", "verify", "-dir", "/data"], stdout=stream, stderr=subprocess.STDOUT).returncode == 0:
                    verified = True
                    break
                time.sleep(5)
        (output / "drain-seconds.txt").write_text(str(int(time.monotonic() - started)))
        stop.set()
        sampler.join()
        if worker:
            stop_process(worker, "worker")
        stop_process(main, "container")
        check = create("--network", "none", "--entrypoint", "/acceptance", "-v", volume + ":/data", image, "-mode", "verify-spool", "-dir", "/data")
        attached(check, "spool-verification.log")
        docker("cp", main + ":/data/.", output / "data")
        exported = True
        result = evaluate(root, output, profile)
        if load.returncode or not verified:
            result["failures"].append("load or final accounting verification failed")
            result["passed"] = False
    except (OSError, subprocess.CalledProcessError, ValueError, KeyError, RuntimeError) as error:
        result["error"] = type(error).__name__ + ": " + str(error) if isinstance(error, RuntimeError) else type(error).__name__
    finally:
        stop.set()
        if sampler:
            sampler.join()
        if not exported and main:
            try:
                for identity in ([worker] if worker else []) + [main]:
                    docker("stop", "-t", "35", identity)
                docker("cp", main + ":/data/.", output / "data")
                exported = True
            except (OSError, subprocess.CalledProcessError):
                pass
        # Only this invocation's positively identified fixtures are disposable,
        # and only after preserving their database. Unknown outcomes remain.
        cleaned = False
        if exported:
            try:
                key, value = label.split("=", 1)
                for identity in created:
                    meta = json.loads(docker("inspect", identity))[0]
                    if meta["Config"]["Labels"].get(key) != value:
                        raise RuntimeError("owned container label changed")
                    docker("stop", "-t", "35", identity)
                    docker("rm", identity)
                meta = json.loads(docker("volume", "inspect", volume))[0]
                if meta["Labels"].get(key) != value:
                    raise RuntimeError("owned volume label changed")
                docker("volume", "rm", volume)
                cleaned = True
            except (OSError, subprocess.CalledProcessError, ValueError, KeyError, RuntimeError) as error:
                result["cleanup_error"] = type(error).__name__
        result.update(finished_at=now(), database_exported=exported, owned_resources_cleaned=cleaned)
        if not cleaned:
            result["passed"] = False
        save(output / "result.json", result)
    return output, result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--checkout", type=Path, required=True)
    parser.add_argument("--revision", required=True)
    parser.add_argument("--go-bin", default="go")
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument("--prepare", action="store_true")
    mode.add_argument("--execute", type=Path, metavar="BUILD_RECEIPT")
    parser.add_argument("--pairs", type=int, default=3, choices=range(1, 4))
    args = parser.parse_args()
    os.umask(0o077)
    validate_profiles()
    root = args.checkout.resolve()
    if command(["git", "rev-parse", "HEAD"], cwd=root) != args.revision:
        parser.error("exact requested revision required")
    if command(["git", "status", "--porcelain", "--untracked-files=no", "--", "backend", "scripts"], cwd=root):
        parser.error("tracked application and acceptance sources must be clean")
    unknown = command(["git", "ls-files", "--others", "--exclude-standard", "backend", "scripts"], cwd=root).splitlines()
    if any("__pycache__" not in Path(name).parts for name in unknown):
        parser.error("uncommitted application or acceptance sources cannot be bound to a revision")
    receipt = {"revision": args.revision, "sources": sources(root), "profiles": PROFILES,
               "parameters": PARAMETERS, "original_post_reserve_gate_bytes": GATE, "prepared": False}
    if not args.prepare and not args.execute:
        print(json.dumps({key: value for key, value in receipt.items() if key != "sources"}
                         | {"source_files": len(receipt["sources"]),
                            "source_manifest_sha256": hashlib.sha256(json.dumps(receipt["sources"], sort_keys=True).encode()).hexdigest()}))
        return
    for row in command(["ps", "-eo", "state=,comm="]).splitlines():
        state, name = row.split(None, 1)
        if state != "Z" and (name in ["go", "compile", "link", "node", "pnpm", "mysql"]
                             or name.endswith((".test", ".tes", ".te", ".t"))):
            parser.error("another local build/test workload is active")
    parent = root / ".codex-local-artifacts/acceptance"
    parent.mkdir(parents=True, exist_ok=True)
    output = Path(tempfile.mkdtemp(prefix="worker-comparison-", dir=parent))
    if args.prepare:
        env = os.environ.copy()
        env.update(CGO_ENABLED="0", GOOS="linux", GOARCH="amd64", GOMAXPROCS="2")
        for name, package in [("zboard", "./cmd/zboard"), ("acceptance", "./tools/acceptance")]:
            with (output / (name + "-build.log")).open("x") as stream:
                result = subprocess.run([args.go_bin, "build", "-p", "1", "-o", str(output / name), package], cwd=root / "backend", env=env, stdout=stream, stderr=subprocess.STDOUT)
            if result.returncode:
                save(output / "build.json", receipt)
                raise SystemExit("build failed; preserved evidence: " + str(output))
        if receipt["sources"] != sources(root):
            raise SystemExit("sources changed during build; no execution admitted")
        receipt.update(prepared=True, binaries={name: sha(output / name) for name in ["zboard", "acceptance"]})
        save(output / "build.json", receipt)
        print(json.dumps({"prepared": True, "build_receipt": str(output / "build.json")}))
        return
    bound = prepared(root, args.execute.resolve())
    for name in bound["binaries"]:
        os.link(args.execute.resolve().parent / name, output / name)
    (output / "Dockerfile").write_text("FROM scratch\nWORKDIR /tmp\nCOPY zboard /zboard\nCOPY acceptance /acceptance\nENTRYPOINT [\"/zboard\"]\n")
    with (output / "image-build.log").open("x") as stream:
        code = subprocess.run(["docker", "build", "--label", "zboard.acceptance.worker-comparison=true", "--iidfile", str(output / "image.txt"), str(output)], stdout=stream, stderr=subprocess.STDOUT).returncode
    if code:
        raise SystemExit("image build failed; evidence preserved: " + str(output))
    image = (output / "image.txt").read_text().strip()
    runs = []
    for pair in range(args.pairs):
        for profile in (["single", "split"] if pair % 2 == 0 else ["split", "single"]):
            directory, result = run_profile(root, output, image, profile)
            runs.append({"directory": str(directory), "profile": profile, "result": result})
            save(output / "comparison.json", {"revision": args.revision, "parameters": PARAMETERS, "runs": runs, "complete": False})
            if not result["passed"]:
                raise SystemExit("profile failed; no retries or benefit claim: " + str(directory))
    unchanged = (sources(root) == bound["sources"]
                 and command(["git", "rev-parse", "HEAD"], cwd=root) == args.revision)
    # Report measurements; starting another process is never a benefit verdict.
    metrics = {profile: {"median_drain_seconds": statistics.median(r["result"]["drain_seconds"] for r in runs if r["profile"] == profile),
                         "median_combined_peak_working_set_mib": statistics.median(r["result"]["combined_peak_working_set_mib"] for r in runs if r["profile"] == profile)} for profile in PROFILES}
    save(output / "comparison.json", {"revision": args.revision, "parameters": PARAMETERS, "runs": runs, "complete": True,
                                      "source_unchanged": unchanged, "measurements": metrics, "benefit_verified": False,
                                      "note": "Inspect request latency, DB waits, RSS, CPU and settling costs together. No autoscaling permission is inferred."})
    print(json.dumps({"complete": True, "source_unchanged": unchanged, "comparison": str(output / "comparison.json"), "benefit_verified": False}))
    if not unchanged:
        raise SystemExit(1)


if __name__ == "__main__":
    main()
