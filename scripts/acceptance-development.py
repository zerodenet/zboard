#!/usr/bin/env python3
"""Serial development acceptance; absent evidence is never a passing stage."""
import argparse
import ast
import datetime
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import socket
import subprocess
import sys
import threading
import time

EXCLUDES = ('src/theme/designTokens.test.ts', 'src/views/commercePurchaseExperience.test.ts',
            'src/utils/adminRowActionsAudit.test.ts', 'src/utils/catalogScalePolicy.test.ts',
            'src/utils/formPolicy.test.ts', 'src/utils/frontendArchitecturePolicy.test.ts')
FUNCTIONAL = ('backend-tidy', 'backend-module-consistency', 'backend-test', 'backend-vet', 'backend-build', 'sqlite-purego',
              'sdk-tidy', 'sdk-module-consistency', 'sdk-test', 'sdk-vet',
              'frontend-install', 'frontend-test', 'frontend-build', 'mysql-full')
PRINCIPAL_STATISTICS = ('principal-statistics-build', 'principal-statistics-sqlite', 'principal-statistics-mysql')
MANUAL = ('p7-upgrade', 'capacity-original', 'worker-prepare', 'worker-comparison', 'autoscale-prepare', 'autoscale') + PRINCIPAL_STATISTICS


def save(path, value):
    path.write_text(json.dumps(value, indent=2, sort_keys=True) + '\n')


def git(root, *args):
    return subprocess.check_output(['git', *args], cwd=root, stderr=subprocess.PIPE)


def source_binding(root):
    # Git-ignore excludes node_modules/dist/cache/fixture artifacts, never a
    # tracked source. Include deleted tracked files and new nonignored sources.
    paths = set(git(root, 'ls-files', '-z', '--cached', '--others', '--exclude-standard').decode().split('\0')) - {''}
    files = {}
    for name in sorted(paths):
        path = root / name
        if path.is_symlink():
            files[name] = {'kind': 'symlink', 'sha256': hashlib.sha256(os.readlink(path).encode()).hexdigest()}
        elif path.is_file():
            digest = hashlib.sha256()
            with path.open('rb') as stream:
                for chunk in iter(lambda: stream.read(1024 * 1024), b''):
                    digest.update(chunk)
            files[name] = {'kind': 'file', 'sha256': digest.hexdigest(), 'executable': bool(path.stat().st_mode & 0o111)}
        else:
            files[name] = {'kind': 'absent'}
    return {'head': git(root, 'rev-parse', 'HEAD').decode().strip(), 'files': files}


def mysql_target(dsn, isolated):
    if not dsn:
        raise ValueError('ZBOARD_TEST_MYSQL_DSN absent; real MySQL not executed')
    if not isolated:
        raise ValueError('--isolated-mysql is required to attest this is a disposable test instance')
    if len(dsn) > 4096:
        raise ValueError('test DSN exceeds bounded input size')
    match = re.fullmatch(r'.+@tcp\((127\.0\.0\.1|\[::1\]):([0-9]{1,5})\)/([^?]*)(?:\?.*)?', dsn)
    if not match or not 1 <= int(match[2]) <= 65535:
        raise ValueError('only explicit numeric loopback TCP test DSNs are accepted')
    if match[3]:
        raise ValueError('test DSN must select no existing database; fixtures create random isolated schemas')
    return match[1].strip('[]'), int(match[2])


def mysql_version(host, port):
    # Read only MySQL's unauthenticated greeting. No SQL, login, server setting,
    # database creation or password is needed for this version preflight.
    with socket.create_connection((host, port), timeout=3) as connection:
        connection.settimeout(3)
        packet = b''
        while len(packet) < 5 or b'\0' not in packet[5:]:
            chunk = connection.recv(256)
            if not chunk or len(packet) + len(chunk) > 4096:
                raise ValueError('unrecognized bounded MySQL handshake')
            packet += chunk
        if packet[4] != 10:
            raise ValueError('unsupported MySQL handshake')
        version = packet[5:].split(b'\0', 1)[0].decode('ascii')
    if not re.fullmatch(r'8\.4\.\d+[-+A-Za-z0-9.]*', version):
        raise ValueError('isolated MySQL 8.4 is required')
    return version


def secrets_for(dsn):
    secrets = [dsn.encode()] if dsn else []
    if '@tcp(' in dsn:
        account = dsn.rsplit('@tcp(', 1)[0]
        if ':' in account and account.split(':', 1)[1]:
            secrets.append(account.split(':', 1)[1].encode())
    return sorted(secrets, key=len, reverse=True)


