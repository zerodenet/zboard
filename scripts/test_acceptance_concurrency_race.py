"""Offline controller contract tests; no Go, SQL, network or subprocess tests."""
import contextlib
import importlib.util
import io
import json
from pathlib import Path
import re
import tempfile
import unittest
from unittest.mock import patch

SPEC=importlib.util.spec_from_file_location('selected_races',Path(__file__).with_name('acceptance-concurrency-race.py'))
R=importlib.util.module_from_spec(SPEC);SPEC.loader.exec_module(R)


def event(action,test=None,package=R.HANDLER,**extra):
    row={'Action':action,'Package':R.MODULE+'/'+package.removeprefix('./')}
    if test is not None: row['Test']=test
    row.update(extra)
    return row


def sequence(name,terminals=('pass','pass','pass')):
    rows=[]
    for terminal in terminals: rows.extend((event('run',name),event(terminal,name)))
    return rows+[event('pass')]


class ResultParser(unittest.TestCase):
    name='TestWalletPaymentCancellationRaceHasOneFinancialOutcome'

    def parse(self,rows): return R.result_series(rows,R.HANDLER,(self.name,))

    def test_go_compiler_progress_is_not_a_foreign_test_identity(self):
        compiler = {'Action': 'build-output', 'ImportPath': R.MODULE+'/internal/handler', 'Output': 'PRIVATE_COMPILER_DIAGNOSTIC'}
        self.assertTrue(self.parse([compiler]+sequence(self.name))['passed'])
        inventory = [compiler, event('output', Output=self.name+'\n'), event('pass'), event('pass', package=R.NETWORK)]
        self.assertIn((R.HANDLER, self.name), R.discovered(inventory))

    def test_compiler_failure_never_becomes_complete_acceptance(self):
        compiler = {'Action': 'build-fail', 'ImportPath': R.MODULE+'/internal/handler'}
        with self.assertRaises(R.Failure): self.parse([compiler]+sequence(self.name))
        with self.assertRaises(R.Failure): R.discovered([compiler])

    def test_actual_three_results_are_preserved(self):
        result=self.parse(sequence(self.name))
        self.assertTrue(result['passed'])
        self.assertEqual(result['tests'],[{'test':self.name,'runs':3,'terminals':['pass']*3,'passed':True}])

    def test_mixed_failure_and_skip_preserve_every_terminal(self):
        for terminals in [('fail','pass','pass'),('pass','skip','fail'),('skip','skip','skip')]:
            with self.subTest(terminals=terminals):
                result=self.parse(sequence(self.name,terminals))
                self.assertFalse(result['passed'])
                self.assertEqual(result['tests'][0]['runs'],3)
                self.assertEqual(result['tests'][0]['terminals'],list(terminals))

    def test_two_results_or_unterminated_run_never_pass(self):
        rows=sequence(self.name,('pass','pass'))
        for rows in [rows,rows[:-1]+[event('run',self.name),event('pass')]]:
            result=self.parse(rows)
            self.assertFalse(result['passed'])
            self.assertEqual(result['tests'][0]['terminals'],['pass','pass'])
            self.assertIn(result['tests'][0]['runs'],(2,3))

    def test_missing_test_preserves_empty_actual_series(self):
        result=self.parse([event('pass')])
        self.assertFalse(result['passed'])
        self.assertEqual(result['tests'][0]['runs'],0)
        self.assertEqual(result['tests'][0]['terminals'],[])

    def test_duplicate_terminal_and_fourth_run_rejected(self):
        for rows in [sequence(self.name)+[event('pass',self.name)],sequence(self.name)+[event('run',self.name)]]:
            with self.assertRaises(R.Failure) as failure: self.parse(rows)
            self.assertEqual(failure.exception.kind,'incomplete_results')

    def test_duplicate_package_completion_rejected(self):
        with self.assertRaises(R.Failure): self.parse(sequence(self.name)+[event('pass')])

    def test_run_before_previous_terminal_rejected(self):
        with self.assertRaises(R.Failure): self.parse([event('run',self.name)]*2)

    def test_foreign_package_and_undeclared_test_rejected(self):
        for row in [event('run',self.name,R.NETWORK),event('run','TestOther'),{'Action':'run','Package':'external/module','Test':self.name}]:
            with self.assertRaises(R.Failure): self.parse([row])

    def test_any_child_skip_or_failure_fails_closed(self):
        for action in ('skip','fail'):
            rows=sequence(self.name)
            rows.insert(1,event(action,self.name+'/real-child'))
            self.assertFalse(self.parse(rows)['passed'])

    def test_package_fail_and_missing_completion_fail_closed(self):
        self.assertFalse(self.parse(sequence(self.name)[:-1])['passed'])
        self.assertFalse(self.parse(sequence(self.name)+[event('fail')])['passed'])


