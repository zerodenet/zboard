"""Synthetic local orchestration/privacy checks; no Go, Docker, SQL or network."""
import contextlib
import copy
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest import mock

spec = importlib.util.spec_from_file_location('p6_once', Path(__file__).with_name('acceptance-p6-once.py'))
p6 = importlib.util.module_from_spec(spec)
spec.loader.exec_module(p6)


class P6OnceTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(); self.addCleanup(self.temporary.cleanup)
        self.home = Path(self.temporary.name)
        self.root = self.home / 'checkout'; self.root.mkdir()
        self.private = self.home / 'private'
        self.head = 'a' * 40
        self.sources = {'backend/current.go': 'b' * 64}
        self.commands = []
        self.gain = True
        self.fail_stage = None
        self.final = mock.Mock(return_value=dict(status='passed_internal_scope', complete=True, run_count=30))
        self.auto = SimpleNamespace(require_source=mock.Mock(return_value=self.sources),
            benefit_receipt=mock.Mock(return_value={'recomputed': {'metric': 'core_p95_ms'}}))
        self.modules = mock.patch.object(p6, 'module', side_effect=[self.auto, SimpleNamespace(), SimpleNamespace()])
        self.modules.start(); self.addCleanup(self.modules.stop)

    def resources(self, _):
        return p6.resource_values(4, 4, 8 << 30, [(400000, 100000)], [(16 << 30, 1 << 30)], 90 << 30, 89 << 30, True)

    def runner(self, root, private, name, arguments):
        self.commands.append((name, arguments))
        directory = self.home / name; directory.mkdir()
        response = {}
        if name.startswith('prepare-'):
            names = ('zboard', 'acceptance', 'acceptance-p6', 'p6busy') if name == 'prepare-autoscale' else ('zboard', 'acceptance')
            for item in names: (directory / item).write_bytes(('same-binary-' + item).encode())
            receipt = directory / 'build.json'
            receipt.write_text(json.dumps(dict(revision=self.head, prepared=True, sources=self.sources,
                binaries={item: p6.sha(directory / item) for item in names})))
            response = {'build_receipt': str(receipt)}
        elif name == 'baseline':
            response = {'evidence': str(directory), 'status': 'baseline_passed'}
            (directory / 'baseline.json').write_text(json.dumps({'original_gate_report': {'passed': True}}))
        elif name == 'comparison':
            (directory / 'benefit.json').write_text('{}')
            (directory / 'comparison.json').write_text(json.dumps(dict(complete=True, runs=[dict(profile='single',result=dict(passed=True, core_p95_ms=5))])))
            response = dict(benefit_verdict='positive_measurement_receipt_created' if self.gain else 'no_positive_measured_gain',
                benefit_receipt=str(directory / 'benefit.json') if self.gain else None, comparison=str(directory / 'comparison.json'))
        else:
            response = {'comparison_report': str(directory / 'comparison.json')}
            (directory / 'comparison.json').write_text('{}')
        return dict(name=name, returncode=1 if self.fail_stage == name else 0, elapsed_seconds=1,
            created=[str(directory)], response=response)

    def execute(self, resource_probe=None):
        return p6.execute(self.root, self.head, self.private, resource_probe or self.resources, self.runner, self.final)

    def test_same_vm_exact_original_commands_and_final_consumer_are_required(self):
        result = self.execute()
        self.assertEqual(result['status'], 'passed_internal_scope')
        self.assertEqual([name for name, _ in self.commands], list(p6.STAGES))
        comparison = self.commands[3][1]
        self.assertIn('acceptance-worker-comparison.py', comparison[1])
        self.assertEqual(comparison[comparison.index('--confirm-benefit-metric') + 1], 'core_p95_ms')
        self.assertEqual(comparison[comparison.index('--pairs') + 1], '3')
        autoscale = self.commands[-1][1]
        self.assertEqual(autoscale[autoscale.index('--scenarios') + 1:], list(p6.SCENARIOS))
        self.assertEqual(autoscale[autoscale.index('--pairs') + 1], '3')
        self.final.assert_called_once()
        self.assertTrue(result['final_raw_consumer_completed'])
        self.assertFalse(result['raw_evidence_uploaded'])
        self.assertEqual(result['external_quota_stop'], 'not_verified')

    def test_cli_refuses_checkout_summary_before_work_or_writing(self):
        output = self.root / 'source-summary.json'
        argv = ['acceptance-p6-once.py', '--checkout', str(self.root), '--revision', self.head,
                '--private-root', str(self.private), '--output', str(output), '--execute']
        stdout = io.StringIO()
        with mock.patch('sys.argv', argv), mock.patch.object(p6, 'execute') as execute, contextlib.redirect_stdout(stdout):
            with self.assertRaises(SystemExit) as result: p6.main()
        self.assertEqual(result.exception.code, 1)
        execute.assert_not_called()
        self.assertFalse(output.exists())
        self.assertEqual(json.loads(stdout.getvalue())['reason'], 'fresh_outside_checkout_summary_required')

    def test_cli_summary_io_failure_emits_no_raw_exception_or_pass(self):
        output = self.home / 'public.json'
        argv = ['acceptance-p6-once.py', '--checkout', str(self.root), '--revision', self.head,
                '--private-root', str(self.private), '--output', str(output), '--execute']
        stdout = io.StringIO()
        with mock.patch('sys.argv', argv), mock.patch.object(p6, 'execute', return_value={'status': 'passed_internal_scope'}), \
                mock.patch.object(p6, 'save', side_effect=OSError('PRIVATE_PATH_SECRET')), contextlib.redirect_stdout(stdout):
            with self.assertRaises(SystemExit) as result: p6.main()
        self.assertEqual(result.exception.code, 1)
        self.assertEqual(json.loads(stdout.getvalue())['reason'], 'safe_summary_write_failed')
        self.assertNotIn('PRIVATE_PATH_SECRET', stdout.getvalue())

    def test_no_positive_prespecified_gain_preserves_measurement_and_stops(self):
        self.gain = False
        result = self.execute()
        self.assertEqual(result['status'], 'inconclusive')
        self.assertEqual(result['reason'], 'no_positive_prespecified_metric_gain')
        self.assertEqual(len(self.commands), 4)
        self.assertIn('comparison_measurements', result)
        self.final.assert_not_called()
        self.assertFalse(result['final_raw_consumer_completed'])

    def test_failed_original_stage_is_never_retried_or_skipped(self):
        self.fail_stage = 'baseline'
        result = self.execute()
        self.assertEqual(result['status'], 'failed')
        self.assertEqual([name for name, _ in self.commands], list(p6.STAGES[:3]))
        self.final.assert_not_called()
        self.assertTrue((self.home / 'baseline/baseline.json').is_file())

    def test_resource_or_missing_probe_denial_starts_no_work_and_no_cleanup(self):
        result = self.execute(lambda _: {'admitted': False})
        self.assertEqual(result['status'], 'admission_failed')
        self.assertEqual(self.commands, [])
        self.assertTrue(self.private.is_dir())

    def test_resource_minima_use_ancestor_limits_and_actual_disk_not_runner_name(self):
        valid = (4, 4, 8 << 30, [(400000, 100000)], [(16 << 30, 1 << 30)], 90 << 30, 89 << 30, True)
        self.assertTrue(p6.resource_values(*valid)['admitted'])
        for index, value in ((0, 2), (1, 2), (2, (4 << 30) - 1), (3, [(200000,100000)]),
                             (4, [(4 << 30, 1)]), (5, p6.GATE - 1), (6, p6.GATE - 1), (7, False)):
            args = list(valid); args[index] = value
            self.assertFalse(p6.resource_values(*args)['admitted'])

    def test_real_cgroup_root_may_be_unbounded_but_nested_unknown_is_not_free(self):
        mount = self.home / "cgroup"; mount.mkdir()
        self.assertEqual(p6.cgroup_limits(mount, mount), ([], []))
        child = mount / "child"; child.mkdir()
        with self.assertRaises(ValueError): p6.cgroup_limits(mount, child)
        (child / "cpu.max").write_text("200000 100000")
        (child / "memory.max").write_text(str(4 << 30))
        (child / "memory.current").write_text("1024")
        self.assertEqual(p6.cgroup_limits(mount, child), ([(200000,100000)],[(4 << 30,1024)]))

    def test_same_revision_different_binary_fails_before_baseline(self):
        original = self.runner
        def changed(*args):
            result = original(*args)
            if result['name'] == 'prepare-comparison':
                path = Path(result['response']['build_receipt']); raw = json.loads(path.read_text())
                (path.parent / 'zboard').write_bytes(b'different binary')
                raw['binaries']['zboard'] = p6.sha(path.parent / 'zboard'); path.write_text(json.dumps(raw))
            return result
        result = p6.execute(self.root, self.head, self.private, self.resources, changed, self.final)
        self.assertEqual(result['status'], 'failed')
        self.assertEqual(len(self.commands), 2)
        self.final.assert_not_called()

    def test_metric_cannot_be_changed_after_inspecting_data(self):
        self.auto.benefit_receipt.return_value = {'recomputed': {'metric': 'drain_seconds'}}
        result = self.execute()
        self.assertEqual(result['status'], 'failed')
        self.assertEqual(len(self.commands), 4)
        self.final.assert_not_called()

    def test_final_revalidation_failure_or_inconclusive_never_produces_pass(self):
        self.final.side_effect = ValueError('PRIVATE secret error with path')
        result = self.execute()
        self.assertEqual(result['status'], 'failed')
        self.assertNotIn('PRIVATE', json.dumps(result))
        self.assertFalse(result['final_raw_consumer_completed'])

    def test_safe_summary_never_copies_inherited_environment_or_raw_payload(self):
        with mock.patch.dict('os.environ', {'GITHUB_TOKEN': 'UNIQUE_PRIVATE_TOKEN', 'UNKNOWN_SECRET': 'UNIQUE_OTHER_SECRET'}):
            result = self.execute()
        public = json.dumps(result)
        for value in ('UNIQUE_PRIVATE_TOKEN', 'UNIQUE_OTHER_SECRET', str(self.home), 'response', 'created', 'stdout', 'stderr'):
            self.assertNotIn(value, public)
        bad = p6.safe_gate({'passed': False, 'failures':['UNIQUE_PRIVATE_TOKEN arbitrary path'],
            'latencies': [{'path': 'PRIVATE', 'count': 20, 'errors': 1, 'p95_ms': 300}]})
        self.assertNotIn('UNIQUE_PRIVATE_TOKEN', json.dumps(bad)); self.assertNotIn('PRIVATE', json.dumps(bad))

    def test_fresh_outside_checkout_evidence_and_retained_raw_are_mandatory(self):
        with self.assertRaises(ValueError):
            p6.execute(self.root, self.head, self.root / 'inside', self.resources, self.runner, self.final)
        self.assertFalse((self.root / 'inside').exists())
        with self.assertRaises(ValueError): p6.retained_fixture(self.root)

    def test_safe_gate_has_only_fixed_endpoint_and_latency_pool_fields(self):
        rows = [dict(path=path, count=20, errors=0, p95_ms=15, budget_ms=250, passed=True) for path in p6.ENDPOINTS]
        rows.append(dict(path="PRIVATE_TOKEN_PATH", count=20, errors=1))
        raw = dict(passed=True, latencies=rows, runtime_diagnostics=dict(samples=2, maximum_sampled_pending_events=8,
            last_consumer=dict(latency_samples=20, latency_bucket_ms=100, p95_upper_ms=200, maximum_ms=155, secret="PRIVATE"),
            database_wait_deltas=dict(accounting_db=dict(wait_count=3, wait_seconds=.25), SECRET_POOL=dict(secret="PRIVATE"))))
        result = p6.safe_gate(raw)
        self.assertEqual([x["endpoint"] for x in result["latency_populations"]], list(p6.ENDPOINTS.values()) + ["unknown"])
        runtime = result["runtime_diagnostics"]
        self.assertEqual(runtime["last_consumer"]["p95_upper_ms"], 200)
        self.assertEqual(runtime["database_wait_deltas"]["accounting_db"]["wait_count"], 3)
        self.assertIsNone(runtime["database_wait_deltas"]["traffic_read_db"]["wait_count"])
        self.assertNotIn("PRIVATE", json.dumps(result)); self.assertNotIn("SECRET_POOL", json.dumps(result))
        missing = p6.safe_gate({})["runtime_diagnostics"]
        self.assertIsNone(missing["last_consumer"]["p95_upper_ms"])
        empty = p6.safe_runtime_diagnostics(dict(last_consumer=dict(latency_samples=0, maximum_ms=0)))
        self.assertIsNone(empty["last_consumer"]["maximum_ms"])

    def write_rows(self, path, rows):
        path.write_text("".join(json.dumps(row) + "\n" for row in rows))

    def test_storage_process_and_sql_export_only_actual_fixed_cost_values(self):
        case = self.home / "cost"; case.mkdir()
        first = dict(files={"zboard.db":100,"zboard.db-wal":30,"events/.emergency-reserve":512,"events/segment":10,"PRIVATE":999})
        (case / "storage-preflight.json").write_text(json.dumps(first))
        self.write_rows(case / "storage.jsonl", [dict(files={"zboard.db":120,"zboard.db-wal":20,"events/.emergency-reserve":512,"events/segment":19}),
            dict(files={"zboard.db":90,"zboard.db-wal":0,"events/.emergency-reserve":512,"events/segment":19})])
        self.write_rows(case / "process-samples.jsonl", [dict(known=True, stats=dict(CPUPerc="20%", secret="PRIVATE"),
            processes=[dict(role="core",rss_kib=100,lifetime_cpu_percent=5,pid="PRIVATE",args="PRIVATE")]),
            dict(known=True, stats=dict(CPUPerc="30%"), processes=[dict(role="core",rss_kib=120,lifetime_cpu_percent=6)])])
        sql = dict(scope="this_process",phase="native_authority_transaction",driver_begin_commit_included=False,
            job_store=dict(claim=dict(known=True,observations=2,maximum_observed_ms=3,maximum_observed_window_p95_ms=2,windows=["PRIVATE"])),
            native_batch_sql=dict(known=True,unique_sample_count=2,complete_sample_count=2,commit_confirmed_sample_count=2,
                cumulative_delta=dict(batches=2,mean_statements_per_batch=4,counts=dict(statement_count=8,text_blob_bound_argument_bytes=99)),
                observed_samples=[dict(secret="PRIVATE")]), unknown=["PRIVATE"])
        result = p6.case_cost(case, SimpleNamespace(runtime_numeric_metrics=lambda _: sql))
        self.assertEqual(result["storage"]["database_bytes"], dict(known=True,observed_samples=3,expected_samples=3,first=100,last=90,peak=120,growth=-10))
        self.assertEqual(result["storage"]["spool_bytes"]["growth"], 9)
        self.assertEqual(result["storage"]["wal_bytes"]["last"], 0)
        self.assertIsNone(result["storage"]["shm_bytes"]["peak"])
        self.assertEqual(result["process"]["docker_cpu_percent"]["peak"], 30)
        self.assertEqual(result["process"]["roles"]["core"]["rss_kib"]["peak"], 120)
        self.assertIsNone(result["process"]["roles"]["worker"]["rss_kib"]["peak"])
        self.assertEqual(result["sql"]["job_store"]["claim"]["maximum_observed_ms"], 3)
        self.assertEqual(result["sql"]["native_batch_sql"]["cumulative_delta"]["counts"]["text_blob_bound_argument_bytes"], 99)
        self.assertIsNone(result["sql"]["physical_database_write_bytes"])
        public = json.dumps(result)
        for value in ("PRIVATE", "windows", '"args"', '"pid"'):
            self.assertNotIn(value, public)
        self.assertNotIn("observed_samples", result["sql"]["native_batch_sql"])
        self.assertNotIn("unknown", result["sql"])

    def test_absent_failed_or_partial_cost_coverage_is_unknown_never_zero(self):
        result = p6.case_cost(self.home)
        self.assertIsNone(result["storage"]["database_bytes"]["first"])
        self.assertIsNone(result["process"]["docker_cpu_percent"]["peak"])
        self.assertIsNone(result["sql"]["job_store"]["finish"]["maximum_observed_ms"])
        self.write_rows(self.home / "process-samples.jsonl", [dict(known=False,stats=dict(CPUPerc="99%"),processes=[]),
            dict(known=True,stats=dict(CPUPerc="10%"),processes=[])])
        result = p6.case_cost(self.home)
        self.assertFalse(result["process"]["docker_cpu_percent"]["known"])
        self.assertIsNone(result["process"]["docker_cpu_percent"]["peak"])
        self.assertEqual(result["process"]["docker_cpu_percent"]["observed_samples"],1)
        raw = dict(scope="PRIVATE",native_batch_sql=dict(known=True,unique_sample_count=99),
                   job_store=dict(finish=dict(known=True,maximum_observed_ms=123)))
        self.assertIsNone(p6.runtime_cost(raw)["native_batch_sql"]["unique_sample_count"])
        self.assertIsNone(p6.runtime_cost(raw)["job_store"]["finish"]["maximum_observed_ms"])
        def failing(_): raise PermissionError("PRIVATE forbidden")
        self.assertNotIn("PRIVATE",json.dumps(p6.case_cost(self.home,SimpleNamespace(runtime_numeric_metrics=failing))))

    def test_comparison_flat_samples_and_owned_no_gain_measurements_preserve_cost(self):
        parent = self.home / "comparison-cost"; parent.mkdir()
        case = parent / "single-new"; case.mkdir()
        self.write_rows(case / "process-samples.jsonl", [dict(stats=[dict(CPUPerc="2%"),dict(CPUPerc="1%")],rss_kib=[100,50],error=None),
            dict(stats=[dict(CPUPerc="2%"),dict(CPUPerc="1%")],rss_kib=[100,50],error=None)])
        (parent / "comparison.json").write_text(json.dumps(dict(runs=[dict(directory=str(case),profile="single",result=dict(passed=True))])))
        record=dict(name="comparison",created=[str(parent)])
        value=p6.measurement_summary(record)
        self.assertEqual(value["runs"][0]["cost"]["process"]["docker_cpu_percent"]["peak"],3)
        self.assertEqual(value["runs"][0]["cost"]["process"]["roles"]["worker"]["rss_kib"]["peak"],50)
        self.assertIsNone(value["runs"][0]["cost"]["process"]["roles"]["core"]["lifetime_cpu_percent"]["peak"])
        (parent / "comparison.json").write_text(json.dumps(dict(runs=[dict(directory=str(self.home),profile="PRIVATE",result={})])))
        with mock.patch.object(p6,'case_cost',side_effect=AssertionError('must not inspect unowned directory')):
            value=p6.measurement_summary(record)
        self.assertEqual(value["runs"][0]["profile"],"unknown")
        self.assertNotIn("cost",value["runs"][0])

    def test_one_shot_workflow_keeps_raw_private_and_adds_no_privileged_auth_or_cleanup(self):
        source = (Path(__file__).parents[1] / '.github/workflows/p6-internal-once.yml').read_text()
        active = '\n'.join(line for line in source.splitlines() if not line.lstrip().startswith('#'))
        for forbidden in ('pull_request_target', 'permissions:', '--privileged', '--security-opt', 'GITHUB_TOKEN',
                          'docker prune', 'docker rm', 'docker stop', 'actions/download-artifact', 'sudo', 'rm -'):
            self.assertNotIn(forbidden, active)
        self.assertIn('types: [labeled]', source)
        self.assertIn('validation:p6-internal', source)
        self.assertEqual(source.count('runs-on:'), 1)
        self.assertEqual(source.count('uses: actions/upload-artifact@v4'), 1)
        self.assertIn('path: ${{ runner.temp }}/p6-public/summary.json', source)
        controller = Path(__file__).with_name('acceptance-p6-once.py').read_text()
        self.assertNotIn('acceptance-capacity-baseline.py', controller)
        self.assertNotIn('os.environ', controller)


if __name__ == '__main__': unittest.main()
