#!/usr/bin/env bash
set -euo pipefail
export LC_ALL=C LANG=C

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
go_bin=${GO_BIN:-go}
mode=${1:-local}
case "$mode" in local|container) ;; *) echo "usage: $0 [local|container]" >&2; exit 2;; esac
mkdir -p "$root/.codex-local-artifacts/acceptance"
output=$(mktemp -d "$root/.codex-local-artifacts/acceptance/accounting-$(date -u +%Y%m%dT%H%M%SZ)-XXXXXX")
sha256() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$@"; else shasum -a 256 "$@"; fi
}
cd "$root"
record_sources() {
  (
    cd "$root"
    while IFS= read -r -d '' source; do
      if test -f "$source"; then sha256 "$source"; fi
    done < <(git ls-files -z --cached --others --exclude-standard backend scripts/benchmark-accounting.sh)
  )
}
verify_build_sources() {
  record_sources > "$output/source-after-build-sha256.txt"
  if ! cmp -s "$output/source-sha256.txt" "$output/source-after-build-sha256.txt"; then
    echo "Sources changed during build; rerun. Evidence: $output" >&2
    exit 1
  fi
}
{
  date -u
  "$go_bin" version
  uname -a
  git rev-parse HEAD
  git status --short
  echo "mode=$mode; batch=32; benchtime=${BENCH_TIME:-100x}; repeats=${BENCH_COUNT:-3}"
} > "$output/environment.txt"
record_sources > "$output/source-sha256.txt"
sha256 "$output/source-sha256.txt" >> "$output/environment.txt"
cd "$root/backend"
if [[ "$mode" == local ]]; then
  "$go_bin" test -c -o "$output/accounting.test" ./internal/handler
  verify_build_sources
  sha256 "$output/accounting.test" >> "$output/environment.txt"
  "$output/accounting.test" -test.run '^$' -test.bench '^BenchmarkZeroAccounting$' \
    -test.benchtime "${BENCH_TIME:-100x}" -test.count "${BENCH_COUNT:-3}" -test.benchmem \
    -test.cpuprofile "$output/cpu.pprof" -test.memprofile "$output/heap.pprof" > "$output/benchmark.txt" 2>&1
else
  # Scratch image contains only the current static test executable, no database
  # daemon, source checkout, host credentials, or dependency downloads.
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 "$go_bin" test -c -o "$output/accounting.test" ./internal/handler
  verify_build_sources
  sha256 "$output/accounting.test" >> "$output/environment.txt"
  printf 'FROM scratch\nWORKDIR /tmp\nCOPY accounting.test /accounting.test\nENV GOMAXPROCS=1\nENTRYPOINT ["/accounting.test"]\n' > "$output/Dockerfile"
  docker version --format '{{.Server.Version}}' >> "$output/environment.txt"
  docker build -q "$output" > "$output/image.txt"
  image_id=$(cat "$output/image.txt")
  container_id=""
  cleanup() {
    if [[ -n "$container_id" ]]; then docker rm -f "$container_id" >/dev/null 2>&1 || true; fi
    docker image rm "$image_id" >/dev/null 2>&1 || true
  }
  trap cleanup EXIT
  container_id=$(docker create --network none --cpus 1 --memory 1g --memory-swap 1g "$image_id" \
    -test.run '^$' -test.bench '^BenchmarkZeroAccounting$' -test.benchtime "${BENCH_TIME:-100x}" \
    -test.count "${BENCH_COUNT:-3}" -test.benchmem -test.cpuprofile /tmp/cpu.pprof -test.memprofile /tmp/heap.pprof)
  docker start "$container_id" >/dev/null
  while [[ "$(docker inspect --format '{{.State.Running}}' "$container_id")" == true ]]; do
    docker stats --no-stream --format '{{json .}}' "$container_id" >> "$output/container-stats.jsonl"
    sleep 2
  done
  docker logs "$container_id" > "$output/benchmark.txt" 2>&1
  docker inspect "$container_id" > "$output/container.json"
  docker cp "$container_id:/tmp/cpu.pprof" "$output/cpu.pprof" >/dev/null
  docker cp "$container_id:/tmp/heap.pprof" "$output/heap.pprof" >/dev/null
  code=$(docker inspect --format '{{.State.ExitCode}}' "$container_id")
  if [[ "$code" != 0 ]]; then echo "benchmark failed; evidence: $output" >&2; exit "$code"; fi
fi
echo "Benchmark evidence: $output"
