package datastore

import (
	"fmt"

	"gorm.io/gorm"
)

// ReconcileFairUseEnforcementSchema installs the temporary business-policy
// overlay used by Fair Use enforcement. Credential lifecycle remains separate:
// restricting a subscription never revokes or mutates its protocol credentials.
func ReconcileFairUseEnforcementSchema(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database is required")
	}
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("open fair use enforcement database: %w", err)
	}
	_, err = sqlDB.Exec(`CREATE TABLE IF NOT EXISTS subscription_fair_use_restrictions (
		subscription_id bigint unsigned NOT NULL,
		active tinyint(1) NOT NULL DEFAULT 0,
		source varchar(16) NOT NULL DEFAULT '',
		policy_scope_type varchar(16) NOT NULL DEFAULT '',
		policy_scope_id bigint unsigned NOT NULL DEFAULT 0,
		policy_revision bigint unsigned NOT NULL DEFAULT 0,
		score int NOT NULL DEFAULT 0,
		reason text NOT NULL,
		started_at datetime(3) NULL,
		restricted_until datetime(3) NULL,
		released_at datetime(3) NULL,
		release_reason text NOT NULL,
		hold_until datetime(3) NULL,
		last_source_evaluation_at datetime(3) NULL,
		revision bigint unsigned NOT NULL DEFAULT 1,
		created_at datetime(3) NOT NULL,
		updated_at datetime(3) NOT NULL,
		PRIMARY KEY (subscription_id),
		KEY idx_fair_use_restriction_active_until (active, restricted_until),
		KEY idx_fair_use_restriction_hold (hold_until)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`)
	if err != nil {
		return fmt.Errorf("reconcile fair use restriction schema: %w", err)
	}
	return nil
}
