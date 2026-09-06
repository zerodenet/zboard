#!/usr/bin/env bash
set -euo pipefail
export LC_ALL=C LANG=C
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
go_bin=${GO_BIN:-go}
artifact=${ZERO_ARTIFACT_DIR:?Set ZERO_ARTIFACT_DIR to a verified Linux Zero artifact directory}
artifact=$(cd "$artifact" && pwd)
scenario=${NODE_SCENARIO:-expiry}
case "$scenario" in
  expiry) test_name=TestRealZeroNodeExpiryRevokesDataPlane ;;
  exhaustion) test_name=TestRealZeroNodeExhaustionRevokesDataPlane ;;
  group_change) test_name=TestRealZeroNodeGroupChangeReplacesDataPlaneAccess ;;
  recovery) test_name=TestRealZeroNodeFailedPublicationSurvivesPublisherCrash ;;
  *) echo 'NODE_SCENARIO must be expiry, exhaustion, group_change or recovery' >&2; exit 1 ;;
esac
mkdir -p "$root/.codex-local-artifacts/acceptance"
output=$(mktemp -d "$root/.codex-local-artifacts/acceptance/zero-revocation-$(date -u +%Y%m%dT%H%M%SZ)-XXXXXX")
mkdir "$output/build" "$output/node-state"
python3 - "$artifact" "$output" <<'PY'
import hashlib,json,pathlib,shutil,sys
source,output=map(pathlib.Path,sys.argv[1:])
verification=json.loads((source/'verification.json').read_text())
actual=hashlib.sha256((source/'zero').read_bytes()).hexdigest()
if actual != verification['binary_sha256']:
    raise SystemExit('Zero binary does not match artifact verification')
