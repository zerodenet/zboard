package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

// reconcileNodeKernelForTask preserves the normal Zero activation path for
// configured nodes, while allowing a newly onboarded VPS to install the Zero
// binary before it has any active protocol listener. Zero intentionally exits
// when a runtime has no inbound listeners, so an empty runtime must be staged
// without treating runtime/control/Connector readiness as an installation
// requirement.
func (h *handlers) reconcileNodeKernelForTask(ctx context.Context, node model.Node, operation *model.NodeOperation, request kernelReconcileRequest) (map[string]interface{}, error) {
	var activeEndpoints int64
	if err := h.db.Model(&model.ProtocolEndpoint{}).
		Where("node_id = ? AND is_active = ?", node.ID, true).
		Count(&activeEndpoints).Error; err != nil {
		return nil, fmt.Errorf("count active protocol endpoints before Zero reconcile: %w", err)
	}
	if activeEndpoints > 0 {
		return h.reconcileNodeKernel(ctx, node, operation, request)
	}
	return h.reconcileNodeKernelDeferred(ctx, node, operation, request)
}

func (h *handlers) reconcileNodeKernelDeferred(ctx context.Context, node model.Node, operation *model.NodeOperation, request kernelReconcileRequest) (map[string]interface{}, error) {
	if err := h.setKernelOperationPhase(operation, "detecting"); err != nil {
		return nil, err
	}
	probe, err := h.probeNodeKernel(node)
	if err != nil {
		return nil, err
	}
	if err := h.updateKernelState(node.ID, map[string]interface{}{
		"platform_os": probe.OperatingSystem, "architecture": probe.Architecture, "libc": probe.Libc,
		"installed_version": probe.Version, "installed_sha256": probe.BinarySHA256,
		"applied_config_sha256": probe.ConfigSHA256, "service_status": probe.ServiceStatus,
		"control_status": probe.ControlStatus, "last_detected_at": time.Now().UTC(),
	}); err != nil {
		return nil, err
	}
	if probe.Architecture != "x86_64" || !probe.Systemd {
		return nil, fmt.Errorf("%w: automatic installation requires Linux x86_64 with systemd (os=%s arch=%s systemd=%t)", errKernelPlatformUnsupported, probe.OperatingSystem, probe.Architecture, probe.Systemd)
	}

	if err := h.setKernelOperationPhase(operation, "resolving_release"); err != nil {
		return nil, err
	}
	release, err := h.resolveZeroRelease(ctx, probe, request.Version)
	if err != nil {
		return nil, fmt.Errorf("resolve Zero release: %w", err)
	}
	operation.DesiredVersion = release.Version
	operation.DesiredSHA256 = release.ArtifactSHA256
	operation.ArtifactURL = release.ArtifactURL
	if err := h.db.Model(operation).Updates(map[string]interface{}{
		"desired_version": release.Version,
		"desired_sha256":  release.ArtifactSHA256,
		"artifact_url":    release.ArtifactURL,
	}).Error; err != nil {
		return nil, err
	}

	if err := h.setKernelOperationPhase(operation, "preparing_connector_credential"); err != nil {
		return nil, err
	}
	credential, err := h.nodeConnectorCredential(node)
	if err != nil {
		return nil, err
	}
	runtimeConfig, configSHA, err := h.compileNodeRuntimeConfig(node, credential.Raw, release.Version)
	if err != nil {
		return nil, err
	}
	inboundCount, err := zeroRuntimeInboundCount(runtimeConfig)
	if err != nil {
		return nil, err
	}
	if inboundCount != 0 {
		// The endpoint set changed after the task was accepted. Fall back to the
		// normal activation path rather than staging a runnable configuration as
		// inactive.
		return h.reconcileNodeKernel(ctx, node, operation, request)
	}

	if compareZeroVersions(probe.Version, release.Version) > 0 && !request.AllowDowngrade {
		_ = h.updateKernelState(node.ID, map[string]interface{}{
			"status":                h.kernelStatus(probe),
			"phase":                 "idle",
			"recommended_action":    "manual_review",
			"desired_version":       release.Version,
			"desired_config_sha256": configSHA,
		})
		return nil, fmt.Errorf("installed Zero %s is newer than selected release %s; set allow_downgrade only after explicit operator confirmation", probe.Version, release.Version)
	}

	if err := h.setKernelOperationPhase(operation, "downloading"); err != nil {
		return nil, err
	}
	binary, binarySHA, err := downloadZeroBinary(ctx, release)
	if err != nil {
		return nil, err
	}
	action := classifyKernelAction(probe, release.Version, binarySHA, configSHA)
	if action == "manual_review" && request.AllowDowngrade {
		action = "downgrade"
	}
	if action == "none" {
		// A runtime-free generation still needs the deferred systemd policy. A
		// previous active/healthy state must not cause us to keep a listener-less
		// runtime running just because binary/config hashes already match.
		action = "configure"
	}
	operation.OperationType = action
	if err := h.db.Model(operation).Update("operation_type", action).Error; err != nil {
		return nil, err
	}

	if err := h.setKernelOperationPhase(operation, "staging"); err != nil {
		return nil, err
	}
	credentialActivated := false
	if credential.IsNew {
		if err := h.db.Model(&model.Node{}).Where("id = ?", node.ID).Updates(map[string]interface{}{
			"node_credential":            credential.Encrypted,
			"node_credential_prefix":     credential.Prefix,
			"node_credential_revoked_at": nil,
		}).Error; err != nil {
			return nil, fmt.Errorf("prepare generated connector credential before Zero staging: %w", err)
		}
		credentialActivated = true
	}
	restoreCredential := func() error {
		if !credentialActivated {
			return nil
		}
		return h.restoreGeneratedNodeCredential(node, credential)
	}
	if err := h.installNodeKernelDeferred(node, operation.ID, binary, binarySHA, runtimeConfig, credential.Raw); err != nil {
		if credentialErr := restoreCredential(); credentialErr != nil {
			return nil, fmt.Errorf("%w; generated connector credential rollback failed: %v", err, credentialErr)
		}
		return nil, err
	}
	rollbackAfterStage := func(cause error) error {
		rollbackErr := h.rollbackNodeKernel(node, operation.ID)
		credentialErr := restoreCredential()
		if rollbackErr != nil || credentialErr != nil {
			return fmt.Errorf("%w; automatic rollback incomplete (kernel=%v credential=%v)", cause, rollbackErr, credentialErr)
		}
		return fmt.Errorf("%w; the staged generation was rolled back", cause)
	}

	if err := h.setKernelOperationPhase(operation, "verifying"); err != nil {
		return nil, rollbackAfterStage(err)
	}
	verified, err := h.probeNodeKernel(node)
	if err != nil {
		return nil, rollbackAfterStage(fmt.Errorf("post-install probe failed: %w", err))
	}
	if !verified.Installed || verified.BinarySHA256 != binarySHA || verified.ConfigSHA256 != configSHA {
		return nil, rollbackAfterStage(fmt.Errorf(
			"post-install deferred verification failed (installed=%t sha_match=%t config_match=%t service=%s control=%s)",
			verified.Installed,
			verified.BinarySHA256 == binarySHA,
			verified.ConfigSHA256 == configSHA,
			verified.ServiceStatus,
			verified.ControlStatus,
		))
	}
	if verified.ServiceStatus == "active" || verified.ControlStatus == "healthy" {
		return nil, rollbackAfterStage(fmt.Errorf(
			"Zero unexpectedly became active without inbound listeners (service=%s control=%s)",
			verified.ServiceStatus,
			verified.ControlStatus,
		))
	}

	summary := fmt.Sprintf("Zero %s %s; binary/config staged, runtime activation and Connector verification deferred until an active inbound is published", release.Version, action)
	state, err := h.finishDeferredKernelOperation(operation, verified, release, binarySHA, configSHA, summary)
	if err != nil {
		return nil, rollbackAfterStage(fmt.Errorf("persist deferred Zero operation: %w", err))
	}
	return map[string]interface{}{
		"state":               state,
		"operation":           operation,
		"changed":             true,
		"action":              action,
		"deferred_activation": true,
	}, nil
}

