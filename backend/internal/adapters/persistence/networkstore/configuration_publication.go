package networkstore

import (
	"context"
	"errors"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ConfigurationPublicationState struct{ DB *gorm.DB }

func (s ConfigurationPublicationState) BeginConfigurationPublication(ctx context.Context, request network.ConfigurationPublicationRequest, now time.Time) (out network.ConfigurationPublicationStart, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var node model.Node
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&node, request.NodeID).Error; err != nil {
			return err
		}
		if node.LifecycleStatus == "deleting" {
			return network.ErrKernelResourceDeleting
		}
		if request.TriggerEndpointID != 0 {
			var endpoint model.ProtocolEndpoint
			if err := tx.Select("id", "node_id").Where("id = ? AND node_id = ?", request.TriggerEndpointID, request.NodeID).First(&endpoint).Error; err != nil {
				return err
			}
		}
		requestedBy := optionalUint(request.RequestedBy)
		row := model.ProtocolDeployment{
			ProtocolEndpointID: request.TriggerEndpointID, NodeID: node.ID, ConfigRevision: uint64(now.UnixNano()),
			Status: "running", RequestedBy: requestedBy, StartedAt: &now,
		}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		var fallbackCount int64
		if err := tx.Model(&model.ProtocolEndpoint{}).
			Where("node_id = ? AND LOWER(protocol) = ? AND mieru_principal_ready = ?", node.ID, "mieru", false).
			Count(&fallbackCount).Error; err != nil {
			return err
		}
		out = network.ConfigurationPublicationStart{Deployment: configurationDeploymentView(row), MieruFallbackCount: fallbackCount}
		return nil
	})
	return
}

func (s ConfigurationPublicationState) SetConfigurationPublicationDesired(ctx context.Context, deployment network.ConfigurationDeployment, configSHA string) (out network.ConfigurationDeployment, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row, err := lockConfigurationDeployment(tx, deployment)
		if err != nil {
			return err
		}
		state, err := lockOrCreateKernelState(tx, deployment.NodeID)
		if err != nil {
			return err
		}
		if err := tx.Model(&row).Update("desired_config_sha256", configSHA).Error; err != nil {
			return err
		}
		if err := tx.Model(&state).Updates(map[string]interface{}{
			"status": "publishing", "phase": "applying_config", "desired_config_sha256": configSHA, "last_error": "",
		}).Error; err != nil {
			return err
		}
		row.DesiredConfigSHA256 = configSHA
		out = configurationDeploymentView(row)
		return nil
	})
	return
}

func (s ConfigurationPublicationState) ActivateConfigurationConnectorCredential(ctx context.Context, deployment network.ConfigurationDeployment, credential network.KernelEncryptedCredential) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := lockConfigurationDeployment(tx, deployment); err != nil {
			return err
		}
		return tx.Model(&model.Node{}).Where("id = ?", deployment.NodeID).Updates(map[string]interface{}{
			"node_credential": credential.Ciphertext, "node_credential_prefix": credential.Prefix, "node_credential_revoked_at": nil,
		}).Error
	})
}

func (s ConfigurationPublicationState) RestoreConfigurationConnectorCredential(ctx context.Context, deployment network.ConfigurationDeployment, snapshot network.KernelConnectorSnapshot) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := lockConfigurationDeployment(tx, deployment); err != nil {
			return err
		}
		return restoreConnectorSnapshot(tx, deployment.NodeID, snapshot)
	})
}

