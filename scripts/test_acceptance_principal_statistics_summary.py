"""Private synthetic policy tests only; no Go, SQL, Docker or network calls."""
import copy
import contextlib
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import tempfile
import unittest
from unittest import mock

spec = importlib.util.spec_from_file_location("principal_statistics_public_summary", Path(__file__).with_name("principal_statistics_public_summary.py"))
summary = importlib.util.module_from_spec(spec)
spec.loader.exec_module(summary)


class PrincipalStatisticsPublicSummaryTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.private = self.root / "private"; self.private.mkdir()
        self.artifacts = self.root / "artifacts"; self.artifacts.mkdir()
        self.helper = self.private / "helper"; self.helper.write_bytes(b"synthetic exact helper")
        self.head = "a" * 40
        self.data = self.root / "mysql-data-new"; self.data.mkdir()
        self.identity = "b" * 64
        self.owner = "123-1"
        (self.private / "mysql-id.txt").write_text(self.identity)
        (self.private / "mysql-owner.txt").write_text(self.owner)
        (self.private / "mysql-data-root.txt").write_text(str(self.data))
        self.meta = [{"Id": self.identity, "Config": {"Labels": {"zboard.acceptance.p4": self.owner},
            "Env": ["PRIVATE_PASSWORD=must-not-publish"]}, "Mounts": [{"Source": str(self.data),
            "Destination": "/var/lib/mysql", "Type": "bind", "RW": True}]}]
        self.save_inspect()
        self.raw = {}
        for driver in ("sqlite", "mysql"):
            child = self.artifacts / ("principal-statistics-" + driver); child.mkdir()
            value = dict(status="passed", executed=True, source_head=self.head, driver=driver,
                helper_sha256=summary.sha(self.helper), source_sha256_before="c" * 64,
                source_sha256_after="c" * 64, source_unchanged=True, source_dirty=False,
                sources=1024, windows=32, timezone="Asia/Kathmandu",
                artifact_dir=str(child), summary_path=str(child / "summary.json"),
                checks={key: True for key in summary.CHECKS},
                snapshots=[dict(phase=phase, rows={key: 2 for key in summary.TABLES},
                    logical_variable_bytes={"raw": 10, "point": 20, "hour": 30},
                    physical={"page_count": 8, "server_datadir": "/PRIVATE/DATABASE", "meaning": "PRIVATE RAW TEXT"},
                    process_memory={"rss_bytes": 4096}, immutable_points_sha256="d" * 64,
                    hourly_sha256="e" * 64, trends_sha256="f" * 64) for phase in summary.PHASES],
                batches=[dict(phase="source", rows=64, gorm_sql_statements=10, elapsed_ms=2.5, error="PRIVATE raw payload")],
                replay_coverage=dict(kind="bounded_reverse_sample", accepted_sources=1024, replayed_sources=64,
                    selection_step=16, excluded_corrupt_sources=1, covers_all_accepted_sources=False),
                error="", arbitrary_secret="MUST_NOT_BE_PUBLIC")
            self.raw[driver] = value
            self.save(driver)

    def save(self, driver):
        value = self.raw[driver]
        Path(value["summary_path"]).write_text(json.dumps(value))
        (self.private / (driver + ".json")).write_text(json.dumps(value))

    def save_inspect(self):
        (self.private / "mysql-inspect.json").write_text(json.dumps(self.meta))

    def collect(self):
        return summary.collect(self.private, self.artifacts, self.head)

    def test_complete_exact_dual_database_scope_has_no_p6_or_external_claim(self):
        result = self.collect()
        self.assertEqual(result["status"], "passed")
        self.assertEqual([row["driver"] for row in result["databases"]], ["sqlite", "mysql"])
        self.assertTrue(result["databases"][1]["mysql_mapping"]["verified"])
        self.assertEqual(result["p6_acceptance"], "not_executed")
        self.assertEqual(result["external_kernel_payment_production"], "not_executed")
        self.assertFalse(result["raw_evidence_uploaded"])
        self.assertFalse(result["databases"][0]["replay_coverage"]["covers_all_accepted_sources"])

    def test_cli_logs_only_the_same_reviewed_summary_as_its_retained_output(self):
        output = self.root / "public.json"
        stdout = io.StringIO()
        arguments = ["principal_statistics_public_summary.py", "--private-root", str(self.private),
                     "--artifact-root", str(self.artifacts), "--revision", self.head, "--output", str(output)]
        with mock.patch('sys.argv', arguments), contextlib.redirect_stdout(stdout):
            with self.assertRaises(SystemExit) as result:
                summary.main()
        self.assertEqual(result.exception.code, 0)
        self.assertEqual(json.loads(stdout.getvalue()), json.loads(output.read_text()))
        self.assertNotIn(str(self.root), stdout.getvalue())
        self.assertNotIn("must-not-publish", stdout.getvalue())

    def test_no_raw_secret_paths_sql_environment_or_unknown_field_is_exported(self):
        public = json.dumps(self.collect())
        for value in (str(self.root), "PRIVATE", "must-not-publish", "MUST_NOT_BE_PUBLIC", "arbitrary_secret", "server_datadir", "Env"):
            self.assertNotIn(value, public)

    def test_missing_mysql_never_turns_sqlite_into_dual_database_pass(self):
        (self.private / "mysql.json").unlink()
        result = self.collect()
        self.assertEqual(result["status"], "failed_or_incomplete")
        self.assertEqual(result["databases"][0]["status"], "passed")
        self.assertEqual(result["databases"][1]["status"], "not_executed")
        self.assertEqual(result["databases"][1]["diagnostic"], "mysql_setup_or_mapping_incomplete")

    def test_changed_source_helper_scope_check_or_snapshot_cannot_pass(self):
        original = copy.deepcopy(self.raw["sqlite"])
        for key, value in (("source_head", "0" * 40), ("helper_sha256", "0" * 64),
                ("source_unchanged", False), ("source_sha256_after", "0" * 64), ("source_dirty", True),
                ("sources", 1000), ("windows", 31), ("timezone", "UTC"), ("executed", False),
                ("checks", {}), ("snapshots", [])):
            with self.subTest(key=key):
                self.raw["sqlite"] = copy.deepcopy(original); self.raw["sqlite"][key] = value; self.save("sqlite")
                self.assertNotEqual(self.collect()["status"], "passed")
        self.raw["sqlite"] = original; self.save("sqlite")
        self.raw["sqlite"]["checks"]["future_guard"] = False; self.save("sqlite")
        self.assertNotEqual(self.collect()["status"], "passed")

    def test_stdout_must_equal_retained_new_owned_summary(self):
        (self.private / "sqlite.json").write_text(json.dumps(self.raw["sqlite"] | {"arbitrary_secret": "changed"}))
        self.assertEqual(self.collect()["databases"][0]["diagnostic"], "invalid_or_missing_owned_evidence")

    def test_existing_or_outside_owned_artifact_parent_cannot_be_adopted(self):
        outside = self.root / "principal-statistics-outside"; outside.mkdir()
        self.raw["sqlite"].update(artifact_dir=str(outside), summary_path=str(outside / "summary.json"))
        self.save("sqlite")
        self.assertNotEqual(self.collect()["status"], "passed")

    def test_symlink_summary_cannot_be_adopted(self):
        target = Path(self.raw["sqlite"]["summary_path"])
        renamed = target.with_name("retained.json"); target.rename(renamed); target.symlink_to(renamed)
        self.assertEqual(self.collect()["databases"][0]["diagnostic"], "invalid_or_missing_owned_evidence")

    def test_mount_must_match_exact_new_identity_owner_rw_type_and_source(self):
        original = copy.deepcopy(self.meta)
        for mode in ("identity", "owner", "readonly", "volume", "source", "duplicate"):
            with self.subTest(mode=mode):
                self.meta = copy.deepcopy(original)
                row = self.meta[0]
                if mode == "identity": row["Id"] = "0" * 64
                if mode == "owner": row["Config"]["Labels"]["zboard.acceptance.p4"] = "other"
                if mode == "readonly": row["Mounts"][0]["RW"] = False
                if mode == "volume": row["Mounts"][0]["Type"] = "volume"
                if mode == "source": row["Mounts"][0]["Source"] = str(self.root)
                if mode == "duplicate": row["Mounts"].append(copy.deepcopy(row["Mounts"][0]))
                self.save_inspect()
                with self.assertRaises(ValueError): summary.verify_mysql_binding(self.private)
                self.assertNotEqual(self.collect()["status"], "passed")

    def test_data_mapping_symlink_or_unowned_directory_is_refused(self):
        alias = self.root / "mysql-data-alias"; alias.symlink_to(self.data, target_is_directory=True)
        (self.private / "mysql-data-root.txt").write_text(str(alias))
        with self.assertRaises(ValueError): summary.verify_mysql_binding(self.private)
        (self.private / "mysql-data-root.txt").write_text(str(self.artifacts))
        with self.assertRaises(ValueError): summary.verify_mysql_binding(self.private)

    def test_failure_class_is_fixed_and_does_not_copy_payload(self):
        self.raw["mysql"].update(status="failed", error="actual migration failed: Error 3140 private-DSN/path/sql")
        self.save("mysql")
        result = self.collect()
        self.assertEqual(result["databases"][1]["diagnostic"], "mysql_3140")
        self.assertNotEqual(result["status"], "passed")
        self.assertNotIn("private-DSN", json.dumps(result))
        self.assertEqual(summary.diagnostic("sensitive unrecognized text"), "helper_failed_unclassified")

    def test_nonfinite_negative_boolean_and_oversized_numeric_values_remain_unknown(self):
        for value in (True, False, -1, float("nan"), float("inf"), "secret", 2**100):
            self.assertIsNone(summary.numeric(value))
        self.assertEqual(summary.numeric(0), 0)
        self.assertEqual(summary.numeric(1.25), 1.25)

    def test_workflow_preserves_one_shot_scope_gates_and_summary_only_upload(self):
        workflow = Path(__file__).parents[1] / ".github/workflows/principal-statistics-once.yml"
        source = workflow.read_text()
        for value in ("types: [labeled]", "validation:principal-statistics", "-sources 1024 -windows 32",
                "-timezone Asia/Kathmandu -min-free-bytes 536870912", "--verify-mysql-binding", "--mount ", "mysql:8.4"):
            self.assertIn(value, source)
        for value in ("pull_request_target", "permissions:", "--privileged", "--security-opt", "docker rm", "docker stop", "docker volume rm", "prune", "--confirm-benefit", "acceptance-autoscale-manual.py", "acceptance-worker-comparison.py"):
            # Comments document the no-cleanup boundary; inspect executable lines.
            self.assertNotIn(value, "\n".join(line for line in source.splitlines() if not line.lstrip().startswith("#")))
        self.assertEqual(source.count("uses: actions/upload-artifact@v4"), 1)
        self.assertIn("path: ${{ runner.temp }}/principal-public/summary.json", source)


if __name__ == "__main__":
    unittest.main()
