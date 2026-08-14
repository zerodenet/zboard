package datastore

import (
	"fmt"

	"gorm.io/gorm"
)

const zeroEventNodeCursorsTable = "zero_event_node_cursors"

// ReconcileZeroEventSchema installs the durable projection state used by the
// Zero event consumer. These tables remain separate from nodes and immutable
// billing records: runtime ordering and connection observations are
// observability state, not node identity or accounting settlement.
func ReconcileZeroEventSchema(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database is required")
	}
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("open zero event schema database: %w", err)
	}
	statements := []struct {
		name string
		sql  string
	}{
		{
			name: "zero event node cursor",
			sql: `CREATE TABLE IF NOT EXISTS zero_event_node_cursors (
			node_id bigint unsigned NOT NULL,
			core_instance_id varchar(128) NOT NULL DEFAULT '',
			sequence bigint unsigned NOT NULL DEFAULT 0,
			config_revision bigint unsigned NOT NULL DEFAULT 0,
			occurred_at datetime(3) NOT NULL,
			updated_at datetime(3) NOT NULL,
			PRIMARY KEY (node_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
		},
		{
			name: "principal flow generation",
			sql: `CREATE TABLE IF NOT EXISTS principal_flow_node_generations (
			node_id bigint unsigned NOT NULL,
			core_instance_id varchar(128) NOT NULL,
			started_at datetime(3) NOT NULL,
			closed_at datetime(3) NULL,
			updated_at datetime(3) NOT NULL,
			PRIMARY KEY (node_id),
			KEY idx_principal_flow_generation_instance (core_instance_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
		},
		{
			name: "principal flow observation",
			sql: `CREATE TABLE IF NOT EXISTS principal_flow_observations (
			id bigint unsigned NOT NULL AUTO_INCREMENT,
			node_id bigint unsigned NOT NULL,
			core_instance_id varchar(128) NOT NULL,
			session_registry_revision bigint unsigned NOT NULL,
			event_id varchar(191) NOT NULL,
			sequence bigint unsigned NOT NULL DEFAULT 0,
			principal_key varchar(191) NOT NULL,
			user_id bigint unsigned NOT NULL DEFAULT 0,
			subscription_id bigint unsigned NOT NULL DEFAULT 0,
			protocol_credential_id bigint unsigned NOT NULL DEFAULT 0,
			protocol_endpoint_id bigint unsigned NOT NULL DEFAULT 0,
			active_flows bigint unsigned NOT NULL,
			observed_at datetime(3) NOT NULL,
			created_at datetime(3) NOT NULL,
			PRIMARY KEY (id),
			UNIQUE KEY uk_principal_flow_observation_event (node_id, event_id),
			UNIQUE KEY uk_principal_flow_observation_revision (node_id, core_instance_id, session_registry_revision, principal_key),
			KEY idx_principal_flow_observation_user_time (user_id, observed_at),
			KEY idx_principal_flow_observation_subscription_time (subscription_id, observed_at),
			KEY idx_principal_flow_observation_principal_time (node_id, principal_key, observed_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
		},
		{
			name: "principal flow current projection",
			sql: `CREATE TABLE IF NOT EXISTS principal_flow_currents (
			node_id bigint unsigned NOT NULL,
			principal_key varchar(191) NOT NULL,
			core_instance_id varchar(128) NOT NULL,
			session_registry_revision bigint unsigned NOT NULL,
			user_id bigint unsigned NOT NULL DEFAULT 0,
			subscription_id bigint unsigned NOT NULL DEFAULT 0,
			protocol_credential_id bigint unsigned NOT NULL DEFAULT 0,
			protocol_endpoint_id bigint unsigned NOT NULL DEFAULT 0,
			active_flows bigint unsigned NOT NULL,
			observed_at datetime(3) NOT NULL,
			updated_at datetime(3) NOT NULL,
			PRIMARY KEY (node_id, principal_key),
			KEY idx_principal_flow_current_user (user_id),
			KEY idx_principal_flow_current_subscription (subscription_id),
			KEY idx_principal_flow_current_instance (node_id, core_instance_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
		},
		{
			name: "principal flow scope current projection",
			sql: `CREATE TABLE IF NOT EXISTS principal_flow_scope_currents (
			scope_type varchar(24) NOT NULL,
			scope_id bigint unsigned NOT NULL,
			active_flows bigint unsigned NOT NULL,
			updated_at datetime(3) NOT NULL,
			PRIMARY KEY (scope_type, scope_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
		},
		{
			name: "principal flow scope observation",
			sql: `CREATE TABLE IF NOT EXISTS principal_flow_scope_observations (
			id bigint unsigned NOT NULL AUTO_INCREMENT,
			scope_type varchar(24) NOT NULL,
			scope_id bigint unsigned NOT NULL,
			active_flows bigint unsigned NOT NULL,
			node_id bigint unsigned NOT NULL,
			core_instance_id varchar(128) NOT NULL DEFAULT '',
			session_registry_revision bigint unsigned NOT NULL DEFAULT 0,
			event_id varchar(191) NOT NULL,
			source varchar(32) NOT NULL DEFAULT 'lifecycle',
			observed_at datetime(3) NOT NULL,
			created_at datetime(3) NOT NULL,
			PRIMARY KEY (id),
			UNIQUE KEY uk_principal_flow_scope_event (scope_type, scope_id, event_id),
			KEY idx_principal_flow_scope_time (scope_type, scope_id, observed_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
		},
	}
	for _, statement := range statements {
		if _, err := sqlDB.Exec(statement.sql); err != nil {
			return fmt.Errorf("reconcile %s schema: %w", statement.name, err)
		}
	}
	return nil
}
