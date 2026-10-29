"""Pure profile/receipt policy tests, not measurements or subprocess workloads."""
import importlib.util
from pathlib import Path
import unittest
from unittest import mock
import tempfile
import json

spec = importlib.util.spec_from_file_location("worker_budget", Path(__file__).with_name("acceptance-worker-comparison.py"))
worker = importlib.util.module_from_spec(spec)
spec.loader.exec_module(worker)

class WorkerBudgetPolicy(unittest.TestCase):
    def test_default_original_and_explicit_extra_profiles_keep_equal_totals(self):
        self.assertEqual(worker.PROFILES, worker.BUDGET_PROFILES["original-1.5cpu"])
        self.assertEqual(worker.PROFILES["single"], [(1500000000, 1610612736)])
        for name, profiles in worker.BUDGET_PROFILES.items():
            worker.validate_profiles(profiles)
        self.assertEqual(worker.BUDGET_PROFILES["autoscale-4cpu"]["single"], [(4000000000, 4294967296)])
        self.assertEqual(worker.BUDGET_PROFILES["autoscale-4cpu"]["split"], [(2000000000, 2147483648)] * 2)
        self.assertEqual(worker.GOMAXPROCS_PROFILES["autoscale-4cpu"], ("4", "2"))
        self.assertEqual((worker.PARAMETERS["seconds"], worker.PARAMETERS["rate"], worker.PARAMETERS["readers"]), (60, 20, 4))
        self.assertEqual(worker.GATE, 1600 << 20)

    def test_unequal_profile_cannot_be_admitted(self):
        with self.assertRaises(ValueError):
            worker.validate_profiles({"single": [(4, 4)], "split": [(2, 2), (1, 2)]})

    def test_original_preparation_cannot_execute_extra_budget(self):
        with tempfile.TemporaryDirectory() as path:
            target = Path(path) / "build.json"
            target.write_text(json.dumps(dict(prepared=True, budget_profile="original-1.5cpu", profiles={k: [list(v) for v in rows] for k, rows in worker.PROFILES.items()})))
            with self.assertRaisesRegex(ValueError, "budget profile differs"):
                worker.prepared(Path(path), target, "autoscale-4cpu")

    def test_missing_profile_layout_cannot_be_assumed_from_a_receipt_label(self):
        with tempfile.TemporaryDirectory() as path:
            target = Path(path) / "build.json"
            target.write_text(json.dumps(dict(prepared=True, budget_profile="original-1.5cpu")))
            with self.assertRaisesRegex(ValueError, "budget profile differs"):
                worker.prepared(Path(path), target)

if __name__ == "__main__":
    unittest.main()
