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


if __name__ == '__main__':
    unittest.main()
