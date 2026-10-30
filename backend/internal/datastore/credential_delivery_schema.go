package datastore

import (
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/credentialstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/publicationstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const credentialDeliverySchemaVersion = "0047_credential_delivery.up.sql"

func preflightCredentialDeliverySchema(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database is required")
	}
	if !db.Migrator().HasTable("schema_migrations") {
		return nil
	}
	var count int64
	if err := db.Table("schema_migrations").Where("version = ?", credentialDeliverySchemaVersion).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return nil
	}
	for _, field := range []struct{ table, column string }{{"protocol_deployments", "credential_targets"}} {
		if !db.Migrator().HasTable(field.table) || !db.Migrator().HasColumn(field.table, field.column) {
			return fmt.Errorf("credential delivery source evidence %s.%s is missing after migration; restore source evidence before continuing", field.table, field.column)
		}
	}
	if db.Migrator().HasTable("node_credential_delivery_states") && db.Migrator().HasTable("nodes") {
		complete := true
		for _, column := range []string{"node_id", "deployment_id", "config_sha256"} {
			complete = complete && db.Migrator().HasColumn("node_credential_delivery_states", column)
		}
		if complete {
			hashMismatch := "source.desired_config_sha256 <> receipt.config_sha256 OR source.applied_config_sha256 <> receipt.config_sha256"
			if !IsSQLite(db) {
				// Historical baseline columns and later receipt tables can inherit
				// different collations. Configuration hashes require exact bytes.
				hashMismatch = "BINARY source.desired_config_sha256 <> BINARY receipt.config_sha256 OR BINARY source.applied_config_sha256 <> BINARY receipt.config_sha256"
			}
			var invalid []uint
			err := db.Table("node_credential_delivery_states AS receipt").Select("receipt.node_id").Joins("JOIN nodes ON nodes.id = receipt.node_id").Joins("LEFT JOIN protocol_deployments AS source ON source.id = receipt.deployment_id").Where("receipt.deployment_id > 0 AND LENGTH(receipt.config_sha256) = 64 AND (source.id IS NULL OR source.node_id <> receipt.node_id OR source.status <> ? OR source.credential_targets = ? OR "+hashMismatch+")", "succeeded", "").Limit(1).Pluck("receipt.node_id", &invalid).Error
			if err != nil {
				return err
			}
			if len(invalid) > 0 {
				return fmt.Errorf("confirmed credential source evidence is missing or mismatched for node%d; restore the immutable source before continuing", invalid[0])
			}
		}
	}
	return nil
}

