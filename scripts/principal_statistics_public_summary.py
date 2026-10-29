#!/usr/bin/env python3
"""Publish only reviewed P4 numeric/hash evidence, never private fixture material."""
import argparse
import hashlib
import json
import math
from pathlib import Path
import re

CHECKS = ("source_unchanged", "accepted_sources_reporting_pending", "corrupt_original_held",
          "real_allocator_anchor_retained", "retirement_preserves_thin_and_statistics_hashes",
          "old_reverse_replay_idempotent", "distinct_late_source_exactly_once",
          "final_authority_and_allocator_floor_valid")
TABLES = ("raw", "point", "hour", "pending", "state", "principal_authority", "financial_ledger",
          "report_receipt", "usage_pending", "observation_headers", "observation_parts")
PHASES = ("empty", "source_pending", "projected", "retired", "replayed", "late_projected", "late_replayed", "final")
BATCHES = ("source", "drain", "retire", "reverse_replay", "late_drain", "retire_after_late")
PHYSICAL = ("page_count", "page_size", "freelist_pages", "allocated_page_bytes", "file_bytes",
            "file_bytes-wal", "file_bytes-shm", "data_length_estimate_bytes", "index_length_estimate_bytes")
PREFLIGHT = ("artifact_free_bytes", "estimated_artifact_workload_bytes", "required_post_estimate_reserve_bytes",
             "mysql_data_free_bytes", "mysql_estimated_workload_bytes", "combined_workload_estimate_bytes",
             "artifact_final_free_bytes", "mysql_data_final_free_bytes")


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def hashed(value):
    return value if isinstance(value, str) and re.fullmatch(r"[0-9a-f]{64}", value) else None


def numeric(value):
    if type(value) is int:
        return value if 0 <= value <= 2**64 - 1 else None
    return value if type(value) is float and math.isfinite(value) and value >= 0 else None


def numbers(value, keys):
    return {key: numeric(value.get(key)) for key in keys}


def load(path):
    if path.is_symlink() or not path.is_file() or not 1 <= path.stat().st_size <= 8 * 1024 * 1024:
        raise ValueError("invalid_evidence")
    return json.loads(path.read_text())


def diagnostic(error):
    # Never copy arbitrary message, SQL, filename, schema, DSN or path text.
    if not isinstance(error, str) or not error:
        return "none"
    for code in (1045, 1049, 1054, 1062, 1146, 1205, 1213, 1406, 1451, 1452, 3140, 3819):
        if re.search(r"\bError " + str(code) + r"\b", error):
            return "mysql_" + str(code)
    if error.startswith("actual migration failed:"):
        return "migration_failed"
    for words, category in (("reserve", "disk_budget"), ("filesystem", "disk_budget"),
                            ("source", "source_or_replay_evidence"), ("binary", "source_binding"),
                            ("provenance", "source_binding"), ("schema CREATE", "schema_create"),
                            ("DSN", "mysql_configuration"), ("MySQL", "mysql_setup"),
                            ("retire", "retention"), ("replay", "replay"), ("project", "projection")):
        if words in error:
            return category
    return "helper_failed_unclassified"


def verify_mysql_binding(private):
    """Validate exactly the newly created CI container and its actual bind mount."""
    identity = (private / "mysql-id.txt").read_text().strip()
    owner = (private / "mysql-owner.txt").read_text().strip()
    data = Path((private / "mysql-data-root.txt").read_text().strip())
    if not re.fullmatch(r"[0-9a-f]{64}", identity) or not owner or data.is_symlink():
        raise ValueError("mysql_binding_invalid")
    if not data.is_dir() or data.resolve().parent != private.resolve().parent or not data.name.startswith("mysql-data-"):
        raise ValueError("mysql_binding_invalid")
    inspected = load(private / "mysql-inspect.json")
    if not isinstance(inspected, list) or len(inspected) != 1:
        raise ValueError("mysql_binding_invalid")
    meta = inspected[0]
    mounts = [m for m in meta.get("Mounts", []) if m.get("Destination") == "/var/lib/mysql"]
    if (meta.get("Id") != identity or meta.get("Config", {}).get("Labels", {}).get("zboard.acceptance.p4") != owner
            or len(mounts) != 1 or mounts[0].get("Type") != "bind" or mounts[0].get("RW") is not True
            or Path(mounts[0].get("Source", "")).resolve() != data.resolve()):
        raise ValueError("mysql_binding_invalid")
    return dict(verified=True, container_id_sha256=hashlib.sha256(identity.encode()).hexdigest(),
                data_mapping_sha256=hashlib.sha256(str(data.resolve()).encode()).hexdigest(),
                inspection_sha256=sha(private / "mysql-inspect.json"))


