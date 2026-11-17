#!/usr/bin/env python3
"""One-shot selected real concurrency tests on SQLite and isolated MySQL8.4.

Discovers every top-level test in the two actual packages before validating the
exact selection. Each driver/package runs once with -race -count=3 -p1 and the
original15m Go timeout. Every run/terminal must occur exactly three times; any
skip, missing, extra, foreign or failed result fails closed. This is selected
financial/revocation/publication coverage, never all repository race coverage.
Raw SQL, commands and diagnostics remain in a new private outside-checkout dir;
only fixed test IDs, counts, hashes, booleans and error enums enter the summary.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import signal
import socket
import subprocess
import sys

MODULE = 'github.com/zerodenet/zboard/backend'
HANDLER = './internal/handler'
NETWORK = './internal/adapters/persistence/networkstore'
FUNDS = (
 'WalletOnlyPaymentRollsBackAndRacesWithoutDoubleSpend',
 'WalletPaymentCancellationRaceHasOneFinancialOutcome',
 'SubscriptionTerminationRacesVerifiedRenewal',
 'ConsumptionRefundConcurrentReplayAndBudget',
 'ConsumptionRefundRacesUnsubscribeWithoutReturningValueTwice',
 'AutomaticRenewalStableWalletCompetitionAndTopUpRetry',
)
NETWORK_TESTS = (
 'TestConnectionRevocationMySQLAndSQLiteFencesRenewal',
 'TestConnectionRevocationMySQLAndSQLiteFencesGroupRegrant',
 'TestPublicationQueuePreservesNewerGenerationAndLeaseOwner',
 'TestPublicationConnectionPhaseMySQLAndSQLiteSurvivesRetryAndNewGeneration',
 'TestUnknownPublicationSurvivesRestartWithoutDuplicateDeploymentOrReadinessChanges',
)
SELECTION = {
 'sqlite': {HANDLER: tuple('Test'+name for name in FUNDS), NETWORK: NETWORK_TESTS},
 'mysql': {HANDLER: tuple('TestMySQL'+name for name in FUNDS) + (
  'TestMySQLAutomaticRenewalVersusManualVerifiedCash',
  'TestMySQLAutomaticRenewalVersusUnsubscribe',
  'TestMySQLControlledWalletAdjustmentConcurrent',), NETWORK: NETWORK_TESTS},
}
KINDS = ('none','invalid_output','preflight_failed','discovery_failed','selection_failed',
         'test_failed','incomplete_results','command_timeout','source_changed','io_error','internal_error')


class Failure(Exception):
    def __init__(self, kind):
        if kind not in KINDS: raise ValueError('invalid class')
        self.kind = kind


def digest(path):
    h = hashlib.sha256()
    with path.open('rb') as stream:
        for block in iter(lambda: stream.read(65536), b''): h.update(block)
    return h.hexdigest()


def safe_new_output(root, path):
    if not path.is_absolute() or path.exists() or path.is_symlink(): raise Failure('invalid_output')
    if not path.parent.is_dir() or any(p.is_symlink() for p in (path, *path.parents)): raise Failure('invalid_output')
    if path.resolve().is_relative_to(root.resolve()): raise Failure('invalid_output')


def save_new(root, path, value):
    safe_new_output(root, path)
    with os.fdopen(os.open(path, os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW, 0o600), 'w') as stream:
        json.dump(value, stream, sort_keys=True, indent=2); stream.write('\n')


def source_binding(root):
    head = subprocess.check_output(['git','rev-parse','HEAD'], cwd=root, text=True, stderr=subprocess.PIPE).strip()
    names = subprocess.check_output(['git','ls-files','-z'], cwd=root, stderr=subprocess.PIPE).decode().split('\0')
    rows = []
    for name in sorted(filter(None,names)):
        p = root/name
        rows.append((name, 'link', hashlib.sha256(os.readlink(p).encode()).hexdigest()) if p.is_symlink() else
                    (name,'file',digest(p)) if p.is_file() else (name,'absent',''))
    return {'head':head,'sha256':hashlib.sha256(json.dumps([head,rows],separators=(',',':')).encode()).hexdigest()}


def events(path):
    with path.open() as stream:
        for line in stream:
            if line.strip():
                value = json.loads(line)
                if not isinstance(value,dict): raise Failure('incomplete_results')
                yield value


def canonical(package):
    if not isinstance(package,str) or not package.startswith(MODULE+'/'): raise Failure('incomplete_results')
    return './'+package[len(MODULE)+1:]


def discovered(data):
    found, complete = set(), set()
    for event in data:
        # Go1.26 compiler events identify ImportPath rather than Package.
        # The process exit status and complete test terminals still decide
        # acceptance; compiler diagnostics are not extra executed test IDs.
        if event.get('Action') in ('build-output', 'build-fail'):
            if event.get('Action') == 'build-fail': raise Failure('discovery_failed')
            continue
        package = canonical(event.get('Package'))
        if package not in (HANDLER,NETWORK): raise Failure('selection_failed')
        if event.get('Action') in ('skip','fail'): raise Failure('discovery_failed')
        if event.get('Action')=='pass' and not event.get('Test'):
            if package in complete: raise Failure('discovery_failed')
            complete.add(package)
        text = event.get('Output','').strip()
        if event.get('Action')=='output' and re.fullmatch(r'Test\w+',text):
            key = package,text
            if key in found: raise Failure('selection_failed')
            found.add(key)
    if complete != {HANDLER,NETWORK}: raise Failure('discovery_failed')
    return found


def exact_selection(inventory):
    for group in SELECTION.values():
        for package, names in group.items():
            if len(set(names))!=len(names) or any((package,name) not in inventory for name in names): raise Failure('selection_failed')
    return True


def result_series(data, package, names):
    wanted=set(names)
    rows={name:{'test':name,'runs':0,'terminals':[],'passed':False} for name in names}
    incomplete=False
    package_pass=False
    for event in data:
        if event.get('Action') in ('build-output', 'build-fail'):
            if event.get('Action') == 'build-fail': raise Failure('incomplete_results')
            continue
        if canonical(event.get('Package'))!=package: raise Failure('incomplete_results')
        action,name=event.get('Action'),event.get('Test','')
        if not name:
            if action in ('skip','fail'): incomplete=True
            if action=='pass':
                if package_pass: raise Failure('incomplete_results')
                package_pass=True
            continue
        top=name.split('/',1)[0]
        if top not in wanted: raise Failure('incomplete_results')
        if '/' in name:
            if action in ('skip','fail'): incomplete=True
            continue
        row=rows[top]
        if action=='run':
            if row['runs']!=len(row['terminals']) or row['runs']>=3: raise Failure('incomplete_results')
            row['runs']+=1
        if action in ('pass','skip','fail'):
            if row['runs']!=len(row['terminals'])+1: raise Failure('incomplete_results')
            row['terminals'].append(action)
    for row in rows.values(): row['passed']=row['runs']==3 and row['terminals']==['pass']*3
    return {'tests':list(rows.values()),'passed':package_pass and not incomplete and all(r['passed'] for r in rows.values())}


def mysql_preflight(dsn, isolated):
    # Empty DBName plus explicit disposable-instance attestation; never select
    # an existing schema. Fixtures themselves CREATE unique random schemas.
    match=re.fullmatch(r'.+@tcp\(127\.0\.0\.1:([0-9]{1,5})\)/\?[^\r\n]*',dsn or '')
    if not isolated or not match or not 1<=int(match[1])<=65535: raise Failure('preflight_failed')
    with socket.create_connection(('127.0.0.1',int(match[1])),timeout=3) as connection:
        connection.settimeout(3); packet=b''
        while len(packet)<5 or b'\0' not in packet[5:]:
            block=connection.recv(256)
            if not block or len(packet)+len(block)>4096: raise Failure('preflight_failed')
            packet+=block
        version=packet[5:].split(b'\0',1)[0]
        if packet[4]!=10 or re.fullmatch(rb'8\.4\.\d+[-+A-Za-z0-9.]*',version) is None: raise Failure('preflight_failed')


def run(command,root,env,out):
    with out.open('xb') as stdout,out.with_suffix('.stderr').open('xb') as stderr:
        child=subprocess.Popen(command,cwd=root/'backend',env=env,stdout=stdout,stderr=stderr,start_new_session=True)
        try: code=child.wait(timeout=900)
        except subprocess.TimeoutExpired:
            os.killpg(child.pid,signal.SIGTERM)
            try: child.wait(timeout=10)
            except subprocess.TimeoutExpired: os.killpg(child.pid,signal.SIGKILL);child.wait(timeout=5)
            raise Failure('command_timeout')
    return code


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--checkout',type=Path,required=True)
    parser.add_argument('--revision',required=True)
    parser.add_argument('--private-dir',type=Path,required=True)
    parser.add_argument('--summary',type=Path,required=True)
    parser.add_argument('--isolated-mysql',action='store_true',help='attest a disposable independent loopback MySQL8.4 instance')
    args=parser.parse_args();os.umask(0o077)
    root=args.checkout.absolute();private=args.private_dir;public=args.summary
    result={'schema_version':1,'source_head':'','source_sha256':'','source_unchanged':False,'inventory_sha256':'',
            'groups':{d:{p:{'executed':False,'passed':False,'tests':[{'test':n,'runs':None,'terminals':None,'passed':False} for n in names]} for p,names in selected.items()} for d,selected in SELECTION.items()},
            'passed':False,'error_class':'none'}
    before=None;can_write=False;created=False
    try:
        safe_new_output(root,public);can_write=True
        safe_new_output(root,private)
        if not re.fullmatch('[a-f0-9]{40}',args.revision) or not shutil.which('go') or not shutil.which('gcc'): raise Failure('preflight_failed')
        if subprocess.check_output(['git','status','--porcelain'],cwd=root,stderr=subprocess.PIPE): raise Failure('preflight_failed')
        before=source_binding(root)
        if before['head']!=args.revision: raise Failure('source_changed')
        result.update(source_head=before['head'],source_sha256=before['sha256'])
        mysql_dsn=os.environ.get('ZBOARD_TEST_MYSQL_DSN')
        mysql_preflight(mysql_dsn,args.isolated_mysql)
        private.mkdir(mode=0o700);created=True
        save_new(root,private/'source-before.json',before)
        env={k:v for k,v in os.environ.items() if not k.startswith('ZBOARD_')}
        env.update(GOMAXPROCS='2',CGO_ENABLED='1',GOFLAGS='-p=1 -mod=readonly')
        inventory=private/'discovery.jsonl'
        command=['go','test','-json','-list','.', '-count=1','-p','1',HANDLER,NETWORK]
        code=run(command,root,env,inventory)
        if code: raise Failure('discovery_failed')
        available=discovered(events(inventory));exact_selection(available)
        result['inventory_sha256']=digest(inventory)
        save_new(root,private/'selection.json',{driver:{p:list(names) for p,names in selected.items()} for driver,selected in SELECTION.items()})
        failed=False
        for driver,selection in SELECTION.items():
            test_env=env.copy()
            if driver=='mysql': test_env['ZBOARD_TEST_MYSQL_DSN']=mysql_dsn
            for package,names in selection.items():
                log=private/(driver+('-handler' if package==HANDLER else '-network')+'.jsonl')
                regex='^('+'|'.join(re.escape(name) for name in names)+')$'
                # One exact package per command prevents cross-package name
                # collisions from executing undeclared tests.
                command=['go','test','-json','-race','-count=3','-p','1','-timeout','15m','-run',regex,package]
                record=result['groups'][driver][package];record['executed']=True
                code=None;timed_out=False
                try: code=run(command,root,test_env,log)
                except Failure as error:
                    if error.kind!='command_timeout': raise
                    timed_out=True
                # Even a timed-out command retains every complete terminal it
                # emitted. Invalid/truncated JSON never becomes a false zero.
                record.update(stdout_sha256=digest(log),stderr_sha256=digest(log.with_suffix('.stderr')))
                save_new(root,private/(log.stem+'-command.json'),{'command':command,'exit_code':code,'timed_out':timed_out})
                try: verdict=result_series(events(log),package,names)
                except (Failure,ValueError):
                    if timed_out: raise Failure('command_timeout')
                    raise Failure('incomplete_results')
                record.update(verdict)
                if timed_out:
                    record['passed']=False
                    raise Failure('command_timeout')
                if code or not verdict['passed']: record['passed']=False;failed=True
        if failed: raise Failure('test_failed')
    except Failure as error: result['error_class']=error.kind
    except OSError: result['error_class']='io_error'
    except Exception: result['error_class']='internal_error'
    finally:
        if before is not None:
            try:
                after=source_binding(root)
                result['source_unchanged']=before==after and not subprocess.check_output(['git','status','--porcelain'],cwd=root,stderr=subprocess.PIPE)
                if created: save_new(root,private/'source-after.json',after)
                if not result['source_unchanged']: result['error_class']='source_changed'
            except OSError: result['error_class']='io_error'
            except Exception: result['error_class']='source_changed'
        result['passed']=result['error_class']=='none' and result['source_unchanged'] and all(group['executed'] and group['passed'] for groups in result['groups'].values() for group in groups.values())
        if can_write:
            try: save_new(root,public,result)
            except Exception: result['error_class']='io_error';result['passed']=False
    print(json.dumps(result,sort_keys=True))
    return 0 if result['passed'] else 1


if __name__=='__main__': raise SystemExit(main())
