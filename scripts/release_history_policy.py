#!/usr/bin/env python3
"""Select obsolete product releases, refs and exclusively obsolete images."""
import argparse
import json
import re

TAG = re.compile(r"^v(\d+)\.(\d+)\.(\d+)(?:-(dev|rc)(?:\.[0-9A-Za-z][0-9A-Za-z.-]*)?)?$")


def parse(tag):
    match = TAG.fullmatch(tag)
    return (tuple(map(int, match.group(1, 2, 3))), match[4]) if match else None


def obsolete(tag, published):
    candidate, current = parse(tag), parse(published)
    if not candidate or not current:
        return False
    family = "dev" if current[1] == "rc" else "rc" if current[1] is None else None
    return family is not None and candidate[1] == family and candidate[0] <= current[0]


def select(kind, pages, published):
    for page in pages:
        for item in page:
            if kind == "releases" and obsolete(item["tag_name"], published):
                yield item["tag_name"]
            elif kind == "refs":
                tag = item["ref"].removeprefix("refs/tags/")
                if obsolete(tag, published):
                    yield tag
            elif kind == "packages":
                tags = item.get("metadata", {}).get("container", {}).get("tags", [])
                # Shared digests carrying stable/current/unrelated tags stay intact.
                if tags and all(obsolete(tag, published) for tag in tags):
                    yield str(item["id"])


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("published")
    parser.add_argument("kind", choices=("releases", "refs", "packages"))
    parser.add_argument("inventory")
    args = parser.parse_args()
    with open(args.inventory, encoding="utf-8") as source:
        for value in select(args.kind, json.load(source), args.published):
            print(value)
