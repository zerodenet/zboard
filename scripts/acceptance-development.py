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
MANUAL = ('p7-upgrade', 'capacity-original', 'worker-prepare', 'worker-comparison', 'autoscale-prepare', 'autoscale')


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
    env.update(GOTOOLCHAIN='local', GOMAXPROCS='2', PYTHONDONTWRITEBYTECODE='1', COREPACK_ENABLE_NETWORK='0')
    private_secrets = secrets_for(dsn)
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
                active_secrets = private_secrets if (overrides or {}).get('ZBOARD_TEST_MYSQL_DSN') else []
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
        if not go_ok or not shutil.which('docker'):
            return skip(name, 'pinned Go and existing Docker are required; neither is installed automatically')
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
                        if run('autoscale', common + ['--execute', built, '--confirmed-benefit-receipt', Path(selected_benefit_receipt).resolve()]):
                            measured = last_json(Path(stages['autoscale']['stdout']))
                            stages['autoscale']['child_summary'] = measured
                            if measured.get('status') == 'measurements_preserved' and measured.get('benefit_verified') is False:
                                stages['autoscale'].update(status='inconclusive', reason='measurements preserved; scaling benefit is not a passing verdict')
                            else:
                                stages['autoscale'].update(status='failed', reason='unrecognized autoscale verdict; no successful P6 verdict inferred')
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
