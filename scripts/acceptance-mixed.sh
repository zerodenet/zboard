#!/usr/bin/env bash
set -euo pipefail
export LC_ALL=C LANG=C
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
go_bin=${GO_BIN:-go}
mkdir -p "$root/.codex-local-artifacts/acceptance"
output=$(mktemp -d "$root/.codex-local-artifacts/acceptance/mixed-$(date -u +%Y%m%dT%H%M%SZ)-XXXXXX")
mkdir "$output/data"
sha256() { if command -v sha256sum >/dev/null; then sha256sum "$@"; else shasum -a 256 "$@"; fi; }
sources() {
  (cd "$root"; while IFS= read -r -d '' source; do
    if test -f "$source"; then sha256 "$source"; fi
  done < <(git ls-files -z --cached --others --exclude-standard backend scripts/acceptance-mixed.sh scripts/acceptance-load.py scripts/acceptance-report.py))
}
{
  date -u
  "$go_bin" version
  uname -a
  git -C "$root" rev-parse HEAD
  git -C "$root" status --short
  docker version --format '{{.Server.Version}}'
} > "$output/environment.txt"
sources > "$output/source-sha256.txt"
cp "$root/scripts/acceptance-load.py" "$output/acceptance-load.py"
cp "$root/scripts/acceptance-report.py" "$output/acceptance-report.py"
cd "$root/backend"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 "$go_bin" build -o "$output/zboard" ./cmd/zboard
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 "$go_bin" build -o "$output/acceptance" ./tools/acceptance
sources > "$output/source-after-build-sha256.txt"
if ! cmp -s "$output/source-sha256.txt" "$output/source-after-build-sha256.txt"; then
  echo "Sources changed during build; rerun. Evidence: $output" >&2; exit 1
fi
sha256 "$output/zboard" "$output/acceptance" >> "$output/environment.txt"
printf 'FROM scratch\nWORKDIR /tmp\nCOPY zboard /zboard\nCOPY acceptance /acceptance\nENV GOMAXPROCS=1\nENTRYPOINT ["/zboard"]\n' > "$output/Dockerfile"
docker build -q "$output" > "$output/image.txt"
image_id=$(cat "$output/image.txt")
container_id=""
auxiliary_id=""
volume_id=""
exported=false
sampler=""
cleanup() {
  if [[ -n "$sampler" ]]; then kill "$sampler" 2>/dev/null || true; wait "$sampler" 2>/dev/null || true; fi
  if [[ -n "$auxiliary_id" ]]; then docker rm -f "$auxiliary_id" >/dev/null 2>&1 || true; fi
  if [[ -n "$container_id" ]]; then
    docker stop -t 30 "$container_id" >/dev/null 2>&1 || true
    docker logs "$container_id" > "$output/server.log" 2>&1 || true
    docker inspect "$container_id" > "$output/container.json" 2>/dev/null || true
    if [[ "$exported" != true ]]; then docker cp "$container_id:/data/." "$output/data" || true; fi
    docker rm -f "$container_id" >/dev/null 2>&1 || true
  fi
  if [[ -n "$volume_id" ]]; then docker volume rm "$volume_id" >/dev/null 2>&1 || true; fi
  docker image rm "$image_id" >/dev/null 2>&1 || true
  echo "Mixed-load evidence: $output"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
volume_id=$(docker volume create --label zboard.acceptance=true)
docker volume inspect "$volume_id" > "$output/volume.json"
auxiliary_id=$(docker create --network none --entrypoint /acceptance -v "$volume_id:/data" "$image_id" \
  -mode seed -dir /data -nodes "${NODES:-10}" -subscriptions "${SUBSCRIPTIONS:-1000}" -records "${HISTORY_RECORDS:-100000}")
docker start -a "$auxiliary_id" > "$output/seed.log" 2>&1
test "$(docker inspect --format '{{.State.ExitCode}}' "$auxiliary_id")" = 0
docker cp "$auxiliary_id:/data/fixture.json" "$output/data/fixture.json"
docker rm "$auxiliary_id" >/dev/null
auxiliary_id=""
container_id=$(docker create --cpus 1 --memory 1g --memory-swap 1g --network bridge \
  -p 127.0.0.1::8080 -v "$volume_id:/data" "$image_id" -f /data/zboard.json)
docker start "$container_id" >/dev/null
port=$(docker port "$container_id" 8080/tcp | sed 's/.*://')
ready=false
for attempt in $(seq 1 120); do
  if curl -fsS "http://127.0.0.1:$port/readyz" > "$output/ready.json" 2>/dev/null; then ready=true; break; fi
  if [[ "$(docker inspect --format '{{.State.Running}}' "$container_id")" != true ]]; then break; fi
  sleep 1
done
if [[ "$ready" != true ]]; then echo "server did not become ready" >&2; exit 1; fi
docker exec "$container_id" /acceptance -mode sample-storage -dir /data > "$output/storage-preflight.json"
python3 - "$output/storage-preflight.json" <<'PY'
import json, sys
sample = json.load(open(sys.argv[1]))
# Default spool needs 512MiB + 1GiB free after its physical reserve is allocated.
# Leave another 64MiB for this short profile and verify actual pressure logs below.
if sample.get("disk_free_bytes", 0) < (1536 + 64) * 1024 * 1024:
    raise SystemExit("Insufficient free Docker disk for normal-pressure acceptance; evidence preserved.")
PY
(
  while [[ "$(docker inspect --format '{{.State.Running}}' "$container_id")" == true ]]; do
    docker stats --no-stream --format '{{json .}}' "$container_id" >> "$output/container-stats.jsonl"
    docker exec "$container_id" /acceptance -mode sample-storage -dir /data >> "$output/storage.jsonl"
    sleep 3
  done
) &
sampler=$!
load_exit=0
python3 "$output/acceptance-load.py" --dir "$output/data" --url "http://127.0.0.1:$port" \
  --seconds "${DURATION_SECONDS:-300}" --rate "${EVENT_RATE:-100}" --readers "${READERS:-4}" > "$output/client.log" 2>&1 || load_exit=$?
reconciled=false
drain_started=$(date +%s)
if [[ -f "$output/data/expected.json" ]]; then
  docker cp "$output/data/expected.json" "$container_id:/data/expected.json"
  for attempt in $(seq 1 24); do
    if docker exec "$container_id" /acceptance -mode verify -dir /data >> "$output/verification.log" 2>&1; then reconciled=true; break; fi
    sleep 5
  done
fi
echo "$(( $(date +%s) - drain_started ))" > "$output/drain-seconds.txt"
kill "$sampler" 2>/dev/null || true
wait "$sampler" 2>/dev/null || true
sampler=""
docker stop -t 30 "$container_id" > "$output/stop.txt"
docker inspect "$container_id" > "$output/container.json"
docker logs "$container_id" > "$output/server.log" 2>&1
auxiliary_id=$(docker create --network none --entrypoint /acceptance -v "$volume_id:/data" "$image_id" -mode verify-spool -dir /data)
docker start -a "$auxiliary_id" > "$output/spool-verification.log" 2>&1
test "$(docker inspect --format '{{.State.ExitCode}}' "$auxiliary_id")" = 0
docker rm "$auxiliary_id" >/dev/null
auxiliary_id=""
docker cp "$container_id:/data/." "$output/data"
exported=true
report_exit=0
python3 "$output/acceptance-report.py" "$output" || report_exit=$?
if [[ "$reconciled" != true || "$load_exit" != 0 || "$report_exit" != 0 ]]; then echo "load or accounting verification failed" >&2; exit 1; fi
