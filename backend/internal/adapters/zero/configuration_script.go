package zero

import "fmt"

// Normal publications never start/restart the kernel. The native apply
// transaction owns persistence and last-known-good runtime rollback.
func BuildConfigurationPublishScript(stage, expectedSHA string, deploymentID uint) string {
	return buildConfigurationPublishScript(stage, expectedSHA, deploymentID, "config")
}

func buildConfigurationPublishScript(stage, expectedSHA string, deploymentID uint, prefix string) string {
	return fmt.Sprintf(`set -eu
stage=%s
expected_sha=%s
backup=%s
generation=%s
receipt=/var/lib/zerodenet/zboard-config.receipt
test "$(id -u)" = "0"
command -v flock >/dev/null
install -d -m 0700 /var/lib/zerodenet
exec 9>/var/lib/zerodenet/config-publication.lock
flock -w 20 9
trap 'rm -rf "$stage"' EXIT
test -f /etc/zerodenet/current.json
if ! systemctl is-active --quiet zero; then
  printf 'ZBOARD_CONFIG_SERVICE_UNAVAILABLE\n' >&2
  exit 1
fi
runtime_identity() {
  if ! /usr/local/bin/zero status --json --socket %s > "$stage/status.json"; then
    printf 'ZBOARD_CONFIG_CONTROL_UNAVAILABLE\n' >&2
    return 1
  fi
  core=$(sed -n 's/^    "core_instance_id": "\([A-Za-z0-9_-]*\)",\{0,1\}$/\1/p' "$stage/status.json")
  revision=$(sed -n 's/^    "config_revision": \([0-9]*\),\{0,1\}$/\1/p' "$stage/status.json")
  if [ -z "$core" ] || [ -z "$revision" ]; then
    printf 'ZBOARD_CONFIG_HOT_APPLY_UNAVAILABLE\n' >&2
    return 1
  fi
}
runtime_identity
before_core=$core
before_revision=$revision
if ! /usr/local/bin/zero validate "$stage/runtime.json" >/dev/null; then
  printf 'ZBOARD_CONFIG_VALIDATION_REJECTED_CURRENT_UNCHANGED\n' >&2
  exit 1
fi
test "$(sha256sum "$stage/runtime.json" | awk '{print $1}')" = "$expected_sha"
file_sha=$(sha256sum /etc/zerodenet/current.json | awk '{print $1}')
if [ -f "$receipt" ]; then
  read -r desired applied instance recorded_revision < "$receipt" || true
  if [ "${desired:-}" = "$expected_sha" ] && [ "${applied:-}" = "$file_sha" ] &&
     [ "${instance:-}" = "$core" ] && [ "${recorded_revision:-}" = "$revision" ]; then
    printf 'ZBOARD_CONFIG_UNCHANGED=%%s\nZBOARD_CONFIG_APPLIED=%%s\n' "$expected_sha" "$expected_sha"
    exit 0
  fi
fi
install -d -m 0700 "$backup"
install -d -m 0755 /etc/zerodenet/generations
cp -L /etc/zerodenet/current.json "$backup/runtime.json"
if [ -f /etc/zerodenet/zero.env ]; then cp /etc/zerodenet/zero.env "$backup/zero.env"; fi
# CLI reload waits for config.apply's applied/reconciled acknowledgement. A
# failure is returned for retry, never converted into a service restart.
/usr/local/bin/zero reload "$stage/runtime.json" --socket %s
runtime_identity
test "$core" = "$before_core"
test "$revision" -ge "$before_revision"
file_sha=$(sha256sum /etc/zerodenet/current.json | awk '{print $1}')
printf 'ZBOARD_CONFIG_COMMITTED=%%s\n' "$expected_sha"
install -m 0600 "$stage/runtime.json" "$generation"
install -m 0600 "$stage/zero.env" /etc/zerodenet/zero.env
printf '%%s %%s %%s %%s\n' "$expected_sha" "$file_sha" "$core" "$revision" > "$receipt.next"
chmod 0600 "$receipt.next"
mv -f "$receipt.next" "$receipt"
printf 'ZBOARD_RUNTIME_RECEIPT=%%s:%%s:%%s\n' "$core" "$revision" "$file_sha"
printf 'ZBOARD_CONFIG_APPLIED=%%s\n' "$expected_sha"
`, shellQuote(stage), shellQuote(expectedSHA), shellQuote(fmt.Sprintf("/var/lib/zerodenet/backups/%s-%d", prefix, deploymentID)), shellQuote(fmt.Sprintf("/etc/zerodenet/generations/%s-%d.json", prefix, deploymentID)), shellQuote(controlSocket), shellQuote(controlSocket))
}

func BuildConfigurationRollbackScript(deploymentID uint) string {
	return fmt.Sprintf(`set -eu
backup=%s
test "$(id -u)" = "0"
test -f "$backup/runtime.json"
exec 9>/var/lib/zerodenet/config-publication.lock
flock -w 20 9
/usr/local/bin/zero reload "$backup/runtime.json" --socket %s
if [ -f "$backup/zero.env" ]; then install -m 0600 "$backup/zero.env" /etc/zerodenet/zero.env; fi
rm -f /var/lib/zerodenet/zboard-config.receipt
printf 'ZBOARD_CONFIG_ROLLED_BACK=1\n'
`, shellQuote(fmt.Sprintf("/var/lib/zerodenet/backups/config-%d", deploymentID)), shellQuote(controlSocket))
}
