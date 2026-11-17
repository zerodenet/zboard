"""Offline controller/privacy/sandbox contracts; no browser, Go, SQL or network."""
import ast
import contextlib
import importlib.util
import json
import io
from pathlib import Path
import signal
import subprocess
import tempfile
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parent.parent
spec = importlib.util.spec_from_file_location('browser_serial', ROOT/'scripts/acceptance-browser-serial.py')
runner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner)


class BrowserSerialPolicyTest(unittest.TestCase):
    def test_http_failure_projection_exposes_only_public_templates_and_counts(self):
        result = runner.public_http_failures(ROOT, [
            {'role': 'admin', 'path': '/api/v1/admin/orders/27/consumption-refunds', 'status': 400},
            {'role': 'admin', 'path': '/api/v1/admin/orders/28/consumption-refunds', 'status': 400},
            {'role': 'PRIVATE_ROLE', 'path': '/api/v1/PRIVATE_TOKEN', 'status': 404},
            {'role': 'owner', 'path': '/api/v1/PRIVATE_TOKEN', 'status': 'PRIVATE_STATUS'},
            {'role': 'owner', 'path': '/api/v1/PRIVATE_TOKEN', 'status': True}])
        self.assertIn({'role': 'admin', 'endpoint_template': '/api/v1/admin/orders/{id}/consumption-refunds', 'status': 400, 'count': 2}, result)
        self.assertIn({'role': 'unknown', 'endpoint_template': 'unknown_endpoint', 'status': 404, 'count': 1}, result)
        self.assertNotIn('PRIVATE_', json.dumps(result))
        self.assertNotIn('27', json.dumps(result))
        self.assertIsNone(runner.public_http_failures(ROOT, None))

    def test_failed_refund_diagnostic_exposes_only_source_owned_labels(self):
        with tempfile.TemporaryDirectory() as directory:
            private = Path(directory)
            (private/'refund').mkdir()
            (private/'refund-private.log').write_text(
                '--- FAIL: TestConsumptionRefundChromiumAcceptance (0.1s)\n'
                '    consumption_refund_browser_test.go:148: PRIVATE_TOKEN_AND_SQL\n'
                '    outside_secret.go:9: PRIVATE_ACCOUNT\n'
                '    consumption_refund_browser_test.go:999999: UNKNOWN_LOCATION\n')
            (private/'refund/browser-private.log').write_text('AssertionError: PRIVATE_PASSWORD')
            (private/'refund/browser-checks.json').write_text(json.dumps({
                'stage': 'owner_ticket', 'checks': {'real_login_owner': True,
                'PRIVATE_CUSTOM_CHECK': False, 'ticket_owned_references': 'PRIVATE_VALUE'},
                'private_error': 'PRIVATE_ERROR', 'api_failures': [{'token': 'PRIVATE_TOKEN'}]}))
            result = runner.failure_diagnostic(ROOT, private, 'refund')
            self.assertEqual(result['go_outcome'], 'failed')
            self.assertEqual(result['source_locations'], ['consumption_refund_browser_test.go:148'])
            self.assertEqual(result['browser_error'], 'browser_assertion_failed')
            self.assertEqual(result['stage'], 'owner_ticket')
            self.assertEqual(result['checks'], {'real_login_owner': True})
            self.assertNotIn('PRIVATE_', json.dumps(result))
            self.assertNotIn('outside_secret', json.dumps(result))

    def test_missing_and_unknown_browser_summary_never_become_false_check_counts(self):
        with tempfile.TemporaryDirectory() as directory:
            private = Path(directory); (private/'refund').mkdir()
            missing = runner.failure_diagnostic(ROOT, private, 'refund')
            self.assertIsNone(missing['checks'])
            self.assertEqual(missing['browser_error'], 'browser_not_started_or_no_evidence')
            (private/'refund/browser-checks.json').write_text(json.dumps({'stage': 'PRIVATE_STAGE', 'checks': []}))
            result = runner.failure_diagnostic(ROOT, private, 'refund')
            self.assertEqual(result['stage'], 'not_observed')
            self.assertIsNone(result['checks'])
            self.assertNotIn('PRIVATE_STAGE', json.dumps(result))

    def test_diagnostic_rejects_symlink_evidence_and_hides_io_errors(self):
        with tempfile.TemporaryDirectory() as directory:
            private = Path(directory); (private/'refund').mkdir()
            secret = private/'not-owned.log'; secret.write_text('AssertionError PRIVATE_TOKEN')
            (private/'refund/browser-private.log').symlink_to(secret)
            result = runner.failure_diagnostic(ROOT, private, 'refund')
            self.assertEqual(result['browser_error'], 'browser_not_started_or_no_evidence')
            with mock.patch.object(Path, 'open', side_effect=OSError('PRIVATE_PATH')):
                result = runner.failure_diagnostic(ROOT, private, 'refund')
            self.assertEqual(result['browser_error'], 'diagnostic_incomplete')
            self.assertNotIn('PRIVATE_PATH', json.dumps(result))

    def test_rejected_checkout_summary_is_never_written(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)/'checkout'; root.mkdir()
            summary = root/'summary.json'
            argv = ['acceptance-browser-serial.py', '--checkout', str(root), '--revision', 'a'*40,
                    '--private-dir', str(Path(directory)/'private'), '--chromium', '/unused', '--summary', str(summary)]
            with mock.patch('sys.argv', argv), mock.patch.object(runner.os, 'geteuid', return_value=1000), \
                    contextlib.redirect_stdout(io.StringIO()):
                self.assertEqual(runner.main(), 1)
            self.assertFalse(summary.exists())

    def test_public_summary_io_error_is_fixed_and_never_passes(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)/'checkout'; root.mkdir()
            argv = ['acceptance-browser-serial.py', '--checkout', str(root), '--revision', 'a'*40,
                    '--private-dir', str(Path(directory)/'private'), '--chromium', '/unused',
                    '--summary', str(Path(directory)/'summary.json')]
            stdout = io.StringIO()
            with mock.patch('sys.argv', argv), mock.patch.object(runner.os, 'geteuid', return_value=1000), \
                    mock.patch.object(runner, 'write_json', side_effect=OSError('PRIVATE_PATH_SECRET')), contextlib.redirect_stdout(stdout):
                self.assertEqual(runner.main(), 1)
            result = json.loads(stdout.getvalue())
            self.assertFalse(result['passed'])
            self.assertEqual(result['error_class'], 'incomplete_evidence')
            self.assertNotIn('PRIVATE_PATH_SECRET', stdout.getvalue())

    def test_public_verdict_never_copies_fixture_or_private_error_values(self):
        private = {'passed': True, 'checks': {'secret-account-name': True},
                   'private_error': 'password-and-token', 'origin': 'private-origin', 'refund_id': 123}
        result = runner.case_verdict(private, {'passed': True, 'password': 'private-password'})
        raw = json.dumps(result)
        for secret in ('secret-account-name', 'password', 'token', 'origin', 'refund_id'):
            self.assertNotIn(secret, raw)
        self.assertEqual(result, {'executed': True, 'passed': True, 'checks_total': 1, 'checks_passed': 1})

    def test_failed_or_missing_financial_evidence_cannot_pass(self):
        browser = {'passed': True, 'checks': {'real_http': True}}
        for financial in (None, {}, {'passed': False}, {'passed': 'true'}):
            self.assertFalse(runner.case_verdict(browser, financial)['passed'])
        self.assertFalse(runner.case_verdict({'passed': False, 'checks': {'real_http': True}}, {'passed': True})['passed'])
        self.assertFalse(runner.case_verdict({'passed': True, 'checks': {'real_http': False}}, {'passed': True})['passed'])

    def test_session_requires_all_four_existing_case_results(self):
        complete = {'passed': True, 'checks': {str(n): {'passed': True} for n in range(4)}}
        self.assertTrue(runner.case_verdict(complete, session=True)['passed'])
        complete['checks'].pop('3')
        self.assertFalse(runner.case_verdict(complete, session=True)['passed'])

    def test_unknown_incomplete_checks_fail_closed(self):
        for checks in (None, {}, {'case': 'true'}, {'case': 1}, {'case': {'arbitrary': True}}):
            with self.assertRaises(runner.AcceptanceFailure):
                runner.case_verdict({'passed': True, 'checks': checks}, {'passed': True})

    def test_new_summary_has_no_executed_or_passing_case(self):
        summary = runner.public_summary({'head': 'a'*40, 'sha256': 'b'*64})
        self.assertFalse(summary['passed'])
        self.assertFalse(summary['source_unchanged'])
        self.assertEqual(tuple(summary['cases']), runner.CASES)
        self.assertTrue(all(not case['executed'] and not case['passed'] for case in summary['cases'].values()))
        with self.assertRaises(ValueError): runner.AcceptanceFailure('raw-sensitive-message')

    def test_source_binding_detects_modified_tracked_file_even_if_head_unchanged(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root/'source.go').write_text('old source')
            def git(command, **kwargs):
                return 'a'*40+'\n' if command[1] == 'rev-parse' else b'source.go\0'
            with mock.patch.object(runner.subprocess, 'check_output', side_effect=git):
                before = runner.source_binding(root)
                (root/'source.go').write_text('new source')
                after = runner.source_binding(root)
            self.assertEqual(before['head'], after['head'])
            self.assertNotEqual(before['sha256'], after['sha256'])

    def test_timeout_signals_only_owned_new_process_group_without_restart(self):
        with tempfile.TemporaryDirectory() as directory:
            process = mock.Mock(pid=4242)
            process.wait.side_effect = [subprocess.TimeoutExpired('owned', 4), subprocess.TimeoutExpired('owned', 15), 0]
            with mock.patch.object(runner.subprocess, 'Popen', return_value=process) as start, mock.patch.object(runner.os, 'killpg') as kill:
                with self.assertRaises(runner.AcceptanceFailure) as raised:
                    runner.run_owned(['owned'], directory, {}, Path(directory)/'private.log', 4, 'case_failed')
            self.assertEqual(raised.exception.kind, 'command_timeout')
            start.assert_called_once()
            self.assertTrue(start.call_args.kwargs['start_new_session'])
            self.assertEqual(kill.call_args_list, [mock.call(4242, signal.SIGTERM), mock.call(4242, signal.SIGKILL)])

    def test_all_browser_launches_enable_sandbox_without_security_bypass(self):
        for name in ('session', 'consumption-refund', 'recurring'):
            source = (ROOT/f'scripts/acceptance-{name}-browser.py').read_text()
            tree = ast.parse(source)
            launches = [node for node in ast.walk(tree) if isinstance(node, ast.Call) and isinstance(node.func, ast.Attribute) and node.func.attr == 'launch']
            self.assertEqual(len(launches), 1)
            options = {kw.arg: kw.value for kw in launches[0].keywords}
            self.assertIs(ast.literal_eval(options['chromium_sandbox']), True)
            arguments = ast.literal_eval(options['args'])
            for flag in arguments:
                self.assertNotIn(flag, ('--no-sandbox', '--disable-setuid-sandbox', '--ignore-certificate-errors', '--disable-web-security'))
            self.assertIn('args.chromium', ast.unparse(options['executable_path']))

    def test_one_shot_workflow_only_uploads_safe_summary_and_never_installs_browser(self):
        workflow = (ROOT/'.github/workflows/browser-acceptance.yml').read_text()
        self.assertIn("github.event.label.name == 'validation:browser-full'", workflow)
        self.assertIn('types: [labeled]', workflow)
        self.assertNotIn('push:', workflow)
        for forbidden in ('apt-get', 'playwright install', 'sudo', 'ln -s', '--no-sandbox', '--ignore-certificate-errors', '--index-url', '--extra-index-url'):
            self.assertNotIn(forbidden, workflow)
        self.assertIn('--retries 0', workflow)
        self.assertIn('path: ${{ runner.temp }}/zboard-browser-summary.json', workflow)
        self.assertNotIn('path: ${{ runner.temp }}/zboard-browser-private', workflow)
        self.assertIn('playwright=='+runner.PLAYWRIGHT_VERSION, (ROOT/'scripts/browser-acceptance-requirements.txt').read_text())
        source = (ROOT/'scripts/acceptance-browser-serial.py').read_text()
        self.assertIn("backend/'internal'/'handler'", source)
        self.assertIn("'TestConsumptionRefundChromiumAcceptance'", source)
        self.assertIn("'TestAutomaticRecurringChromiumAcceptance'", source)
        self.assertIn("'5m'", source)
        self.assertIn("'4m'", source)
        self.assertIn("case_verdict", source)
        for filename in ('consumption_refund_browser_test.go', 'automatic_recurring_browser_test.go'):
            go = (ROOT/'backend/internal/handler'/filename).read_text()
            self.assertIn('os.Getenv("ZBOARD_BROWSER_CHROMIUM")', go)
            self.assertIn('"--chromium", chromium', go)


if __name__ == '__main__':
    unittest.main()
