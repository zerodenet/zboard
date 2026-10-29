#!/usr/bin/env python3
"""Serial existing browser cases, exact source, fresh private fixtures, safe summary only."""
import argparse
import hashlib
import importlib.metadata
import json
import os
from pathlib import Path
import re
import shutil
import signal
import subprocess
import sys
import traceback

PLAYWRIGHT_VERSION = '1.62.0'
CASES = ('session', 'refund', 'recurring')
ERRORS = ('none', 'preflight_failed', 'build_failed', 'case_failed', 'command_timeout',
          'incomplete_evidence', 'source_changed', 'internal_error')


class AcceptanceFailure(Exception):
    def __init__(self, kind):
        if kind not in ERRORS: raise ValueError('unknown result class')
        self.kind = kind


def sha(path):
    h = hashlib.sha256()
    with Path(path).open('rb') as stream:
        for part in iter(lambda: stream.read(1024 * 1024), b''): h.update(part)
    return h.hexdigest()


def write_json(path, value):
    with path.open('x') as stream: json.dump(value, stream, indent=2); stream.write('\n')
    path.chmod(0o600)


def source_binding(root):
    head = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=root, text=True).strip()
    names = subprocess.check_output(['git', 'ls-files', '-z'], cwd=root).decode().split('\0')
    rows = []
    for name in sorted(filter(None, names)):
        p = root / name
        if p.is_symlink(): rows.append((name, 'symlink', hashlib.sha256(os.readlink(p).encode()).hexdigest()))
        elif p.is_file(): rows.append((name, 'file', sha(p)))
        else: rows.append((name, 'absent', ''))
    digest = hashlib.sha256(json.dumps([head, rows], separators=(',', ':')).encode()).hexdigest()
    return {'head': head, 'sha256': digest, 'files': rows}


def tree_hash(directory):
    rows = [(str(p.relative_to(directory)), sha(p)) for p in sorted(directory.rglob('*')) if p.is_file()]
    return hashlib.sha256(json.dumps(rows, separators=(',', ':')).encode()).hexdigest()


def case_verdict(browser, financial=None, session=False):
    """Never publish raw labels, errors, paths, identities, screenshots or HTTP data."""
    checks = browser.get('checks')
    if not isinstance(checks, dict) or not checks: raise AcceptanceFailure('incomplete_evidence')
    values = []
    for value in checks.values():
        flag = value.get('passed') if session and isinstance(value, dict) else value
        if type(flag) is not bool: raise AcceptanceFailure('incomplete_evidence')
        values.append(flag)
    passed = browser.get('passed') is True and all(values)
    if session: passed = passed and len(values) == 4
    else: passed = passed and isinstance(financial, dict) and financial.get('passed') is True
    return {'executed': True, 'passed': passed, 'checks_total': len(values), 'checks_passed': sum(values)}


def public_summary(binding=None):
    return {'schema_version': 1, 'source_head': binding['head'] if binding else '',
            'source_sha256': binding['sha256'] if binding else '', 'source_unchanged': False,
            'binary_sha256': {}, 'frontend_source_sha256': '', 'frontend_dist_sha256': '',
            'browser_executable_sha256': '', 'playwright_version_verified': False,
            'cases': {name: {'executed': False, 'passed': False, 'checks_total': 0, 'checks_passed': 0} for name in CASES},
            'passed': False, 'error_class': 'none'}