func reconcileCredentialDeliverySchema(db *gorm.DB) error {
	var recorded int64
	if err := db.Table("schema_migrations").Where("version = ?", credentialDeliverySchemaVersion).Count(&recorded).Error; err != nil {
		return err
	}
	for _, field := range []struct{ table, column, mysql, sqlite string }{
		{"protocol_deployments", "credential_targets", "LONGTEXT NOT NULL", "TEXT NOT NULL DEFAULT ''"},
		{"protocol_credentials", "delivery_required", "boolean NOT NULL DEFAULT false", "INTEGER NOT NULL DEFAULT 0"},
		{"network_entries", "delivery_required", "boolean NOT NULL DEFAULT false", "INTEGER NOT NULL DEFAULT 0"},
	} {
		if db.Migrator().HasColumn(field.table, field.column) {
			continue
		}
		definition := field.mysql
		if IsSQLite(db) {
			definition = field.sqlite
		}
		// MySQL's explicit constant default for a new LONGTEXT column requires an
		// expression on8.4. Existing rows receive the unknown legacy marker.
		if recorded > 0 && field.column == "delivery_required" {
			definition = "boolean NOT NULL DEFAULT true"
			if IsSQLite(db) {
				definition = "INTEGER NOT NULL DEFAULT 1"
			}
		}
		if field.column == "credential_targets" && !IsSQLite(db) {
			definition = "LONGTEXT NOT NULL DEFAULT ('')"
		}
		if err := db.Exec("ALTER TABLE " + field.table + " ADD COLUMN " + field.column + " " + definition).Error; err != nil {
			return err
		}
	}
	repair := false
	for _, table := range []struct {
		name    string
		columns []string
	}{
		{"credential_delivery_confirmations", []string{"credential_id", "node_id", "protocol_endpoint_id", "deployment_id", "material_sha256", "endpoint_sha256", "config_sha256", "policy_revision", "confirmed_at"}},
		{"network_entry_delivery_confirmations", []string{"entry_id", "node_id", "deployment_id", "material_sha256", "config_sha256", "confirmed_at"}},
		{"node_credential_delivery_states", []string{"node_id", "deployment_id", "config_sha256", "confirmed_at"}},
	} {
		if !db.Migrator().HasTable(table.name) {
			repair = true
			continue
		}
		for _, column := range table.columns {
			if !db.Migrator().HasColumn(table.name, column) {
				repair = true
				break
			}
		}
	}
	if repair {
		// Validate every recoverable source before changing auxiliary tables. DDL
		// may auto-commit on MySQL; immutable deployment manifests are never erased.
		if err := visitConfirmedCredentialManifests(db, func(model.ProtocolDeployment, network.CredentialDeliveryManifest) error { return nil }); err != nil {
			return err
		}
		for _, table := range []string{"credential_delivery_confirmations", "node_credential_delivery_states", "network_entry_delivery_confirmations"} {
			if err := db.Migrator().DropTable(table); err != nil {
				return err
			}
		}
	}
	if err := db.AutoMigrate(&model.CredentialDeliveryConfirmation{}, &model.NodeCredentialDeliveryState{}, &model.NetworkEntryDeliveryConfirmation{}); err != nil {
		return err
	}
	if err := visitConfirmedCredentialManifests(db, func(deployment model.ProtocolDeployment, manifest network.CredentialDeliveryManifest) error {
		targets := manifest.Targets
		var existing model.NodeCredentialDeliveryState
		found := db.Where("node_id = ?", deployment.NodeID).Limit(1).Find(&existing)
		if found.Error != nil {
			return found.Error
		}
		if found.RowsAffected > 0 && existing.DeploymentID > deployment.ID {
			return nil
		}
		if found.RowsAffected > 0 && existing.DeploymentID == deployment.ID && existing.ConfigSHA256 == deployment.AppliedConfigSHA256 {
			var count int64
			if err := db.Model(&model.CredentialDeliveryConfirmation{}).Where("node_id = ? AND config_sha256 = ?", deployment.NodeID, deployment.AppliedConfigSHA256).Count(&count).Error; err != nil {
				return err
			}
			if count == int64(len(targets)) {
				complete := true
				if manifest.NetworkEntries != nil {
					var routes int64
					if err := db.Model(&model.NetworkEntryDeliveryConfirmation{}).Where("node_id = ? AND config_sha256 = ?", deployment.NodeID, deployment.AppliedConfigSHA256).Count(&routes).Error; err != nil {
						return err
					}
					complete = routes == int64(len(*manifest.NetworkEntries))
				}
				if complete {
					return nil
				}
			}
		}
		return db.Transaction(func(tx *gorm.DB) error {
			// Startup recovery must not overwrite a newer publication committed
			// after its source scan. Match the publisher's node fence, then re-read.
			if IsSQLite(tx) {
				if err := tx.Model(&model.Node{}).Where("id = ?", deployment.NodeID).UpdateColumn("is_enabled", gorm.Expr("is_enabled")).Error; err != nil {
					return err
				}
			}
			var node model.Node
			locked := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Where("id = ?", deployment.NodeID).Limit(1).Find(&node)
			if locked.Error != nil {
				return locked.Error
			}
			if locked.RowsAffected == 0 {
				return nil
			}
			var current model.NodeCredentialDeliveryState
			fresh := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("node_id = ?", deployment.NodeID).Limit(1).Find(&current)
			if fresh.Error != nil {
				return fresh.Error
			}
			if fresh.RowsAffected > 0 && current.DeploymentID > deployment.ID {
				return nil
			}
			if err := tx.Model(&model.CredentialDeliveryConfirmation{}).Where("node_id = ?", deployment.NodeID).Update("config_sha256", "").Error; err != nil {
				return err
			}
			rows := make([]model.CredentialDeliveryConfirmation, 0, len(targets))
			for _, target := range targets {
				rows = append(rows, model.CredentialDeliveryConfirmation{EndpointSHA256: target.EndpointSHA256, CredentialID: target.CredentialID, NodeID: deployment.NodeID, ProtocolEndpointID: target.ProtocolEndpointID, DeploymentID: deployment.ID, MaterialSHA256: target.MaterialSHA256, ConfigSHA256: deployment.AppliedConfigSHA256, PolicyRevision: target.PolicyRevision, ConfirmedAt: *deployment.FinishedAt})
			}
			if len(rows) > 0 {
				if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "credential_id"}}, DoUpdates: clause.AssignmentColumns([]string{"node_id", "protocol_endpoint_id", "deployment_id", "material_sha256", "endpoint_sha256", "config_sha256", "policy_revision", "confirmed_at"})}).CreateInBatches(&rows, 256).Error; err != nil {
					return err
				}
			}
			if manifest.NetworkEntries != nil {
				if err := tx.Model(&model.NetworkEntryDeliveryConfirmation{}).Where("node_id = ?", deployment.NodeID).Update("config_sha256", "").Error; err != nil {
					return err
				}
				current, err := credentialstore.CurrentDeliveryEntries(tx, *manifest.NetworkEntries, true)
				if err != nil {
					return err
				}
				routes := make([]model.NetworkEntryDeliveryConfirmation, 0, len(current))
				for _, target := range current {
					routes = append(routes, model.NetworkEntryDeliveryConfirmation{EntryID: target.NetworkEntryID, NodeID: deployment.NodeID, DeploymentID: deployment.ID, MaterialSHA256: target.MaterialSHA256, ConfigSHA256: deployment.AppliedConfigSHA256, ConfirmedAt: *deployment.FinishedAt})
				}
				if len(routes) > 0 {
					if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "entry_id"}}, DoUpdates: clause.AssignmentColumns([]string{"node_id", "deployment_id", "material_sha256", "config_sha256", "confirmed_at"})}).CreateInBatches(&routes, 256).Error; err != nil {
						return err
					}
				}
			}
			return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "node_id"}}, DoUpdates: clause.AssignmentColumns([]string{"deployment_id", "config_sha256", "confirmed_at"})}).Create(&model.NodeCredentialDeliveryState{NodeID: deployment.NodeID, DeploymentID: deployment.ID, ConfigSHA256: deployment.AppliedConfigSHA256, ConfirmedAt: *deployment.FinishedAt}).Error
		})
	}); err != nil {
		return err
	}
	if !db.Migrator().HasIndex("protocol_deployments", "idx_deployment_node_status_id") {
		if err := db.Exec("CREATE INDEX idx_deployment_node_status_id ON protocol_deployments(node_id,status,id)").Error; err != nil {
			return err
		}
	}
	if err := recoverUnconfirmedDeliveryIntents(db); err != nil {
		return err
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: credentialDeliverySchemaVersion, AppliedAt: time.Now().UTC()}).Error
}

