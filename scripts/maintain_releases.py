#!/usr/bin/env python3
"""Reconcile published release history and refresh the current release notes."""
import json
import os
from pathlib import Path
import subprocess

from release_history_policy import obsolete, parse
from wait_release_ci import latest_ci


def gh(*args):
    return subprocess.check_output(["gh", *args], text=True)


def pages(endpoint):
    return json.loads(gh("api", "--paginate", "--slurp", endpoint))


def flatten(value):
    return [row for page in value for row in page]


def release_order(tag):
    version, family = parse(tag)
    return version, 1 if family is None else 0, tag


def main():
    repo = os.environ["GITHUB_REPOSITORY"]
    owner = os.environ["GITHUB_REPOSITORY_OWNER"]
    package = f"orgs/{owner}/packages/container/{os.environ.get('ZBOARD_GHCR_PACKAGE', 'zboard')}"
    output = Path("artifacts/release-maintenance")
    output.mkdir(parents=True, exist_ok=True)
    releases = flatten(pages(f"repos/{repo}/releases?per_page=100"))
    refs = flatten(pages(f"repos/{repo}/git/matching-refs/tags/"))
    images = flatten(pages(f"{package}/versions?per_page=100"))
    (output / "inventory-before.json").write_text(json.dumps(
        {"releases": releases, "refs": refs, "images": images}, indent=2))
    published = [release for release in releases if not release["draft"]
                 and parse(release["tag_name"]) and parse(release["tag_name"])[1] in (None, "rc")]
    current = max(published, key=lambda release: release_order(release["tag_name"]))
    current_tag = current["tag_name"]

    def ci(tag):
        sha = subprocess.check_output(["git", "rev-parse", f"{tag}^{{commit}}"], text=True).strip()
        return latest_ci(pages(f"repos/{repo}/actions/workflows/ci.yml/runs?head_sha={sha}&event=push&per_page=100"), sha)

    current_ci = ci(current_tag)
    if not current_ci or current_ci["status"] != "completed" or current_ci["conclusion"] != "success":
        raise SystemExit("Maintenance blocked: current release does not have successful complete CI")
    stable = [release["tag_name"] for release in published if parse(release["tag_name"])[1] is None]
    latest_stable = max(stable, key=release_order, default=None)
    phases = [tag for tag in (latest_stable, current_tag) if tag]
    history = lambda tag: any(obsolete(tag, phase) for phase in phases)
    tag_names = [ref["ref"].removeprefix("refs/tags/") for ref in refs]
    failed = {}
    for tag in tag_names:
        parsed = parse(tag)
        if tag == current_tag or history(tag) or not parsed or parsed[1] != "rc":
            continue
        run = ci(tag)
        if run and run["status"] == "completed" and run["conclusion"] != "success":
            failed[tag] = {"run": run["id"], "conclusion": run["conclusion"]}
    remove = lambda tag: history(tag) or tag in failed
    notes = Path(".github/release-notes") / (current_tag.split("-rc.")[0] + "-rc.md" if parse(current_tag)[1] == "rc" else current_tag + ".md")
    body = notes.read_text()
    plan = {
        "current": current_tag, "current_release_id": current["id"], "notes": str(notes),
        "failed_rc": failed,
        "releases": [{"id": release["id"], "tag": release["tag_name"]} for release in releases if remove(release["tag_name"])],
        "tags": [tag for tag in tag_names if remove(tag)],
        "images": [image["id"] for image in images if image.get("metadata", {}).get("container", {}).get("tags", [])
                   and all(remove(tag) for tag in image["metadata"]["container"]["tags"])],
    }
    assert current_tag not in plan["tags"]
    assert not any(parse(tag) is None or parse(tag)[1] is None for tag in plan["tags"])
    (output / "plan.json").write_text(json.dumps(plan, indent=2))
    print(json.dumps(plan, indent=2), flush=True)
    for release in plan["releases"]:
        gh("api", "--method", "DELETE", f"repos/{repo}/releases/{release['id']}")
    for tag in plan["tags"]:
        gh("api", "--method", "DELETE", f"repos/{repo}/git/refs/tags/{tag}")
    for version in plan["images"]:
        gh("api", "--method", "DELETE", f"{package}/versions/{version}")
    payload = output / "notes-update.json"
    payload.write_text(json.dumps({"body": body}))
    gh("api", "--method", "PATCH", f"repos/{repo}/releases/{current['id']}", "--input", str(payload))
    after = flatten(pages(f"repos/{repo}/git/matching-refs/tags/"))
    remaining = {ref["ref"].removeprefix("refs/tags/") for ref in after}
    assert not remaining.intersection(plan["tags"]) and current_tag in remaining
    release_after = json.loads(gh("api", f"repos/{repo}/releases/{current['id']}"))
    assert release_after["body"] == body
    (output / "result.json").write_text(json.dumps({"status": "success", "remaining_tags": sorted(remaining),
        "deleted_releases": len(plan["releases"]), "deleted_tags": len(plan["tags"]), "deleted_images": len(plan["images"]),
        "updated_release": current_tag}, indent=2))
    print("Release maintenance and notes read-back completed", flush=True)


if __name__ == "__main__":
    main()
