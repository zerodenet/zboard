package zero

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
)

type ConfigurationReconciliationResult struct {
	Output    string
	Applied   bool
	RetrySafe bool
}

// Reconcile never reloads Zero or writes configuration/credentials. A live
// receipt must match the desired digest, persisted file, process and revision.
// Without that proof an in-flight transaction on the same core stays unknown.
func (p ConfigurationPublisher) Reconcile(ctx context.Context, desiredSHA string) (ConfigurationReconciliationResult, error) {
	if p.Dialer == nil || !kernelSHA256Pattern.MatchString(desiredSHA) {
		return ConfigurationReconciliationResult{}, errors.New("invalid configuration reconciliation request")
	}
	session, err := p.Dialer.Dial(ctx)
	if err != nil {
		return ConfigurationReconciliationResult{}, network.ConfigurationOutcomeUnknown(err)
	}
	defer session.Close()
	output, err := session.RunWithLifetime(ctx, buildConfigurationReconciliationScript(desiredSHA), true)
	result := ConfigurationReconciliationResult{Output: strings.TrimSpace(output)}
	if err != nil {
		return result, network.ConfigurationOutcomeUnknown(operationError(ctx, err))
	}
	result.Applied = strings.Contains(output, "ZBOARD_CONFIG_APPLIED="+desiredSHA)
	result.RetrySafe = strings.Contains(output, "ZBOARD_CONFIG_RETRY_SAFE=1")
	if !result.Applied && !result.RetrySafe {
		return result, network.ConfigurationOutcomeUnknown(errors.New("missing conclusive configuration receipt"))
	}
	return result, nil
}

const configurationReceiptShell = `
phase() {
  printf 'ZBOARD_CONFIG_PHASE=%s at=%s\n' "$1" "$(date -u +%s)"
}
receipt=/var/lib/zerodenet/zboard-config.receipt
pending=/var/lib/zerodenet/zboard-config.pending
runtime_identity() {
  phase status
  if ! timeout 8s /usr/local/bin/zero status --json --socket /run/zerodenet/control.sock > "$stage/status.json"; then
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
receipt_matches() {
  desired= applied= instance= recorded_revision=
  [ -f "$receipt" ] || return 1
  read -r desired applied instance recorded_revision < "$receipt" || return 1
  [ "$desired" = "$1" ] && [ "$applied" = "$file_sha" ] &&
    [ "$instance" = "$core" ] && [ "$recorded_revision" = "$revision" ]
}
read_pending() {
  pending_core= pending_revision= pending_sha= pending_phase=
  read -r pending_core pending_revision pending_sha pending_phase < "$pending" || return 1
  pending_phase=${pending_phase:-applying}
  [ -n "$pending_core" ] && [ -n "$pending_sha" ] || return 1
  case "$pending_phase" in prepared|applying) ;; *) return 1;; esac
  case "$pending_revision" in ''|*[!0-9]*) return 1;; esac
}
`

func buildConfigurationReconciliationScript(desiredSHA string) string {
	return fmt.Sprintf(`set -eu
expected_sha=%s
stage=$(mktemp -d /tmp/zboard-zero-reconcile-XXXXXX)
trap 'rm -rf "$stage"' EXIT
exec 9>/var/lib/zerodenet/config-publication.lock
flock -w 2 9
%s
runtime_identity
file_sha=$(sha256sum /etc/zerodenet/current.json | awk '{print $1}')
retry_safe=0
if [ -f "$pending" ]; then
  read_pending || { printf 'ZBOARD_CONFIG_OUTCOME_UNKNOWN=1\n'; exit 1; }
  if [ "$pending_core" = "$core" ] && [ "$pending_phase" != prepared ]; then
    if ! receipt_matches "$pending_sha" || [ "$recorded_revision" -le "$pending_revision" ]; then
      printf 'ZBOARD_CONFIG_OUTCOME_UNKNOWN=1\n'
      exit 1
    fi
  fi
  retry_safe=1
fi
if receipt_matches "$expected_sha"; then
  printf 'ZBOARD_CONFIG_APPLIED=%%s\n' "$expected_sha"
elif [ "$retry_safe" = 1 ]; then
  # No unfinished transaction remains on this core. Keep the latest intent;
  # the normal publisher will validate and apply it under the same node lock.
  printf 'ZBOARD_CONFIG_RETRY_SAFE=1\n'
else
  printf 'ZBOARD_CONFIG_OUTCOME_UNKNOWN=1\n'
  exit 1
fi
`, shellQuote(desiredSHA), configurationReceiptShell)
}