class Discovery(unittest.TestCase):
    def inventory(self):
        rows=[]
        selected={package:set() for package in (R.HANDLER,R.NETWORK)}
        for group in R.SELECTION.values():
            for package,names in group.items(): selected[package].update(names)
        for package,names in selected.items():
            for name in sorted(names): rows.append(event('output',package=package,Output=name+'\n'))
            rows.append(event('pass',package=package))
        return rows

    def test_full_inventory_before_all_exact_selections(self):
        rows=self.inventory()+[event('output',Output='TestUnselectedLegitimateCase\n')]
        inventory=R.discovered(rows)
        self.assertTrue(R.exact_selection(inventory))
        self.assertEqual(sum(map(len,R.SELECTION['sqlite'].values())),11)
        self.assertEqual(sum(map(len,R.SELECTION['mysql'].values())),14)
        # Cross-package selection cannot accidentally run a same-named test.
        for group in R.SELECTION.values():
            for names in group.values():
                selector=re.compile('^('+'|'.join(re.escape(n) for n in names)+')$')
                self.assertFalse(selector.fullmatch('TestUnselectedLegitimateCase'))
                self.assertFalse(selector.fullmatch(names[0]+'Extra'))

    def test_missing_and_duplicate_discovery_rejected(self):
        rows=self.inventory()
        inventory=R.discovered(rows[1:])
        with self.assertRaises(R.Failure): R.exact_selection(inventory)
        with self.assertRaises(R.Failure): R.discovered(rows+[rows[0]])
        with self.assertRaises(R.Failure): R.discovered(rows[:-1])
        with self.assertRaises(R.Failure): R.discovered(rows+[event('skip')])
        with self.assertRaises(R.Failure): R.discovered(rows+[event('pass')])