def database_summary(private, artifact_parent, driver, head, helper_hash):
    result = dict(driver=driver, status="not_executed", executed=False, diagnostic="not_executed")
    path = private / (driver + ".json")
    if not path.is_file():
        if driver == "mysql" and (private / "mysql-data-root.txt").is_file():
            result["diagnostic"] = "mysql_setup_or_mapping_incomplete"
        return result
    raw = load(path)
    result["executed"] = raw.get("executed") is True
    result["diagnostic"] = diagnostic(raw.get("error"))
    result["status"] = "failed" if result["executed"] else "not_executed"
    if not result["executed"]:
        return result
    artifact = Path(raw.get("artifact_dir", ""))
    summary = Path(raw.get("summary_path", ""))
    if (not artifact.is_absolute() or not summary.is_absolute() or artifact.is_symlink() or summary.is_symlink()
            or artifact.resolve().parent != artifact_parent.resolve()
            or not artifact.name.startswith("principal-statistics-") or summary.resolve() != artifact.resolve() / "summary.json"):
        raise ValueError("invalid_evidence")
    retained = load(summary)
    if retained != raw:
        raise ValueError("invalid_evidence")
    checks = raw.get("checks", {})
    before, after = hashed(raw.get("source_sha256_before")), hashed(raw.get("source_sha256_after"))
    valid = (raw.get("status") == "passed" and raw.get("driver") == driver
             and raw.get("source_head") == head and raw.get("helper_sha256") == helper_hash
             and raw.get("source_dirty") is False and raw.get("source_unchanged") is True
             and before is not None and before == after and raw.get("sources") == 1024 and type(raw.get("sources")) is int
             and raw.get("windows") == 32 and type(raw.get("windows")) is int and raw.get("timezone") == "Asia/Kathmandu"
             and all(checks.get(key) is True for key in CHECKS) and all(v is True for v in checks.values())
             and [row.get("phase") for row in raw.get("snapshots", [])] == list(PHASES))
    if driver == "mysql":
        result["mysql_mapping"] = verify_mysql_binding(private)
    result.update(status="passed" if valid else "failed", retained_summary_sha256=sha(summary),
                  helper_sha256=hashed(raw.get("helper_sha256")), source_sha256_before=before,
                  source_sha256_after=after, source_unchanged=raw.get("source_unchanged") is True,
                  checks={key: checks.get(key) is True for key in CHECKS},
                  preflight=numbers(raw.get("preflight", {}), PREFLIGHT))
    result["snapshots"] = []
    for row in raw.get("snapshots", []):
        if row.get("phase") not in PHASES:
            continue
        result["snapshots"].append(dict(phase=row["phase"], rows=numbers(row.get("rows", {}), TABLES),
            logical_variable_bytes=numbers(row.get("logical_variable_bytes", {}), ("raw", "point", "hour")),
            physical=numbers(row.get("physical", {}), PHYSICAL),
            process_memory=numbers(row.get("process_memory", {}), ("go_heap_alloc_bytes", "rss_bytes", "high_water_rss_bytes")),
            immutable_points_sha256=hashed(row.get("immutable_points_sha256")),
            hourly_sha256=hashed(row.get("hourly_sha256")), trends_sha256=hashed(row.get("trends_sha256"))))
    result["batches"] = [dict(phase=row["phase"], **numbers(row, ("rows", "gorm_sql_statements", "elapsed_ms")),
                               expected_held=row.get("expected_held") is True, diagnostic=diagnostic(row.get("error")))
                         for row in raw.get("batches", []) if row.get("phase") in BATCHES]
    replay = raw.get("replay_coverage", {})
    result["replay_coverage"] = dict(kind="bounded_reverse_sample" if replay.get("kind") == "bounded_reverse_sample" else "unknown",
        **numbers(replay, ("accepted_sources", "replayed_sources", "excluded_corrupt_sources", "selection_step")),
        covers_all_accepted_sources=replay.get("covers_all_accepted_sources") is True)
    return result


def collect(private, artifact_parent, head):
    if not re.fullmatch(r"[0-9a-f]{40}", head):
        raise ValueError("invalid_source_head")
    result = dict(version=1, scope="p4_auxiliary_statistics_only", source_head=head, status="failed_or_incomplete",
                  sources=1024, windows=32, timezone="Asia/Kathmandu", minimum_free_bytes=536870912,
                  p6_acceptance="not_executed", external_kernel_payment_production="not_executed",
                  raw_evidence_uploaded=False, databases=[])
    helper = private / "helper"
    if not helper.is_file() or helper.is_symlink():
        result["diagnostic"] = "helper_build_not_completed"
        return result
    for driver in ("sqlite", "mysql"):
        try:
            row = database_summary(private, artifact_parent, driver, head, sha(helper))
        except (OSError, ValueError, KeyError, TypeError, AttributeError, OverflowError):
            row = dict(driver=driver, status="failed", executed=False, diagnostic="invalid_or_missing_owned_evidence")
        result["databases"].append(row)
    if all(row["status"] == "passed" for row in result["databases"]):
        result["status"] = "passed"
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--private-root", type=Path, required=True)
    parser.add_argument("--artifact-root", type=Path)
    parser.add_argument("--revision")
    parser.add_argument("--output", type=Path)
    parser.add_argument("--verify-mysql-binding", action="store_true")
    args = parser.parse_args()
    if args.verify_mysql_binding:
        verify_mysql_binding(args.private_root)
        return
    if args.output is None or args.artifact_root is None or args.revision is None:
        parser.error("summary requires revision, artifact-root and output")
    result = collect(args.private_root, args.artifact_root, args.revision)
    args.output.parent.mkdir(parents=True, exist_ok=True)
    with args.output.open("x") as stream:
        json.dump(result, stream, indent=2, allow_nan=False); stream.write("\n")
    # The same reviewed allowlist is visible in the job log; inspecting a
    # result never requires publishing or retrieving the raw fixture material.
    print(json.dumps(result, allow_nan=False), flush=True)
    raise SystemExit(0 if result["status"] == "passed" else 1)


if __name__ == "__main__":
    main()
