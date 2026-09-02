package datastore

import (
	"fmt"

	"gorm.io/gorm"
)

// ReconcileOperationsSchema installs additive operational resources that must
// also be available to databases whose squashed pre-release baseline was
// already recorded before the resource was introduced.
func ReconcileOperationsSchema(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database is required")
	}
	if IsSQLite(db) {
		return nil
	}
	if err := db.Exec(`CREATE TABLE IF NOT EXISTS announcements (
		id bigint unsigned NOT NULL AUTO_INCREMENT,
		title varchar(160) NOT NULL,
		content text NOT NULL,
		severity varchar(16) NOT NULL DEFAULT 'info',
		audience varchar(16) NOT NULL DEFAULT 'all',
		status varchar(16) NOT NULL DEFAULT 'draft',
		dismissible tinyint(1) NOT NULL DEFAULT 1,
		starts_at datetime(3) NULL,
		ends_at datetime(3) NULL,
		created_by bigint unsigned NOT NULL DEFAULT 0,
		revision bigint unsigned NOT NULL DEFAULT 1,
		created_at datetime(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
		updated_at datetime(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
		PRIMARY KEY (id),
		KEY idx_announcements_visibility (status, starts_at, ends_at),
		KEY idx_announcements_audience (audience),
		KEY idx_announcements_created_by (created_by)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`).Error; err != nil {
		return fmt.Errorf("reconcile announcement schema: %w", err)
	}
	return nil
}
