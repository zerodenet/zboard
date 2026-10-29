"""Selection-policy tests; no Go command or database is invoked here."""
import importlib.util
from pathlib import Path
import re
import json
import subprocess
import sys
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('mysql_profiles', Path(__file__).with_name('mysql_profiles.py'))
profiles = importlib.util.module_from_spec(spec)
spec.loader.exec_module(profiles)


class MySQLProfilesTest(unittest.TestCase):
    def policy(self):
        return {'version': 1, 'extended': [{'package': './handler', 'test': 'TestMySQLScale',
                'reason': 'Scale only', 'scale': '1200 captured sources'}]}

    def inventory(self):
        return {('./handler', 'TestMySQLScale'), ('./handler', 'TestMySQLMoney'),
                ('./newpackage', 'TestMySQLNewAuthorityRecovery')}

    def test_new_packages_and_tests_default_to_core(self):
        inventory = self.inventory()
        parts = profiles.partition(inventory, self.policy())
        self.assertEqual(parts['extended'], {('./handler', 'TestMySQLScale')})
        self.assertIn(('./newpackage', 'TestMySQLNewAuthorityRecovery'), parts['core'])
        self.assertEqual(parts['core'] | parts['extended'], inventory)
        self.assertFalse(parts['core'] & parts['extended'])

    def test_compiled_unicode_test_identifiers_are_not_silently_omitted(self):
        event = {'Action': 'output', 'Package': 'example/backend/handler', 'Output': 'TestMySQL余额一致\n'}
        found = profiles.listed_tests([event], 'example/backend')
        self.assertEqual(found, {('./handler', 'TestMySQL余额一致')})
        self.assertTrue(re.fullmatch(profiles.positive_regex({'TestMySQL余额一致'}), 'TestMySQL余额一致'))
        self.assertIn(('./handler', 'TestMySQL余额一致'), profiles.partition(self.inventory() | found, self.policy())['core'])

    def test_stale_duplicate_and_unreasoned_entries_fail_closed(self):
        for mode in ('stale', 'duplicate', 'reason', 'scale'):
            with self.subTest(mode=mode):
                policy = self.policy()
                if mode == 'stale':
                    policy['extended'][0]['test'] = 'TestMySQLDeleted'
                elif mode == 'duplicate':
                    policy['extended'] *= 2
                else:
                    policy['extended'][0][mode] = ''
                with self.assertRaises(ValueError):
                    profiles.partition(self.inventory(), policy)

    def test_positive_regex_is_exact_not_prefix_and_cannot_be_empty(self):
        expression = profiles.positive_regex({'TestMySQLMoney', 'TestMySQLScale'})
        self.assertTrue(re.fullmatch(expression, 'TestMySQLMoney'))
        self.assertIsNone(re.fullmatch(expression, 'TestMySQLMoneyMore'))
        self.assertNotIn('?!', expression)
        with self.assertRaises(ValueError):
            profiles.positive_regex(set())

    def test_copy_group_remains_last_and_full_selection_is_covered(self):
        inventory = self.inventory() | {(profiles.JOBSTORE, 'TestLease'), (profiles.COPYSTORE, 'TestMySQLCopy')}
        parts = profiles.partition(inventory, self.policy())
        groups = profiles.groups(parts['core'], inventory)
        self.assertEqual([g[0] for g in groups], ['business', 'jobs', 'copy'])
        self.assertEqual(set().union(*(g[3] for g in groups)), parts['core'])

    def test_cross_package_regex_cannot_reinclude_extended_test(self):
        inventory = self.inventory() | {('./newpackage', 'TestMySQLScale')}
        parts = profiles.partition(inventory, self.policy())
        with self.assertRaisesRegex(ValueError, 'collision'):
            profiles.groups(parts['core'], inventory)

    def test_dynamic_inventory_only_accepts_exact_actual_test_output(self):
        events = [{'Action': 'output', 'Package': 'example/backend/internal/a', 'Output': 'TestMySQLMoney\n'},
                  {'Action': 'output', 'Package': 'example/backend/internal/a', 'Output': 'ok example/backend/internal/a\n'},
                  {'Action': 'output', 'Package': 'example/backend/internal/a', 'Output': 'BenchmarkMySQL\n'}]
        self.assertEqual(profiles.listed_tests(events, 'example/backend'), {('./internal/a', 'TestMySQLMoney')})

    def test_inventory_failure_exposes_bounded_compiler_output_only(self):
        events = [{'Action': 'output', 'Output': 'ordinary test body output'},
                  {'Action': 'build-output', 'Output': 'handler_test.go:12: undefined: missing\n'},
                  {'Action': 'build-fail', 'ImportPath': 'example/backend/handler'}]
        self.assertEqual(profiles.inventory_compiler_diagnostics(events), ['handler_test.go:12: undefined: missing'])
        bounded = profiles.inventory_compiler_diagnostics([{'Action': 'build-output', 'Output': ('x' * 500 + '\n') * 30}])
        self.assertEqual(len(bounded), 20)
        self.assertTrue(all(len(line) == 400 for line in bounded))

    def test_missing_skipped_failed_and_unexpected_results_are_incomplete(self):
        expected = {('./handler', 'TestMySQLMoney')}
        normal = {'Action': 'pass', 'Package': 'example/backend/handler', 'Test': 'TestMySQLMoney'}
        self.assertTrue(profiles.result_summary([normal], expected, 'example/backend')['complete'])
        for events in ([], [{**normal, 'Action': 'skip'}], [{**normal, 'Action': 'fail'}],
                       [normal, {**normal, 'Test': 'TestUnexpected'}],
                       [normal, {**normal, 'Action': 'skip', 'Test': 'TestMySQLMoney/optional'}]):
            with self.subTest(events=events):
                self.assertFalse(profiles.result_summary(events, expected, 'example/backend')['complete'])
        with self.assertRaisesRegex(ValueError, 'duplicate'):
            profiles.result_summary([normal, normal], expected, 'example/backend')

    def test_execution_failure_exposes_source_identity_without_raw_payload(self):
        events = [
            {'Action': 'run', 'Package': 'example/backend/handler', 'Test': 'TestMySQLMoney'},
            {'Action': 'output', 'Package': 'example/backend/handler', 'Test': 'TestMySQLMoney',
             'Output': '    money_test.go:33: private-diagnostic-payload database credentials\n'},
            {'Action': 'fail', 'Package': 'example/backend/handler', 'Test': 'TestMySQLMoney'},
        ]
        summary = profiles.result_summary(events, {('./handler', 'TestMySQLMoney')}, 'example/backend')
        lines = profiles.execution_failure_diagnostics(events, summary)
        rendered = '\n'.join(lines)
        self.assertIn('TestMySQLMoney', rendered)
        self.assertIn('money_test.go:33', rendered)
        self.assertNotIn('private-diagnostic-payload', rendered)
        self.assertNotIn('database credentials', rendered)
        self.assertNotIn('Active at incomplete', rendered)

    def test_incomplete_execution_preserves_missing_and_active_without_blame(self):
        expected = {('./handler', 'TestMySQLPending'), ('./handler', 'TestMySQLNotStarted')}
        events = [{'Action': 'run', 'Package': 'example/backend/handler', 'Test': 'TestMySQLPending'}]
        summary = profiles.result_summary(events, expected, 'example/backend')
        lines = profiles.execution_failure_diagnostics(events, summary)
        rendered = '\n'.join(lines)
        self.assertIn('not a cause attribution', rendered)
        self.assertIn('TestMySQLPending', rendered)
        self.assertIn('TestMySQLNotStarted', rendered)
        self.assertFalse(summary['complete'])
        self.assertLessEqual(len(lines), 40)

    def test_failure_sources_ignore_passing_test_logs_and_mask_error_values(self):
        package = 'example/backend/handler'
        events = [
            {'Action': 'output', 'Package': package, 'Test': 'TestMySQLPassing',
             'Output': '    passing_test.go:9: intentional passing log\n'},
            {'Action': 'pass', 'Package': package, 'Test': 'TestMySQLPassing'},
            {'Action': 'output', 'Package': package, 'Test': 'TestMySQLMoney',
             'Output': '    money_test.go:33: subscription value evidence is incomplete or inconsistent private-payload\n'},
            {'Action': 'fail', 'Package': package, 'Test': 'TestMySQLMoney'},
        ]
        expected = {('./handler', 'TestMySQLPassing'), ('./handler', 'TestMySQLMoney')}
        summary = profiles.result_summary(events, expected, 'example/backend')
        rendered = '\n'.join(profiles.execution_failure_diagnostics(events, summary))
        self.assertIn('money_test.go:33', rendered)
        self.assertIn('value_evidence_inconsistent', rendered)
        self.assertNotIn('passing_test.go', rendered)
        self.assertNotIn('private-payload', rendered)


    def test_download_diagnostics_do_not_corrupt_json_evidence(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder); log = root / 'run.jsonl'; commands = []
            command = [sys.executable, '-c', "import json,sys; sys.stderr.write('go: downloading example\\n'); print(json.dumps({'Action':'pass'}))"]
            self.assertEqual(profiles.run_command(command, root, log, commands), 0)
            self.assertEqual(profiles.read_events(log), [{'Action': 'pass'}])
            self.assertIn('go: downloading', Path(commands[0]['stderr']).read_text())

    def test_fingerprints_bind_production_and_untracked_sql_not_just_tests(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder); subprocess.run(['git', 'init', '-q', str(root)], check=True)
            backend = root / 'backend'; backend.mkdir()
            source = backend / 'source.go'; source.write_text('package main\n')
            (backend / 'go.mod').write_text('module example/backend\n')
            (backend / 'pending.sql').write_text('SELECT 1;\n')
            (backend / 'private.env').write_text('not evidence content\n')
            before = profiles.source_fingerprints(root)
            self.assertEqual(set(before), {'backend/source.go', 'backend/go.mod', 'backend/pending.sql'})
            source.write_text('package changed\n')
            self.assertNotEqual(before, profiles.source_fingerprints(root))


if __name__ == '__main__':
    unittest.main()
