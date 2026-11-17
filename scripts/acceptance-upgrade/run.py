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
import re
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

def failure_diagnostics(path):
    """Report source positions only; leave arbitrary SQL/payloads in the log."""
    locations = set()
    with path.open(errors="replace") as stream:
        for line in stream:
            for match in re.finditer(r"\b([A-Za-z0-9_]+(?:_test)?\.go):(\d+)(?::\d+)?:", line):
                locations.add((match[1], int(match[2])))
    return [{"file": name, "line": number} for name, number in sorted(locations)[:20]]

def failure_error_classes(path):
    """Only fixed public error classes from assertions, never their payloads."""
    known = {
        "UNIQUE constraint failed": "sqlite_unique_constraint",
        "FOREIGN KEY constraint failed": "sqlite_foreign_key",
        "CHECK constraint failed": "sqlite_check_constraint",
        "no such table": "sqlite_missing_table",
        "no such column": "sqlite_missing_column",
        "database is locked": "sqlite_locked",
        "order funding conflict": "funding_conflict",
        "wallet facts unavailable": "wallet_unavailable",
        "wallet amount exceeds supported integer range": "wallet_range",
        "payment fact conflict": "payment_fact_conflict",
        "order requires current active buyer": "buyer_unavailable",
        "commerce requires current administrator": "administrator_required",
        "order transition rejected": "order_transition",
        "discount quote unavailable": "discount_quote_unavailable",
        "invalid discount quote": "discount_quote_invalid",
        "discount reservation is not yet confirmed": "discount_reservation_pending",
        "discount reservation evidence conflicts": "discount_reservation_conflict",
        "subscription value evidence is incomplete or inconsistent": "value_evidence_inconsistent",
        "subscription value requires purchase/payment reconciliation": "value_reconciliation",
        "subscription value quote changed": "value_quote_changed",
        "subscription quota cycle reset is pending": "quota_reset_pending",
        "trusted recurring purchase": "recurring_payment_phase",
        "checkout admission:": "checkout_admission_phase",
    }
    classes = set()
    with path.open(errors="replace") as stream:
        for line in stream:
            if not re.search(r"\b[A-Za-z0-9_]+_test\.go:\d+:", line):
                continue
            classes.update(label for fragment, label in known.items() if fragment in line)
            for match in re.finditer(r"\bError (1045|1049|1054|1062|1146|1205|1213|1406|1451|1452|3140|3819)\b", line):
                classes.add("mysql_" + match[1])
            match = re.search(r"\bcreate topup: (400|401|403|409|500|503)\b", line)
            if match:
                classes.add("topup_http_" + match[1])
    return sorted(classes)

def existing_parent(path):
    value = path.resolve()
    while not value.exists():
        if value.parent == value: raise RuntimeError("no existing filesystem parent")
        value = value.parent
    return value