func zeroRuntimeInboundCount(payload []byte) (int, error) {
	var config struct {
		Inbounds []json.RawMessage `json:"inbounds"`
	}
	if err := json.Unmarshal(payload, &config); err != nil {
		return 0, fmt.Errorf("inspect compiled Zero runtime configuration: %w", err)
	}
	return len(config.Inbounds), nil
}

func (h *handlers) finishDeferredKernelOperation(operation *model.NodeOperation, probe kernelProbe, release zeroRelease, binarySHA, configSHA, summary string) (model.NodeKernelState, error) {
	now := time.Now().UTC()
	updates := map[string]interface{}{
		"status": "degraded", "phase": "idle", "recommended_action": "configure",
		"platform_os": probe.OperatingSystem, "architecture": probe.Architecture, "libc": probe.Libc,
		"desired_version": release.Version, "installed_version": probe.Version,
		"desired_sha256": binarySHA, "installed_sha256": probe.BinarySHA256,
		"desired_config_sha256": configSHA, "applied_config_sha256": probe.ConfigSHA256,
		"service_status": probe.ServiceStatus, "control_status": probe.ControlStatus,
		"last_detected_at": now, "last_error": "", "active_operation_id": nil,
	}
	err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.NodeKernelState{}).Where("node_id = ?", operation.NodeID).Updates(updates).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.Node{}).Where("id = ?", operation.NodeID).Updates(map[string]interface{}{
			"version":         probe.Version,
			"ssh_verified_at": now,
		}).Error; err != nil {
			return err
		}
		operation.Status, operation.Phase, operation.ResultSummary, operation.FinishedAt = "succeeded", "completed", summary, &now
		return tx.Save(operation).Error
	})
	if err != nil {
		return model.NodeKernelState{}, err
	}
	return h.ensureKernelState(operation.NodeID)
}