def copy_redacted(pipe, target, secrets):
    keep = max((len(s) for s in secrets), default=1) - 1
    pending = b''
    while True:
        chunk = pipe.read(65536)
        pending += chunk
        cut = len(pending) if not chunk else max(0, len(pending) - keep)
        # A credential crossing the streaming boundary must be retained or
        # included completely, never partially emitted before replacement.
        changed = True
        while changed:
            changed = False
            for secret in secrets:
                start = pending.find(secret)
                while start != -1:
                    if start < cut < start + len(secret):
                        cut = start + len(secret)
                        changed = True
                    start = pending.find(secret, start + 1)
        ready, pending = pending[:cut], pending[cut:]
        for secret in secrets:
            ready = ready.replace(secret, b'[REDACTED]')
        target.write(ready)
        if not chunk:
            break
    target.flush()
    pipe.close()


def go_summary(path):
    counts = {'pass': 0, 'skip': 0, 'fail': 0}
    skips = []
    for line in path.read_text(errors='replace').splitlines():
        if not line.strip():
            continue
        event = json.loads(line)
        if event.get('Test') and event.get('Action') in counts:
            counts[event['Action']] += 1
            if event['Action'] == 'skip':
                skips.append({'package': event.get('Package'), 'test': event['Test']})
    return {'test_events': counts, 'skips': skips,
            'scope_note': 'Backend DSN is unset: MySQL/external skips are not MySQL or external acceptance.'}


def go_result_complete(summary, pure_sqlite=False):
    counts = summary['test_events']
    return counts['pass'] > 0 and counts['fail'] == 0 and (not pure_sqlite or counts['skip'] == 0)


def last_json(path):
    for line in reversed(path.read_text(errors='replace').splitlines()):
        try:
            value = json.loads(line)
            if isinstance(value, dict):
                return value
        except ValueError:
            pass
    return {}


def literal_revision_pins(source):
    constants = {}
    def literal(node):
        if isinstance(node, ast.Name) and node.id in constants:
            return constants[node.id]
        if isinstance(node, (ast.Tuple, ast.List)):
            return tuple(literal(item) for item in node.elts)
        return ast.literal_eval(node)
    for node in ast.parse(source).body:
        if isinstance(node, ast.Assign) and isinstance(node.targets[0], ast.Name) and node.targets[0].id in ('PIN', 'REVISIONS'):
            constants[node.targets[0].id] = literal(node.value)
    return constants.get('REVISIONS', ())


def principal_statistics_verdict(stdout, artifact_parent, prior_children, driver, head, helper_sha256):
    """Fail closed on plan-only, old source, the other DB or reused artifacts."""
    if stdout.get('status') != 'passed' or stdout.get('executed') is not True or stdout.get('driver') != driver:
        raise ValueError('principal statistics child did not execute this database successfully')
    if stdout.get('source_head') != head or stdout.get('source_unchanged') is not True or stdout.get('helper_sha256') != helper_sha256:
        raise ValueError('principal statistics child source/helper differs')
    for name in ('source_sha256_before', 'source_sha256_after'):
        if not re.fullmatch(r'[0-9a-f]{64}', str(stdout.get(name, ''))):
            raise ValueError('principal statistics child lacks source binding')
    if stdout['source_sha256_before'] != stdout['source_sha256_after']:
        raise ValueError('principal statistics child changed source')
    if type(stdout.get('sources')) is not int or not 1000 <= stdout['sources'] <= 100000 or type(stdout.get('windows')) is not int or not 32 <= stdout['windows'] <= 64 or stdout.get('timezone') != 'Asia/Kathmandu':
        raise ValueError('principal statistics original source/window/timezone coverage missing')
    if not isinstance(stdout.get('artifact_dir'), str) or not isinstance(stdout.get('summary_path'), str):
        raise ValueError('principal statistics artifact metadata must contain paths')
    artifact_path = Path(stdout.get('artifact_dir', ''))
    summary_path = Path(stdout.get('summary_path', ''))
    if not artifact_path.is_absolute() or not summary_path.is_absolute() or artifact_path.is_symlink() or summary_path.is_symlink():
        raise ValueError('principal statistics artifact paths must be owned absolute paths')
    artifact = artifact_path.resolve()
    parent = artifact_parent.resolve()
    summary = summary_path.resolve()
    if artifact.parent != parent or artifact in prior_children or summary != artifact / 'summary.json' or not summary.is_file() or summary.is_symlink():
        raise ValueError('principal statistics child has no new owned summary')
    if not 1 <= summary.stat().st_size <= 8 * 1024 * 1024:
        raise ValueError('principal statistics retained summary exceeds evidence bound')
    actual = json.loads(summary.read_text())
    if actual != stdout:
        raise ValueError('principal statistics stdout and retained summary differ')
    return {'summary_path': str(summary), 'summary_sha256': hashlib.sha256(summary.read_bytes()).hexdigest(),
            'artifact_dir': str(artifact), 'driver': driver, 'source_head': head, 'helper_sha256': helper_sha256,
            'source_unchanged': True, 'executed': True, 'sources': actual['sources'], 'windows': actual['windows'], 'timezone': actual['timezone']}


