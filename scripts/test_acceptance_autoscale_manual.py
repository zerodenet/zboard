#!/usr/bin/env python3
"""Offline validator unit fixtures; none of these rows are capacity evidence."""
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

spec = importlib.util.spec_from_file_location("p6_manual", Path(__file__).with_name("acceptance-autoscale-manual.py"))
p6 = importlib.util.module_from_spec(spec)
spec.loader.exec_module(p6)


class OfflineContract(unittest.TestCase):
    def test_original_limits_are_unchanged_and_additional_budget_is_separate(self):
        self.assertEqual(p6.BASELINE, dict(nanocpus=1_000_000_000, memory=1_073_741_824))
        self.assertEqual(p6.COMPARISON, dict(nanocpus=4_000_000_000, memory=4_294_967_296))
        self.assertEqual((p6.PARAMETERS["rate"], p6.PARAMETERS["readers"], p6.PARAMETERS["seconds"]), (20, 4, 60))
        self.assertEqual(p6.WORKING_SET_MIB, 700)
        self.assertEqual(p6.POST_RESERVE_GATE, 1600 << 20)
        self.assertEqual(p6.RUNTIME_DISK_GATE, 1536 << 20)

    def test_process_roles_and_native_cost_come_from_actual_samples(self):
        text = "PID PPID COMMAND RSS %CPU COMMAND\n1 0 zboard 1000 3.0 /zboard -f /data/zboard.json -projection-workers-max=1\n2 1 zboard 2000 4.0 /zboard -projection-worker -projection-worker-background\n3 1 plugin 131072 180.0 /data/plugins/sockets/run-abc/plugin\n"
        rows = p6.parse_top(text)
        self.assertEqual([r["role"] for r in rows], ["core", "worker", "plugin"])
        self.assertEqual(rows[2]["rss_kib"], 131072)
        self.assertEqual(rows[2]["lifetime_cpu_percent"], 180)
        with self.assertRaises(ValueError):
            p6.parse_top("HEADER\n1 ? zboard unknown\n")

    def fixture(self, directory, unknown=False, memory="100MiB / 4GiB"):
        # Intentionally synthetic test-only values prove refusal behavior.
        # They never get written to a real execution or benefit receipt.
        sample = dict(known=True, stats=dict(MemUsage=memory), processes=[dict(pid=1, role="core")])
        (directory / "process-samples.jsonl").write_text((json.dumps(sample) + "\n") * 2)
        resources = dict(resources=dict(Known=not unknown, CPUCapacity=4))
        (directory / "resources.jsonl").write_text((json.dumps(resources) + "\n") * 3)
        (directory / "server.log").write_text("local projection supervisor: running=0 draining=0 desired=0 reason=in_process_capacity_sufficient\n")
        (directory / "acceptance-result.json").write_text(json.dumps(dict(passed=True, failures=[])))
        (directory / "load-exit.json").write_text(json.dumps(dict(returncode=0)))
        (directory / "reconciliation.json").write_text(json.dumps(dict(passed=True)))

    def test_unknown_resources_and_missing_expansion_never_pass(self):
        with tempfile.TemporaryDirectory() as path:
            directory = Path(path)
            self.fixture(directory, unknown=True)
            result = p6.analyze(directory, True, "backlog")
            self.assertEqual(result["status"], "inconclusive")
            self.assertGreaterEqual(len(result["incomplete"]), 2)
            self.assertEqual(result["quota_stop"]["status"], "external_required")
            self.assertIsNone(result["runtime_numeric_metrics"]["physical_database_write_bytes"])
            self.assertFalse(result["runtime_numeric_metrics"]["native_batch_sql"]["known"])

    def test_combined_working_set_cannot_be_weakened_for_workers_or_plugins(self):
        with tempfile.TemporaryDirectory() as path:
            directory = Path(path)
            self.fixture(directory, memory="701MiB / 4GiB")
            result = p6.analyze(directory, False, "normal")
            self.assertEqual(result["status"], "failed")
            self.assertTrue(any("700MiB" in s for s in result["failures"]))

    def test_slowwrite_is_not_accepted_from_a_configured_duration(self):
        with tempfile.TemporaryDirectory() as path:
            directory = Path(path)
            self.fixture(directory)
            (directory / "slowwrite.jsonl").write_text(json.dumps(dict(committed=False, held_seconds=9)) + "\n")
            result = p6.analyze(directory, True, "dbslow")
            self.assertEqual(result["status"], "inconclusive")
            self.assertTrue(any("committed SQLite" in s for s in result["incomplete"]))

    def test_configured_plugin_cost_is_not_a_measured_plugin(self):
        with tempfile.TemporaryDirectory() as path:
            directory = Path(path)
            self.fixture(directory)
            result = p6.analyze(directory, True, "pluginbusy")
            self.assertEqual(result["status"], "inconclusive")
            self.assertTrue(any("measured native" in s for s in result["incomplete"]))

    def test_native_ring_is_deduplicated_and_unknown_commit_rows_stay_unknown(self):
        with tempfile.TemporaryDirectory() as path:
            directory = Path(path)
            (directory / "data").mkdir()
            at = "2026-10-27T00:00:10+00:00"
            sample = dict(sequence=1, statistics_complete=True, commit_confirmed=None,
                          affected_write_rows=None, statement_count=3)
            def row(count, stamp=at):
                call = dict(known=True, sample_count=1, observed_at=stamp, maximum_ms=4, mean_ms=4, p95_ms=4)
                counts = {key: count for key in ("statement_count", "read_count", "write_count", "unknown_statement_count", "statement_errors",
                          "observed_affected_write_rows", "unknown_write_rows", "text_blob_bound_argument_bytes", "source_payload_bytes")}
                return dict(at=at, status=200, runtime=dict(job_store=dict(scope="this_process", timing="durable_store_call", claim=call, finish=call),
                    native_batch_sql=dict(scope="this_process", phase="native_authority_transaction", reset_at="instance1", known=True,
                        observed_at=stamp, fresh_batch_count=1, driver_begin_commit_included=False, physical_bytes_measured=False,
                        totals_complete=True, callback_failures=0, batches=count, totals=counts, samples=[sample])))
            target = directory / "data/runtime-samples.jsonl"
            target.write_text("\n".join(json.dumps(row(n)) for n in (1, 2)))
            result = p6.runtime_numeric_metrics(directory)
            self.assertEqual(result["native_batch_sql"]["unique_sample_count"], 1)
            self.assertEqual(result["native_batch_sql"]["commit_unknown_sample_count"], 1)
            self.assertIsNone(result["native_batch_sql"]["cumulative_delta"]["affected_write_rows"])
            self.assertEqual(result["unknown"], [])
            target.write_text(json.dumps(row(2, "2026-10-26T00:00:00+00:00")))
            stale = p6.runtime_numeric_metrics(directory)
            self.assertFalse(stale["job_store"]["claim"]["known"])
            self.assertIsNone(stale["job_store"]["claim"]["maximum_observed_ms"])
            self.assertFalse(stale["native_batch_sql"]["known"])

    def test_missing_or_reset_native_totals_never_become_zero(self):
        with tempfile.TemporaryDirectory() as path:
            result = p6.runtime_numeric_metrics(Path(path))
            self.assertIsNone(result["native_batch_sql"]["cumulative_delta"])
            self.assertTrue(result["unknown"])

