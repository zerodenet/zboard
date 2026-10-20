#!/usr/bin/env python3
"""Bind one unmodified mixed-load run to a fixed checkout and optional read-only observations.

Default: inspect only. --execute performs exactly one run; it never retries a
load, changes gates, removes evidence, or manages workers. The original runner
retains ownership of its isolated containers, volume and normal cleanup.
"""
import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import signal
import sqlite3
import subprocess
import tempfile
import threading
import time

PIN = '7ca9f63d0cf12a03713f3a242c508dcbf82e4856'
REVISIONS = (PIN, '53a056d32633dcbda9b243553d85a88fe9fc2090', '4dfb6642c87d1900882afdc90d4467369e98fce1', '308689e38cff794865b633cde7193656ca2ecd4c', 'e5accd905897f9ea9eaa7ceb3dc69ede95762094')
RUNNERS = ['scripts/acceptance-mixed.sh', 'scripts/acceptance-load.py', 'scripts/acceptance-report.py']


def command(arguments, cwd=None):
    return subprocess.check_output(arguments, cwd=cwd, text=True, stderr=subprocess.PIPE).strip()


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def source_manifest(checkout):
    files = subprocess.check_output(['git', 'ls-files', '-z', '--cached', '--others', '--exclude-standard', 'backend', *RUNNERS], cwd=checkout).decode().split('\0')
    return ''.join(f'{sha(checkout / name)}  {name}\n' for name in files if name and (checkout / name).is_file())


def write_json(path, value):
    path.write_text(json.dumps(value, indent=2) + '\n')
    path.chmod(0o600)


def now():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


