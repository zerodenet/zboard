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

type ConfigurationReconciliationRequest struct {
	DesiredSHA256        string
	LaterCompletedSHA256 string
}

// Reconcile only observes. Applied requires a receipt for this live generation.
// RetrySafe ends an old intent without confirming its credentials/readiness;
// the normal publisher must then compile and apply the latest intent.
func (p ConfigurationPublisher) Reconcile(ctx context.Context, request ConfigurationReconciliationRequest) (ConfigurationReconciliationResult, error) {
	if p.Dialer == nil || !kernelSHA256Pattern.MatchString(request.DesiredSHA256) || (request.LaterCompletedSHA256 != "" && !kernelSHA256Pattern.MatchString(request.LaterCompletedSHA256)) {
		return ConfigurationReconciliationResult{}, errors.New("invalid configuration reconciliation request")
	}
	session, err := p.Dialer.Dial(ctx)
	if err != nil {
		return ConfigurationReconciliationResult{}, network.ConfigurationOutcomeUnknown(err)
	}
	defer session.Close()
	output, err := session.RunWithLifetime(ctx, buildConfigurationReconciliationScript(request.DesiredSHA256, request.LaterCompletedSHA256), true)
	result := ConfigurationReconciliationResult{Output: strings.TrimSpace(output)}
	if err != nil {
		return result, network.ConfigurationOutcomeUnknown(operationError(ctx, configurationReconciliationError(output, err)))
	}
	result.Applied = strings.Contains(output, "ZBOARD_CONFIG_APPLIED="+request.DesiredSHA256)
	result.RetrySafe = strings.Contains(output, "ZBOARD_CONFIG_RETRY_SAFE=1")
	if !result.Applied && !result.RetrySafe {
		return result, network.ConfigurationOutcomeUnknown(errors.New("missing conclusive configuration receipt"))
	}
	return result, nil
}

// Only protocol markers become user-facing diagnostics. Never echo a native
// status/configuration dump (which may contain credentials or connection data).
func configurationReconciliationError(output string, cause error) error {
	reasons := []struct{ marker, message string }{
		{"publication_lock_busy", "节点上仍有配置发布持有锁，暂不能核对；等待该操作结束后自动复查"},
		{"invalid_pending", "节点的未完成发布记录损坏，无法确认执行边界；需检查发布记录，当前配置和凭据保持不变"},
		{"same_process_applying", "当前 Zero 进程仍有未确认的配置应用，尚无完成回执；保留当前配置并继续只读核对"},
		{"persisted_config_changed", "节点配置文件与已确认回执的摘要不一致，不能据此结束旧发布；需核查文件变更"},
		{"restart_without_evidence", "节点已重启，配置回执属于旧进程，且没有匹配的已完成发布证据；暂不能结束旧发布"},
		{"missing_receipt", "节点缺少配置完成回执，无法确认旧发布是否执行完成；当前配置和凭据保持不变"},
		{"invalid_receipt", "节点的配置完成回执字段损坏或不完整，无法核对旧发布；需检查回执文件"},
		{"missing_config_file", "节点当前配置文件不存在，无法核对持久化配置；当前运行进程和凭据保持不变"},
		{"receipt_not_matching", "节点回执与待核对发布不匹配，也未匹配后续成功发布；暂不能确认旧发布结果"},
		{"runtime_changed", "核对期间节点进程、配置版本或配置文件发生变化，将重新读取后再核对"},
	}
	for _, reason := range reasons {
		if strings.Contains(output, "ZBOARD_CONFIG_RECONCILE_BLOCKED="+reason.marker+"\n") || strings.HasSuffix(strings.TrimSpace(output), "ZBOARD_CONFIG_RECONCILE_BLOCKED="+reason.marker) {
			return fmt.Errorf("%s：%w", reason.message, cause)
		}
	}
	if strings.Contains(output, "ZBOARD_CONFIG_CONTROL_UNAVAILABLE") {
		return fmt.Errorf("读取 Zero 控制接口失败或超过 8 秒，无法核对实际运行状态：%w", cause)
	}
	if strings.Contains(output, "ZBOARD_CONFIG_HOT_APPLY_UNAVAILABLE") {
		return fmt.Errorf("Zero 控制接口未返回有效的进程标识和配置版本，无法核对发布结果：%w", cause)
	}
	return fmt.Errorf("读取节点配置核对证据失败：%w", cause)
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
blocked() {
  printf 'ZBOARD_CONFIG_OUTCOME_UNKNOWN=1\nZBOARD_CONFIG_RECONCILE_BLOCKED=%s\n' "$1"
  exit 1
}
check_pending() {
  retry_safe=0
  if [ -f "$pending" ]; then
    read_pending || blocked invalid_pending
    if [ "$pending_core" = "$core" ] && [ "$pending_phase" != prepared ]; then
      if ! receipt_matches "$pending_sha" || [ "$recorded_revision" -le "$pending_revision" ]; then
        blocked same_process_applying
      fi
    fi
    retry_safe=1
  fi
}
valid_receipt() {
  desired= applied= instance= recorded_revision=
  [ -f "$receipt" ] || return 1
  read -r desired applied instance recorded_revision < "$receipt" || return 1
  [ "${#desired}" -eq 64 ] && [ "${#applied}" -eq 64 ] && [ -n "$instance" ] || return 1
  case "$desired$applied" in *[!a-f0-9]*) return 1;; esac
  case "$recorded_revision" in ''|*[!0-9]*) return 1;; esac
}
`

func buildConfigurationReconciliationScript(desiredSHA string, laterCompleted ...string) string {
	laterSHA := ""
	if len(laterCompleted) > 0 {
		laterSHA = laterCompleted[0]
	}
	return fmt.Sprintf(`set -eu