class Outputs(unittest.TestCase):
    def test_new_summary_is_private_and_never_overwrites(self):
        with tempfile.TemporaryDirectory() as directory:
            outer=Path(directory);root=outer/'checkout';root.mkdir();summary=outer/'summary.json'
            R.save_new(root,summary,{'passed':False,'error_class':'test_failed'})
            self.assertEqual(summary.stat().st_mode&0o777,0o600)
            original=summary.read_bytes()
            with self.assertRaises(R.Failure): R.save_new(root,summary,{'passed':True})
            self.assertEqual(original,summary.read_bytes())

    def test_checkout_and_symlink_parent_or_target_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            outer=Path(directory);root=outer/'checkout';root.mkdir()
            (outer/'link').symlink_to(root,target_is_directory=True)
            (outer/'target-link').symlink_to(outer/'absent')
            for path in [root/'summary.json',outer/'link'/'summary.json',outer/'target-link']:
                with self.assertRaises(R.Failure): R.safe_new_output(root,path)

    def test_invalid_destination_outputs_only_fixed_failure(self):
        with tempfile.TemporaryDirectory() as directory:
            outer=Path(directory);root=outer/'checkout';root.mkdir();secret=outer/'PRIVATE-DATA-DO-NOT-PRINT';secret.write_text('sensitive')
            stream=io.StringIO()
            argv=['controller','--checkout',str(root),'--revision','a'*40,'--private-dir',str(outer/'private'),'--summary',str(secret)]
            with patch('sys.argv',argv),contextlib.redirect_stdout(stream): code=R.main()
            self.assertEqual(code,1);result=json.loads(stream.getvalue())
            self.assertEqual(result['error_class'],'invalid_output')
            self.assertFalse(result['passed']);self.assertNotIn(str(secret),stream.getvalue())
            self.assertEqual(secret.read_text(),'sensitive')
            self.assertTrue(all(row['runs'] is None for g in result['groups'].values() for p in g.values() for row in p['tests']))

    def test_io_failure_never_prints_exception_or_secret(self):
        with tempfile.TemporaryDirectory() as directory:
            outer=Path(directory);root=outer/'checkout';root.mkdir();stream=io.StringIO()
            argv=['controller','--checkout',str(root),'--revision','a'*40,'--private-dir',str(outer/'private'),'--summary',str(outer/'summary')]
            with patch('sys.argv',argv),patch.object(R,'safe_new_output',side_effect=PermissionError('PRIVATE-SECRET')),contextlib.redirect_stdout(stream): code=R.main()
            self.assertEqual(code,1);self.assertEqual(json.loads(stream.getvalue())['error_class'],'io_error')
            self.assertNotIn('PRIVATE-SECRET',stream.getvalue())

    def test_summary_write_io_failure_is_fixed_and_does_not_publish_pass(self):
        with tempfile.TemporaryDirectory() as directory:
            outer=Path(directory);root=outer/'checkout';root.mkdir();stream=io.StringIO()
            summary=outer/'summary.json'
            argv=['controller','--checkout',str(root),'--revision','a'*40,'--private-dir',str(outer/'private'),'--summary',str(summary)]
            # Stop before git/Go/network; exercise the actual finally summary
            # write rather than manufacturing a parser success or a false zero.
            with patch('sys.argv',argv),patch.object(R.shutil,'which',return_value=None),patch.object(R.os,'open',side_effect=PermissionError('PRIVATE-SECRET')),contextlib.redirect_stdout(stream): code=R.main()
            self.assertEqual(code,1);result=json.loads(stream.getvalue())
            self.assertEqual(result['error_class'],'io_error');self.assertFalse(result['passed'])
            self.assertNotIn('PRIVATE-SECRET',stream.getvalue());self.assertFalse(summary.exists())


class Isolation(unittest.TestCase):
    def test_nonloopback_named_schema_and_missing_attestation_fail_before_network(self):
        for dsn,isolated in [('root:fixture@tcp(other:3306)/?parseTime=true',True),('root:fixture@tcp(127.0.0.1:3306)/existing?parseTime=true',True),('root:fixture@tcp(127.0.0.1:3306)/?parseTime=true',False)]:
            with patch.object(R.socket,'create_connection',side_effect=AssertionError('must not connect')):
                with self.assertRaises(R.Failure): R.mysql_preflight(dsn,isolated)

    def test_workflow_only_publishes_exact_safe_summary(self):
        workflow=Path(__file__).parents[1]/'.github/workflows/concurrency-race.yml'
        text=workflow.read_text();uploads=text.split('uses: actions/upload-artifact@v4')
        self.assertEqual(len(uploads),2)
        self.assertIn('path: ${{ runner.temp }}/zboard-concurrency-summary.json',uploads[1])
        self.assertNotIn('private',uploads[1]);self.assertNotIn('**',uploads[1])
        self.assertIn('"127.0.0.1:3306:3306"',text)
        self.assertIn('types: [labeled]',text)
        self.assertIn('validation:concurrency-race',text)
        self.assertNotIn('pull_request_target',text)
        source=Path(R.__file__).read_text()
        self.assertIn("'-race','-count=3','-p','1','-timeout','15m'",source)
        self.assertIn("before==after",source)
        self.assertIn("'-list','.'",source)


if __name__=='__main__': unittest.main()
