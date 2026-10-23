package networkstore

import (
	"context"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s ConfigurationPublicationState) UncertainConfigurationPublication(ctx context.Context, deployment network.ConfigurationDeployment, message, output string, now time.Time) (out network.ConfigurationDeployment, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row model.ProtocolDeployment
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND node_id = ? AND status IN ?", deployment.ID, deployment.NodeID, []string{"running", "reconciling"}).First(&row).Error; err != nil {
			return err
		}
		first := row.Status == "running"
		if err := tx.Model(&row).Updates(map[string]interface{}{"status": "reconciling", "error": message, "output": output, "finished_at": nil}).Error; err != nil {
			return err
		}
		state, err := lockOrCreateKernelState(tx, deployment.NodeID)
		if err != nil {
			return err
		}
		// Applied digest, readiness, quota and node observations remain untouched.
		if err := tx.Model(&state).Updates(map[string]interface{}{"status": "reconciling", "phase": "awaiting_configuration", "last_error": message}).Error; err != nil {
			return err
		}
		if first {
			// Ensure a recovery check exists without replacing a newer intent,
			// its generation, attribution or an active worker's lease.
			queued := model.NodeConfigPublish{NodeID: row.NodeID, EndpointID: row.ProtocolEndpointID, Generation: 1, Reconciling: true, NextAttemptAt: now, LeaseUntil: publicationIdleLease}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&queued).Error; err != nil {
				return err
			}
			if err := tx.Model(&model.NodeConfigPublish{}).Where("node_id = ?", row.NodeID).Update("reconciling", true).Error; err != nil {
				return err
			}
		}

		row.Status, row.Error, row.Output, row.FinishedAt = "reconciling", message, output, nil
		out = configurationDeploymentView(row)
		return nil
	})
	return
}

func (s ConfigurationPublicationState) ResolveConfigurationPublication(ctx context.Context, deployment network.ConfigurationDeployment, applied bool, output string, now time.Time) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row model.ProtocolDeployment
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND node_id = ? AND status = ?", deployment.ID, deployment.NodeID, "reconciling").First(&row).Error; err != nil {
			return err
		}
		updates := map[string]interface{}{"status": "superseded", "output": output, "error": "", "finished_at": now}
		if applied {
			updates["status"] = "succeeded"
			updates["applied_config_sha256"] = row.DesiredConfigSHA256
		}
		if err := tx.Model(&row).Updates(updates).Error; err != nil {
			return err
		}
		if applied {
			if err := confirmCredentialDelivery(tx, row, now); err != nil {
				return err
			}
		}
		// A subsequent publication recompiles the latest intent and commits its
		// readiness. An old acknowledgement must not enable newly added endpoints.
		stateUpdates := map[string]interface{}{"phase": "queued", "last_error": ""}
		if applied {
			stateUpdates["applied_config_sha256"] = row.DesiredConfigSHA256
		}
		if err := tx.Model(&model.NodeKernelState{}).Where("node_id = ?", row.NodeID).Updates(stateUpdates).Error; err != nil {
			return err
		}
		return tx.Model(&model.NodeOperation{}).Where("node_id = ? AND status = ?", row.NodeID, "reconciling").Updates(map[string]interface{}{"status": "superseded", "phase": "completed", "finished_at": now}).Error
	})
}