func (h *handlers) installNodeKernelDeferred(node model.Node, operationID uint, binary []byte, binarySHA string, runtimeConfig []byte, apiKey string) error {
	if !sha256Pattern.MatchString(binarySHA) || strings.TrimSpace(apiKey) == "" {
		return fmt.Errorf("invalid staged Zero binary or connector credential")
	}
	stage := "/tmp/zboard-zero-" + uuid.NewString()
	conn, _, err := h.dialNodeSSH(node)
	if err != nil {
		return err
	}
	defer conn.Close()
	timeout := time.AfterFunc(4*time.Minute, func() { _ = conn.Close() })
	defer timeout.Stop()
	if output, err := h.runNodeSSHSession(conn, node, "install -d -m 0700 "+shellQuote(stage), false); err != nil {
		return fmt.Errorf("create Zero staging directory: %w: %s", err, output)
	}
	files := []struct {
		path string
		mode string
		data []byte
	}{
		{stage + "/zero", "0700", binary},
		{stage + "/runtime.json", "0600", runtimeConfig},
		{stage + "/zero.env", "0600", []byte("ZERO_PANEL_API_KEY=" + apiKey + "\n")},
		{stage + "/zero.service", "0644", []byte(zeroDeferredSystemdUnit)},
	}
	for _, file := range files {
		if err := uploadSSHFile(conn, file.path, file.mode, file.data); err != nil {
			return fmt.Errorf("stage %s: %w", file.path, err)
		}
	}
	output, err := h.runNodeSSHSession(conn, node, buildZeroDeferredInstallScript(stage, binarySHA, operationID), true)
	if err != nil {
		return fmt.Errorf("stage Zero without runtime activation (automatic rollback attempted): %w: %s", err, truncateKernelError(output))
	}
	return nil
}

// The deferred unit is enabled immediately so normal boot semantics are in
// place, but ExecCondition prevents an installation generation from starting.
// The first protocol publish switches current.json to a config-* generation;
// the existing config publisher's systemctl restart then activates Zero and
// the condition remains true on subsequent boots.
const zeroDeferredSystemdUnit = `[Unit]
Description=Zero network kernel
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=root
EnvironmentFile=/etc/zerodenet/zero.env
RuntimeDirectory=zerodenet
RuntimeDirectoryMode=0750
ExecCondition=/bin/sh -c 'target="$(readlink /etc/zerodenet/current.json 2>/dev/null || true)"; case "$target" in /etc/zerodenet/generations/config-*.json) exit 0;; *) exit 1;; esac'
ExecStart=/usr/local/bin/zero run --control-socket /run/zerodenet/control.sock /etc/zerodenet/current.json
Restart=on-failure
RestartSec=3s
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
`

