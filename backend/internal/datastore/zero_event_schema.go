package datastore

import (
	"fmt"

	"gorm.io/gorm"
)

const zeroEventNodeCursorsTable = "zero_event_node_cursors"

// ReconcileZeroEventSchema installs the small durable cursor used by the Zero
// node projector. The cursor is intentionally separate from nodes: runtime
// event ordering must not turn the node entity row itself into another hot
// telemetry write target.
func ReconcileZeroEventSchema(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database is required")
	}
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("open zero event schema database: %w", err)
	}
	_, err = sqlDB.Exec(`CREATE TABLE IF NOT EXISTS zero_event_node_cursors (
		node_id bigint unsigned NOT NULL,
		core_instance_id varchar(128) NOT NULL DEFAULT '',
		sequence bigint unsigned NOT NULL DEFAULT 0,
		config_revision bigint unsigned NOT NULL DEFAULT 0,
		occurred_at datetime(3) NOT NULL,
		updated_at datetime(3) NOT NULL,
		PRIMARY KEY (node_id)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`)
	if err != nil {
		return fmt.Errorf("reconcile zero event node cursor schema: %w", err)
	}
	return nil
}