def autoscale_internal_result(measured, revision):
    if measured.get('status') != 'passed_internal_scope':
        return 'inconclusive' if measured.get('status') in ('inconclusive', 'measurements_preserved') and measured.get('benefit_verified') is False else 'failed'
    required = ('complete', 'all_scenarios_passed', 'source_unchanged', 'binaries_unchanged', 'benefit_prerequisite_revalidated', 'benefit_verified')
    if measured.get('revision') != revision or measured.get('scope') != 'p6_internal_only' or any(measured.get(key) is not True for key in required):
        return 'failed'
    if type(measured.get('run_count')) is not int or measured['run_count'] != 30 or type(measured.get('required_run_count')) is not int or measured['required_run_count'] != 30 or measured.get('external_quota_stop') != 'not_verified' or measured.get('failures') != [] or measured.get('incomplete') != []:
        return 'failed'
    report_path = measured.get('comparison_report')
    if not isinstance(report_path, str) or not Path(report_path).is_absolute():
        return 'failed'
    try:
        report = Path(report_path)
        if not 1 <= report.stat().st_size <= 32 * 1024 * 1024 or hashlib.sha256(report.read_bytes()).hexdigest() != measured.get('comparison_sha256'):
            return 'failed'
        actual = json.loads(report.read_text())
        # The retained complete scenario matrix, not merely stdout booleans,
        # must agree with the narrow result before this wrapper can aggregate it.
        for key in ('revision', 'status', 'scope', 'complete', 'run_count', 'required_run_count', *required, 'external_quota_stop', 'failures', 'incomplete'):
            if type(actual.get(key)) is not type(measured.get(key)) or actual.get(key) != measured.get(key):
                return 'failed'
        runs = actual.get('runs', [])
        expected = {(pair, scenario, auto) for pair in range(3) for scenario in ('normal', 'backlog', 'crash', 'dbslow', 'pluginbusy') for auto in (False, True)}
        seen, paths = set(), set()
        if len(runs) != 30:
            return 'failed'
        for run in runs:
            key = (run.get('pair'), run.get('scenario'), run.get('auto'))
            path = run.get('evidence')
            result = run.get('result', {})
            if type(run.get('pair')) is not int or type(run.get('auto')) is not bool or key not in expected or key in seen or not isinstance(path, str) or not path or str(Path(path).resolve()) in paths:
                return 'failed'
            if result.get('status') != 'passed_internal_scope' or result.get('failures') != [] or result.get('incomplete') != [] or result.get('original_gate_report', {}).get('passed') is not True:
                return 'failed'
            seen.add(key); paths.add(str(Path(path).resolve()))
        return 'passed' if seen == expected else 'failed'
    except (OSError, ValueError, TypeError, AttributeError):
        return 'failed'


def principal_statistics_selected(sqlite_only):
    return PRINCIPAL_STATISTICS[:2] if sqlite_only else PRINCIPAL_STATISTICS


def requested_stages(args):
    names = set(FUNCTIONAL)
    if args.sqlite_only:
        names.discard('mysql-full')
    if not args.with_sdk:
        names = {name for name in names if not name.startswith('sdk-')}
    if not args.with_frontend:
        names = {name for name in names if not name.startswith('frontend-')}
    # Explicitly skipping install still leaves a truthful incomplete functional
    # receipt. It is never silently erased from the requested checks.
    for enabled, stages in ((args.execute_p7, ('p7-upgrade',)),
                            (args.execute_capacity, ('capacity-original',)),
                            (args.execute_worker_comparison, ('worker-prepare', 'worker-comparison')),
                            (args.execute_autoscale, ('autoscale-prepare', 'autoscale'))):
        if enabled:
            names.update(stages)
    if getattr(args, 'execute_principal_statistics', False):
        names.update(principal_statistics_selected(args.sqlite_only))
    return names


