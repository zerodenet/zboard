"""Pure policy fixtures; never execute Go/Node/MySQL/Docker or the controller."""
import argparse
import importlib.util
import io
from pathlib import Path
import unittest
from unittest import mock
import tempfile

spec = importlib.util.spec_from_file_location('acceptance_development', Path(__file__).with_name('acceptance-development.py'))
acceptance = importlib.util.module_from_spec(spec)
spec.loader.exec_module(acceptance)


class DevelopmentAcceptancePolicyTest(unittest.TestCase):
    def test_mysql_requires_explicit_empty_database_loopback_and_isolation(self):
        self.assertEqual(acceptance.mysql_target('test:p@tcp(127.0.0.1:3307)/?parseTime=true', True), ('127.0.0.1', 3307))
        self.assertEqual(acceptance.mysql_target('test:p@tcp([::1]:3307)/', True), ('::1', 3307))
        for dsn, isolated in [('', True), ('u:p@tcp(127.0.0.1:3306)/', False),
                              ('u:p@tcp(example.com:3306)/', True), ('u:p@tcp(localhost:3306)/', True),
                              ('u:p@tcp(127.0.0.1:3306)/production', True), ('u:p@tcp(127.0.0.1:0)/', True)]:
            with self.subTest(dsn=dsn, isolated=isolated):
                with self.assertRaises(ValueError):
                    acceptance.mysql_target(dsn, isolated)

    def test_skipped_mysql_and_manual_do_not_produce_full_pass(self):
        stages = {name: {'status': 'passed'} for name in acceptance.FUNCTIONAL + acceptance.MANUAL}
        stages['mysql-full'] = {'status': 'not_executed'}
        stages['autoscale'] = {'status': 'not_executed'}
        result = acceptance.aggregate(stages, True)
        self.assertEqual(result['functional_status'], 'failed_or_incomplete')
        self.assertEqual(result['full_development_acceptance'], 'failed_or_incomplete')
        self.assertIn('mysql-full', result['not_executed'])

    def test_source_change_invalidates_even_all_successful_commands(self):
        stages = {name: {'status': 'passed'} for name in acceptance.FUNCTIONAL + acceptance.MANUAL}
        result = acceptance.aggregate(stages, False)
        self.assertEqual(result['functional_status'], 'failed_or_incomplete')
        self.assertEqual(result['full_development_acceptance'], 'failed_or_incomplete')

    def test_sqlite_only_is_explicit_reduced_scope_not_mysql_pass(self):
        args = argparse.Namespace(sqlite_only=True, with_sdk=True, with_frontend=True,
                                  execute_p7=False, execute_capacity=False, execute_worker_comparison=False, execute_autoscale=False)
        selected = acceptance.requested_stages(args)
        self.assertNotIn('mysql-full', selected)
        self.assertIn('sqlite-purego', selected)
        self.assertIn('frontend-install', selected)
        args.execute_worker_comparison = True
        self.assertIn('worker-prepare', acceptance.requested_stages(args))
        self.assertIn('worker-comparison', acceptance.requested_stages(args))

    def test_original_capacity_pins_accept_literal_name_reference_not_code(self):
        self.assertEqual(acceptance.literal_revision_pins("PIN='old'\nREVISIONS=(PIN, 'new')\n"), ('old', 'new'))
        with self.assertRaises(ValueError):
            acceptance.literal_revision_pins("REVISIONS=(run_code(),)\n")

    def test_stream_credentials_are_removed_across_chunk_boundaries(self):
        class Chunks:
            def __init__(self):
                self.chunks = iter([b'prefixvery-', b'private-', b'password suffix', b''])
            def read(self, _):
                return next(self.chunks)
            def close(self):
                pass
        output = io.BytesIO()
        acceptance.copy_redacted(Chunks(), output, [b'very-private-password'])
        self.assertEqual(output.getvalue(), b'prefix[REDACTED] suffix')

    def test_binding_detects_tracked_deleted_and_untracked_content_changes(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / 'tracked.go').write_text('package before')
            (root / 'new.py').write_text('untracked source')
            def git_fixture(_, *args):
                return b'exact-head\n' if args[0] == 'rev-parse' else b'tracked.go\0new.py\0'
            with mock.patch.object(acceptance, 'git', side_effect=git_fixture):
                before = acceptance.source_binding(root)
                (root / 'new.py').write_text('changed untracked source')
                self.assertNotEqual(before, acceptance.source_binding(root))
                (root / 'tracked.go').unlink()
                self.assertEqual(acceptance.source_binding(root)['files']['tracked.go'], {'kind': 'absent'})

    def test_actual_sqlite_evidence_cannot_be_all_skips(self):
        known = {'test_events': {'pass': 127, 'skip': 43, 'fail': 0}}
        self.assertTrue(acceptance.go_result_complete(known))
        self.assertFalse(acceptance.go_result_complete(known, pure_sqlite=True))
        self.assertFalse(acceptance.go_result_complete({'test_events': {'pass': 0, 'skip': 43, 'fail': 0}}))
        self.assertTrue(acceptance.go_result_complete({'test_events': {'pass': 58, 'skip': 0, 'fail': 0}}, pure_sqlite=True))

    def test_frontend_keeps_exact_existing_six_exclusions(self):
        self.assertEqual(len(acceptance.EXCLUDES), 6)
        self.assertEqual(len(set(acceptance.EXCLUDES)), 6)
        self.assertIn('src/utils/frontendArchitecturePolicy.test.ts', acceptance.EXCLUDES)

