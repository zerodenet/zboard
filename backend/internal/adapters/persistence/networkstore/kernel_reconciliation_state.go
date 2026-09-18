package networkstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type KernelReconciliationState struct{ DB *gorm.DB }

func (s KernelReconciliationState) BeginReconciliation(ctx context.Context, request network.KernelDetectionRequest, now time.Time) (out network.KernelOperation, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var node model.Node
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&node, request.NodeID).Error; err != nil {
			return err
		}
		if node.LifecycleStatus == "deleting" {
			return network.ErrKernelResourceDeleting
		}
		var actor model.User
		err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND is_admin = ? AND status = ?", request.ActorID, true, "active").First(&actor).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return network.ErrKernelPermission
		}
		if err != nil {
			return err
		}
		state, err := lockOrCreateKernelState(tx, request.NodeID)
		if err != nil {
			return err
		}
		if state.ActiveOperationID != nil {
			var active model.NodeOperation
			if err := tx.First(&active, *state.ActiveOperationID).Error; err == nil && active.Status == "running" {
				return network.ErrKernelOperationRunning
			}
		}
		row := model.NodeOperation{NodeID: request.NodeID, OperationType: "reconcile", Status: "running", Phase: "queued", RequestedBy: actor.ID, StartedAt: &now}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		if err := tx.Model(&state).Updates(map[string]interface{}{"phase": "queued", "active_operation_id": row.ID, "last_error": ""}).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.AuditLog{UserID: &actor.ID, Actor: actor.Email, Action: "node.kernel.reconcile", Target: fmt.Sprintf("node:%d", request.NodeID), Detail: fmt.Sprintf("operation=%d", row.ID)}).Error; err != nil {
			return err
		}
		out = kernelOperationView(row)
		return nil
	})
	return
}

func (s KernelReconciliationState) SetKernelPhase(ctx context.Context, operation network.KernelOperation, phase string) (out network.KernelOperation, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		state, row, err := lockOwnedKernelOperation(tx, operation)
		if err != nil {
			return err
		}
		if err := tx.Model(&row).Update("phase", phase).Error; err != nil {
			return err
		}
		if err := tx.Model(&state).Update("phase", phase).Error; err != nil {
			return err
		}
		row.Phase = phase
		out = kernelOperationView(row)
		return nil
	})
	return
}

func (s KernelReconciliationState) RecordKernelProbe(ctx context.Context, operation network.KernelOperation, probe network.KernelProbe, now time.Time) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		state, _, err := lockOwnedKernelOperation(tx, operation)
		if err != nil {
			return err
		}
		return tx.Model(&state).Updates(map[string]interface{}{
			"platform_os": probe.OperatingSystem, "architecture": probe.Architecture, "libc": probe.Libc,
			"installed_version": probe.Version, "installed_sha256": probe.BinarySHA256,
			"applied_config_sha256": probe.ConfigSHA256, "service_status": probe.ServiceStatus,
			"control_status": probe.ControlStatus, "last_detected_at": now,
		}).Error
	})
}

func (s KernelReconciliationState) RecordKernelRelease(ctx context.Context, operation network.KernelOperation, release network.KernelRelease) (out network.KernelOperation, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		_, row, err := lockOwnedKernelOperation(tx, operation)
		if err != nil {
			return err
		}
		if err := tx.Model(&row).Updates(map[string]interface{}{"desired_version": release.Version, "desired_sha256": release.ArtifactSHA256, "artifact_url": release.ArtifactURL}).Error; err != nil {
			return err
		}
		row.DesiredVersion, row.DesiredSHA256, row.ArtifactURL = release.Version, release.ArtifactSHA256, release.ArtifactURL
		out = kernelOperationView(row)
		return nil
	})
	return
}

func (s KernelReconciliationState) RecordKernelAction(ctx context.Context, operation network.KernelOperation, action string) (out network.KernelOperation, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		_, row, err := lockOwnedKernelOperation(tx, operation)
		if err != nil {
			return err
		}
		if err := tx.Model(&row).Update("operation_type", action).Error; err != nil {
			return err
		}
		row.OperationType = action
		out = kernelOperationView(row)
		return nil
	})
	return
}

func (s KernelReconciliationState) EnsureKernelTrafficCredential(ctx context.Context, operation network.KernelOperation, credential network.KernelEncryptedCredential) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, _, err := lockOwnedKernelOperation(tx, operation); err != nil {
			return err
		}
		return tx.Model(&model.Node{}).
			Where("id = ? AND traffic_secret = '' AND traffic_secret_revoked_at IS NULL", operation.NodeID).
			Updates(map[string]interface{}{
				"traffic_secret":            credential.Ciphertext,
				"traffic_secret_prefix":     credential.Prefix,
				"traffic_secret_revoked_at": nil,
			}).Error
	})
}

func (s KernelReconciliationState) ActivateKernelConnectorCredential(ctx context.Context, operation network.KernelOperation, credential network.KernelEncryptedCredential) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, _, err := lockOwnedKernelOperation(tx, operation); err != nil {
			return err
		}
		return tx.Model(&model.Node{}).Where("id = ?", operation.NodeID).Updates(map[string]interface{}{
			"node_credential":            credential.Ciphertext,
			"node_credential_prefix":     credential.Prefix,
			"node_credential_revoked_at": nil,
		}).Error
	})
}