def disk_budget(repo, artifacts, args, git):
    mib = 1024 * 1024
    source = {}
    for revision in (BASELINE, args.revision):
        total = 0
        for line in git("ls-tree", "-r", "-l", revision, "backend").splitlines():
            fields = line.partition("\t")[0].split()
            if len(fields) != 4 or fields[1] != "blob" or not fields[3].isdigit():
                raise RuntimeError("unexpected archived backend tree entry")
            total += int(fields[3])
        source[revision] = total
    goenv = subprocess.check_output([args.go_bin, "env", "GOCACHE", "GOTMPDIR"], cwd=repo, text=True).splitlines()
    cache = goenv[0] if goenv else ""
    buildtmp = goenv[1] if len(goenv) > 1 and goenv[1] else tempfile.gettempdir()
    drivers = 2 if args.driver == "both" else 1
    categories = [("new_artifacts", artifacts, sum(source[r] for r in (BASELINE,args.revision)) + 16*mib + 2*args.binary_budget_mib*mib + drivers*128*mib + 32*mib),
                  ("compiler_temporary", Path(buildtmp), args.build_temp_budget_mib*mib)]
    if cache and cache != "off": categories.append(("cache_growth_estimate", Path(cache), args.cache_budget_mib*mib))
    if args.driver in ("mysql", "both"):
        # Conservatively reserve here too: a container overlay/data directory
        # may share the checkout/cache host filesystem despite another namespace.
        categories.append(("mysql_possible_host_colocation",repo,args.mysql_growth_budget_mib*mib))
    groups = {}
    for label,path,needed in categories:
        parent = existing_parent(path); device = str(parent.stat().st_dev)
        group = groups.setdefault(device, {"paths":[], "categories":{}, "free_bytes":shutil.disk_usage(parent).free, "required_bytes":args.reserve_mib*mib})
        group["paths"].append(str(path)); group["categories"][label] = needed; group["required_bytes"] += needed
    for group in groups.values(): group["sufficient"] = group["free_bytes"] >= group["required_bytes"]
    mysql = None
    if args.driver in ("mysql", "both") and args.mysql_container:
        # Read the explicit test data filesystem, not an unrelated host path.
        lines = subprocess.check_output(["docker", "exec", args.mysql_container, "df", "-Pk", "/var/lib/mysql"], text=True).splitlines()
        fields = lines[-1].split()
        available = int(fields[3])*1024
        required = (args.mysql_growth_budget_mib+args.reserve_mib)*mib
        mysql = {"path":"explicit isolated container:/var/lib/mysql", "free_bytes":available, "required_bytes":required, "sufficient":available>=required}
    return {"backend_blob_bytes":source, "filesystems":list(groups.values()), "mysql_data_filesystem":mysql,
            "scope":"Planning estimates for two new source exports/binaries, new fixture backups, compiler temporary/cache growth and test data. Not a hard write quota, throughput/capacity gate, physical shrink or production guarantee. No existing data/cache/backup is moved or cleaned."}