expected_sha=%s
later_sha=%s
stage=$(mktemp -d /tmp/zboard-zero-reconcile-XXXXXX)
trap 'rm -rf "$stage"' EXIT
exec 9>/var/lib/zerodenet/config-publication.lock
%s
flock -w 2 9 || blocked publication_lock_busy
runtime_identity
[ -f /etc/zerodenet/current.json ] || blocked missing_config_file
file_sha=$(sha256sum /etc/zerodenet/current.json | awk '{print $1}')
before_core=$core
before_revision=$revision
check_pending
outcome=retry
if receipt_matches "$expected_sha"; then
  outcome=applied
elif [ "$retry_safe" = 1 ]; then
  : # Prepared, acknowledged or terminated-core transaction; no second apply.
else
  [ -f "$receipt" ] || blocked missing_receipt
  valid_receipt || blocked invalid_receipt
  [ "$applied" = "$file_sha" ] || blocked persisted_config_changed
  if [ -n "$later_sha" ] && [ "$desired" = "$later_sha" ]; then
    if [ "$instance" = "$core" ] && [ "$recorded_revision" != "$revision" ]; then
      blocked receipt_not_matching
    fi
    printf 'ZBOARD_CONFIG_RECONCILIATION=superseded_by_confirmed_publication\n'
  elif [ "$desired" = "$expected_sha" ] && [ "$instance" != "$core" ]; then
    printf 'ZBOARD_CONFIG_RECONCILIATION=completed_before_restart\n'
  elif [ "$instance" != "$core" ]; then
    blocked restart_without_evidence
  else
    blocked receipt_not_matching
  fi
fi
# A service restart or an external native apply during the read invalidates
# these observations. Never carry a receipt across that boundary.
runtime_identity
[ "$core" = "$before_core" ] && [ "$revision" = "$before_revision" ] || blocked runtime_changed
[ "$(sha256sum /etc/zerodenet/current.json | awk '{print $1}')" = "$file_sha" ] || blocked runtime_changed
if [ "$outcome" = applied ]; then
  printf 'ZBOARD_CONFIG_APPLIED=%%s\n' "$expected_sha"
else
  printf 'ZBOARD_CONFIG_RETRY_SAFE=1\n'
fi
`, shellQuote(desiredSHA), shellQuote(laterSHA), configurationReceiptShell)
}