func visitConfirmedCredentialManifests(db *gorm.DB, visit func(model.ProtocolDeployment, network.CredentialDeliveryManifest) error) error {
	// One latest confirmed manifest per node, streamed by ID. Historical intents
	// without a manifest cannot manufacture credential-level confirmation.
	latest := db.Model(&model.ProtocolDeployment{}).Select("MAX(id)").Where("status = ? AND credential_targets <> ?", "succeeded", "").Group("node_id")
	var cursor uint
	for {
		var rows []model.ProtocolDeployment
		if err := db.Where("id IN (?) AND id > ?", latest, cursor).Order("id").Limit(32).Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			if row.FinishedAt == nil || len(row.AppliedConfigSHA256) != 64 || row.AppliedConfigSHA256 != row.DesiredConfigSHA256 {
				return fmt.Errorf("deployment%d lacks exact confirmed credential evidence", row.ID)
			}
			manifest, err := network.DecodeConfigurationDeliveryManifest(row.CredentialTargets)
			if err != nil {
				return fmt.Errorf("deployment%d: %w", row.ID, err)
			}
			if manifest.NetworkEntries != nil {
				current, err := credentialstore.CurrentDeliveryEntries(db, *manifest.NetworkEntries, false)
				if err != nil {
					return err
				}
				manifest.NetworkEntries = &current
			}
			if err := visit(row, manifest); err != nil {
				return err
			}
			cursor = row.ID
		}
		if len(rows) < 32 {
			return nil
		}
	}
}

// Re-run after an interrupted field repair as well as first startup. A known
// configuration's missing projection is rebuilt above; genuinely unconfirmed
// managed bindings require the ordinary publisher, never a synthetic receipt.
func recoverUnconfirmedDeliveryIntents(db *gorm.DB) error {
	var nodes []uint
	hashMatch := "state.config_sha256 = confirmation.config_sha256"
	if !IsSQLite(db) {
		hashMatch = "BINARY state.config_sha256 = BINARY confirmation.config_sha256"
	}
	credentials := `SELECT DISTINCT credential.node_id FROM protocol_credentials AS credential
 LEFT JOIN credential_delivery_confirmations AS confirmation ON confirmation.credential_id = credential.id AND confirmation.node_id = credential.node_id AND confirmation.protocol_endpoint_id = credential.protocol_endpoint_id
 LEFT JOIN node_credential_delivery_states AS state ON state.node_id = credential.node_id AND ` + hashMatch + `
 WHERE credential.delivery_required = true AND credential.status = 'active' AND (confirmation.credential_id IS NULL OR state.node_id IS NULL)`
	entries := `SELECT DISTINCT entry.node_id FROM network_entries AS entry
 LEFT JOIN network_entry_delivery_confirmations AS confirmation ON confirmation.entry_id = entry.id AND confirmation.node_id = entry.node_id
 LEFT JOIN node_credential_delivery_states AS state ON state.node_id = entry.node_id AND ` + hashMatch + `
 WHERE entry.delivery_required = true AND entry.enabled = true AND entry.node_id > 0 AND (confirmation.entry_id IS NULL OR state.node_id IS NULL)`
	if err := db.Raw("SELECT DISTINCT pending.node_id FROM (" + credentials + " UNION " + entries + ") AS pending JOIN nodes ON nodes.id = pending.node_id ORDER BY pending.node_id").Scan(&nodes).Error; err != nil {
		return err
	}
	for _, nodeID := range nodes {
		if err := db.Transaction(func(tx *gorm.DB) error {
			var queue model.NodeConfigPublish
			found := tx.Where("node_id = ?", nodeID).Limit(1).Find(&queue)
			if found.Error != nil {
				return found.Error
			}
			if found.RowsAffected > 0 && queue.Phase != "connections" {
				return nil
			}
			return publicationstore.Enqueue(tx, nodeID, 0, 0)
		}); err != nil {
			return err
		}
	}
	return nil
}