def aggregate(stages, unchanged):
    successful = lambda names: all(stages[name]['status'] == 'passed' for name in names)
    return {'functional_status': 'passed' if unchanged and successful(FUNCTIONAL) else 'failed_or_incomplete',
            'full_development_acceptance': 'passed' if unchanged and successful(FUNCTIONAL + MANUAL) else 'failed_or_incomplete',
            'source_unchanged': unchanged,
            'not_executed': [name for name, stage in stages.items() if stage['status'] == 'not_executed'],
            'failed_or_uncertain': [name for name, stage in stages.items() if stage['status'] in ('failed', 'uncertain')],
            'inconclusive': [name for name, stage in stages.items() if stage['status'] == 'inconclusive'],
            'external_acceptance': 'not_executed: real payment/coupon durability, kernel stop confirmation and production are separate'}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output-dir', type=Path, required=True, help='new private directory outside checkout')
    parser.add_argument('--sqlite-only', action='store_true', help='explicit reduced scope; never full MySQL acceptance')
    parser.add_argument('--isolated-mysql', action='store_true', help='attest supplied loopback DSN is a disposable test instance')
    parser.add_argument('--with-frontend', action=argparse.BooleanOptionalAction, default=True)
    parser.add_argument('--with-sdk', action=argparse.BooleanOptionalAction, default=True)
    parser.add_argument('--skip-project-install', action='store_true', help='mark lockfile install not_executed; do not claim complete checks')
    parser.add_argument('--execute-p7', action='store_true')
    parser.add_argument('--mysql-container', help='existing isolated container; P7 verifies the loopback mapping')
    parser.add_argument('--execute-capacity', action='store_true')
    parser.add_argument('--execute-worker-comparison', action='store_true')
    parser.add_argument('--worker-budget-profile', choices=('original-1.5cpu', 'autoscale-4cpu'), default='original-1.5cpu')
    parser.add_argument('--worker-benefit-metric', choices=('core_p95_ms', 'drain_seconds', 'received_to_settlement_p95_ms'))
    parser.add_argument('--execute-autoscale', action='store_true')
    parser.add_argument('--execute-principal-statistics', action='store_true', help='explicit source-bound P4 growth/retention/replay manual stage')
    parser.add_argument('--principal-mysql-data-root', type=Path, help='existing isolated MySQL actual data filesystem; child retains its 512MiB minimum gate')
    parser.add_argument('--confirmed-benefit-receipt', type=Path)
    parser.add_argument('--quiet-window', action='store_true', help='explicit quiet window for manual P7/load/worker stages')
    args = parser.parse_args(argv)
    os.umask(0o077)
    root = Path(__file__).resolve().parents[1]
    out = args.output_dir.resolve()
    if out == root or root in out.parents:
        parser.error('output directory must be outside checkout, preserving source binding')
    out.mkdir(parents=True, exist_ok=False)
    env = os.environ.copy()
    dsn = env.pop('ZBOARD_TEST_MYSQL_DSN', '')
    principal_dsn = env.pop('ZBOARD_ACCEPTANCE_MYSQL_DSN', '')
    env.update(GOTOOLCHAIN='local', GOMAXPROCS='2', PYTHONDONTWRITEBYTECODE='1', COREPACK_ENABLE_NETWORK='0')
    private_secrets = sorted(set(secrets_for(dsn) + secrets_for(principal_dsn)), key=len, reverse=True)
    stages = {name: {'status': 'not_executed', 'reason': 'not reached'} for name in FUNCTIONAL + MANUAL}
    receipt = {'started_utc': datetime.datetime.now(datetime.timezone.utc).isoformat(), 'stages': stages,
               'scope': 'sqlite_only' if args.sqlite_only else 'functional_with_actual_mysql', 'versions': {}}
    before = source_binding(root)
    save(out / 'source-before.json', before)

    def flush():
        save(out / 'summary.json', receipt)

    def skip(name, reason):
        stages[name] = {'status': 'not_executed', 'reason': reason}
        flush()
        return False

    def run(name, command, cwd=root, overrides=None, json_output=False):
        suffix = 'jsonl' if json_output else 'stdout.log'
        stdout, stderr = out / (name + '.' + suffix), out / (name + '.stderr.log')
        stage = {'status': 'running', 'command': list(map(str, command)), 'cwd': str(cwd),
                 'stdout': str(stdout), 'stderr': str(stderr), 'started_utc': datetime.datetime.now(datetime.timezone.utc).isoformat()}
        stages[name] = stage
        flush()
        print(f'{name}: started; private evidence {stdout.name}', flush=True)
        started = time.monotonic()
        process = None
        threads = []
        stream_errors = []
        def copy_stream(pipe, stream):
            try:
                active_secrets = private_secrets if ((overrides or {}).get('ZBOARD_TEST_MYSQL_DSN') or (overrides or {}).get('ZBOARD_ACCEPTANCE_MYSQL_DSN')) else []
                copy_redacted(pipe, stream, active_secrets)
            except OSError as exc:
                stream_errors.append(type(exc).__name__)
        try:
            with stdout.open('xb') as output, stderr.open('xb') as errors:
                process = subprocess.Popen(list(map(str, command)), cwd=cwd, env=env | (overrides or {}),
                                           stdout=subprocess.PIPE, stderr=subprocess.PIPE)
                stage['pid'] = process.pid
                for pipe, stream in ((process.stdout, output), (process.stderr, errors)):
                    thread = threading.Thread(target=copy_stream, args=(pipe, stream), daemon=True)
                    thread.start()
                    threads.append(thread)
                code = process.wait()
                for thread in threads:
                    thread.join()
            stage.update(exit_code=code, status='passed' if code == 0 and not stream_errors else 'failed')
            if stream_errors:
                stage['stream_errors'] = stream_errors
            if json_output:
                stage.update(go_summary(stdout))
                if not go_result_complete(stage, name == 'sqlite-purego'):
                    stage.update(status='failed', reason='no actual passing test evidence, test failure, or skipped required SQLite checks')
        except KeyboardInterrupt:
            stage.update(status='uncertain', reason='interrupted; inspect recorded PID/owned resources before another run')
            if process is not None:
                stage['observed_exit_code'] = process.poll()
            raise
        except (OSError, ValueError) as exc:
            stage.update(status='failed', reason=type(exc).__name__)
        finally:
            stage['elapsed_seconds'] = round(time.monotonic() - started, 3)
            flush()
        print(f'{name}: {stage["status"]}', flush=True)
        return stage['status'] == 'passed'

    def ready(tool, command, expected):
        if not shutil.which(tool):
            receipt['versions'][tool] = {'status': 'missing'}
            return False
        name = 'version-' + tool
        if not run(name, command):
            receipt['versions'][tool] = {'status': 'failed'}
            return False
        version = Path(stages[name]['stdout']).read_text().strip()
        valid = bool(re.fullmatch(expected, version))
        receipt['versions'][tool] = {'status': 'matched' if valid else 'mismatch', 'observed': version}
        return valid

    def manual_ready(name, requested):
        if not requested:
            return skip(name, 'manual stage requires its explicit --execute option')
        if not args.quiet_window:
            return skip(name, 'explicit --quiet-window required; original child workload/resource checks remain authoritative')
        if not go_ok:
            return skip(name, 'pinned Go is required; no toolchain is installed automatically')
        if name not in PRINCIPAL_STATISTICS and not shutil.which('docker'):
            return skip(name, 'existing Docker is required for this original child; it is not installed automatically')
        if not mysql_ok and name == 'p7-upgrade':
            return skip(name, 'both-database upgrade requires isolated MySQL 8.4 preflight')
        if before != source_binding(root) or git(root, 'status', '--porcelain').strip():
            return skip(name, 'manual probe requires exact clean committed source; no reset or cleanup is performed')
        return True

    try:
        go_ok = ready('go', ['go', 'version'], r'go version go1\.26\.8 \S+')
        node_ok = ready('node', ['node', '--version'], r'v22\.23\.0') if args.with_frontend else False
        pnpm_ok = ready('pnpm', ['pnpm', '--version'], r'11\.9\.0') if args.with_frontend else False
        mysql_ok = False
        if not args.sqlite_only:
            try:
                host, port = mysql_target(dsn, args.isolated_mysql)
                receipt['versions']['mysql'] = {'status': 'matched', 'version': mysql_version(host, port), 'host': host, 'port': port}
                mysql_ok = True
            except (OSError, ValueError):
                receipt['versions']['mysql'] = {'status': 'unavailable', 'reason': 'isolated loopback DSN/attestation/MySQL 8.4 greeting required'}
        backend = root / 'backend'
        if go_ok:
            modules = {name: hashlib.sha256((backend / name).read_bytes()).hexdigest() for name in ('go.mod', 'go.sum')}
            run('backend-tidy', ['go', 'mod', 'tidy'], backend)
            same = all(hashlib.sha256((backend / name).read_bytes()).hexdigest() == digest for name, digest in modules.items())
            stages['backend-module-consistency'] = {'status': 'passed' if same else 'failed', 'reason': 'go.mod/go.sum before/after equality; changes preserved, never reset'}
            flush()
        else:
            skip('backend-tidy', 'Go 1.26.8 unavailable; no toolchain installation')
            skip('backend-module-consistency', 'pinned Go unavailable')
        commands = [('backend-test', ['go', 'test', '-json', '-p', '2', './...', '-count=1'], {}, True),
                    ('backend-vet', ['go', 'vet', '-p', '2', './...'], {}, False),
                    ('backend-build', ['go', 'build', '-p', '2', '-o', out / 'zboard', './cmd/zboard'], {}, False),
                    ('sqlite-purego', ['go', 'test', '-json', '-p', '2', './internal/datastore', '-run', 'SQLite', '-count=1'], {'CGO_ENABLED': '0'}, True)]
        for name, command, overrides, json_output in commands:
            run(name, command, backend, overrides, json_output) if go_ok else skip(name, 'Go 1.26.8 unavailable; no toolchain installation')
        sdk = backend / 'pkg/pluginapi'
        if go_ok and args.with_sdk:
            modules = {name: hashlib.sha256((sdk / name).read_bytes()).hexdigest() for name in ('go.mod', 'go.sum')}
            run('sdk-tidy', ['go', 'mod', 'tidy'], sdk)
            same = all(hashlib.sha256((sdk / name).read_bytes()).hexdigest() == digest for name, digest in modules.items())
            stages['sdk-module-consistency'] = {'status': 'passed' if same else 'failed', 'reason': 'go.mod/go.sum before/after equality; changes preserved, never reset'}
            flush()
            run('sdk-test', ['go', 'test', '-json', '-p', '2', './...', '-count=1'], sdk, json_output=True)
            run('sdk-vet', ['go', 'vet', '-p', '2', './...'], sdk)
        else:
            for name in FUNCTIONAL:
                if name.startswith('sdk-'):
                    skip(name, 'SDK explicitly deselected or pinned Go unavailable')
        frontend = root / 'frontend'
        if args.with_frontend and node_ok and pnpm_ok:
            installed = True
            if args.skip_project_install:
                skip('frontend-install', 'explicit --skip-project-install; no complete functional pass')
            else:
                installed = run('frontend-install', ['pnpm', 'install', '--frozen-lockfile', '--package-import-method=copy'], frontend)
            if installed:
                run('frontend-test', ['pnpm', 'test', '--no-file-parallelism', *[part for p in EXCLUDES for part in ('--exclude', p)]], frontend,
                    {'NODE_OPTIONS': '--max-old-space-size=4096'})
                run('frontend-build', ['pnpm', 'build'], frontend)
            else:
                skip('frontend-test', 'project lockfile dependency preparation failed')
                skip('frontend-build', 'project lockfile dependency preparation failed')
        else:
            for name in ('frontend-install', 'frontend-test', 'frontend-build'):
                skip(name, 'frontend explicitly deselected or pinned Node/pnpm unavailable')
        if go_ok and mysql_ok:
            if run('mysql-full', [sys.executable, root / 'scripts/mysql_profiles.py', '--profile', 'full', '--output-dir', out / 'mysql-full'],
                   overrides={'ZBOARD_TEST_MYSQL_DSN': dsn}):
                nested = json.loads((out / 'mysql-full/receipt.json').read_text())
                stages['mysql-full']['mysql_receipt'] = str(out / 'mysql-full/receipt.json')
                if nested.get('status') != 'passed' or nested.get('profile') != 'full' or nested.get('plan_only'):
                    stages['mysql-full'].update(status='failed', reason='nested full matrix receipt not executed/passed')
        else:
            skip('mysql-full', 'explicit SQLite-only scope or isolated MySQL 8.4/pinned Go unavailable; no SQLite substitute')
        if manual_ready('principal-statistics-build', args.execute_principal_statistics):
            helper_source = backend / 'tools/acceptance-principal-statistics/main.go'
            helper = out / 'principal-statistics-helper'
            artifact_parent = out / 'principal-statistics-artifacts'
            if not helper_source.is_file():
                skip('principal-statistics-build', 'P4 principal statistics execution CLI is not integrated')
            elif run('principal-statistics-build', ['go', 'build', '-p', '2', '-o', helper, './tools/acceptance-principal-statistics'], backend):
                if not helper.is_file() or helper.is_symlink() or before != source_binding(root):
                    stages['principal-statistics-build'].update(status='failed', reason='helper build lacks exact unchanged source/binary evidence')
                else:
                    helper_hash = hashlib.sha256(helper.read_bytes()).hexdigest()
                    stages['principal-statistics-build'].update(helper_path=str(helper), helper_sha256=helper_hash,
                        source_head=before['head'], source_binding_sha256=hashlib.sha256(json.dumps(before, sort_keys=True).encode()).hexdigest())
                    artifact_parent.mkdir(mode=0o700)
                    for driver in ('sqlite', 'mysql'):
                        name = 'principal-statistics-' + driver
                        if driver == 'mysql':
                            if args.sqlite_only:
                                skip(name, 'explicit SQLite-only scope; no MySQL growth/retention/replay acceptance')
                                continue
                            if not args.principal_mysql_data_root or not args.principal_mysql_data_root.is_dir():
                                skip(name, 'existing isolated MySQL actual data filesystem is required')
                                continue
                            try:
                                host, port = mysql_target(principal_dsn, args.isolated_mysql)
                                version = mysql_version(host, port)
                                receipt['versions']['principal_statistics_mysql'] = {'status': 'matched', 'version': version, 'host': host, 'port': port}
                            except (OSError, ValueError):
                                skip(name, 'ZBOARD_ACCEPTANCE_MYSQL_DSN must attest an isolated numeric-loopback MySQL8.4 instance with no database')
                                continue
                        if before != source_binding(root) or hashlib.sha256(helper.read_bytes()).hexdigest() != helper_hash:
                            skip(name, 'source/helper changed; no stale binary execution')
                            continue
                        prior_children = {path.resolve() for path in artifact_parent.iterdir()}
                        command = [helper, '-driver', driver, '-artifact-root', artifact_parent, '-sources', '1024', '-windows', '32', '-timezone', 'Asia/Kathmandu', '-min-free-bytes', str(512 * 1024 * 1024), '-execute']
                        overrides = {}
                        if driver == 'mysql':
                            command += ['-isolated-mysql', '-mysql-data-root', args.principal_mysql_data_root.resolve()]
                            overrides['ZBOARD_ACCEPTANCE_MYSQL_DSN'] = principal_dsn
                        if run(name, command, overrides=overrides):
                            try:
                                evidence = principal_statistics_verdict(last_json(Path(stages[name]['stdout'])), artifact_parent, prior_children, driver, before['head'], helper_hash)
                                if before != source_binding(root) or hashlib.sha256(helper.read_bytes()).hexdigest() != helper_hash:
                                    raise ValueError('source/helper changed during P4 execution')
                                stages[name]['principal_statistics_receipt'] = evidence
                            except (OSError, ValueError):
                                stages[name].update(status='failed', reason='child execution summary is absent, plan-only, stale, incomplete or not source/helper bound')
                        flush()
        for name in PRINCIPAL_STATISTICS[1:]:
            if stages[name]['status'] == 'not_executed' and stages[name]['reason'] == 'not reached':
                skip(name, 'principal statistics helper was not requested, ready or successfully built; no automatic retry')
        if manual_ready('p7-upgrade', args.execute_p7):
            if not args.mysql_container:
                skip('p7-upgrade', 'existing isolated MySQL container name required for mapping and backup verification')
            elif not (root / 'scripts/acceptance-upgrade/current_authority_probe.go.txt').is_file():
                skip('p7-upgrade', 'current authority/migration probe not integrated; old narrow probe cannot replace full P7')
            else:
                script = root / 'scripts/acceptance-upgrade/run.py'
                command = [sys.executable, script, '--checkout', root, '--revision', before['head'], '--driver', 'both', '--go-bin', shutil.which('go'), '--mysql-container', args.mysql_container, '--execute']
                if '--artifact-root' in script.read_text():
                    command += ['--artifact-root', out / 'p7-artifacts']
                if run('p7-upgrade', command, overrides={'ZBOARD_TEST_MYSQL_DSN': dsn}):
                    if last_json(Path(stages['p7-upgrade']['stdout'])).get('status') != 'passed':
                        stages['p7-upgrade'].update(status='failed', reason='P7 child has no passing execution receipt')
        if manual_ready('capacity-original', args.execute_capacity):
            script = root / 'scripts/acceptance-capacity-baseline.py'
            reviewed_pins = literal_revision_pins(script.read_text())
            # REVISIONS is an all-literal tuple in the existing controller. Never
            # add a pin, substitute old evidence or relax its original gate here.
            if before['head'] in reviewed_pins:
                if run('capacity-original', [sys.executable, script, '--checkout', root, '--revision', before['head'], '--go-bin', shutil.which('go'), '--execute']):
                    if last_json(Path(stages['capacity-original']['stdout'])).get('passed') is not True:
                        stages['capacity-original'].update(status='failed', reason='original gates not observed passing')
            else:
                # The new manual harness exposes the same original-budget
                # baseline at exact current HEAD; it does not edit old pins.
                script = root / 'scripts/acceptance-autoscale-manual.py'
                if not script.is_file() or '--execute-baseline' not in script.read_text():
                    skip('capacity-original', 'no reviewed old pin or integrated exact-head original-budget baseline mode')
                else:
                    common = [sys.executable, script, '--checkout', root, '--revision', before['head']]
                    if run('capacity-prepare', common + ['--prepare']):
                        built = last_json(Path(stages['capacity-prepare']['stdout'])).get('build_receipt')
                        if not built:
                            skip('capacity-original', 'prepare returned no source-bound build receipt')
                        elif run('capacity-original', common + ['--execute-baseline', built]):
                            measured = last_json(Path(stages['capacity-original']['stdout']))
                            if measured.get('status') != 'baseline_passed':
                                stages['capacity-original'].update(status='failed', reason='new exact-head original-budget baseline gates not observed passing')
        generated_benefit_receipt = None
        if manual_ready('worker-comparison', args.execute_worker_comparison):
            script = root / 'scripts/acceptance-worker-comparison.py'
            common = [sys.executable, script, '--checkout', root, '--revision', before['head'], '--go-bin', shutil.which('go'), '--budget-profile', args.worker_budget_profile]
            if args.worker_benefit_metric:
                common += ['--confirm-benefit-metric', args.worker_benefit_metric]
            if run('worker-prepare', common + ['--prepare']):
                built = last_json(Path(stages['worker-prepare']['stdout'])).get('build_receipt')
                if not built:
                    skip('worker-comparison', 'prepare returned no build receipt')
                elif run('worker-comparison', common + ['--execute', built]):
                    measured = last_json(Path(stages['worker-comparison']['stdout']))
                    stages['worker-comparison']['benefit_verified'] = measured.get('benefit_verified', False)
                    generated_benefit_receipt = measured.get('benefit_receipt')
                    stages['worker-comparison']['benefit_verdict'] = measured.get('benefit_verdict')
                    if not measured.get('complete') or not measured.get('source_unchanged'):
                        stages['worker-comparison'].update(status='failed', reason='three-pair measurement not complete/source bound')
        if manual_ready('autoscale', args.execute_autoscale):
            script = root / 'scripts/acceptance-autoscale-manual.py'
            selected_benefit_receipt = args.confirmed_benefit_receipt or generated_benefit_receipt
            if not script.is_file() or not selected_benefit_receipt:
                skip('autoscale', 'integrated harness and same-revision confirmed measurement receipt are required')
            else:
                common = [sys.executable, script, '--checkout', root, '--revision', before['head']]
                if run('autoscale-prepare', common + ['--prepare']):
                    built = last_json(Path(stages['autoscale-prepare']['stdout'])).get('build_receipt')
                    if not built:
                        skip('autoscale', 'prepare returned no build receipt; inspect authoritative harness output')
                    else:
                        if run('autoscale', common + ['--execute', built, '--confirmed-benefit-receipt', Path(selected_benefit_receipt).resolve(), '--pairs', '3']):
                            measured = last_json(Path(stages['autoscale']['stdout']))
                            stages['autoscale']['child_summary'] = measured
                            verdict = autoscale_internal_result(measured, before['head'])
                            stages['autoscale'].update(status=verdict, reason='strict source-bound internal P6 verdict; external kernel/payment/production remain unverified')
    except KeyboardInterrupt:
        receipt['interrupted'] = True
    except (OSError, ValueError, subprocess.CalledProcessError) as exc:
        receipt['controller_error'] = type(exc).__name__
    finally:
        after = source_binding(root)
        save(out / 'source-after.json', after)
        receipt.update(aggregate(stages, before == after))
        if receipt.get('controller_error') or receipt.get('interrupted'):
            receipt['full_development_acceptance'] = 'failed_or_incomplete'
        selected = requested_stages(args)
        receipt['requested_stages'] = sorted(selected)
        receipt['requested_scope_status'] = 'passed' if before == after and not receipt.get('controller_error') and not receipt.get('interrupted') and all(stages[name]['status'] == 'passed' for name in selected) else 'failed_or_incomplete'
        receipt['finished_utc'] = datetime.datetime.now(datetime.timezone.utc).isoformat()
        flush()
        print(json.dumps({key: receipt[key] for key in ('requested_scope_status', 'functional_status', 'full_development_acceptance', 'source_unchanged', 'not_executed', 'failed_or_uncertain', 'inconclusive')}))
        print('Private full receipt: ' + str(out / 'summary.json'))
    return 0 if receipt['requested_scope_status'] == 'passed' else 1


if __name__ == '__main__':
    sys.exit(main())