def release_old_mysql_fixture(process, log, fixture, metadata):
    """Release only this run's old fixture after its immutable backup exists.

    No downstream probe may use the old schema. Failure is never retried or
    converted into successful handoff; the fixture's original2m owner deadline
    and the old test's original5m timeout remain unchanged.
    """
    release = fixture / "snapshot-complete"
    if not release.exists(): release.touch(exist_ok=False)
    try:
        result = process.wait(timeout=150)
    except subprocess.TimeoutExpired as error:
        metadata["old_seed_outcome"] = "uncertain; inspect the original owned process before another run"
        metadata["old_seed_pid"] = process.pid
        raise RuntimeError("old fixture release not confirmed; no downstream clone started") from error
    finally:
        log.close()
        metadata["old_seed_log_sha256"] = digest(Path(log.name))
    metadata["old_seed_exit_code"] = result
    if result != 0:
        raise RuntimeError("old fixture failed; no downstream clone started")
    metadata["old_seed_released_before_current_clone"] = True
    write(fixture / "old-owner-released.json", {"exit_code": result, "pid": process.pid,
          "log_sha256": metadata["old_seed_log_sha256"], "finished_utc": now()})


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--checkout", type=Path, required=True)
    parser.add_argument("--revision", required=True)
    parser.add_argument("--driver", choices=("sqlite", "mysql", "both"), required=True)
    parser.add_argument("--go-bin", default="go")
    parser.add_argument("--execute", action="store_true")
    parser.add_argument("--mysql-container", help="explicit existing test container; its loopback port must match the opt-in test DSN")
    parser.add_argument("--artifact-root", type=Path, help="new random run children only; default is the existing ignored evidence root")
    parser.add_argument("--binary-budget-mib", type=int, default=256)
    parser.add_argument("--build-temp-budget-mib", type=int, default=512)
    parser.add_argument("--cache-budget-mib", type=int, default=256)
    parser.add_argument("--mysql-growth-budget-mib", type=int, default=128)
    parser.add_argument("--reserve-mib", type=int, default=256)
    args = parser.parse_args()
    if args.mysql_container and not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_.-]{0,127}",args.mysql_container):
        parser.error("an explicit existing container name is required")
    if min(args.binary_budget_mib,args.build_temp_budget_mib,args.cache_budget_mib,args.mysql_growth_budget_mib,args.reserve_mib)<1:
        parser.error("positive explicit planning budgets required")
    os.umask(0o077)
    repo = args.checkout.resolve()
    templates = Path(__file__).resolve().parent
    def git(*arguments):
        return subprocess.check_output(["git", *arguments], cwd=repo, text=True).strip()
    if git("rev-parse", "HEAD") != args.revision or git("status", "--porcelain"):
        parser.error("exact clean revision required; no existing source is reset")
    subprocess.run(["git", "merge-base", "--is-ancestor", BASELINE, args.revision], cwd=repo, check=True)
    names = ("old_seed_probe.go.txt", "current_upgrade_probe.go.txt", "snapshot_probe.go.txt", "restore_probe.go.txt", "current_authority_probe.go.txt", "current_mysql_copy_probe.go.txt")
    metadata = {"baseline": BASELINE, "current": args.revision, "driver": args.driver,
                "templates": {name: digest(templates / name) for name in names},
                "controller_sha256": digest(Path(__file__).resolve()), "execute": args.execute}
    root = (args.artifact_root or repo/".codex-local-artifacts"/"acceptance").resolve()
    plan = disk_budget(repo,root,args,git)
    metadata["disk_budget"] = plan
    metadata["artifact_root"] = str(root)
    if not args.execute:
        print(json.dumps(metadata)); return 0
    if any(not item["sufficient"] for item in plan["filesystems"]) or (plan["mysql_data_filesystem"] and not plan["mysql_data_filesystem"]["sufficient"]):
        print(json.dumps({"status":"not_started", "reason":"insufficient planned free space", "disk_budget":plan}))
        return 2
    for line in subprocess.check_output(["ps", "-eo", "state,comm"], text=True).splitlines()[1:]:
        parts = line.split()
        if len(parts) == 2 and parts[0] != "Z" and (parts[1] in ("go", "compile", "link", "node", "pnpm", "zboard") or parts[1].endswith(".test")):
            parser.error("another local workload is active; no duplicate run")
    if args.driver in ("mysql", "both") and (not os.environ.get("ZBOARD_TEST_MYSQL_DSN") or not args.mysql_container):
        parser.error("explicit isolated loopback test DSN required")
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
        if result.returncode:
            for location in failure_diagnostics(out / (label + ".log")):
                print(json.dumps({"failed_probe": label, "source": location}), flush=True)
            for category in failure_error_classes(out / (label + ".log")):
                print(json.dumps({"failed_probe": label, "observed_error_class": category}), flush=True)
            raise RuntimeError(label + " failed; preserve original log")
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
            if label == "current":
                for probe, name in ((names[4], "convergence_current_authority_probe_test.go"),
                                    (names[5], "convergence_current_mysql_copy_probe_test.go")):
                    path = base/"internal"/"handler"/name
                    with path.open("xb") as stream: stream.write((templates/probe).read_bytes())
            if label == "old":
                path = base / "internal" / "handler" / "convergence_restore_probe_test.go"
                with path.open("xb") as stream:
                    stream.write((templates / names[3]).read_bytes())
            run(label+"-build", [args.go_bin, "test", "-c", "-p", "1", "-mod=readonly", "-buildvcs=false", "-o", str(out/(label+".test")), "./internal/handler"], base)
            metadata[label + "_binary_sha256"] = digest(out/(label+".test"))
            metadata[label + "_binary_bytes"] = (out/(label+".test")).stat().st_size
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
                write(fixture / "old-seed-started.json", {"command": old_command, "pid": process.pid, "started_utc": now()})
                deadline = time.monotonic()+120
                while not (fixture/"old-ready.json").exists():
                    if process.poll() is not None: raise RuntimeError("old MySQL fixture stopped before snapshot; preserve log")
                    if time.monotonic() > deadline: raise RuntimeError("old MySQL snapshot readiness expired")
                    time.sleep(0.2)
                backup = fixture/"old-source.sql"
            result["old_backup_sha256"] = digest(backup)
            if args.driver != "both":
                metadata["old_backup_sha256"] = result["old_backup_sha256"]
            if driver == "mysql":
                # old-ready follows old pool closure and the completed dump.
                # Reap that owner now; downstream5m probes must never depend
                # on its separate2m snapshot wait or its schema cleanup.
                processes.remove((process, old_log, fixture))
                # A failed/uncertain release must not be attempted again by
                # finally. Its original PID and terminal status remain in the
                # receipt; no downstream probe starts unless release succeeds.
                release_old_mysql_fixture(process, old_log, fixture, metadata)
                run(prefix+"current-copy-prepare", [str(out/"current.test"), "-test.run", "^TestConvergencePrepareMySQLUpgradeCopy$", "-test.v", "-test.timeout", "5m"], fixture)
                result["current_mysql_destination"] = json.loads((fixture/"current-copy-ready.json").read_text())
                result["current_mysql_destination_retained"] = True
            run(prefix+"current-upgrade", [str(out/"current.test"), "-test.run", "^TestConvergenceUpgradeFromOldVersion$", "-test.v", "-test.timeout", "5m"], fixture)
            env["ZBOARD_UPGRADE_RECOVERY"] = "1"
            run(prefix+"current-recovery", [str(out/"current.test"), "-test.run", "^TestConvergenceUpgradeFromOldVersion$", "-test.v", "-test.timeout", "5m"], fixture)
            for phase,test in (("current-authority-seed","TestConvergenceCurrentAuthoritySeed"),
                               ("current-authority-reopen","TestConvergenceCurrentAuthorityReopen"),
                               ("current-authority-backup-restore","TestConvergenceCurrentAuthorityBackupRestore")):
                run(prefix+phase,[str(out/"current.test"),"-test.run","^"+test+"$","-test.v","-test.timeout","5m"],fixture)
                if phase == "current-authority-seed":
                    current_backup = fixture/("current-source.db" if driver == "sqlite" else "current-source.sql")
                    result["current_backup_sha256"] = digest(current_backup)
                    result["current_authority_snapshot_sha256"] = digest(fixture/"current-authority.json")
            result["current_authority_restore"] = json.loads((fixture/"authority-restore-result.json").read_text())
            if digest(current_backup) != result["current_backup_sha256"]:
                raise RuntimeError("immutable current backup changed")
            result["current_backup_unchanged"] = True
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
            try:
                release_old_mysql_fixture(process, log, fixture, metadata)
            except Exception as error:
                metadata["status"] = "failed"
                metadata["old_seed_release_error"] = type(error).__name__ + ": " + str(error)
        try:
            metadata["final_disk_budget_observation"] = disk_budget(repo,root,args,git)
        except Exception as error:
            metadata["final_disk_observation_error"] = type(error).__name__ + ": " + str(error)
            metadata["status"] = "failed"
        # Inspect only this newly-created run tree, never previous evidence roots.
        owned_files = [p.stat() for p in out.rglob("*") if p.is_file()]
        metadata["new_artifact_regular_file_bytes"] = sum(item.st_size for item in owned_files)
        metadata["new_artifact_allocated_bytes"] = sum(item.st_blocks*512 for item in owned_files)
        metadata["finished_utc"] = now()
        metadata["primary_worktree_clean"] = not git("status", "--porcelain")
        write(out/"final.json", metadata)
    print(json.dumps({key:metadata.get(key) for key in ("status","output","baseline","current","driver","error","backup_unchanged","primary_worktree_clean")}))
    return 0 if metadata["status"] == "passed" else 1

if __name__ == "__main__":
    raise SystemExit(main())