def sqlite_space(path):
    if not path.is_file():
        return {'available': False, 'reason': 'exported database unavailable'}
    try:
        with sqlite3.connect(path.as_uri() + '?mode=ro', uri=True) as db:
            db.execute('PRAGMA query_only=ON')
            page_size = db.execute('PRAGMA page_size').fetchone()[0]
            page_count = db.execute('PRAGMA page_count').fetchone()[0]
            free_pages = db.execute('PRAGMA freelist_count').fetchone()[0]
            rows = db.execute("SELECT coalesce(s.type,'internal'), SUM(d.pgsize), COUNT(*) FROM dbstat d LEFT JOIN sqlite_schema s ON s.name=d.name GROUP BY coalesce(s.type,'internal')").fetchall()
            return {'available': True, 'scope': 'stopped exported SQLite only; not a live SQL latency measurement', 'page_size': page_size, 'page_count': page_count, 'freelist_pages': free_pages, 'allocated_bytes': page_size * page_count, 'objects': {kind: {'bytes': size, 'pages': pages} for kind, size, pages in rows}}
    except sqlite3.Error as error:
        return {'available': False, 'reason': type(error).__name__, 'scope': 'dbstat unavailable; no size or index coverage inferred'}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--checkout', type=Path, required=True)
    parser.add_argument('--revision', choices=REVISIONS, default=PIN, help='explicit fixed comparison source; default is the verified 0053 baseline')
    parser.add_argument('--go-bin', default='go')
    parser.add_argument('--execute', action='store_true', help='explicitly run one 60-second load after the quiet window is approved')
    args = parser.parse_args()
    os.umask(0o077)
    checkout = args.checkout.resolve()
    commit = command(['git', 'rev-parse', 'HEAD'], checkout)
    if commit != args.revision or command(['git', 'status', '--porcelain'], checkout):
        parser.error('requires the exact clean pinned checkout; existing work is never reset or replaced')
    sources = source_manifest(checkout)
    preflight = {'commit': commit, 'expected_revision': args.revision, 'source_manifest_sha256': hashlib.sha256(sources.encode()).hexdigest(), 'runner_sha256': {name: sha(checkout / name) for name in RUNNERS}, 'observer_sha256': sha(Path(__file__).resolve()), 'go_version': command([args.go_bin, 'version']), 'execute': args.execute}
    if not args.execute:
        print(json.dumps(preflight))
        return 0

    evidence_root = checkout / '.codex-local-artifacts' / 'acceptance'
    evidence_root.mkdir(parents=True, mode=0o700, exist_ok=True)
    previous_outputs = set(evidence_root.glob('mixed-*'))
    controller = Path(tempfile.mkdtemp(prefix='baseline-controller-', dir=evidence_root))
    (controller / 'source-before.sha256').write_text(sources)
    write_json(controller / 'preflight.json', preflight)
    original_containers = set(command(['docker', 'ps', '-aq', '--no-trunc']).splitlines())
    parameters = {'NODES': '2', 'SUBSCRIPTIONS': '20', 'HISTORY_RECORDS': '2000', 'DURATION_SECONDS': '60', 'EVENT_RATE': '20', 'READERS': '4'}
    env = os.environ.copy()
    env.update(parameters | {'GO_BIN': args.go_bin, 'GOMAXPROCS': '1', 'GOFLAGS': '-p=1'})
    log = (controller / 'runner.log').open('w')
    started = now()
    process = subprocess.Popen(['bash', str(checkout / RUNNERS[0])], cwd=checkout, env=env, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
    stop = threading.Event()
    observations = {'container_id': None, 'samples': 0, 'errors': []}
    output = [None]

    def observe():
        while not stop.is_set():
            try:
                if output[0] is None:
                    created = set(evidence_root.glob('mixed-*')) - previous_outputs
                    if len(created) == 1:
                        output[0] = created.pop()
                    elif len(created) > 1:
                        observations['errors'].append('multiple new outputs; RSS observer refused ambiguous target')
                        return
                image_file = output[0] / 'image.txt' if output[0] else None
                if observations['container_id'] is None and image_file and image_file.is_file():
                    image = image_file.read_text().strip()
                    for container in command(['docker', 'ps', '-q', '--no-trunc', '--filter', f'ancestor={image}']).splitlines():
                        if container in original_containers:
                            continue
                        meta = json.loads(command(['docker', 'inspect', container]))[0]
                        profile = meta['HostConfig']
                        if meta['Image'] == image and meta['Config']['Entrypoint'] == ['/zboard'] and profile['NanoCpus'] == 1000000000 and profile['Memory'] == 1073741824 and profile['MemorySwap'] == 1073741824:
                            observations['container_id'] = container
                            break
                if observations['container_id']:
                    top = command(['docker', 'top', observations['container_id'], '-eo', 'pid,comm,rss'])
                    rows = [line.split() for line in top.splitlines()[1:]]
                    server = [row for row in rows if len(row) == 3 and row[1] == 'zboard' and row[2].isdigit()]
                    if len(server) == 1:
                        with (controller / 'rss.jsonl').open('a') as stream:
                            stream.write(json.dumps({'at': now(), 'rss_kib': int(server[0][2]), 'scope': 'docker top zboard process RSS; sampled, not continuous peak'}) + '\n')
                        observations['samples'] += 1
                    else:
                        observations['errors'].append('zboard RSS row unavailable; no zero inferred')
            except (OSError, subprocess.CalledProcessError, ValueError, KeyError) as error:
                observations['errors'].append(type(error).__name__)
            stop.wait(3)

    sampler = threading.Thread(target=observe, daemon=True)
    sampler.start()
    try:
        exit_code = process.wait()
    except BaseException:
        if process.poll() is None:
            os.killpg(process.pid, signal.SIGTERM)
            try:
                process.wait(timeout=45)
            except subprocess.TimeoutExpired:
                # Do not remove or kill any Docker object independently of the
                # original runner. Preserve the uncertain outcome for review.
                write_json(controller / 'interrupted.json', {'at': now(), 'runner_pid': process.pid, 'outcome': 'runner did not confirm exit after termination; inspect owned evidence/container before retry'})
        raise
    finally:
        stop.set()
        sampler.join(timeout=10)
        log.close()

    after = source_manifest(checkout)
    (controller / 'source-after.sha256').write_text(after)
    binding = {'checkout_commit_unchanged': command(['git', 'rev-parse', 'HEAD'], checkout) == args.revision, 'checkout_still_clean': not command(['git', 'status', '--porcelain'], checkout), 'source_before_after_equal': sources == after}
    original = None
    space = {'available': False, 'reason': 'runner output unavailable'}
    if output[0] is not None:
        out = output[0]
        for name in ('source-sha256.txt', 'source-after-build-sha256.txt'):
            binding[name + '_matches_observer'] = (out / name).is_file() and (out / name).read_text() == sources
        environment = (out / 'environment.txt').read_text() if (out / 'environment.txt').is_file() else ''
        binaries = {}
        for name in ('zboard', 'acceptance'):
            path = out / name
            binaries[name] = sha(path) if path.is_file() else None
            binding[name + '_binary_before_after_equal'] = binaries[name] is not None and f'{binaries[name]}  {path}' in environment
        write_json(controller / 'binary-after.json', binaries)
        for name in ('acceptance-load.py', 'acceptance-report.py'):
            binding[name + '_copy_matches_source'] = (out / name).is_file() and sha(out / name) == preflight['runner_sha256']['scripts/' + name]
        if (out / 'acceptance-result.json').is_file():
            original = json.loads((out / 'acceptance-result.json').read_text())
        space = sqlite_space((out / 'data' / 'zboard.db').resolve())
    rss_path = controller / 'rss.jsonl'
    rss = [json.loads(line) for line in rss_path.read_text().splitlines()] if rss_path.is_file() else []
    observations.update({'available': bool(rss), 'first_rss_kib': rss[0]['rss_kib'] if rss else None, 'last_rss_kib': rss[-1]['rss_kib'] if rss else None, 'sampled_peak_rss_kib': max((row['rss_kib'] for row in rss), default=None)})
    result = {'passed': exit_code == 0 and bool(original and original['passed']) and all(binding.values()), 'expected_revision': args.revision, 'started_at': started, 'finished_at': now(), 'parameters': parameters, 'original_runner_exit': exit_code, 'original_acceptance_passed': original['passed'] if original else None, 'hash_binding': binding, 'rss_observations': observations, 'sqlite_space': space, 'original_evidence_directory': str(output[0]) if output[0] else None, 'note': 'Supplemental RSS/dbstat do not replace or relax any original gate. One short single-process run; no worker scaling or capacity ceiling claim.'}
    write_json(controller / 'baseline-binding.json', result)
    print(json.dumps({'passed': result['passed'], 'original_runner_exit': exit_code, 'all_hash_bindings': all(binding.values()), 'rss_available': bool(rss), 'dbstat_available': space['available'], 'evidence_directory': str(controller), 'original_evidence_directory': result['original_evidence_directory']}))
    return 0 if result['passed'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
