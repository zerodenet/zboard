package datastore

import (
	"fmt"

	"gorm.io/gorm"
)

// ReconcileFairUseTelemetrySchema installs the immutable short-window activity
// facts used by Zboard's Fair Use evaluator. These facts are deliberately
// separate from traffic accounting and Principal active-flow projections:
// billing, current concurrency and connection-start behaviour have different
// semantics and must not share mutable state.
func ReconcileFairUseTelemetrySchema(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database is required")
	}
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("open fair use telemetry database: %w", err)
	}
	_, err = sqlDB.Exec(`CREATE TABLE IF NOT EXISTS subscription_flow_start_events (
		id bigint unsigned NOT NULL AUTO_INCREMENT,
		node_id bigint unsigned NOT NULL,
		core_instance_id varchar(128) NOT NULL DEFAULT '',
		event_id varchar(191) NOT NULL,
		sequence bigint unsigned NOT NULL DEFAULT 0,
		principal_key varchar(191) NOT NULL DEFAULT '',
		user_id bigint unsigned NOT NULL DEFAULT 0,
		subscription_id bigint unsigned NOT NULL DEFAULT 0,
		protocol_credential_id bigint unsigned NOT NULL DEFAULT 0,
		protocol_endpoint_id bigint unsigned NOT NULL DEFAULT 0,
		mapping_state varchar(16) NOT NULL DEFAULT 'unmapped',
		occurred_at datetime(3) NOT NULL,
		received_at datetime(3) NOT NULL,
		created_at datetime(3) NOT NULL,
		PRIMARY KEY (id),
		UNIQUE KEY uk_subscription_flow_start_event (node_id, event_id),
		KEY idx_subscription_flow_start_subscription_time (subscription_id, occurred_at),
		KEY idx_subscription_flow_start_subscription_node_time (subscription_id, node_id, occurred_at),
		KEY idx_subscription_flow_start_user_time (user_id, occurred_at),
		KEY idx_subscription_flow_start_received (received_at)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`)
	if err != nil {
		return fmt.Errorf("reconcile fair use flow-start telemetry schema: %w", err)
	}
	return nil
}
