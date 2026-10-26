#!/usr/bin/env python3
"""Build exact old/current isolated probes, preserve old backup, and rehearse upgrade.

No service, worker, external payment or production migration is started. This
controller preserves every result and never retries a failed run automatically.
"""
import argparse
import datetime
import hashlib
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile
import time

BASELINE = "7c7283fb34840883fccaba1de6e0a98edb24dfb1"

def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()

def now():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()

def write(path, data):
    with path.open("x") as output:
        json.dump(data, output, indent=2)
        output.write("\n")
    path.chmod(0o600)

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--checkout", type=Path, required=True)
    parser.add_argument("--revision", required=True)
    parser.add_argument("--driver", choices=("sqlite", "mysql", "both"), required=True)
    parser.add_argument("--go-bin", default="go")
    parser.add_argument("--execute", action="store_true")
    parser.add_argument("--mysql-container", help="explicit existing test container; its loopback port must match the opt-in test DSN")
    args = parser.parse_args()
    os.umask(0o077)
    repo = args.checkout.resolve()
    templates = Path(__file__).resolve().parent
    def git(*arguments):
        return subprocess.check_output(["git", *arguments], cwd=repo, text=True).strip()
    if git("rev-parse", "HEAD") != args.revision or git("status", "--porcelain"):
        parser.error("exact clean revision required; no existing source is reset")
    subprocess.run(["git", "merge-base", "--is-ancestor", BASELINE, args.revision], cwd=repo, check=True)
    names = ("old_seed_probe.go.txt", "current_upgrade_probe.go.txt", "snapshot_probe.go.txt", "restore_probe.go.txt")
    metadata = {"baseline": BASELINE, "current": args.revision, "driver": args.driver,
                "templates": {name: digest(templates / name) for name in names},
                "controller_sha256": digest(Path(__file__).resolve()), "execute": args.execute}
    if not args.execute:
        print(json.dumps(metadata)); return 0
    for line in subprocess.check_output(["ps", "-eo", "state,comm"], text=True).splitlines()[1:]:
        parts = line.split()
        if len(parts) == 2 and parts[0] != "Z" and (parts[1] in ("go", "compile", "link", "node", "pnpm") or parts[1].endswith(".test")):
            parser.error("another local workload is active; no duplicate run")
    if args.driver in ("mysql", "both") and (not os.environ.get("ZBOARD_TEST_MYSQL_DSN") or not args.mysql_container):
        parser.error("explicit isolated loopback test DSN required")
    root = repo / ".codex-local-artifacts" / "acceptance"
    root.mkdir(parents=True, exist_ok=True)
    out = Path(tempfile.mkdtemp(prefix="upgrade-" + args.driver + "-", dir=root))
    metadata.update(started_utc=now(), output=str(out), commands=[], status="pending")
    write(out / "started.json", metadata)
    env = os.environ.copy()
    env.update(GOMAXPROCS="2", GOFLAGS="-p=1")
    if args.driver == "sqlite": env.pop("ZBOARD_TEST_MYSQL_DSN", None)
    env.update(ZBOARD_UPGRADE_FIXTURE_DIR=str(out), ZBOARD_UPGRADE_FIXTURE_DRIVER=args.driver)
    env["ZBOARD_UPGRADE_MYSQL_CONTAINER"] = args.mysql_container or ""
    env.pop("ZBOARD_UPGRADE_RECOVERY", None)
    def run(label, command, cwd):
        entry = {"label": label, "command": command, "started_utc": now()}
        start = time.monotonic()
        with (out / (label + ".log")).open("x") as stream:
            result = subprocess.run(command, cwd=cwd, env=env, stdout=stream, stderr=subprocess.STDOUT)
        entry.update(exit_code=result.returncode, wall_seconds=round(time.monotonic()-start, 3), log_sha256=digest(out / (label+".log")), finished_utc=now())
        metadata["commands"].append(entry)
        write(out / (label + "-command.json"), entry)
        if result.returncode: raise RuntimeError(label + " failed; preserve original log")
    processes = []
    try:
        for label, revision, template in (("old", BASELINE, names[0]), ("current", args.revision, names[1])):
            archive = subprocess.check_output(["git", "archive", revision, "backend"], cwd=repo)
            destination = out / (label + "-source")
            destination.mkdir()
            with tarfile.open(fileobj=io.BytesIO(archive)) as tar:
                for member in tar.getmembers():
                    path = Path(member.name)
                    if path.is_absolute() or ".." in path.parts or not (member.isdir() or member.isfile()):
                        raise RuntimeError("unsafe archive member; no extraction")
                tar.extractall(destination, filter="data")
            base = destination / "backend"
            product_files = {str(p.relative_to(base)): digest(p) for p in base.rglob("*") if p.is_file()}
            metadata[label + "_source"] = {"revision": revision, "archive_sha256": hashlib.sha256(archive).hexdigest(), "files": product_files}
            for name, target in ((template, "convergence_version_probe_test.go"), (names[2], "convergence_snapshot_probe_test.go")):
                path = base / "internal" / "handler" / target
                with path.open("xb") as stream: stream.write((templates/name).read_bytes())
            if label == "old":
                path = base / "internal" / "handler" / "convergence_restore_probe_test.go"
                with path.open("xb") as stream:
                    stream.write((templates / names[3]).read_bytes())
            run(label+"-build", [args.go_bin, "test", "-c", "-p", "1", "-mod=readonly", "-buildvcs=false", "-o", str(out/(label+".test")), "./internal/handler"], base)
            metadata[label + "_binary_sha256"] = digest(out/(label+".test"))
        metadata["driver_results"] = {}
        mysql_dsn = env.get("ZBOARD_TEST_MYSQL_DSN")
        drivers = ("sqlite", "mysql") if args.driver == "both" else (args.driver,)
        for driver in drivers:
            fixture = out / driver if args.driver == "both" else out
            fixture.mkdir(exist_ok=True)
            prefix = driver + "-" if args.driver == "both" else ""
            env.update(ZBOARD_UPGRADE_FIXTURE_DIR=str(fixture), ZBOARD_UPGRADE_FIXTURE_DRIVER=driver)
            env.pop("ZBOARD_UPGRADE_RECOVERY", None)
            if driver == "sqlite":
                env.pop("ZBOARD_TEST_MYSQL_DSN", None)
            else:
                env["ZBOARD_TEST_MYSQL_DSN"] = mysql_dsn
            result = {"status": "pending", "output": str(fixture)}
            metadata["driver_results"][driver] = result
            write(fixture / "driver-started.json", {"driver": driver, "started_utc": now()})
            old_command = [str(out/"old.test"), "-test.run", "^TestConvergenceOldVersionFixture$", "-test.v", "-test.timeout", "5m"]
            if driver == "sqlite":
                run(prefix+"old-seed", old_command, fixture)
                backup = fixture/"old-source.db"
                shutil.copyfile(backup, fixture/"current.db")
            else:
                old_log = (out/(prefix+"old-seed.log")).open("x")
                process = subprocess.Popen(old_command, cwd=fixture, env=env, stdout=old_log, stderr=subprocess.STDOUT)
                processes.append((process, old_log, fixture))
                deadline = time.monotonic()+120
                while not (fixture/"old-ready.json").exists():
                    if process.poll() is not None: raise RuntimeError("old MySQL fixture stopped before snapshot; preserve log")
                    if time.monotonic() > deadline: raise RuntimeError("old MySQL snapshot readiness expired")
                    time.sleep(0.2)
                backup = fixture/"old-source.sql"
            result["old_backup_sha256"] = digest(backup)
            if args.driver != "both":
                metadata["old_backup_sha256"] = result["old_backup_sha256"]
            run(prefix+"current-upgrade", [str(out/"current.test"), "-test.run", "^TestConvergenceUpgradeFromOldVersion$", "-test.v", "-test.timeout", "5m"], fixture)
            env["ZBOARD_UPGRADE_RECOVERY"] = "1"
            run(prefix+"current-recovery", [str(out/"current.test"), "-test.run", "^TestConvergenceUpgradeFromOldVersion$", "-test.v", "-test.timeout", "5m"], fixture)
            run(prefix+"old-backup-restore", [str(out/"old.test"), "-test.run", "^TestConvergenceRestoreOldBackup$", "-test.v", "-test.timeout", "5m"], fixture)
            result["old_backup_restore"] = json.loads((fixture / "restore-result.json").read_text())
            if digest(backup) != result["old_backup_sha256"]:
                raise RuntimeError("immutable old backup changed")
            result.update(status="passed", backup_unchanged=True)
            write(fixture / "driver-final.json", result)
        for label in ("old", "current"):
            base = out/(label+"-source")/"backend"
            if any(digest(base/name) != value for name,value in metadata[label+"_source"]["files"].items()):
                raise RuntimeError("archived product source changed during probe")
        metadata["backup_unchanged"] = True
        metadata["status"] = "passed"
    except Exception as error:
        metadata["status"] = "failed"
        metadata["error"] = type(error).__name__ + ": " + str(error)
        for result in metadata.get("driver_results", {}).values():
            if result["status"] == "pending":
                result.update(status="failed", error=metadata["error"])
    finally:
        for process, log, fixture in processes:
            # Only this test's random schema is eligible for its normal fixture
            # cleanup. The old SQL backup and all logs remain preserved.
            (fixture/"snapshot-complete").touch(exist_ok=False)
            try:
                result = process.wait(timeout=150)
            except subprocess.TimeoutExpired:
                result = process.poll()
                metadata["status"] = "failed"
                metadata["old_seed_outcome"] = "uncertain; inspect the original owned process before another run"
                metadata["old_seed_pid"] = process.pid
            log.close()
            metadata["old_seed_exit_code"] = result
            metadata["old_seed_log_sha256"] = digest(Path(log.name))
            if result != 0: metadata["status"] = "failed"
        metadata["finished_utc"] = now()
        metadata["primary_worktree_clean"] = not git("status", "--porcelain")
        write(out/"final.json", metadata)
    print(json.dumps({key:metadata.get(key) for key in ("status","output","baseline","current","driver","error","backup_unchanged","primary_worktree_clean")}))
    return 0 if metadata["status"] == "passed" else 1

if __name__ == "__main__":
    raise SystemExit(main())
