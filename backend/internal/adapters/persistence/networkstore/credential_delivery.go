package networkstore

import (
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/credentialstore"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// The frozen manifest is the source; projections can be rebuilt without
// observing a heartbeat or assigning a new credential to an old receipt.
func confirmCredentialDelivery(tx *gorm.DB, deployment model.ProtocolDeployment, now time.Time) error {
	if deployment.CredentialTargets == "" {
		return nil
	} // historical intent, no per-binding evidence
	manifest, err := network.DecodeConfigurationDeliveryManifest(deployment.CredentialTargets)
	if err != nil {
		return err
	}
	targets := manifest.Targets
	for start := 0; start < len(targets); start += 256 {
		end := start + 256
		if end > len(targets) {
			end = len(targets)
		}
		batch := targets[start:end]
		confirmations := make([]model.CredentialDeliveryConfirmation, 0, len(batch))
		for _, target := range batch {
			confirmations = append(confirmations, model.CredentialDeliveryConfirmation{EndpointSHA256: target.EndpointSHA256, CredentialID: target.CredentialID, NodeID: deployment.NodeID, ProtocolEndpointID: target.ProtocolEndpointID, DeploymentID: deployment.ID, MaterialSHA256: target.MaterialSHA256, ConfigSHA256: deployment.DesiredConfigSHA256, PolicyRevision: target.PolicyRevision, ConfirmedAt: now})
		}

		if len(confirmations) > 0 {
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "credential_id"}}, DoUpdates: clause.AssignmentColumns([]string{"node_id", "protocol_endpoint_id", "deployment_id", "material_sha256", "endpoint_sha256", "config_sha256", "policy_revision", "confirmed_at"})}).CreateInBatches(&confirmations, 256).Error; err != nil {
				return err
			}
		}
	}
	if manifest.NetworkEntries != nil {
		current, err := credentialstore.CurrentDeliveryEntries(tx, *manifest.NetworkEntries, true)
		if err != nil {
			return err
		}
		rows := make([]model.NetworkEntryDeliveryConfirmation, 0, len(current))
		for _, target := range current {
			rows = append(rows, model.NetworkEntryDeliveryConfirmation{EntryID: target.NetworkEntryID, NodeID: deployment.NodeID, DeploymentID: deployment.ID, MaterialSHA256: target.MaterialSHA256, ConfigSHA256: deployment.DesiredConfigSHA256, ConfirmedAt: now})
		}
		if len(rows) > 0 {
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "entry_id"}}, DoUpdates: clause.AssignmentColumns([]string{"node_id", "deployment_id", "material_sha256", "config_sha256", "confirmed_at"})}).CreateInBatches(&rows, 256).Error; err != nil {
				return err
			}
		}
	}
	return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "node_id"}}, DoUpdates: clause.AssignmentColumns([]string{"deployment_id", "config_sha256", "confirmed_at"})}).Create(&model.NodeCredentialDeliveryState{NodeID: deployment.NodeID, DeploymentID: deployment.ID, ConfigSHA256: deployment.DesiredConfigSHA256, ConfirmedAt: now}).Error
}

func enqueueUnconfirmedCredentialPublication(tx *gorm.DB, nodeID uint) error {
	var ids []uint
	err := tx.Table("protocol_credentials AS credential").Select("credential.id").
		Joins("LEFT JOIN credential_delivery_confirmations AS confirmation ON confirmation.credential_id = credential.id AND confirmation.node_id = credential.node_id AND confirmation.protocol_endpoint_id = credential.protocol_endpoint_id").
		Joins("LEFT JOIN node_credential_delivery_states AS state ON state.node_id = credential.node_id AND state.config_sha256 = confirmation.config_sha256").
		Where("credential.node_id = ? AND credential.delivery_required = ? AND credential.status = ? AND (confirmation.credential_id IS NULL OR state.node_id IS NULL)", nodeID, true, "active").Limit(1).Pluck("credential.id", &ids).Error
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		if err := tx.Table("network_entries AS entry").Select("entry.id").Joins("LEFT JOIN network_entry_delivery_confirmations AS confirmation ON confirmation.entry_id = entry.id AND confirmation.node_id = entry.node_id").Joins("LEFT JOIN node_credential_delivery_states AS state ON state.node_id = entry.node_id AND state.config_sha256 = confirmation.config_sha256").Where("entry.node_id = ? AND entry.delivery_required = ? AND entry.enabled = ? AND entry.deployment_mode <> ? AND (confirmation.entry_id IS NULL OR state.node_id IS NULL)", nodeID, true, true, "external").Limit(1).Pluck("entry.id", &ids).Error; err != nil {
			return err
		}
	}
	if len(ids) == 0 {
		return nil
	}
	return EnqueuePublication(tx, nodeID, 0, 0)
}
