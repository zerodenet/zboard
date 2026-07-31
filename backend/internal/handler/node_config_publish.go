package handler

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

const nodeConfigPublishTimeout = 2 * time.Minute

func (h *handlers) nodePublishLock(nodeID uint) *sync.Mutex {
	value, _ := h.nodePublishLocks.LoadOrStore(nodeID, &sync.Mutex{})
	return value.(*sync.Mutex)
}

func (h *handlers) scheduleNodeConfigPublish(nodeID, endpointID, requestedBy uint) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), nodeConfigPublishTimeout)
		defer cancel()
		_, _, _ = h.publishNodeConfigForNode(ctx, nodeID, endpointID, requestedBy)
	}()
}

func (h *handlers) scheduleSubscriptionConfigPublishes(subscriptionID, requestedBy uint) {
	var endpoints []model.ProtocolEndpoint
	if err := h.db.Model(&model.ProtocolEndpoint{}).
		Joins("JOIN node_group_endpoints ON node_group_endpoints.protocol_endpoint_id = protocol_endpoints.id").
		Joins("JOIN subscriptions ON subscriptions.node_group_id = node_group_endpoints.node_group_id").
		Where("subscriptions.id = ? AND protocol_endpoints.is_active = ?", subscriptionID, true).
		Order("protocol_endpoints.id asc").Find(&endpoints).Error; err != nil {
		return
	}
	seen := map[uint]struct{}{}
	for _, endpoint := range endpoints {
		if _, exists := seen[endpoint.NodeID]; exists {
			continue
		}
		seen[endpoint.NodeID] = struct{}{}
		h.scheduleNodeConfigPublish(endpoint.NodeID, endpoint.ID, requestedBy)
	}
}

// Any authenticated Connector delivery is an opportunity to reconcile
// time-based credential expiry without adding a second panel-specific channel.
func (h *handlers) reconcileExpiredCredentials(now time.Time) {
	h.expiryReconcileMu.Lock()
	if !h.lastExpiryReconcile.IsZero() && now.Sub(h.lastExpiryReconcile) < 20*time.Second {
		h.expiryReconcileMu.Unlock()
		return
	}
	h.lastExpiryReconcile = now
	h.expiryReconcileMu.Unlock()

	started := time.Now().UTC()
	if err := expireSubscriptions(h.db, 0, started); err != nil {
		return
	}
	var expired []model.ProtocolCredential
	if err := h.db.Where("status = ? AND updated_at >= ?", "expired", started).Find(&expired).Error; err != nil {
		return
	}
	seen := map[uint]struct{}{}
	for _, credential := range expired {
		if _, exists := seen[credential.NodeID]; exists {
			continue
		}
		seen[credential.NodeID] = struct{}{}
		h.scheduleNodeConfigPublish(credential.NodeID, credential.ProtocolEndpointID, 0)
	}
}

func (h *handlers) publishNodeConfig(ctx context.Context, endpointID, requestedBy uint) (model.ProtocolDeployment, time.Duration, error) {
	var endpoint model.ProtocolEndpoint
	if err := h.db.First(&endpoint, endpointID).Error; err != nil {
		return model.ProtocolDeployment{}, 0, err
	}
	return h.publishNodeConfigForNode(ctx, endpoint.NodeID, endpoint.ID, requestedBy)
}

func (h *handlers) publishNodeConfigForNode(ctx context.Context, nodeID, triggerEndpointID, requestedBy uint) (model.ProtocolDeployment, time.Duration, error) {
	started := time.Now()
	lock := h.nodePublishLock(nodeID)
	lock.Lock()
	defer lock.Unlock()

	return h.publishNodeConfigForNodeLocked(ctx, nodeID, triggerEndpointID, requestedBy, false, started)
}

