package datastore

import (
	"fmt"

	"gorm.io/gorm"
)

// ReconcileCommerceSchema evolves the squashed pre-release baseline without
// introducing a second numbered migration. The repository intentionally keeps
// a single 0001 migration until the first public release, so additive schema
// changes must be safe for both fresh installations and databases that already
// recorded that baseline.
func ReconcileCommerceSchema(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database is required")
	}
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("open commerce schema database: %w", err)
	}

	var billingModeColumn int
	if err := sqlDB.QueryRow(
		`SELECT COUNT(*)
		   FROM information_schema.columns
		  WHERE table_schema = DATABASE()
		    AND table_name = 'plan_skus'
		    AND column_name = 'billing_mode'`,
	).Scan(&billingModeColumn); err != nil {
		return fmt.Errorf("inspect plan sku billing mode: %w", err)
	}
	if billingModeColumn == 0 {
		if _, err := sqlDB.Exec(
			"ALTER TABLE plan_skus ADD COLUMN billing_mode varchar(20) NOT NULL DEFAULT 'periodic' AFTER sku_type",
		); err != nil {
			return fmt.Errorf("add plan sku billing mode: %w", err)
		}
		if _, err := sqlDB.Exec(
			`UPDATE plan_skus
			    SET billing_mode = CASE
			      WHEN billing_unit = 'once' OR sku_type = 'traffic_pack' THEN 'one_time'
			      ELSE 'periodic'
			    END`,
		); err != nil {
			return fmt.Errorf("backfill plan sku billing mode: %w", err)
		}
	}

	if _, err := sqlDB.Exec(`CREATE TABLE IF NOT EXISTS plan_sku_operations (
		id bigint unsigned NOT NULL AUTO_INCREMENT,
		plan_sku_id bigint unsigned NOT NULL,
		operation varchar(20) NOT NULL,
		created_at datetime(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
		PRIMARY KEY (id),
		UNIQUE KEY uk_plan_sku_operations_sku_operation (plan_sku_id, operation),
		KEY idx_plan_sku_operations_operation (operation, plan_sku_id),
		CONSTRAINT fk_plan_sku_operations_sku
		  FOREIGN KEY (plan_sku_id) REFERENCES plan_skus (id) ON DELETE CASCADE
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`); err != nil {
		return fmt.Errorf("create plan sku operations: %w", err)
	}

	if _, err := sqlDB.Exec(`INSERT INTO plan_sku_operations (plan_sku_id, operation)
		SELECT plan_skus.id,
		       CASE plan_skus.sku_type
		         WHEN 'renewal' THEN 'renew'
		         WHEN 'upgrade' THEN 'change'
		         WHEN 'traffic_pack' THEN 'addon'
		         ELSE 'purchase'
		       END
		  FROM plan_skus
		 WHERE NOT EXISTS (
		       SELECT 1
		         FROM plan_sku_operations
		        WHERE plan_sku_operations.plan_sku_id = plan_skus.id
		 )`); err != nil {
		return fmt.Errorf("backfill plan sku operations: %w", err)
	}

	if _, err := sqlDB.Exec(`UPDATE plan_skus
		SET traffic_bytes = CASE WHEN billing_mode = 'one_time' THEN traffic_bytes ELSE 0 END,
		    device_limit = 0,
		    speed_limit_mbps = 0`); err != nil {
		return fmt.Errorf("remove duplicated plan sku entitlements: %w", err)
	}
	return nil
}
