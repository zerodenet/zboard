#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
test_root=$(mktemp -d)
trap 'rm -rf -- "$test_root"' EXIT
mkdir -p "$test_root/deploy" "$test_root/other-cwd"
cp "$repo_root/deploy/docker/prepare-host-dirs.sh" "$test_root/deploy/prepare-host-dirs.sh"

# Defaults must be relative to the deployment script, not the caller's cwd.
(
  cd "$test_root/other-cwd"
  sh "$test_root/deploy/prepare-host-dirs.sh" >/dev/null
)
test -d "$test_root/deploy/files"
test ! -e "$test_root/other-cwd/files"
test "$(ls -ld "$test_root/deploy/files" | cut -c 1-10)" = "drwx------"
printf 'retained upload\n' > "$test_root/deploy/files/retained"
sh "$test_root/deploy/prepare-host-dirs.sh" >/dev/null
test "$(cat "$test_root/deploy/files/retained")" = "retained upload"

# Absolute and relative custom upload locations retain the same semantics.
ZBOARD_FILE_HOST_DIR="$test_root/private uploads" sh "$test_root/deploy/prepare-host-dirs.sh" >/dev/null
test -d "$test_root/private uploads"
test "$(ls -ld "$test_root/private uploads" | cut -c 1-10)" = "drwx------"
ZBOARD_FILE_HOST_DIR=./custom/files sh "$test_root/deploy/prepare-host-dirs.sh" >/dev/null
test -d "$test_root/deploy/custom/files"
printf 'prepare-host-dirs tests passed\n'