class InternalVerdictContract(unittest.TestCase):
    def runs(self):
        # Synthetic rows test verdict refusal only, never capacity evidence.
        return [dict(pair=pair, scenario=scenario, auto=auto,
                     evidence=f'/synthetic/{pair}-{scenario}-{int(auto)}',
                     result=dict(status='passed_internal_scope', failures=[], incomplete=[], original_gate_report=dict(passed=True)))
                for pair in range(3) for scenario in p6.SCENARIOS for auto in (False, True)]

    def test_all_thirty_original_gate_results_and_revalidated_gain_can_pass_internal_only(self):
        result = p6.internal_scope_verdict(self.runs(), True, True, True)
        self.assertEqual(result['status'], 'passed_internal_scope')
        self.assertTrue(result['benefit_verified'])
        self.assertEqual(result['scope'], 'p6_internal_only')
        self.assertEqual(result['external_quota_stop'], 'not_verified')

    def test_partial_unknown_or_failed_scenario_keeps_full_verdict_unpassed(self):
        for kind in ('partial', 'inconclusive', 'gate-failed', 'duplicate', 'reused-evidence'):
            with self.subTest(kind=kind):
                rows = self.runs()
                if kind == 'partial': rows.pop()
                if kind == 'inconclusive': rows[0]['result'].update(status='inconclusive', incomplete=['unknown'])
                if kind == 'gate-failed': rows[0]['result']['original_gate_report']['passed'] = False
                if kind == 'duplicate': rows[-1] = rows[0]
                if kind == 'reused-evidence': rows[-1]['evidence'] = rows[0]['evidence']
                result = p6.internal_scope_verdict(rows, True, True, True)
                self.assertNotEqual(result['status'], 'passed_internal_scope')
                self.assertFalse(result['benefit_verified'])
        for source, binary, benefit in ((False, True, True), (True, False, True), (True, True, False)):
            self.assertFalse(p6.internal_scope_verdict(self.runs(), source, binary, benefit)['benefit_verified'])


if __name__ == "__main__":
    unittest.main()