func (s ConfigurationPublicationState) CompleteConfigurationPublication(ctx context.Context, deployment network.ConfigurationDeployment, completion network.ConfigurationPublicationCompletion, now time.Time) (out network.ConfigurationDeployment, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row, err := lockConfigurationDeployment(tx, deployment)
		if err != nil {
			return err
		}
		state, err := lockOrCreateKernelState(tx, deployment.NodeID)
		if err != nil {
			return err
		}
		if err := tx.Model(&row).Updates(map[string]interface{}{
			"status": "succeeded", "applied_config_sha256": completion.ConfigSHA,
			"output": completion.Output, "error": "", "finished_at": now,
		}).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.Node{}).Where("id = ?", deployment.NodeID).Updates(map[string]interface{}{
			"last_sync_at": now, "ssh_verified_at": now,
		}).Error; err != nil {
			return err
		}
		if network.ConfigurationMieruReadinessCanCommit(completion.MieruAccess, completion.MieruFallbackCount, completion.SuppressMieruFallback) {
			if err := tx.Model(&model.ProtocolEndpoint{}).
				Where("node_id = ? AND LOWER(protocol) = ?", deployment.NodeID, "mieru").
				Update("mieru_principal_ready", completion.MieruAccess).Error; err != nil {
				return err
			}
			credentialStatus := "prepared"
			if completion.MieruAccess {
				credentialStatus = "active"
			}
			endpointIDs := tx.Model(&model.ProtocolEndpoint{}).Select("id").Where("node_id = ? AND LOWER(protocol) = ?", deployment.NodeID, "mieru")
			if err := tx.Model(&model.ProtocolCredential{}).
				Where("protocol_endpoint_id IN (?) AND status IN ?", endpointIDs, []string{"active", "prepared"}).
				Update("status", credentialStatus).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&model.ProtocolEndpoint{}).
			Where("node_id = ? AND LOWER(protocol) IN ?", deployment.NodeID, []string{"trojan", "hysteria2"}).
			Update("managed_principal_ready", completion.ManagedPrincipalAccess).Error; err != nil {
			return err
		}
		if err := tx.Model(&state).Updates(map[string]interface{}{
			"status": "healthy", "phase": "idle", "desired_config_sha256": completion.ConfigSHA,
			"applied_config_sha256": completion.ConfigSHA, "service_status": "active", "control_status": "healthy",
			"last_error": "", "last_healthy_at": completion.LastHealthyAt,
		}).Error; err != nil {
			return err
		}
		row.Status, row.AppliedConfigSHA256, row.Output, row.Error, row.FinishedAt = "succeeded", completion.ConfigSHA, completion.Output, "", &now
		out = configurationDeploymentView(row)
		return nil
	})
	return
}

func (s ConfigurationPublicationState) FailConfigurationPublication(ctx context.Context, deployment network.ConfigurationDeployment, message, output string, now time.Time) (out network.ConfigurationDeployment, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row, err := lockConfigurationDeployment(tx, deployment)
		if err != nil {
			return err
		}
		state, err := lockOrCreateKernelState(tx, deployment.NodeID)
		if err != nil {
			return err
		}
		if err := tx.Model(&row).Updates(map[string]interface{}{
			"status": "failed", "output": output, "error": message, "finished_at": now,
		}).Error; err != nil {
			return err
		}
		if err := tx.Model(&state).Updates(map[string]interface{}{
			"status": "apply_failed", "phase": "idle", "last_error": message,
		}).Error; err != nil {
			return err
		}
		row.Status, row.Output, row.Error, row.FinishedAt = "failed", output, message, &now
		out = configurationDeploymentView(row)
		return nil
	})
	return
}

func lockConfigurationDeployment(tx *gorm.DB, deployment network.ConfigurationDeployment) (model.ProtocolDeployment, error) {
	var row model.ProtocolDeployment
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND node_id = ? AND status = ?", deployment.ID, deployment.NodeID, "running").First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.ProtocolDeployment{}, network.ErrConfigurationDeploymentLost
	}
	return row, err
}

func restoreConnectorSnapshot(tx *gorm.DB, nodeID uint, snapshot network.KernelConnectorSnapshot) error {
	return tx.Model(&model.Node{}).Where("id = ?", nodeID).Updates(map[string]interface{}{
		"node_credential": snapshot.Credential.Ciphertext, "node_credential_prefix": snapshot.Credential.Prefix,
		"node_credential_revoked_at": snapshot.RevokedAt, "connector_last_seen_at": snapshot.ConnectorLastSeenAt,
		"last_seen_at": snapshot.LastSeenAt, "is_online": snapshot.IsOnline, "status": snapshot.Status,
		"version": snapshot.Version, "uptime_seconds": snapshot.UptimeSeconds, "active_flows": snapshot.ActiveFlows,
		"bytes_up": snapshot.BytesUp, "bytes_down": snapshot.BytesDown,
	}).Error
}

func configurationDeploymentView(row model.ProtocolDeployment) network.ConfigurationDeployment {
	return network.ConfigurationDeployment{
		ID: row.ID, ProtocolEndpointID: row.ProtocolEndpointID, NodeID: row.NodeID, ConfigRevision: row.ConfigRevision,
		DesiredConfigSHA256: row.DesiredConfigSHA256, AppliedConfigSHA256: row.AppliedConfigSHA256,
		Status: row.Status, RequestedBy: row.RequestedBy, Output: row.Output, Error: row.Error,
		StartedAt: row.StartedAt, FinishedAt: row.FinishedAt,
	}
}

func optionalUint(value uint) *uint {
	if value == 0 {
		return nil
	}
	return &value
}
