"""Synthetic refusal fixtures only, never benchmark evidence."""
import hashlib
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest import mock

spec = importlib.util.spec_from_file_location("autoscale_receipt", Path(__file__).with_name("acceptance-autoscale-manual.py"))
p6 = importlib.util.module_from_spec(spec)
spec.loader.exec_module(p6)

class AutoscaleReceiptPolicy(unittest.TestCase):
    def rejected(self, report, message):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            target = root / "comparison.json"
            target.write_text(json.dumps(dict(revision="exact", complete=True, source_unchanged=True, **report)))
            receipt = root / "receipt.json"
            receipt.write_text(json.dumps(dict(revision="exact", comparison_report=str(target), report_sha256=hashlib.sha256(target.read_bytes()).hexdigest())))
            with mock.patch.object(p6, "source_manifest", return_value={}) as manifest:
                with self.assertRaisesRegex(ValueError, message):
                    p6.benefit_receipt(receipt, "exact", root)
                manifest.assert_not_called()

    def test_old_budget_never_admits_autoscale(self):
        self.rejected(dict(budget_profile="original-1.5cpu", parameters=p6.PARAMETERS), "separate 4CPU")

    def test_changed_load_is_not_the_original_gate(self):
        self.rejected(dict(budget_profile="autoscale-4cpu", parameters=dict(p6.PARAMETERS, rate=2)), "unchanged original load")

    def test_two_pairs_do_not_substitute_for_three(self):
        self.rejected(dict(budget_profile="autoscale-4cpu", parameters=p6.PARAMETERS, runs=[]), "exactly three")

    def test_copied_directory_does_not_become_independent_pairs(self):
        runs = [dict(profile=profile, directory="/tmp/one-evidence") for profile in ("single", "split") for _ in range(3)]
        self.rejected(dict(budget_profile="autoscale-4cpu", parameters=p6.PARAMETERS, runs=runs), "cannot reuse")

if __name__ == "__main__":
    unittest.main()
