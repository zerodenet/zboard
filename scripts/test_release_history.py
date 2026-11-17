import contextlib
import io
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import maintain_releases
import wait_release_ci
from release_history_policy import obsolete, select
from wait_release_ci import latest_ci


class ReleaseHistoryTests(unittest.TestCase):
    def test_phase_and_version_scope_preserves_newer_and_unrelated_tags(self):
        for tag in ("v0.0.2-dev.1", "v0.0.3-dev", "v0.0.3-dev.2"):
            self.assertTrue(obsolete(tag, "v0.0.3-rc.1"))
        for tag in ("v0.0.4-dev.1", "v0.0.3-rc.1", "v0.0.2", "backend/pkg/pluginapi/v0.0.1", "other-dev.1"):
            self.assertFalse(obsolete(tag, "v0.0.3-rc.1"))
        self.assertTrue(obsolete("v0.0.2-rc.1", "v0.0.2"))
        self.assertFalse(obsolete("v0.0.3-rc.1", "v0.0.2"))
        self.assertFalse(obsolete("v0.0.10-dev.1", "v0.0.9-rc.1"))

    def test_all_pages_and_shared_image_tags(self):
        images = [[{"id": 1, "metadata": {"container": {"tags": ["v0.0.3-dev.1"]}}}],
                  [{"id": 2, "metadata": {"container": {"tags": ["v0.0.3-dev.2", "latest"]}}},
                   {"id": 3, "metadata": {"container": {"tags": []}}}]]
        self.assertEqual(list(select("packages", images, "v0.0.3-rc.1")), ["1"])
        refs = [[{"ref": "refs/tags/v0.0.2-dev.1"}], [{"ref": "refs/tags/v0.0.3-dev.1"}]]
        self.assertEqual(list(select("refs", refs, "v0.0.3-rc.1")), ["v0.0.2-dev.1", "v0.0.3-dev.1"])

    def test_ci_requires_exact_push_commit_and_latest_attempt(self):
        run = {"id": 1, "head_sha": "a" * 40, "event": "push", "path": ".github/workflows/ci.yml", "conclusion": "success"}
        wrong = [{**run, "id": 20, "head_sha": "b" * 40}, {**run, "id": 21, "event": "pull_request"},
                 {**run, "id": 22, "path": ".github/workflows/release.yml"}]
        failed = {**run, "id": 2, "conclusion": "failure"}
        self.assertEqual(latest_ci([{"workflow_runs": [run, *wrong]}, {"workflow_runs": [failed]}], "a" * 40), failed)

    def test_failed_complete_ci_blocks_release_immediately(self):
        run = {"id": 1, "head_sha": "a" * 40, "event": "push", "path": ".github/workflows/ci.yml",
               "status": "completed", "conclusion": "failure", "html_url": "https://github.com/org/repo/actions/runs/1"}
        response = type("Response", (), {"stdout": json.dumps([{"workflow_runs": [run]}])})()
        with patch.dict(os.environ, {"GITHUB_REPOSITORY": "org/repo"}), \
             patch("sys.argv", ["wait_release_ci.py", "a" * 40]), \
             patch.object(wait_release_ci.subprocess, "run", return_value=response), \
             patch.object(wait_release_ci.time, "sleep") as sleep, contextlib.redirect_stdout(io.StringIO()):
            with self.assertRaisesRegex(SystemExit, "Release blocked: complete CI concluded failure"):
                wait_release_ci.main()
            sleep.assert_not_called()

    def test_maintenance_deletes_only_obsolete_and_completed_failed_rc(self):
        current = "v0.0.3-rc.3"
        notes = "Current RC notes\n"
        tags = ["v0.0.2", "v0.0.2-rc.1", "v0.0.2-dev.1", "v0.0.3-dev.1", "v0.0.3-rc.1", "v0.0.3-rc.2", current, "backend/pkg/pluginapi/v0.0.1"]
        releases = [{"id": 1, "tag_name": "v0.0.2", "draft": False}, {"id": 2, "tag_name": current, "draft": False},
                    {"id": 3, "tag_name": "v0.0.3-rc.1", "draft": False}]
        removed = set()
        mutations = []

        def fake_pages(endpoint):
            if "/actions/" in endpoint:
                tag_number = int(endpoint.split("head_sha=")[1].split("&")[0], 16)
                tag = tags[tag_number - 1]
                return [{"workflow_runs": [{"id": tag_number, "head_sha": f"{tag_number:040x}", "event": "push",
                    "path": ".github/workflows/ci.yml", "status": "in_progress" if tag.endswith("rc.2") else "completed",
                    "conclusion": "failure" if tag.endswith("rc.1") else "success"}]}]
            if "/releases?" in endpoint:
                return [releases]
            if "/matching-refs/" in endpoint:
                return [[{"ref": "refs/tags/" + tag} for tag in tags if tag not in removed]]
            if "/versions?" in endpoint:
                return [[{"id": 10, "metadata": {"container": {"tags": ["v0.0.3-rc.1"]}}},
                         {"id": 11, "metadata": {"container": {"tags": [current, "v0.0.3-dev.1"]}}}]]
            raise AssertionError(endpoint)

        def fake_gh(*args):
            if "--method" in args:
                mutations.append(args)
                if "/git/refs/tags/" in args[3]:
                    removed.add(args[3].split("/tags/")[1])
                return ""
            return json.dumps({"body": notes})

        def fake_git(args, **kwargs):
            tag = args[-1].split("^")[0]
            return f"{tags.index(tag) + 1:040x}\n"

        with tempfile.TemporaryDirectory() as folder:
            previous = Path.cwd()
            try:
                os.chdir(folder)
                Path(".github/release-notes").mkdir(parents=True)
                Path(".github/release-notes/v0.0.3-rc.md").write_text(notes)
                with patch.dict(os.environ, {"GITHUB_REPOSITORY": "org/repo", "GITHUB_REPOSITORY_OWNER": "org"}), \
                     patch.object(maintain_releases, "pages", fake_pages), patch.object(maintain_releases, "gh", fake_gh), \
                     patch.object(maintain_releases.subprocess, "check_output", fake_git), contextlib.redirect_stdout(io.StringIO()):
                    maintain_releases.main()
                result = json.loads(Path("artifacts/release-maintenance/result.json").read_text())
                self.assertEqual(removed, {"v0.0.2-rc.1", "v0.0.2-dev.1", "v0.0.3-dev.1", "v0.0.3-rc.1"})
                self.assertEqual(result["deleted_images"], 1)
                self.assertIn("v0.0.3-rc.2", result["remaining_tags"])
                self.assertEqual(mutations[-1][1:3], ("--method", "PATCH"))
            finally:
                os.chdir(previous)


if __name__ == "__main__":
    unittest.main()