func (h *handlers) publishNodeConfigForNodeLocked(ctx context.Context, nodeID, triggerEndpointID, requestedBy uint, suppressMieruFallback bool, started time.Time) (model.ProtocolDeployment, time.Duration, error) {
	node, err := h.loadNode(nodeID)
	if err != nil {
		return model.ProtocolDeployment{}, time.Since(started), err
	}
	var mieruFallbackCount int64
	if h.zeroMieruAccess {
		if err := h.db.Model(&model.ProtocolEndpoint{}).
			Where("node_id = ? AND LOWER(protocol) = ? AND mieru_principal_ready = ?", node.ID, "mieru", false).
			Count(&mieruFallbackCount).Error; err != nil {
			return model.ProtocolDeployment{}, time.Since(started), err
		}
	}
	startedAt := time.Now().UTC()
	deployment := model.ProtocolDeployment{
		ProtocolEndpointID: triggerEndpointID,
		NodeID:             node.ID,
		ConfigRevision:     uint64(startedAt.UnixNano()),
		Status:             "running",
		RequestedBy:        requestedBy,
		StartedAt:          &startedAt,
	}
	if err := h.db.Create(&deployment).Error; err != nil {
		return deployment, time.Since(started), err
	}
	fail := func(cause error, output string) (model.ProtocolDeployment, time.Duration, error) {
		finished := time.Now().UTC()
		message := cause.Error()
		_ = h.db.Model(&deployment).Updates(map[string]interface{}{
			"status": "failed", "output": strings.TrimSpace(output), "error": message, "finished_at": finished,
		}).Error
		_, _ = h.ensureKernelState(node.ID)
		_ = h.updateKernelState(node.ID, map[string]interface{}{
			"status": "apply_failed", "phase": "idle", "last_error": message,
		})
		deployment.Status = "failed"
		deployment.Error = message
		deployment.Output = strings.TrimSpace(output)
		deployment.FinishedAt = &finished
		return deployment, time.Since(started), cause
	}
	if err := h.validateNodeSSH(node); err != nil {
		return fail(err, "")
	}
	probe, err := h.probeNodeKernel(node)
	if err != nil {
		return fail(fmt.Errorf("detect installed Zero before publishing config: %w", err), "")
	}
	if !probe.Installed || strings.TrimSpace(probe.Version) == "" {
		return fail(fmt.Errorf("detect installed Zero before publishing config: Zero is not installed"), "")
	}
	credential, err := h.nodeConnectorCredential(node)
	if err != nil {
		return fail(err, "")
	}
	if credential.IsNew {
		if err := h.db.Model(&node).Updates(map[string]interface{}{
			"node_credential":            credential.Encrypted,
			"node_credential_prefix":     credential.Prefix,
			"node_credential_revoked_at": nil,
		}).Error; err != nil {
			return fail(err, "")
		}
	}
	runtimeConfig, configSHA, err := h.compileNodeRuntimeConfigWithOptions(node, credential.Raw, probe.Version, suppressMieruFallback)
	if err != nil {
		if credential.IsNew {
			_ = h.restoreGeneratedNodeCredential(node, credential)
		}
		return fail(err, "")
	}
	deployment.DesiredConfigSHA256 = configSHA
	if err := h.db.Model(&deployment).Update("desired_config_sha256", configSHA).Error; err != nil {
		return fail(err, "")
	}
	_, _ = h.ensureKernelState(node.ID)
	_ = h.updateKernelState(node.ID, map[string]interface{}{
		"status": "publishing", "phase": "applying_config", "desired_config_sha256": configSHA, "last_error": "",
	})

	stage := "/tmp/zboard-zero-config-" + uuid.NewString()
	conn, _, err := h.dialNodeSSH(node)
	if err != nil {
		return fail(err, "")
	}
	defer conn.Close()
	if output, err := h.runNodeSSHSession(conn, node, "install -d -m 0700 "+shellQuote(stage), false); err != nil {
		return fail(fmt.Errorf("create Zero config staging directory: %w", err), output)
	}
	if err := uploadSSHFile(conn, stage+"/runtime.json", "0600", runtimeConfig); err != nil {
		return fail(err, "")
	}
	if err := uploadSSHFile(conn, stage+"/zero.env", "0600", []byte("ZERO_PANEL_API_KEY="+credential.Raw+"\n")); err != nil {
		return fail(err, "")
	}
	output, err := h.runNodeSSHSession(conn, node, buildZeroConfigPublishScript(stage, configSHA, deployment.ID), true)
	if err != nil {
		if credential.IsNew {
			_ = h.restoreGeneratedNodeCredential(node, credential)
		}
		return fail(fmt.Errorf("activate Zero config (rollback attempted): %w", err), output)
	}
	activatedAt := time.Now().UTC()
	connectorEventAt, err := h.waitForNodeConnectorEvent(ctx, node.ID, activatedAt)
	if err != nil {
		rollbackOutput, rollbackErr := h.runNodeSSHSession(conn, node, buildZeroConfigRollbackScript(deployment.ID), true)
		if rollbackErr != nil {
			err = fmt.Errorf("%w; config rollback failed: %v", err, rollbackErr)
		}
		if strings.TrimSpace(rollbackOutput) != "" {
			output = strings.TrimSpace(output) + "\n" + strings.TrimSpace(rollbackOutput)
		}
		if credential.IsNew {
			_ = h.restoreGeneratedNodeCredential(node, credential)
		}
		return fail(err, output)
	}
	finished := time.Now().UTC()
	if err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&deployment).Updates(map[string]interface{}{
			"status": "succeeded", "applied_config_sha256": configSHA, "output": strings.TrimSpace(output), "error": "", "finished_at": finished,
		}).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.Node{}).Where("id = ?", node.ID).Updates(map[string]interface{}{"last_sync_at": finished, "ssh_verified_at": finished}).Error; err != nil {
			return err
		}
		if mieruReadinessCanCommit(h.zeroMieruAccess, mieruFallbackCount, suppressMieruFallback) {
			if err := tx.Model(&model.ProtocolEndpoint{}).
				Where("node_id = ? AND LOWER(protocol) = ?", node.ID, "mieru").
				Update("mieru_principal_ready", h.zeroMieruAccess).Error; err != nil {
				return err
			}
			mieruCredentialStatus := protocolCredentialStatusPrepared
			if h.zeroMieruAccess {
				mieruCredentialStatus = protocolCredentialStatusActive
			}
			mieruEndpointIDs := tx.Model(&model.ProtocolEndpoint{}).
				Select("id").
				Where("node_id = ? AND LOWER(protocol) = ?", node.ID, "mieru")
			if err := tx.Model(&model.ProtocolCredential{}).
				Where("protocol_endpoint_id IN (?) AND status IN ?", mieruEndpointIDs,
					[]string{protocolCredentialStatusActive, protocolCredentialStatusPrepared}).
				Update("status", mieruCredentialStatus).Error; err != nil {
				return err
			}
		}
		return tx.Model(&model.NodeKernelState{}).Where("node_id = ?", node.ID).Updates(map[string]interface{}{
			"status": "healthy", "phase": "idle", "desired_config_sha256": configSHA,
			"applied_config_sha256": configSHA, "service_status": "active", "control_status": "healthy",
			"last_error": "", "last_healthy_at": connectorEventAt,
		}).Error
	}); err != nil {
		return fail(err, output)
	}
	deployment.Status = "succeeded"
	deployment.AppliedConfigSHA256 = configSHA
	deployment.Output = strings.TrimSpace(output)
	deployment.FinishedAt = &finished
	if h.zeroMieruAccess && mieruFallbackCount > 0 && !suppressMieruFallback {
		return h.publishNodeConfigForNodeLocked(ctx, node.ID, triggerEndpointID, requestedBy, true, started)
	}
	return deployment, time.Since(started), nil
}

