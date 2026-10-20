"""Small observer checks; no server, Docker object, or load is started."""
import contextlib
import importlib.util
import io
from pathlib import Path
import sqlite3
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('capacity_baseline', Path(__file__).with_name('acceptance-capacity-baseline.py'))
baseline = importlib.util.module_from_spec(spec)
spec.loader.exec_module(baseline)


class CapacityObserverTests(unittest.TestCase):
    def test_manifest_matches_original_sha256sum_and_tracks_untracked_sources(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            subprocess.run(['git', 'init', '-q', root], check=True)
            (root / 'backend').mkdir()
            (root / 'backend/tracked.go').write_text('package example\n')
            (root / '.gitignore').write_text('backend/ignored\n')
            subprocess.run(['git', 'add', '.'], cwd=root, check=True)
            (root / 'backend/untracked.go').write_text('package example\n')
            (root / 'backend/ignored').write_text('private fixture')
            manifest = baseline.source_manifest(root)
            original = subprocess.check_output(['bash', '-c', "while IFS= read -r -d '' source; do if test -f \"$source\"; then sha256sum \"$source\"; fi; done < <(git ls-files -z --cached --others --exclude-standard backend scripts/acceptance-mixed.sh scripts/acceptance-load.py scripts/acceptance-report.py)"], cwd=root, text=True)
            self.assertEqual(manifest, original)
            self.assertIn('backend/untracked.go', manifest)
            self.assertNotIn('ignored', manifest)

    def test_missing_export_is_unknown_and_is_not_created(self):
        with tempfile.TemporaryDirectory() as directory:
            missing = Path(directory) / 'missing.db'
            self.assertFalse(baseline.sqlite_space(missing)['available'])
            self.assertFalse(missing.exists())

    def test_stopped_sqlite_observation_preserves_database_bytes(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'export.db'
            with sqlite3.connect(path) as db:
                db.executescript('CREATE TABLE facts(id INTEGER PRIMARY KEY, value TEXT); CREATE INDEX facts_value ON facts(value); INSERT INTO facts(value) VALUES (\'retained\');')
            before = baseline.sha(path)
            result = baseline.sqlite_space(path)
            self.assertTrue(result['available'], result)
            self.assertGreater(result['objects']['index']['bytes'], 0)
            self.assertGreater(result['objects']['table']['bytes'], 0)
            self.assertEqual(baseline.sha(path), before)

    def test_wrong_or_dirty_checkout_refuses_before_docker_or_execution(self):
        for replies in (['wrong-head'], [baseline.PIN, ' M backend/changed.go']):
            with self.subTest(replies=replies), patch.object(sys, 'argv', ['observer', '--checkout', '/tmp', '--execute']), patch.object(baseline, 'command', side_effect=replies) as command, patch.object(baseline.subprocess, 'Popen') as run, contextlib.redirect_stderr(io.StringIO()):
                with self.assertRaises(SystemExit) as error:
                    baseline.main()
                self.assertEqual(error.exception.code, 2)
                self.assertFalse(run.called)
                self.assertTrue(all(call.args[0][0] == 'git' for call in command.call_args_list))

    def test_comparison_revision_is_explicit_and_preflight_never_runs_load(self):
        revision = baseline.REVISIONS[1]
        with patch.object(sys, 'argv', ['observer', '--checkout', '/tmp', '--revision', revision]), patch.object(baseline, 'command', side_effect=[revision, '', 'go version fixture']), patch.object(baseline, 'source_manifest', return_value='manifest\n'), patch.object(baseline, 'sha', return_value='fixture-hash'), patch.object(baseline.subprocess, 'Popen') as run, contextlib.redirect_stdout(io.StringIO()) as output:
            self.assertEqual(baseline.main(), 0)
            self.assertIn(revision, output.getvalue())
            self.assertIn('"execute": false', output.getvalue())
            self.assertFalse(run.called)

    def test_unapproved_revision_refuses_before_source_or_docker_reads(self):
        with patch.object(sys, 'argv', ['observer', '--checkout', '/tmp', '--revision', 'develop', '--execute']), patch.object(baseline, 'command') as command, contextlib.redirect_stderr(io.StringIO()):
            with self.assertRaises(SystemExit) as error:
                baseline.main()
            self.assertEqual(error.exception.code, 2)
            self.assertFalse(command.called)


if __name__ == '__main__':
    unittest.main()
