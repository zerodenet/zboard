package datastore

import (
	"fmt"

	"gorm.io/gorm"
)

// SQLite creates this model through databaseModels. MySQL also needs additive
// DDL after an already-recorded baseline. Keep retirement history independent
// of the node row so removing a node never removes its progress or receipt.
func reconcileNodeRetirementSchema(db *gorm.DB) error {
	if IsSQLite(db) {
		return nil
	}
	if err := db.Exec(`CREATE TABLE IF NOT EXISTS node_retirements (
		node_id bigint unsigned NOT NULL,
		node_name varchar(100) NOT NULL,
		actor_id bigint unsigned NOT NULL,
		mode varchar(16) NOT NULL,
		state varchar(24) NOT NULL,
		run_id varchar(36) NOT NULL,
		attempt bigint NOT NULL,
		steps text NOT NULL,
		last_error text NOT NULL,
		created_at datetime(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
		updated_at datetime(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
		PRIMARY KEY (node_id)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`).Error; err != nil {
		return fmt.Errorf("reconcile node retirement schema: %w", err)
	}
	return nil
}
