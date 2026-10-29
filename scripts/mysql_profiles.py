#!/usr/bin/env python3
"""Fail-closed MySQL test partition; no test is omitted by default."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import time

JOBSTORE = './internal/adapters/persistence/jobstore'
COPYSTORE = './internal/adapters/persistence/platformstore'
TEST_NAME = re.compile(r'Test\w+\Z')


def read_events(path):
    events = []
    for number, line in enumerate(Path(path).read_text().splitlines(), 1):
        if not line.strip():
            continue
        try:
            event = json.loads(line)
        except ValueError as exc:
            raise ValueError(f'{path}:{number}: non-JSON go output') from exc
        if not isinstance(event, dict):
            raise ValueError(f'{path}:{number}: invalid go event')
        events.append(event)
    return events


def canonical_package(package, module):
    if not package.startswith(module + '/'):
        raise ValueError(f'package outside backend module: {package}')
    return './' + package[len(module) + 1:]


def listed_tests(events, module):
    found = set()
    for event in events:
        # go test -json -list emits one output event per listed top-level test.
        line = event.get('Output', '').strip()
        if event.get('Action') == 'output' and TEST_NAME.fullmatch(line):
            found.add((canonical_package(event['Package'], module), line))
    return found


def inventory_compiler_diagnostics(events):
    # Compilation happens before test execution. Expose its bounded structured
    # errors in the job log while retaining the complete original artifact.
    lines = []
    for event in events:
        if event.get('Action') != 'build-output':
            continue
        for line in event.get('Output', '').splitlines():
            if line.strip():
                lines.append(line[:400])
                if len(lines) == 20:
                    return lines
    return lines


def partition(inventory, profile):
    if profile.get('version') != 1 or set(profile) != {'version', 'extended'}:
        raise ValueError('unsupported profile format')
    if not inventory:
        raise ValueError('empty MySQL inventory')
    extended = set()
    for entry in profile['extended']:
        if set(entry) != {'package', 'test', 'reason', 'scale'}:
            raise ValueError('extended entry must identify package/test/reason/scale')
        key = entry['package'], entry['test']
        if not key[0].startswith('./') or not TEST_NAME.fullmatch(key[1]):
            raise ValueError(f'invalid exact test identity: {key}')
        if not entry['reason'].strip() or not entry['scale'].strip():
            raise ValueError(f'missing reviewable classification reason: {key}')
        if key in extended:
            raise ValueError(f'duplicate extended entry: {key}')
        if key not in inventory:
            raise ValueError(f'stale or unknown extended entry: {key}')
        extended.add(key)
    core = inventory - extended
    if not core or not extended or core & extended or core | extended != inventory:
        raise ValueError('partition is empty, overlapping or incomplete')
    return {'core': core, 'extended': extended}


def positive_regex(tests):
    if not tests or any(not TEST_NAME.fullmatch(test) for test in tests):
        raise ValueError('cannot run empty or non-exact test selection')
    return '^(' + '|'.join(sorted(tests)) + ')$'


def groups(selection, inventory):
    # Copy's server-wide source lock must never overlap financial race tests.
    business = {package for package, _ in selection} - {JOBSTORE, COPYSTORE}
    result = []
    for label, packages in [('business', sorted(business)), ('jobs', [JOBSTORE]), ('copy', [COPYSTORE])]:
        packages = [p for p in packages if any(item[0] == p for item in selection)]
        if not packages:
            continue
        names = {name for package, name in selection if package in packages}
        actual = {(package, name) for package, name in inventory if package in packages and name in names}
        wanted = {(package, name) for package, name in selection if package in packages}
        if actual != wanted:
            # A shared test name must not silently re-add an extended test in
            # another package when a group uses one positive -run expression.
            raise ValueError(f'cross-package selection collision in {label}: {sorted(actual ^ wanted)}')
        result.append((label, packages, positive_regex(names), wanted))
    covered = set().union(*(item[3] for item in result))
    if covered != selection:
        raise ValueError('execution groups do not cover selected profile')
    return result


def result_summary(events, expected, module):
    terminal = {}
    skipped = []
    failed = []
    for event in events:
        action, name = event.get('Action'), event.get('Test', '')
        if action in ('skip', 'fail'):
            detail = {'package': event.get('Package'), 'test': name}
            (skipped if action == 'skip' else failed).append(detail)
        if action not in ('pass', 'skip', 'fail') or not name or '/' in name:
            continue
        key = canonical_package(event['Package'], module), name
        if key in terminal:
            raise ValueError(f'duplicate terminal result: {key}')
        terminal[key] = action
    missing, extra = expected - terminal.keys(), terminal.keys() - expected
    passed = sum(action == 'pass' for action in terminal.values())
    return {'passed': passed, 'skipped': skipped, 'failed': failed,
            'missing': sorted(missing), 'unexpected': sorted(extra),
            'complete': not (skipped or failed or missing or extra) and passed == len(expected)}


def execution_failure_diagnostics(events, summary):
    """Bounded source identities only; raw SQL, assertions and payloads stay in artifacts."""
    lines = [f"Incomplete MySQL group: passed={summary.get('passed', 0)} "
             f"failed={len(summary.get('failed', []))} missing={len(summary.get('missing', []))} "
             f"skipped={len(summary.get('skipped', []))}"]
    for failed in summary.get('failed', [])[:12]:
        lines.append('Failed test: ' + json.dumps(
            [failed.get('package'), failed.get('test') or '<package>'], ensure_ascii=True)[:400])
    failed_tests = {(item.get('package'), item.get('test'))
                    for item in summary.get('failed', []) if item.get('test')}
    locations, active, hints = set(), set(), set()
    known_errors = {
        'subscription value requires purchase/payment reconciliation': 'purchase_payment_reconciliation',
        'subscription value evidence is incomplete or inconsistent': 'value_evidence_inconsistent',
        'subscription quota cycle reset is pending': 'quota_reset_pending',
        'subscription value quote changed': 'value_quote_changed',
        'Error 1406': 'storage_value_too_long',
        'Error 1452': 'database_foreign_key_reference',
        'Error 3140': 'database_invalid_json',
        'Error 1213': 'database_deadlock',
        'Error 1205': 'database_lock_wait_timeout',
        'Error 3572': 'database_nowait_lock',
    }
    for event in events:
        package, test = event.get('Package', ''), event.get('Test', '')
        if test and event.get('Action') == 'run':
            active.add((package, test))
        elif test and event.get('Action') in ('pass', 'fail', 'skip'):
            active.discard((package, test))
        if event.get('Action') == 'output' and (package, test) in failed_tests:
            # Go's assertion position is useful without echoing a struct,
            # credentials, SQL arguments or arbitrary test output into logs.
            for match in re.finditer(r'(?m)^\s+([A-Za-z0-9_]+_test\.go):(\d+):', event.get('Output', '')):
                locations.add(f'{match.group(1)}:{match.group(2)}')
            for fragment, label in known_errors.items():
                if fragment in event.get('Output', ''):
                    hints.add((test, label))
    lines.extend('Assertion source: ' + location for location in sorted(locations)[:12])
    # Fixed vocabulary only. These are observations within a failed test, not
    # a root-cause assertion or permission to expose its arbitrary error values.
    lines.extend('Observed error class: ' + json.dumps(item, ensure_ascii=True)[:400]
                 for item in sorted(hints)[:12])
    if active:
        lines.append('Active at incomplete execution (not a cause attribution):')
        lines.extend(json.dumps(item, ensure_ascii=True)[:400] for item in sorted(active)[:12])
    if summary.get('missing'):
        lines.append('Missing terminal tests: ' + json.dumps(summary['missing'][:8], ensure_ascii=True)[:800])
    # Completed durations distinguish cumulative preparation/test cost from
    # the goroutine active at timeout, without publishing SQL or raw payloads.
    timings = []
    for event in events:
        elapsed = event.get('Elapsed')
        if (event.get('Action') == 'pass' and event.get('Test') and
                '/' not in event['Test'] and isinstance(elapsed, (int, float)) and
                not isinstance(elapsed, bool) and 2 <= elapsed < 100000):
            timings.append((elapsed, event.get('Package', ''), event['Test']))
    for elapsed, package, test in sorted(timings, reverse=True)[:10]:
        lines.append('Completed test duration: ' + json.dumps(
            [package, test, elapsed], ensure_ascii=True)[:500])
    return lines


def write_json(path, value):
    Path(path).write_text(json.dumps(value, indent=2, sort_keys=True) + '\n')


def run_command(command, cwd, log, commands):
    started = time.time()
    print(f'Starting {log.name}: {len(command)} arguments; output {log}', flush=True)
    stderr = log.with_suffix(log.suffix + '.stderr')
    with log.open('xb') as output, stderr.open('xb') as errors:
        code = subprocess.run(command, cwd=cwd, stdout=output, stderr=errors, check=False).returncode
    record = {'command': command, 'log': str(log), 'stderr': str(stderr), 'exit_code': code,
              'stdout_sha256': hashlib.sha256(log.read_bytes()).hexdigest(),
              'stderr_sha256': hashlib.sha256(stderr.read_bytes()).hexdigest(),
              'elapsed_seconds': round(time.time() - started, 3)}
    commands.append(record)
    print(f'Finished {log.name}: exit={code}, elapsed={record["elapsed_seconds"]}s', flush=True)
    return code


def source_fingerprints(root):
    selected = {'scripts/mysql_profiles.py', 'scripts/mysql_extended_tests.json',
                'scripts/test_mysql_profiles.py', '.github/workflows/ci.yml',
                '.github/workflows/mysql-extended.yml'}
    result = subprocess.run(['git', 'ls-files', '--cached', '--others', '--exclude-standard',
                             'backend', *sorted(selected)], cwd=root, capture_output=True, text=True, check=True)
    found = {}
    for name in sorted(set(result.stdout.splitlines())):
        path = root / name
        relevant = name in selected or (name.startswith('backend/') and
                    (path.suffix in ('.go', '.sql') or path.name in ('go.mod', 'go.sum') or
                     name in ('backend/api/openapi.yaml', 'backend/architecture/capability-ownership.json')))
        if not relevant:
            continue
        if path.is_symlink() or not path.is_file():
            raise ValueError(f'ordinary source file required: {name}')
        found[name] = hashlib.sha256(path.read_bytes()).hexdigest()
    return found


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--profile', required=True, choices=('core', 'extended', 'full'))
    parser.add_argument('--output-dir', required=True, type=Path, help='new directory preserving all JSON and receipts')
    parser.add_argument('--plan-only', action='store_true', help='compile/list actual tests; do not run test bodies')
    args = parser.parse_args(argv)
    root = Path(__file__).resolve().parents[1]
    backend = root / 'backend'
    output = args.output_dir.resolve()
    # Never overwrite evidence from a previous invocation.
    output.mkdir(parents=True, exist_ok=False)
    receipt = {'profile': args.profile, 'plan_only': args.plan_only, 'status': 'failed', 'commands': [], 'results': []}
    fingerprints = None
    try:
        if not args.plan_only and not os.environ.get('ZBOARD_TEST_MYSQL_DSN', '').strip():
            raise ValueError('ZBOARD_TEST_MYSQL_DSN is required; skipped SQLite fallback is not MySQL acceptance')
        module = re.search(r'^module\s+(\S+)', (backend / 'go.mod').read_text(), re.M).group(1)
        profile_path = root / 'scripts' / 'mysql_extended_tests.json'
        profile = json.loads(profile_path.read_text())
        # Anchor exact test sources as well as HEAD: new uncommitted tests must
        # also be included in local development evidence.
        fingerprints = source_fingerprints(root)
        write_json(output / 'source-sha256.json', fingerprints)
        head = subprocess.run(['git', 'rev-parse', 'HEAD'], cwd=root, capture_output=True, text=True, check=True).stdout.strip()
        receipt['head'] = head
        inventory = set()
        for label, expression, packages in [('mysql', 'MySQL', ['./...']), ('jobs', '^Test', [JOBSTORE])]:
            log = output / f'inventory-{label}.jsonl'
            command = ['go', 'test', '-json', '-p', '2', '-count=1', '-timeout', '15m', '-list', expression, *packages]
            if run_command(command, backend, log, receipt['commands']):
                try:
                    for line in inventory_compiler_diagnostics(read_events(log)):
                        print(f'Inventory compiler: {line}', flush=True)
                except ValueError:
                    print('Inventory stdout is not valid Go JSON; original output is preserved.', flush=True)
                raise ValueError(f'{label} inventory failed; see preserved JSONL')
            inventory |= listed_tests(read_events(log), module)
        required = {'./internal/datastore', './internal/handler', './internal/application',
                    './internal/adapters/persistence/networkstore', JOBSTORE, COPYSTORE}
        if not required <= {p for p, _ in inventory}:
            raise ValueError('inventory lost an existing mandatory MySQL package')
        # Every name with MySQL anywhere is included across ./..., plus the
        # complete generic jobstore suite whose fixtures select DB by DSN.
        partitions = partition(inventory, profile)
        write_json(output / 'inventory.json', sorted(inventory))
        write_json(output / 'partition.json', {k: sorted(v) for k, v in partitions.items()})
        plan = []
        selected_profiles = ['core', 'extended'] if args.profile == 'full' else [args.profile]
        for chosen in selected_profiles:
            for label, packages, expression, expected in groups(partitions[chosen], inventory):
                command = ['go', 'test', '-json', '-p', '2', *packages, '-run', expression, '-count=1', '-timeout', '15m']
                plan.append({'profile': chosen, 'group': label, 'command': command, 'expected': sorted(expected)})
        write_json(output / 'plan.json', plan)
        receipt['counts'] = {k: len(v) for k, v in partitions.items()}
        if args.plan_only:
            receipt['status'] = 'planned_not_executed'
            return 0
        complete = True
        # Keep running independent groups/profiles after a failed package, and
        # preserve failure. No retries, skips or timeout changes manufacture green.
        for item in plan:
            log = output / f'{item["profile"]}-{item["group"]}.jsonl'
            code = run_command(item['command'], backend, log, receipt['commands'])
            try:
                events = read_events(log)
                summary = result_summary(events, {tuple(v) for v in item['expected']}, module)
            except ValueError as exc:
                summary = {'complete': False, 'error': str(exc)}
                events = []
            if code != 0 or not summary['complete']:
                for diagnostic in execution_failure_diagnostics(events, summary):
                    print(f'{item["profile"]}-{item["group"]}: {diagnostic}', flush=True)
            receipt['results'].append({'profile': item['profile'], 'group': item['group'], 'exit_code': code, **summary})
            complete = complete and code == 0 and summary['complete']
            write_json(output / 'receipt.json', receipt)
        receipt['status'] = 'passed' if complete else 'failed_or_incomplete'
        return 0 if complete else 1
    except (OSError, ValueError, subprocess.CalledProcessError) as exc:
        receipt['error'] = str(exc)
        print(str(exc), file=sys.stderr)
        return 1
    finally:
        changed = False
        if fingerprints is not None:
            try:
                after = source_fingerprints(root)
                write_json(output / 'source-sha256-after.json', after)
                head_after = subprocess.run(['git', 'rev-parse', 'HEAD'], cwd=root, capture_output=True, text=True, check=True).stdout.strip()
                receipt['head_after'] = head_after
                receipt['source_unchanged'] = after == fingerprints and head_after == receipt.get('head')
                changed = not receipt['source_unchanged']
                if changed:
                    receipt['changed_sources'] = sorted(n for n in set(after) | set(fingerprints) if after.get(n) != fingerprints.get(n))
            except (OSError, ValueError, subprocess.CalledProcessError) as exc:
                receipt['source_after_error'] = str(exc)
                changed = True
            if changed:
                receipt['status'] = 'failed_source_changed_or_unverifiable'
        write_json(output / 'receipt.json', receipt)
        if changed:
            return 1


if __name__ == '__main__':
    sys.exit(main())
