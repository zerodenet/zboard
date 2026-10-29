"""Comparison evidence must include both processes and retain source identity."""
import copy
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import test_acceptance_report as baseline

spec = importlib.util.spec_from_file_location("worker_comparison", Path(__file__).with_name("acceptance-worker-comparison.py"))
comparison = importlib.util.module_from_spec(spec)
spec.loader.exec_module(comparison)


class WorkerComparisonTests(unittest.TestCase):
    write = baseline.AcceptanceReportTests.write

    def setUp(self):
        baseline.AcceptanceReportTests.setUp(self)
        meta = json.loads((self.directory / "container.json").read_text())
        worker = copy.deepcopy(meta)
        worker[0]["HostConfig"].update(NanoCpus=500000000, Memory=536870912, MemorySwap=536870912)
        self.write("worker.json", worker)
        self.write("process-samples.jsonl", {"error": None, "stats": [
            {"MemUsage": "100MiB / 1GiB"}, {"MemUsage": "100MiB / 512MiB"}], "rss_kib": [100, 100]})

    def result(self):
        return comparison.evaluate(Path(__file__).resolve().parent.parent, self.directory, "split")

    def test_split_checks_the_worker_budget_and_its_exit(self):
        self.assertTrue(self.result()["passed"])
        meta = json.loads((self.directory / "worker.json").read_text())
        meta[0]["State"]["ExitCode"] = 137
        self.write("worker.json", meta)
        self.assertFalse(self.result()["passed"])

    def test_unknown_worker_resource_samples_are_not_zero(self):
        self.write("process-samples.jsonl", {"error": "CalledProcessError", "stats": []})
        self.assertFalse(self.result()["passed"])

    def test_total_process_memory_is_counted(self):
        self.write("process-samples.jsonl", {"error": None, "stats": [
            {"MemUsage": "400MiB / 1GiB"}, {"MemUsage": "400MiB / 512MiB"}]})
        result = self.result()
        self.assertEqual(result["combined_peak_working_set_mib"], 800)
        self.assertFalse(result["passed"])

    def test_profiles_must_have_identical_total_resources(self):
        comparison.validate_profiles()
        with patch.dict(comparison.PROFILES, {"split": [(1000000000, 1073741824)]}):
            with self.assertRaises(ValueError):
                comparison.validate_profiles()

    def test_changed_binary_is_not_accepted_as_prepared_source(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            binary = root / "zboard"
            binary.write_bytes(b"first")
            receipt = root / "build.json"
            receipt.write_text(json.dumps({"prepared": True, "sources": {"source": "hash"},
                                          "profiles": {key: [list(row) for row in rows] for key, rows in comparison.PROFILES.items()},
                                          "revision": "commit", "binaries": {"zboard": comparison.sha(binary)}}))
            with patch.object(comparison, "sources", return_value={"source": "hash"}), patch.object(comparison, "command", return_value="commit"):
                comparison.prepared(root, receipt)
                binary.write_bytes(b"changed")
                with self.assertRaises(ValueError):
                    comparison.prepared(root, receipt)


if __name__ == "__main__":
    unittest.main()
