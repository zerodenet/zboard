"""Ownership/provenance policy tests; no Go, Docker or database is invoked."""
import ast
import hashlib
import importlib.util
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parent.parent
TEMPLATES = ROOT / "scripts" / "acceptance-upgrade"
spec = importlib.util.spec_from_file_location("upgrade_controller", TEMPLATES / "run.py")
upgrade = importlib.util.module_from_spec(spec)
# Import must not create an untracked nested __pycache__: the controller
# intentionally requires an exact clean primary checkout before exporting.
previous_bytecode_policy = sys.dont_write_bytecode
try:
    sys.dont_write_bytecode = True
    spec.loader.exec_module(upgrade)
finally:
    sys.dont_write_bytecode = previous_bytecode_policy


class UpgradeOwnershipHandoffTest(unittest.TestCase):
    def release(self, outcome):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        root = Path(directory.name)
        path = root / "old-seed.log"
        log = path.open("w")
        log.write("old fixture completed its immutable backup\n")
        log.flush()
        events = []

        class Process:
            pid = 77
            def wait(self, timeout):
                events.append(("wait", timeout, (root / "snapshot-complete").exists()))
                if isinstance(outcome, Exception):
                    raise outcome
                return outcome

        return Process(), log, root, events

    def test_explicit_release_is_confirmed_before_downstream_can_start(self):
        process, log, root, events = self.release(0)
        metadata = {}
        upgrade.release_old_mysql_fixture(process, log, root, metadata)
        self.assertEqual(events, [("wait", 150, True)])
        self.assertTrue(log.closed)
        self.assertTrue((root / "old-owner-released.json").exists())
        self.assertEqual(metadata["old_seed_exit_code"], 0)
        self.assertTrue(metadata["old_seed_released_before_current_clone"])
        self.assertEqual(metadata["old_seed_log_sha256"], hashlib.sha256((root / "old-seed.log").read_bytes()).hexdigest())

    def test_nonzero_owner_exit_does_not_permit_downstream(self):
        process, log, root, events = self.release(1)
        metadata = {}
        with self.assertRaisesRegex(RuntimeError, "old fixture failed"):
            upgrade.release_old_mysql_fixture(process, log, root, metadata)
        self.assertEqual(metadata["old_seed_exit_code"], 1)
        self.assertNotIn("old_seed_released_before_current_clone", metadata)
        self.assertTrue(log.closed)
        self.assertTrue((root / "old-seed.log").exists())

    def test_uncertain_exit_is_preserved_without_killing_or_restarting_owner(self):
        process, log, root, events = self.release(subprocess.TimeoutExpired("old.test", 150))
        metadata = {}
        with self.assertRaisesRegex(RuntimeError, "release not confirmed"):
            upgrade.release_old_mysql_fixture(process, log, root, metadata)
        self.assertEqual(events, [("wait", 150, True)])
        self.assertIn("uncertain", metadata["old_seed_outcome"])
        self.assertEqual(metadata["old_seed_pid"], 77)
        self.assertNotIn("old_seed_released_before_current_clone", metadata)
        self.assertTrue(log.closed)

    def test_controller_orders_release_then_independent_copy_then_upgrade(self):
        tree = ast.parse((TEMPLATES / "run.py").read_text())
        main = next(node for node in tree.body if isinstance(node, ast.FunctionDef) and node.name == "main")
        calls = [node for node in ast.walk(main) if isinstance(node, ast.Call)]
        release = min(node.lineno for node in calls if isinstance(node.func, ast.Name) and node.func.id == "release_old_mysql_fixture")
        # Remove the owner from finally's release list before the first attempt:
        # a timeout is uncertain, so it must never silently retry in finally.
        remove = min(node.lineno for node in calls if isinstance(node.func, ast.Attribute) and isinstance(node.func.value, ast.Name) and node.func.value.id == "processes" and node.func.attr == "remove")
        copy = min(node.lineno for node in calls if isinstance(node.func, ast.Name) and node.func.id == "run" and node.args and isinstance(node.args[0], ast.BinOp) and isinstance(node.args[0].right, ast.Constant) and node.args[0].right.value == "current-copy-prepare")
        migrate = min(node.lineno for node in calls if isinstance(node.func, ast.Name) and node.func.id == "run" and node.args and isinstance(node.args[0], ast.BinOp) and isinstance(node.args[0].right, ast.Constant) and node.args[0].right.value == "current-upgrade")
        self.assertLess(remove, release)
        self.assertLess(release, copy)
        self.assertLess(copy, migrate)

    def test_current_mysql_requires_verified_copy_and_never_deletes_it(self):
        copy = (TEMPLATES / "current_mysql_copy_probe.go.txt").read_text()
        current = (TEMPLATES / "current_upgrade_probe.go.txt").read_text()
        authority = (TEMPLATES / "current_authority_probe.go.txt").read_text()
        self.assertIn('CREATE DATABASE `', copy)
        self.assertNotIn('CREATE DATABASE IF NOT EXISTS', copy)
        self.assertNotIn('DROP DATABASE', copy)
        self.assertIn('current-create-confirmed.json', copy)
        self.assertIn('current-copy-ready.json', copy)
        self.assertIn('!reflect.DeepEqual(actual, old.Frozen)', copy)
        self.assertIn('readCurrentUpgradeDestination(t, dir)', authority)
        self.assertIn('currentAuthorityMySQL(t, dir)', current)
        self.assertNotIn('cfg.DBName, cfg.ParseTime, cfg.Loc = identity.Schema', current)
        self.assertIn('strings.TrimSpace(line) == cfg.Addr', authority)
        self.assertIn('^127\\.0\\.0\\.1:[0-9]+$', authority)

    def test_one_shot_workflow_preserves_loopback_and_original_probe_limits(self):
        workflow = (ROOT / '.github/workflows/upgrade-acceptance.yml').read_text()
        self.assertIn('types: [labeled]', workflow)
        self.assertIn("github.event.label.name == 'validation:upgrade-full'", workflow)
        self.assertNotIn('push:', workflow)
        self.assertIn('"127.0.0.1:3306:3306"', workflow)
        self.assertIn('fetch-depth: 0', workflow)
        self.assertIn('--driver both', workflow)
        self.assertIn('if: always()', workflow)
        self.assertNotIn('retry', workflow.lower())
        controller = (TEMPLATES / 'run.py').read_text()
        self.assertNotIn('"10m"', controller)
        self.assertNotIn('"15m"', controller)
        old = (TEMPLATES / 'old_seed_probe.go.txt').read_text()
        self.assertIn('time.NewTimer(2 * time.Minute)', old)

    def test_artifact_upload_is_positive_named_fixture_allowlist(self):
        workflow = (ROOT / '.github/workflows/upgrade-acceptance.yml').read_text()
        block = workflow.split('          path: |\n', 1)[1].split('          if-no-files-found:', 1)[0]
        paths = [line.strip() for line in block.splitlines() if line.strip()]
        prefix = '${{ runner.temp }}/zboard-upgrade-acceptance/'
        self.assertTrue(paths)
        for path in paths:
            self.assertTrue(path.startswith(prefix), path)
            self.assertNotIn('**', path)
            self.assertNotIn('.codex', path)
            self.assertNotIn('old-source/', path)
            self.assertNotIn('current-source/', path)
            self.assertFalse(path.endswith(('/', '/*', '.test')), path)
            if 'upgrade-both-*' in path:
                self.assertEqual(path.count('*'), 1, path)
        self.assertIn(prefix + 'upgrade-both-*/mysql/old-source.sql', paths)
        self.assertIn(prefix + 'upgrade-both-*/mysql/current-source.sql', paths)
        self.assertIn(prefix + 'upgrade-both-*/sqlite/old-source.db', paths)
        self.assertIn(prefix + 'upgrade-both-*/sqlite/current-source.db', paths)
        self.assertIn(prefix + 'upgrade-both-*/final.json', paths)



if __name__ == '__main__':
    unittest.main()