class PrincipalStatisticsPolicyTest(unittest.TestCase):
    def complete_child(self, parent, **changes):
        artifact = parent / 'new-child'
        artifact.mkdir()
        value = {'status': 'passed', 'executed': True, 'driver': 'sqlite', 'source_head': 'exact-head',
                 'helper_sha256': 'a' * 64, 'source_unchanged': True,
                 'source_sha256_before': 'b' * 64, 'source_sha256_after': 'b' * 64,
                 'sources': 1024, 'windows': 32, 'timezone': 'Asia/Kathmandu',
                 'artifact_dir': str(artifact), 'summary_path': str(artifact / 'summary.json')}
        value.update(changes)
        acceptance.save(artifact / 'summary.json', value)
        return value

    def test_new_growth_stage_is_manual_and_required_for_full_acceptance(self):
        stages = {name: {'status': 'passed'} for name in acceptance.FUNCTIONAL + acceptance.MANUAL}
        stages['principal-statistics-mysql'] = {'status': 'not_executed'}
        result = acceptance.aggregate(stages, True)
        self.assertEqual(result['functional_status'], 'passed')
        self.assertEqual(result['full_development_acceptance'], 'failed_or_incomplete')
        args = argparse.Namespace(sqlite_only=True, with_sdk=True, with_frontend=True,
                                  execute_p7=False, execute_capacity=False, execute_worker_comparison=False,
                                  execute_autoscale=False, execute_principal_statistics=False)
        self.assertNotIn('principal-statistics-build', acceptance.requested_stages(args))
        args.execute_principal_statistics = True
        self.assertIn('principal-statistics-sqlite', acceptance.requested_stages(args))
        self.assertNotIn('principal-statistics-mysql', acceptance.requested_stages(args))
        args.sqlite_only = False
        self.assertIn('principal-statistics-mysql', acceptance.requested_stages(args))

    def test_source_bound_execution_requires_exact_driver_and_new_owned_summary(self):
        with tempfile.TemporaryDirectory() as temporary:
            parent = Path(temporary)
            child = self.complete_child(parent)
            evidence = acceptance.principal_statistics_verdict(child, parent, set(), 'sqlite', 'exact-head', 'a' * 64)
            self.assertEqual(evidence['driver'], 'sqlite')
            self.assertEqual(evidence['summary_sha256'], acceptance.hashlib.sha256(Path(child['summary_path']).read_bytes()).hexdigest())
            with self.assertRaises(ValueError):
                acceptance.principal_statistics_verdict(child, parent, set(), 'mysql', 'exact-head', 'a' * 64)
            with self.assertRaises(ValueError):
                acceptance.principal_statistics_verdict(child, parent, {Path(child['artifact_dir']).resolve()}, 'sqlite', 'exact-head', 'a' * 64)

    def test_zero_exit_plan_missing_source_and_weakened_coverage_never_pass(self):
        cases = [{'status': 'plan', 'executed': False}, {'executed': False}, {'source_unchanged': False},
                 {'source_head': 'old-head'}, {'helper_sha256': 'c' * 64}, {'sources': 999}, {'sources': True},
                 {'windows': 16}, {'windows': 0}, {'timezone': 'UTC'}, {'source_sha256_before': ''},
                 {'source_sha256_after': 'c' * 64}, {'artifact_dir': None}, {'summary_path': []}, {'summary_path': 1}]
        for changes in cases:
            with self.subTest(changes=changes), tempfile.TemporaryDirectory() as temporary:
                parent = Path(temporary)
                child = self.complete_child(parent, **changes)
                with self.assertRaises(ValueError):
                    acceptance.principal_statistics_verdict(child, parent, set(), 'sqlite', 'exact-head', 'a' * 64)

    def test_retained_summary_cannot_differ_from_stdout_or_escape_its_new_parent(self):
        with tempfile.TemporaryDirectory() as temporary:
            parent = Path(temporary)
            child = self.complete_child(parent)
            acceptance.save(Path(child['summary_path']), {**child, 'status': 'failed'})
            with self.assertRaises(ValueError):
                acceptance.principal_statistics_verdict(child, parent, set(), 'sqlite', 'exact-head', 'a' * 64)
            with self.assertRaises(ValueError):
                acceptance.principal_statistics_verdict(child, parent / 'other', set(), 'sqlite', 'exact-head', 'a' * 64)

    def test_manual_child_keeps_original_resource_coverage_and_secret_environment_boundary(self):
        source = Path(acceptance.__file__).read_text()
        self.assertIn("principal_dsn = env.pop('ZBOARD_ACCEPTANCE_MYSQL_DSN', '')", source)
        self.assertIn("overrides['ZBOARD_ACCEPTANCE_MYSQL_DSN'] = principal_dsn", source)
        self.assertIn("'-sources', '1024', '-windows', '32', '-timezone', 'Asia/Kathmandu'", source)
        self.assertIn("'-min-free-bytes', str(512 * 1024 * 1024)", source)
        self.assertIn("manual_ready('principal-statistics-build', args.execute_principal_statistics)", source)
        self.assertIn("name not in PRINCIPAL_STATISTICS and not shutil.which('docker')", source)