shutil.copy2(source/'zero',output/'build/zero')
shutil.copy2(source/'verification.json',output/'zero-verification.json')
PY
sources() {
  (cd "$root"; while IFS= read -r -d '' file; do
    if test -f "$file"; then shasum -a 256 "$file"; fi
  done < <(git ls-files -z --cached --others --exclude-standard backend scripts/acceptance-node.sh scripts/acceptance/zero-node))
}
sources > "$output/source-sha256.txt"
cp "$root/scripts/acceptance/zero-node/"* "$output/build/"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 "$go_bin" -C "$root/backend" test -c -o "$output/handler.test" ./internal/handler
sources > "$output/source-after-build-sha256.txt"
cmp "$output/source-sha256.txt" "$output/source-after-build-sha256.txt"
shasum -a 256 "$output/handler.test" > "$output/test-binary-sha256.txt"
"$go_bin" version > "$output/environment.txt"
docker version --format '{{.Server.Version}}' >> "$output/environment.txt"
git -C "$root" rev-parse HEAD >> "$output/environment.txt"
git -C "$root" status --short >> "$output/environment.txt"
printf 'node_base_image=%s\n' "${NODE_BASE_IMAGE:-debian:trixie-slim}" >> "$output/environment.txt"
ssh-keygen -q -t ed25519 -N '' -C zboard-isolated-acceptance -f "$output/id_ed25519"
image_id=""; network_id=""; node_id=""; driver_id=""
cleanup() {
  if test -n "$driver_id"; then
    docker logs "$driver_id" > "$output/driver.log" 2>&1 || true
    docker inspect "$driver_id" > "$output/driver-container.json" || true
    docker rm -fv "$driver_id" >/dev/null 2>&1 || true
  fi
  if test -n "$node_id"; then
    docker exec "$node_id" journalctl --no-pager -u zero -u zboard-acceptance-echo > "$output/node-journal.log" 2>&1 || true
    docker inspect "$node_id" > "$output/node-container.json" || true
    docker stop -t 20 "$node_id" >/dev/null 2>&1 || true
    docker rm -fv "$node_id" >/dev/null 2>&1 || true
  fi
  if test -n "$network_id"; then docker network rm "$network_id" >/dev/null 2>&1 || true; fi
  if test -n "$image_id"; then docker image rm "$image_id" >/dev/null 2>&1 || true; fi
  echo "Real Zero evidence: $output"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
# Proxy build args are optional and are not persisted as node environment.
docker build -q --build-arg "BASE_IMAGE=${NODE_BASE_IMAGE:-debian:trixie-slim}" \
  --build-arg http_proxy --build-arg https_proxy "$output/build" > "$output/image.txt" 2> "$output/build.log"
image_id=$(cat "$output/image.txt")
docker image inspect "$image_id" > "$output/image.json"
network_id=$(docker network create --internal --label zboard.acceptance=zero-node "zboard-$(basename "$output")")
# systemd gets a private cgroup namespace without bind-mounting host cgroup
# paths. Explicit writable host mounts are confined to this run's evidence.
node_id=$(docker create --privileged --cgroupns=private --cpus 1 --memory 512m --memory-swap 512m \
  --tmpfs /run --tmpfs /run/lock --tmpfs /tmp --network "$network_id" --network-alias zero-acceptance-node \
  --label zboard.acceptance=zero-node \
  --mount "type=bind,source=$output/id_ed25519.pub,target=/root/.ssh/authorized_keys,readonly" \
  --mount "type=bind,source=$output/node-state,target=/var/lib/zerodenet" "$image_id")
docker start "$node_id" >/dev/null
docker exec "$node_id" dpkg-query -W systemd openssh-server > "$output/node-packages.txt"
ready=false
for attempt in $(seq 1 60); do
  if docker exec "$node_id" systemctl is-active --quiet ssh; then ready=true; break; fi
  if test "$(docker inspect --format '{{.State.Running}}' "$node_id")" != true; then break; fi
  sleep 1
done
if test "$ready" != true; then echo 'SSH service did not become ready' >&2; exit 1; fi
docker exec "$node_id" stat -f -c '{"block_size":%S,"available_blocks":%a,"total_blocks":%b}' /var/lib/zerodenet > "$output/node-storage.json"
python3 - "$output/node-storage.json" <<'PY'
import json,sys
s=json.load(open(sys.argv[1])); available=s['block_size']*s['available_blocks'];total=s['block_size']*s['total_blocks']
if available < max(1024**3,total*5//100)+64*1024**2:
    raise SystemExit('Dedicated node directory lacks space for unchanged Zero outbox defaults')
PY
fingerprint=$(docker exec "$node_id" ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub -E sha256 | awk '{print $2}')
driver_id=$(docker create --network "$network_id" --cpus 2 --memory 768m --memory-swap 768m \
  --label zboard.acceptance=zero-node-driver --mount "type=bind,source=$output,target=/evidence" \
  -e ZBOARD_TEST_ZERO_NODE=isolated-docker -e ZBOARD_TEST_ZERO_SSH_ADDR=zero-acceptance-node:22 \
  -e ZBOARD_TEST_ZERO_PROXY_ADDR=zero-acceptance-node:8443 -e ZBOARD_TEST_ZERO_SSH_KEY=/evidence/id_ed25519 \
  -e "ZBOARD_TEST_ZERO_HOST_KEY=$fingerprint" "$image_id" \
  /evidence/handler.test -test.run "^${test_name}$" -test.v -test.timeout 4m)
docker start -a "$driver_id" > "$output/driver.log" 2>&1
driver_exit=$(docker inspect --format '{{.State.ExitCode}}' "$driver_id")
python3 - "$output" "$driver_exit" "$scenario" <<'PY'
import json,pathlib,sys
p=pathlib.Path(sys.argv[1]); results=[]
for line in (p/'driver.log').read_text().splitlines():
    if 'ZBOARD_ZERO_ACCEPTANCE_RESULT=' in line:
        results.append(json.loads(line.split('ZBOARD_ZERO_ACCEPTANCE_RESULT=',1)[1]))
passed=sys.argv[2]=='0' and len(results)==1 and results[0].get('scenario')==sys.argv[3] and results[0].get('passed') is True
report={'passed':passed,'scenarios':results,'driver_exit_code':int(sys.argv[2]),'scope':'Selected real Zero scenario only; this result does not complete the other revocation, recovery or performance requirements.'}
(p/'result.json').write_text(json.dumps(report,indent=2)+'\n')
if not passed: raise SystemExit('Real Zero '+sys.argv[3]+' acceptance failed; evidence preserved')
PY
