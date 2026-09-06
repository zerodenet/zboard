"""Regression checks for evidence gates; run with python3 -m unittest discover -s scripts."""
import importlib.util
import json
import pathlib
import tempfile
import unittest

spec = importlib.util.spec_from_file_location("acceptance_report", pathlib.Path(__file__).with_name("acceptance-report.py"))
report = importlib.util.module_from_spec(spec)
spec.loader.exec_module(report)


class AcceptanceReportTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.directory = pathlib.Path(self.temp.name)
        (self.directory / "data").mkdir()
        self.write("data/load-result.json", {
            "metrics": {"/api/zero/events": {"count": 100, "errors": 0, "p50_ms": 1, "p95_ms": 2, "p99_ms": 3}},
            "generator_dropped": 0, "undelivered_jobs": 0,
        })
        self.write("data/verification.json", {"passed": True})
        self.write("data/spool-verification.json", {"passed": True})
        self.write("container.json", [{
            "HostConfig": {"NanoCpus": 1000000000, "Memory": 1073741824, "MemorySwap": 1073741824},
            "State": {"OOMKilled": False, "ExitCode": 0},
        }])
        self.write("container-stats.jsonl", {"MemUsage": "100MiB / 1GiB"})
        self.write("storage.jsonl", {"disk_free_bytes": 2 * 1024**3})
        (self.directory / "server.log").write_text("Zero event spool pressure changed: level=normal\n")
        (self.directory / "drain-seconds.txt").write_text("1\n")
        runtime = {"status": 200, "runtime": {
            "event_consumer": {}, "event_spool": {"pending_events": 0},
            "accounting_db": {"wait_count": 2, "wait_seconds": 0.5},
            "traffic_read_db": {"wait_count": 1, "wait_seconds": 0.2},
        }}
        (self.directory / "data/runtime-samples.jsonl").write_text((json.dumps(runtime) + "\n") * 2)

    def write(self, name, data):
        (self.directory / name).write_text(json.dumps(data) + "\n")

    def test_valid_resource_and_correctness_evidence_passes(self):
        result = report.evaluate(self.directory)
        self.assertTrue(result["passed"], result)
        self.assertEqual(result["minimum_disk_free_bytes"], 2 * 1024**3)

    def test_old_missing_disk_samples_cannot_be_normal_capacity(self):
        self.write("storage.jsonl", {"files": {"zboard.db": 1000}})
        result = report.evaluate(self.directory)
        self.assertFalse(result["passed"])
        self.assertIsNone(result["minimum_disk_free_bytes"])

    def test_a_single_low_disk_sample_fails_even_after_recovery(self):
        with (self.directory / "storage.jsonl").open("a") as output:
            output.write(json.dumps({"disk_free_bytes": 500 * 1024**2}) + "\n")
        result = report.evaluate(self.directory)
        self.assertFalse(result["passed"])
        self.assertEqual(result["minimum_disk_free_bytes"], 500 * 1024**2)

    def test_pressure_between_resource_samples_is_not_ignored(self):
        (self.directory / "server.log").write_text(
            "Zero event spool pressure changed: level=emergency\n"
            "Zero event spool pressure changed: level=normal\n"
        )
        result = report.evaluate(self.directory)
        self.assertFalse(result["passed"])
        self.assertEqual(result["spool_pressure_levels"], ["emergency", "normal"])

    def test_resource_headroom_never_overrides_accounting_failure(self):
        self.write("data/verification.json", {"passed": False})
        self.assertFalse(report.evaluate(self.directory)["passed"])

    def test_missing_runtime_observation_is_not_zero_wait(self):
        (self.directory / "data/runtime-samples.jsonl").unlink()
        result = report.evaluate(self.directory)
        self.assertFalse(result["passed"])
        self.assertEqual(result["runtime_diagnostics"], {})


if __name__ == "__main__":
    unittest.main()