class AutoscaleInternalPolicyTest(unittest.TestCase):
    def fixture(self, directory):
        rows = [dict(pair=pair, scenario=scenario, auto=auto, evidence=str(directory / f'{pair}-{scenario}-{int(auto)}'),
                     result=dict(status='passed_internal_scope', failures=[], incomplete=[], original_gate_report=dict(passed=True)))
                for pair in range(3) for scenario in ('normal', 'backlog', 'crash', 'dbslow', 'pluginbusy') for auto in (False, True)]
        verdict = dict(status='passed_internal_scope', revision='exact-head', scope='p6_internal_only', complete=True,
                       all_scenarios_passed=True, run_count=30, required_run_count=30, source_unchanged=True,
                       binaries_unchanged=True, benefit_prerequisite_revalidated=True, benefit_verified=True,
                       external_quota_stop='not_verified', failures=[], incomplete=[])
        report = directory / 'comparison.json'
        acceptance.save(report, {**verdict, 'runs': rows})
        return {**verdict, 'comparison_report': str(report), 'comparison_sha256': acceptance.hashlib.sha256(report.read_bytes()).hexdigest()}

    def test_exact_retained_thirty_case_internal_verdict_is_required(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            measured = self.fixture(directory)
            self.assertEqual(acceptance.autoscale_internal_result(measured, 'exact-head'), 'passed')
            self.assertEqual(acceptance.autoscale_internal_result(measured, 'other-head'), 'failed')
            for changed in ({'run_count': 10}, {'source_unchanged': False}, {'benefit_verified': False},
                            {'benefit_prerequisite_revalidated': False}, {'scope': 'production_verified'},
                            {'external_quota_stop': 'passed'}, {'comparison_sha256': 'bad'}):
                self.assertEqual(acceptance.autoscale_internal_result({**measured, **changed}, 'exact-head'), 'failed')
            actual = acceptance.json.loads(Path(measured['comparison_report']).read_text())
            actual['runs'].pop()
            acceptance.save(Path(measured['comparison_report']), actual)
            measured['comparison_sha256'] = acceptance.hashlib.sha256(Path(measured['comparison_report']).read_bytes()).hexdigest()
            self.assertEqual(acceptance.autoscale_internal_result(measured, 'exact-head'), 'failed')

    def test_old_measurement_and_new_unknown_receipts_remain_inconclusive(self):
        for status in ('measurements_preserved', 'inconclusive'):
            self.assertEqual(acceptance.autoscale_internal_result({'status': status, 'benefit_verified': False}, 'exact-head'), 'inconclusive')
        self.assertEqual(acceptance.autoscale_internal_result({'status': 'passed_internal_scope'}, 'exact-head'), 'failed')


if __name__ == "__main__":
    unittest.main()