def run_owned(command, cwd, env, log_path, timeout, error_class):
    """Only this explicit child session is signalled after its fixed deadline."""
    with log_path.open('x') as log:
        process = subprocess.Popen(command, cwd=cwd, env=env, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
        try:
            code = process.wait(timeout=timeout)
        except subprocess.TimeoutExpired:
            os.killpg(process.pid, signal.SIGTERM)
            try: process.wait(timeout=15)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait(timeout=5)
            raise AcceptanceFailure('command_timeout')
    if code != 0: raise AcceptanceFailure(error_class)

def private_failure(directory):
    try:
        (directory/'failure-private.txt').write_text(traceback.format_exc())
        return True
    except OSError:
        return False


# Positive source-owned labels only. No private error/payload/path is copied.
DIAGNOSTIC_LABELS = {
    "checks": {
        "session": [
            "account_switch_discards_old_draft",
            "account_switch_does_not_replay_old_checkout",
            "account_switch_isolation",
            "account_switch_keeps_route_with_fresh_context",
            "background_inert",
            "current_fixture_token_present",
            "current_permissions_refreshed",
            "dialog_initial_focus",
            "dialog_outside_inert_boundary",
            "dialog_tab_focus_trapped",
            "escape_does_not_dismiss_reauthentication",
            "expired_write_did_not_mutate_setting",
            "financial_write_not_replayed",
            "fixture_only_accounts",
            "fresh_isolated_fixture",
            "inert_prevents_background_focus",
            "no_topup_or_wallet_entry_created",
            "one_reauthentication_dialog",
            "revoked_admin_draft_not_exposed",
            "revoked_admin_editor_removed",
            "revoked_admin_get_is_forbidden",
            "revoked_admin_session_still_valid",
            "same_account_admin_revocation",
            "same_account_settings",
            "settings_background_focus_restored",
            "settings_original_dom_preserved",
            "settings_original_url_preserved",
            "settings_same_account_dom_preserved",
            "settings_same_account_draft_preserved",
            "settings_same_account_url_preserved",
            "settings_write_not_replayed",
            "stable_frontend_snapshot",
            "two_real_http_401_responses",
            "wallet_original_url_preserved",
            "wallet_same_account_draft_preserved"
        ],
        "refund": [
            "actual_auth_available_admin",
            "actual_auth_available_owner",
            "actual_refund_identity",
            "admin_reads_original_owned_ticket_without_reply_write",
            "all_replayed_payloads_identical",
            "confirmation_before_post_0",
            "confirmation_before_post_1",
            "corrected_intent_requires_new_explicit_key",
            "exact_refund_post_roles_and_count",
            "explicit_approval_status_1",
            "explicit_approval_status_2",
            "explicit_same_key_replay_1",
            "explicit_same_key_replay_2",
            "external_release_requests_blocked_before_network",
            "foreign_owner_ticket_rejected",
            "isolated_login_tokens",
            "known_result_reload_and_get_never_post",
            "no_automatic_financial_writes_during_reads",
            "only_approval_references_and_amount",
            "only_expected_permission_and_owner_refusals",
            "owner_cannot_approve_admin_refund",
            "owner_wallet_once",
            "real_login_admin",
            "real_login_owner",
            "real_owner_ticket_created",
            "real_role_admin",
            "real_role_owner",
            "separate_browser_contexts",
            "terminal_access_not_recreated",
            "terminal_subscription_not_active",
            "ticket_context_never_auto_submits",
            "ticket_owned_references"
        ],
        "recurring": [
            "120_base_20_discount_100_debit_visible",
            "defaults_disabled_without_automatic_put",
            "explicit_put_1",
            "explicit_put_2",
            "explicit_put_3",
            "explicit_put_4",
            "four_explicit_mutations_no_automatic_replay",
            "no_api_failures_or_mock_fallbacks",
            "no_external_requests",
            "one_explicit_enable_no_replay",
            "only_consent_references_1",
            "only_consent_references_2",
            "only_consent_references_3",
            "only_consent_references_4",
            "real_http_login",
            "real_put_success_1",
            "real_put_success_2",
            "real_put_success_3",
            "real_put_success_4",
            "unavailable_keeps_withdrawable_consent",
            "worker_order_visible_without_consent_write"
        ]
    },
    "stages": {
        "session": [
            "account_switch_isolation",
            "complete",
            "financial_write_not_replayed",
            "frontend_snapshot",
            "same_account_admin_revocation",
            "same_account_settings",
            "seed"
        ],
        "refund": [
            "actual_admin_approval",
            "actual_audit_and_support_readback",
            "admin_same_owner_ticket_boundary",
            "complete",
            "launch",
            "owner_historical_order",
            "owner_ticket",
            "owner_wallet_and_terminal_rights",
            "reload_and_explicit_same_key_replays"
        ],
        "recurring": [
            "complete",
            "enable",
            "launch",
            "offer",
            "provider_unavailable",
            "real_login",
            "real_worker_debit"
        ]
    }
}
CASE_GO = {'refund': ('TestConsumptionRefundChromiumAcceptance', 'consumption_refund_browser_test.go'),
           'recurring': ('TestAutomaticRecurringChromiumAcceptance', 'automatic_recurring_browser_test.go')}


def public_http_failures(root, failures):
    """Only repository OpenAPI templates, fixed roles/statuses and counts."""
    if not isinstance(failures, list): return None
    templates = re.findall(r'^  (/api/v1/[A-Za-z0-9_/{\}-]+):$',
                           (root/'backend/api/openapi.yaml').read_text(), re.MULTILINE)
    patterns = []
    for template in templates:
        parts = re.split(r'(\{[A-Za-z_]\w*\})', template)
        pattern = ''.join('[^/?]+' if re.fullmatch(r'\{[A-Za-z_]\w*\}', part) else re.escape(part) for part in parts)
        patterns.append((template, re.compile(pattern)))
    counts = {}
    for row in failures[:1000]:
        if not isinstance(row, dict) or type(row.get('status')) is not int or not 400 <= row['status'] <= 599: continue
        role = row.get('role') if row.get('role') in ('owner', 'admin') else 'unknown'
        path = row.get('path')
        endpoint = next((template for template, pattern in patterns if isinstance(path, str) and pattern.fullmatch(path)), 'unknown_endpoint')
        key = role, endpoint, row['status']
        counts[key] = counts.get(key, 0) + 1
    return [{'role': role, 'endpoint_template': endpoint, 'status': status, 'count': count}
            for (role, endpoint, status), count in sorted(counts.items())]


def failure_diagnostic(root, private, case):
    result = {'go_outcome': 'not_observed', 'source_locations': [],
              'browser_error': 'not_observed', 'stage': 'not_observed', 'checks': None, 'http_failures': None}
    def bounded(path):
        if path.is_symlink() or not path.is_file(): return ''
        with path.open('rb') as stream: return stream.read(512 * 1024).decode('utf-8', errors='replace')
    try:
        output = bounded(private/(case+'-private.log'))
        if case in CASE_GO:
            test, filename = CASE_GO[case]
            if 'panic: test timed out' in output: result['go_outcome'] = 'timeout'
            elif '--- SKIP: '+test in output: result['go_outcome'] = 'skipped'
            elif '--- FAIL: '+test in output: result['go_outcome'] = 'failed'
            elif '--- PASS: '+test in output: result['go_outcome'] = 'passed'
            limit = len((root/'backend/internal/handler'/filename).read_text().splitlines())
            locations = set()
            for match in re.finditer(r'(?m)^\s+'+re.escape(filename)+r':([0-9]+):', output):
                line = int(match[1])
                if 0 < line <= limit: locations.add(filename+':'+str(line))
            result['source_locations'] = sorted(locations)[:12]
        browser_text = bounded(private/case/'browser-private.log')
        fragments = (
            ("No module named 'playwright'", 'browser_dependency_missing'),
            ('No usable sandbox', 'browser_sandbox_unavailable'),
            ('BrowserType.launch', 'browser_launch_failed'),
            ('Go evidence check:', 'coordination_timeout'),
            ('AssertionError', 'browser_assertion_failed'),
            ('TimeoutError', 'browser_timeout'),
        )
        for fragment, kind in fragments:
            if fragment in browser_text:
                result['browser_error'] = kind; break
        summary = bounded(private/case/'browser-checks.json')
        if summary:
            data = json.loads(summary)
            result['http_failures'] = public_http_failures(root, data.get('api_failures'))
            stage = data.get('stage')
            if stage in DIAGNOSTIC_LABELS['stages'][case]: result['stage'] = stage
            checks = data.get('checks')
            if isinstance(checks, dict):
                result['checks'] = {name: value for name, value in checks.items()
                    if name in DIAGNOSTIC_LABELS['checks'][case] and type(value) is bool}
        elif case != 'session' and not browser_text:
            result['browser_error'] = 'browser_not_started_or_no_evidence'
    except (OSError, ValueError, TypeError, KeyError):
        result['browser_error'] = 'diagnostic_incomplete'
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--checkout', type=Path, required=True)
    parser.add_argument('--revision', required=True)
    parser.add_argument('--chromium', type=Path, required=True)
    parser.add_argument('--private-dir', type=Path, required=True)
    parser.add_argument('--summary', type=Path, required=True)
    args = parser.parse_args()
    os.umask(0o077)
    root, private, summary_path = args.checkout.resolve(), args.private_dir.absolute(), args.summary.absolute()
    result = public_summary()
    before = None
    created = False
    summary_permitted = False
    active_case = None
    try:
        if os.geteuid() == 0 or sys.platform != 'linux': raise AcceptanceFailure('preflight_failed')
        if not re.fullmatch('[a-f0-9]{40}', args.revision): raise AcceptanceFailure('preflight_failed')
        if private.exists() or private.is_symlink() or summary_path.exists() or summary_path.is_symlink(): raise AcceptanceFailure('preflight_failed')
        if private.resolve().is_relative_to(root) or summary_path.resolve().is_relative_to(root): raise AcceptanceFailure('preflight_failed')
        summary_permitted = True
        if not args.chromium.is_absolute() or not args.chromium.is_file() or not os.access(args.chromium, os.X_OK): raise AcceptanceFailure('preflight_failed')
        try: playwright_version = importlib.metadata.version('playwright')
        except importlib.metadata.PackageNotFoundError: raise AcceptanceFailure('preflight_failed')
        if playwright_version != PLAYWRIGHT_VERSION: raise AcceptanceFailure('preflight_failed')
        if subprocess.check_output(['git', 'status', '--porcelain'], cwd=root): raise AcceptanceFailure('preflight_failed')
        before = source_binding(root)
        if before['head'] != args.revision: raise AcceptanceFailure('preflight_failed')
        result = public_summary(before)
        result['playwright_version_verified'] = True
        result['browser_executable_sha256'] = sha(args.chromium)
        if shutil.disk_usage(private.parent).free < 2 * 1024**3: raise AcceptanceFailure('preflight_failed')
        private.mkdir(mode=0o700)
        created = True
        write_json(private/'source-before.json', before)
        result['frontend_source_sha256'] = tree_hash(root/'frontend'/'src')
        env = {k: v for k, v in os.environ.items() if not k.startswith('ZBOARD_')}
        env.update(GOMAXPROCS='2', GOFLAGS='-p=1 -mod=readonly', PYTHONDONTWRITEBYTECODE='1',
                   PATH=str(Path(sys.executable).parent)+os.pathsep+os.environ.get('PATH', ''))
        backend = root/'backend'
        binaries = {'server': private/'zboard', 'seed': private/'seed', 'handler': private/'handler.test'}
        for label, target, package in (('server', binaries['server'], './cmd/zboard'), ('seed', binaries['seed'], './tools/acceptance')):
            run_owned(['go', 'build', '-p', '1', '-mod=readonly', '-o', str(target), package], backend, env, private/(label+'-build-private.log'), 900, 'build_failed')
            result['binary_sha256'][label] = sha(target)
        run_owned(['go', 'test', '-c', '-p', '1', '-mod=readonly', '-o', str(binaries['handler']), './internal/handler'], backend, env, private/'handler-build-private.log', 900, 'build_failed')
        result['binary_sha256']['handler'] = sha(binaries['handler'])
        run_owned(['pnpm', 'build'], root/'frontend', env, private/'frontend-build-private.log', 600, 'build_failed')
        web = private/'web'
        dist = root/'frontend'/'dist'
        dist_hash = tree_hash(dist)
        shutil.copytree(dist, web)
        if tree_hash(web) != dist_hash or tree_hash(dist) != dist_hash: raise AcceptanceFailure('source_changed')
        result['frontend_dist_sha256'] = dist_hash
        case_root = private/'session'
        case_root.mkdir(mode=0o700)
        active_case = 'session'
        result['cases']['session']['executed'] = True
        run_owned([sys.executable, str(root/'scripts/acceptance-session-browser.py'), '--server-binary', str(binaries['server']), '--seed-binary', str(binaries['seed']), '--backend-source', str(root), '--web-dir', str(web), '--chromium', str(args.chromium), '--evidence-root', str(case_root)], root, env, private/'session-private.log', 300, 'case_failed')
        children = list(case_root.glob('session-browser-*'))
        if len(children) != 1: raise AcceptanceFailure('incomplete_evidence')
        evidence = children[0]/'result.json'
        result['cases']['session'] = case_verdict(json.loads(evidence.read_text()), session=True)
        result['cases']['session']['result_sha256'] = sha(evidence)
        if not result['cases']['session']['passed']: raise AcceptanceFailure('case_failed')
        for case, prefix, test, script, timeout in (
            ('refund', 'ZBOARD_CONSUMPTION_REFUND_BROWSER', 'TestConsumptionRefundChromiumAcceptance', 'acceptance-consumption-refund-browser.py', '5m'),
            ('recurring', 'ZBOARD_RECURRING_BROWSER', 'TestAutomaticRecurringChromiumAcceptance', 'acceptance-recurring-browser.py', '4m')):
            case_root = private/case
            case_root.mkdir(mode=0o700)
            case_env = env.copy()
            case_env.update({prefix: '1', prefix+'_EVIDENCE_DIR': str(case_root), prefix+'_WEB_DIR': str(web),
                             prefix+'_SCRIPT': str(root/'scripts'/script), 'ZBOARD_BROWSER_CHROMIUM': str(args.chromium)})
            active_case = case
            result['cases'][case]['executed'] = True
            run_owned([str(binaries['handler']), '-test.run', '^'+test+'$', '-test.timeout', timeout, '-test.v'], backend/'internal'/'handler', case_env, private/(case+'-private.log'), 300 if case == 'refund' else 240, 'case_failed')
            browser_path, financial_path = case_root/'browser-checks.json', case_root/'financial-checks.json'
            result['cases'][case] = case_verdict(json.loads(browser_path.read_text()), json.loads(financial_path.read_text()))
            result['cases'][case].update(result_sha256=sha(browser_path), financial_sha256=sha(financial_path))
            if not result['cases'][case]['passed']: raise AcceptanceFailure('case_failed')
        if any(sha(path) != result['binary_sha256'][name] for name, path in binaries.items()) or tree_hash(web) != dist_hash or tree_hash(dist) != dist_hash: raise AcceptanceFailure('source_changed')
    except AcceptanceFailure as error:
        result['error_class'] = error.kind
        if created and active_case in CASES:
            result['failure_diagnostic'] = failure_diagnostic(root, private, active_case)
        if created and not private_failure(private): result['error_class'] = 'incomplete_evidence'
    except Exception:
        result['error_class'] = 'internal_error'
        if created and not private_failure(private): result['error_class'] = 'incomplete_evidence'
    finally:
        if before is not None:
            try:
                after = source_binding(root)
                result['source_unchanged'] = before == after and not subprocess.check_output(['git', 'status', '--porcelain'], cwd=root)
                if created: write_json(private/'source-after.json', after)
                if not result['source_unchanged']: result['error_class'] = 'source_changed'
            except Exception: result['error_class'] = 'source_changed'
        result['passed'] = result['error_class'] == 'none' and result['source_unchanged'] and all(result['cases'][name]['executed'] and result['cases'][name]['passed'] for name in CASES)
        if summary_permitted and not summary_path.exists() and not summary_path.is_symlink():
            try:
                write_json(summary_path, result)
            except OSError:
                result.update(passed=False, error_class='incomplete_evidence')
    print(json.dumps(result, sort_keys=True))
    return 0 if result['passed'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
