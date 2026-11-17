#!/usr/bin/env python3
"""Require the exact release commit's complete push CI before publishing."""
import argparse
import json
import os
import re
import subprocess
import time


def latest_ci(pages, sha):
    runs = [run for page in pages for run in page["workflow_runs"]
            if run["head_sha"] == sha and run["event"] == "push"
            and run["path"].split("@")[0] == ".github/workflows/ci.yml"]
    return max(runs, key=lambda run: run["id"], default=None)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("sha")
    args = parser.parse_args()
    if not re.fullmatch(r"[0-9a-f]{40}", args.sha):
        parser.error("a full commit SHA is required")
    repository = os.environ["GITHUB_REPOSITORY"]
    endpoint = f"repos/{repository}/actions/workflows/ci.yml/runs?head_sha={args.sha}&event=push&per_page=100"
    started = time.monotonic()
    previous = None
    while time.monotonic() - started < 5400:
        response = subprocess.run(["gh", "api", "--paginate", "--slurp", endpoint],
                                  check=True, capture_output=True, text=True)
        run = latest_ci(json.loads(response.stdout), args.sha)
        state = (run["id"], run["status"], run["conclusion"]) if run else None
        if state != previous:
            print(f"Release CI: {state or 'waiting for push CI registration'}", flush=True)
            previous = state
        if run and run["status"] == "completed":
            if run["conclusion"] != "success":
                raise SystemExit(f"Release blocked: complete CI concluded {run['conclusion']} ({run['html_url']})")
            print(f"Release CI passed: {run['html_url']}", flush=True)
            return
        if not run and time.monotonic() - started >= 120:
            raise SystemExit("Release blocked: no push CI exists for this commit; push its branch first")
        time.sleep(15)
    raise SystemExit("Release blocked: complete CI did not pass within 90 minutes")


if __name__ == "__main__":
    main()
