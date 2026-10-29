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
            result['cases'][case]['executed'] = True
            run_owned([str(binaries['handler']), '-test.run', '^'+test+'$', '-test.timeout', timeout, '-test.v'], backend/'internal'/'handler', case_env, private/(case+'-private.log'), 300 if case == 'refund' else 240, 'case_failed')
            browser_path, financial_path = case_root/'browser-checks.json', case_root/'financial-checks.json'
            result['cases'][case] = case_verdict(json.loads(browser_path.read_text()), json.loads(financial_path.read_text()))
            result['cases'][case].update(result_sha256=sha(browser_path), financial_sha256=sha(financial_path))
            if not result['cases'][case]['passed']: raise AcceptanceFailure('case_failed')
        if any(sha(path) != result['binary_sha256'][name] for name, path in binaries.items()) or tree_hash(web) != dist_hash or tree_hash(dist) != dist_hash: raise AcceptanceFailure('source_changed')
    except AcceptanceFailure as error:
        result['error_class'] = error.kind
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
