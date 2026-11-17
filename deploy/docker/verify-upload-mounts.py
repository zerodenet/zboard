#!/usr/bin/env python3
"""Validate upload persistence in a rendered Compose config without logging secrets."""
import json
import pathlib
import sys

config = json.load(sys.stdin)
sources = set()
checked = 0
for name, service in config.get("services", {}).items():
    if name != "zboard" and not name.startswith("zboard-"):
        continue
    directory = service.get("environment", {}).get("ZBOARD_FILE_STORAGE_DIR")
    if directory != "/var/lib/zboard/files":
        raise SystemExit(f"{name}: upload directory must be explicitly configured")
    mounts = [v for v in service.get("volumes", []) if v.get("target") == directory]
    if len(mounts) != 1 or mounts[0].get("type") != "bind" or mounts[0].get("read_only", False):
        raise SystemExit(f"{name}: uploads require one writable host bind mount")
    source = mounts[0].get("source", "")
    if not source or not pathlib.Path(source).is_absolute():
        raise SystemExit(f"{name}: upload host directory must resolve to an absolute path")
    sources.add(source)
    checked += 1
if not checked or len(sources) != 1:
    raise SystemExit("application instances must share one durable upload directory")
print(f"Verified persistent upload mount for {checked} application service(s)")