func (s KernelReconciliationState) RestoreKernelConnectorCredential(ctx context.Context, operation network.KernelOperation, snapshot network.KernelConnectorSnapshot) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, _, err := lockOwnedKernelOperation(tx, operation); err != nil {
			return err
		}
		return tx.Model(&model.Node{}).Where("id = ?", operation.NodeID).Updates(map[string]interface{}{
			"node_credential":            snapshot.Credential.Ciphertext,
			"node_credential_prefix":     snapshot.Credential.Prefix,
			"node_credential_revoked_at": snapshot.RevokedAt,
			"connector_last_seen_at":     snapshot.ConnectorLastSeenAt,
			"last_seen_at":               snapshot.LastSeenAt,
			"is_online":                  snapshot.IsOnline,
			"status":                     snapshot.Status,
			"version":                    snapshot.Version,
			"uptime_seconds":             snapshot.UptimeSeconds,
			"active_flows":               snapshot.ActiveFlows,
			"bytes_up":                   snapshot.BytesUp,
			"bytes_down":                 snapshot.BytesDown,
		}).Error
	})
}

func (s KernelReconciliationState) BlockKernelDowngrade(ctx context.Context, operation network.KernelOperation, probe network.KernelProbe, release network.KernelRelease, configSHA, message string, now time.Time) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		state, row, err := lockOwnedKernelOperation(tx, operation)
		if err != nil {
			return err
		}
		assessment := network.AssessKernelProbe(probe, true)
		if err := tx.Model(&state).Updates(map[string]interface{}{
			"status": assessment.Status, "phase": "idle", "recommended_action": "manual_review",
			"desired_version": release.Version, "desired_config_sha256": configSHA,
			"last_error": message, "active_operation_id": nil,
		}).Error; err != nil {
			return err
		}
		return tx.Model(&row).Updates(map[string]interface{}{"status": "failed", "phase": "resolving_release", "error": message, "finished_at": now}).Error
	})
}

func (s KernelReconciliationState) CompleteKernelReconciliation(ctx context.Context, operation network.KernelOperation, probe network.KernelProbe, release network.KernelRelease, binarySHA, configSHA, summary string, changed, connectorVerified bool, connectorWarning string, now time.Time) (out network.KernelReconciliationResult, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		state, row, err := lockOwnedKernelOperation(tx, operation)
		if err != nil {
			return err
		}
		assessment := network.AssessKernelProbe(probe, true)
		stateUpdates := map[string]interface{}{
			"status": assessment.Status, "phase": "idle", "recommended_action": "none",
			"platform_os": probe.OperatingSystem, "architecture": probe.Architecture, "libc": probe.Libc,
			"desired_version": release.Version, "installed_version": probe.Version,
			"desired_sha256": binarySHA, "installed_sha256": probe.BinarySHA256,
			"desired_config_sha256": configSHA, "applied_config_sha256": probe.ConfigSHA256,
			"service_status": probe.ServiceStatus, "control_status": probe.ControlStatus,
			"last_detected_at": now, "last_error": "", "active_operation_id": nil,
		}
		if assessment.Status == "healthy" {
			stateUpdates["last_healthy_at"] = now
		}
		if err := tx.Model(&state).Updates(stateUpdates).Error; err != nil {
			return err
		}
		if probe.Installed && probe.Version != "" {
			if err := tx.Model(&model.Node{}).Where("id = ?", operation.NodeID).Updates(map[string]interface{}{"version": probe.Version, "ssh_verified_at": now}).Error; err != nil {
				return err
			}
		}
		if err := enqueueMieruReadinessPublication(tx, operation.NodeID); err != nil {
			return err
		}
		if err := tx.Model(&row).Updates(map[string]interface{}{"status": "succeeded", "phase": "completed", "result_summary": summary, "error": "", "finished_at": now}).Error; err != nil {
			return err
		}
		if err := tx.First(&state, "node_id = ?", operation.NodeID).Error; err != nil {
			return err
		}
		if err := tx.First(&row, operation.ID).Error; err != nil {
			return err
		}
		out = network.KernelReconciliationResult{
			State: kernelStateView(state), Operation: kernelOperationView(row), Changed: changed,
			Action: row.OperationType, ConnectorVerified: connectorVerified, ConnectorWarning: connectorWarning,
		}
		return nil
	})
	return
}

func (s KernelReconciliationState) FailKernelReconciliation(ctx context.Context, operation network.KernelOperation, phase string, failure error, unsupported bool, now time.Time) error {
	return (KernelDetection{DB: s.DB}).FailDetection(ctx, operation, phase, failure, unsupported, now)
}

func enqueueMieruReadinessPublication(tx *gorm.DB, nodeID uint) error {
	var endpoint model.ProtocolEndpoint
	read := tx.Where("node_id = ? AND LOWER(protocol) = ? AND is_active = ? AND mieru_principal_ready = ?", nodeID, "mieru", true, false).
		Order("id asc").Limit(1).Find(&endpoint)
	if read.Error != nil || endpoint.ID == 0 {
		return read.Error
	}
	return EnqueueNodePublication(tx, nodeID, endpoint.ID, 0)
}
