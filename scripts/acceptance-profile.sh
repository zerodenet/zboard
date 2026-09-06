#!/usr/bin/env bash
set -euo pipefail
export LC_ALL=C LANG=C
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
source_dir=$(cd "${1:?pass a completed mixed-load evidence directory}" && pwd)
case "$source_dir" in "$root/.codex-local-artifacts/acceptance/"*) ;; *) echo "only isolated acceptance artifacts are allowed" >&2; exit 1 ;; esac
python3 - "$source_dir/container.json" <<'PY'
import json,sys
state=json.load(open(sys.argv[1]))[0]['State']
if state['Running']: raise SystemExit('source server was not stopped before export')
PY
output=$(mktemp -d "$root/.codex-local-artifacts/acceptance/read-profile-$(date -u +%Y%m%dT%H%M%SZ)-XXXXXX")
go_bin=${GO_BIN:-go}
sources() {
  (cd "$root"; while IFS= read -r -d '' file; do
    if test -f "$file"; then shasum -a 256 "$file"; fi
  done < <(git ls-files -z --cached --others --exclude-standard backend scripts/acceptance-profile.sh))
}
sources > "$output/source-sha256.txt"
(
  cd "$root/backend"
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 "$go_bin" build -o "$output/acceptance" ./tools/acceptance
)
sources > "$output/source-after-build-sha256.txt"
cmp "$output/source-sha256.txt" "$output/source-after-build-sha256.txt"
shasum -a 256 "$output/acceptance" > "$output/binary-sha256.txt"
printf '%s\n' "$source_dir" > "$output/fixture-source.txt"
"$go_bin" version > "$output/environment.txt"
docker version --format '{{.Server.Version}}' >> "$output/environment.txt"
printf 'FROM scratch\nWORKDIR /tmp\nCOPY acceptance /acceptance\nENV GOMAXPROCS=1\nENTRYPOINT ["/acceptance"]\n' > "$output/Dockerfile"
image_id=$(docker build -q "$output")
volume_id=""
container_id=""
cleanup() {
  if test -n "$container_id"; then
    docker logs "$container_id" > "$output/profile.log" 2>&1 || true
    docker inspect "$container_id" > "$output/container.json" || true
    docker cp "$container_id:/data/read-profile.json" "$output/read-profile.json" || true
    docker rm -fv "$container_id" >/dev/null 2>&1 || true
  fi
  if test -n "$volume_id"; then docker volume rm "$volume_id" >/dev/null 2>&1 || true; fi
  docker image rm "$image_id" >/dev/null 2>&1 || true
  echo "Read-profile evidence: $output"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
volume_id=$(docker volume create --label zboard.acceptance=profile)
container_id=$(docker create --cpus 1 --memory 1g --memory-swap 1g --network none -v "$volume_id:/data" "$image_id" -mode profile-reads -dir /data)
for file in fixture.json zboard.db; do
  docker cp "$source_dir/data/$file" "$container_id:/data/$file"
done
for file in zboard.db-wal zboard.db-shm; do
  if test -f "$source_dir/data/$file"; then docker cp "$source_dir/data/$file" "$container_id:/data/$file"; fi
done
docker start -a "$container_id" > "$output/profile.log" 2>&1
test "$(docker inspect --format '{{.State.ExitCode}}' "$container_id")" = 0