func mieruReadinessCanCommit(enabled bool, fallbackCount int64, suppressFallback bool) bool {
	return !enabled || fallbackCount == 0 || suppressFallback
}

func buildZeroConfigPublishScript(stage, expectedSHA string, deploymentID uint) string {
	generation := fmt.Sprintf("/etc/zerodenet/generations/config-%d.json", deploymentID)
	backup := fmt.Sprintf("/var/lib/zerodenet/backups/config-%d", deploymentID)
	return fmt.Sprintf(`set -eu
stage=%s
generation=%s
backup=%s
expected_sha=%s
test "$(id -u)" = "0"
test -x /usr/local/bin/zero
command -v systemctl >/dev/null
set -a
. "$stage/zero.env"
set +a
/usr/local/bin/zero validate "$stage/runtime.json" >/dev/null
actual_sha="$(sha256sum "$stage/runtime.json" | awk '{print $1}')"
test "$actual_sha" = "$expected_sha"
install -d -m 0755 /etc/zerodenet/generations /var/lib/zerodenet/backups
install -d -m 0700 "$backup"
old_link="$(readlink /etc/zerodenet/current.json 2>/dev/null || true)"
had_env=0
if [ -f /etc/zerodenet/zero.env ]; then cp -a /etc/zerodenet/zero.env "$backup/zero.env"; had_env=1; fi
printf '%%s\n' "$old_link" > "$backup/old_link"
printf '%%s\n' "$had_env" > "$backup/had_env"
rollback() {
  if [ -n "$old_link" ]; then ln -sfn "$old_link" /etc/zerodenet/current.json; else rm -f /etc/zerodenet/current.json; fi
  if [ "$had_env" = "1" ]; then cp -a "$backup/zero.env" /etc/zerodenet/zero.env; else rm -f /etc/zerodenet/zero.env; fi
  systemctl restart zero >/dev/null 2>&1 || true
}
trap 'rc=$?; if [ "$rc" != "0" ]; then rollback; fi; exit "$rc"' EXIT
install -m 0600 "$stage/runtime.json" "$generation"
install -m 0600 "$stage/zero.env" /etc/zerodenet/zero.env
ln -sfn "$generation" /etc/zerodenet/current.json.next
mv -Tf /etc/zerodenet/current.json.next /etc/zerodenet/current.json
systemctl restart zero
healthy=0
for attempt in 1 2 3 4 5 6 7 8 9 10; do
  if systemctl is-active --quiet zero && /usr/local/bin/zero status --json --socket %s >/dev/null 2>&1; then healthy=1; break; fi
  sleep 1
done
test "$healthy" = "1"
test "$(sha256sum /etc/zerodenet/current.json | awk '{print $1}')" = "$expected_sha"
trap - EXIT
rm -rf "$stage"
printf 'ZBOARD_CONFIG_APPLIED=%%s\n' "$expected_sha"
`, shellQuote(stage), shellQuote(generation), shellQuote(backup), shellQuote(expectedSHA), shellQuote(zeroControlSocket))
}

func buildZeroConfigRollbackScript(deploymentID uint) string {
	generation := fmt.Sprintf("/etc/zerodenet/generations/config-%d.json", deploymentID)
	backup := fmt.Sprintf("/var/lib/zerodenet/backups/config-%d", deploymentID)
	return fmt.Sprintf(`set -eu
generation=%s
backup=%s
test "$(id -u)" = "0"
test -d "$backup"
old_link="$(cat "$backup/old_link" 2>/dev/null || true)"
had_env="$(cat "$backup/had_env" 2>/dev/null || printf 0)"
if [ -n "$old_link" ]; then ln -sfn "$old_link" /etc/zerodenet/current.json; else rm -f /etc/zerodenet/current.json; fi
if [ "$had_env" = "1" ]; then cp -a "$backup/zero.env" /etc/zerodenet/zero.env; else rm -f /etc/zerodenet/zero.env; fi
rm -f "$generation"
systemctl restart zero
printf 'ZBOARD_CONFIG_ROLLED_BACK=1\n'
`, shellQuote(generation), shellQuote(backup))
}