func buildZeroDeferredInstallScript(stage, binarySHA string, operationID uint) string {
	generation := fmt.Sprintf("/etc/zerodenet/generations/%d.json", operationID)
	backup := fmt.Sprintf("/var/lib/zerodenet/backups/%d", operationID)
	return fmt.Sprintf(`set -eu
stage=%s
generation=%s
backup=%s
expected_sha=%s
test "$(id -u)" = "0"
test "$(uname -s)" = "Linux"
test "$(uname -m)" = "x86_64"
command -v systemctl >/dev/null
actual_sha="$(sha256sum "$stage/zero" | awk '{print $1}')"
test "$actual_sha" = "$expected_sha"
set -a
. "$stage/zero.env"
set +a
"$stage/zero" build_info >/dev/null
"$stage/zero" validate "$stage/runtime.json" >/dev/null
install -d -m 0755 /usr/local/bin /etc/zerodenet/generations /var/lib/zerodenet/backups
install -d -m 0700 "$backup"
had_bin=0; had_env=0; had_service=0; old_active=0; old_enabled=0
old_link="$(readlink /etc/zerodenet/current.json 2>/dev/null || true)"
if [ -f /usr/local/bin/zero ]; then cp -a /usr/local/bin/zero "$backup/zero"; had_bin=1; fi
if [ -f /etc/zerodenet/zero.env ]; then cp -a /etc/zerodenet/zero.env "$backup/zero.env"; had_env=1; fi
if [ -f /etc/systemd/system/zero.service ]; then cp -a /etc/systemd/system/zero.service "$backup/zero.service"; had_service=1; fi
if systemctl is-active --quiet zero >/dev/null 2>&1; then old_active=1; fi
if systemctl is-enabled --quiet zero >/dev/null 2>&1; then old_enabled=1; fi
printf '%%s\n' "$had_bin" > "$backup/had_bin"
printf '%%s\n' "$had_env" > "$backup/had_env"
printf '%%s\n' "$had_service" > "$backup/had_service"
printf '%%s\n' "$old_active" > "$backup/old_active"
printf '%%s\n' "$old_enabled" > "$backup/old_enabled"
printf '%%s\n' "$old_link" > "$backup/old_link"
rollback() {
  if [ "$had_bin" = "1" ]; then
    install -m 0755 "$backup/zero" /usr/local/bin/zero.rollback
    mv -f /usr/local/bin/zero.rollback /usr/local/bin/zero
  else
    rm -f /usr/local/bin/zero
  fi
  if [ -n "$old_link" ]; then ln -sfn "$old_link" /etc/zerodenet/current.json; else rm -f /etc/zerodenet/current.json; fi
  if [ "$had_env" = "1" ]; then cp -a "$backup/zero.env" /etc/zerodenet/zero.env; else rm -f /etc/zerodenet/zero.env; fi
  if [ "$had_service" = "1" ]; then cp -a "$backup/zero.service" /etc/systemd/system/zero.service; else rm -f /etc/systemd/system/zero.service; fi
  systemctl daemon-reload >/dev/null 2>&1 || true
  if [ "$old_enabled" = "1" ]; then systemctl enable zero >/dev/null 2>&1 || true; else systemctl disable zero >/dev/null 2>&1 || true; fi
  if [ "$old_active" = "1" ]; then systemctl restart zero >/dev/null 2>&1 || true; else systemctl stop zero >/dev/null 2>&1 || true; fi
}
trap 'rc=$?; if [ "$rc" != "0" ]; then rollback; fi; exit "$rc"' EXIT
install -m 0755 "$stage/zero" /usr/local/bin/zero.next
mv -f /usr/local/bin/zero.next /usr/local/bin/zero
install -m 0600 "$stage/runtime.json" "$generation"
install -m 0600 "$stage/zero.env" /etc/zerodenet/zero.env
install -m 0644 "$stage/zero.service" /etc/systemd/system/zero.service
ln -sfn "$generation" /etc/zerodenet/current.json.next
mv -Tf /etc/zerodenet/current.json.next /etc/zerodenet/current.json
systemctl daemon-reload
systemctl enable zero >/dev/null
systemctl stop zero >/dev/null 2>&1 || true
systemctl reset-failed zero >/dev/null 2>&1 || true
trap - EXIT
rm -rf "$stage"
printf 'ZBOARD_KERNEL_STAGED=1\n'
`, shellQuote(stage), shellQuote(generation), shellQuote(backup), shellQuote(binarySHA))
}
