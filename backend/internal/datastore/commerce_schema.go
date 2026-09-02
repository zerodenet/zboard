package datastore

import (
	"database/sql"
	"fmt"

	"gorm.io/gorm"
)

type commerceColumnSpec struct {
	table      string
	name       string
	definition string
	after      string
}

var commerceOrderSnapshotColumns = []commerceColumnSpec{
	{table: "orders", name: "plan_name", definition: "varchar(80) NOT NULL DEFAULT ''", after: "status"},
	{table: "orders", name: "sku_name", definition: "varchar(80) NOT NULL DEFAULT ''", after: "plan_name"},
	{table: "orders", name: "billing_unit", definition: "varchar(16) NOT NULL DEFAULT ''", after: "sku_name"},
	{table: "orders", name: "billing_value", definition: "int NOT NULL DEFAULT 0", after: "billing_unit"},
	{table: "orders", name: "renewal_effect", definition: "varchar(32) NOT NULL DEFAULT ''", after: "billing_value"},
	{table: "orders", name: "traffic_bytes", definition: "bigint NOT NULL DEFAULT 0", after: "renewal_effect"},
	{table: "orders", name: "device_limit", definition: "int NOT NULL DEFAULT 0", after: "traffic_bytes"},
	{table: "orders", name: "speed_limit_mbps", definition: "int NOT NULL DEFAULT 0", after: "device_limit"},
}

// ReconcileCommerceSchema evolves the squashed pre-release baseline without
// introducing a second numbered migration. The repository intentionally keeps
// a single 0001 migration until the first public release, so additive schema
// changes must be safe for both fresh installations and databases that already
// recorded that baseline.
func ReconcileCommerceSchema(db *gorm.DB) error {
	if IsSQLite(db) {
		return nil
	}
	if db == nil {
		return fmt.Errorf("database is required")
	}
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("open commerce schema database: %w", err)
	}

	if err := reconcilePlanSKUCommerceSchema(sqlDB); err != nil {
		return err
	}
	if err := reconcileOrderSnapshotSchema(sqlDB); err != nil {
		return err
	}
	return nil
}

func reconcilePlanSKUCommerceSchema(sqlDB *sql.DB) error {
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

	var entitlementModeColumn int
	if err := sqlDB.QueryRow(
		`SELECT COUNT(*)
		   FROM information_schema.columns
		  WHERE table_schema = DATABASE()
		    AND table_name = 'plan_skus'
		    AND column_name = 'entitlement_mode'`,
	).Scan(&entitlementModeColumn); err != nil {
		return fmt.Errorf("inspect plan sku entitlement mode: %w", err)
	}
	if entitlementModeColumn == 0 {
		if _, err := sqlDB.Exec(
			"ALTER TABLE plan_skus ADD COLUMN entitlement_mode varchar(24) NOT NULL DEFAULT 'plan' AFTER billing_mode",
		); err != nil {
			return fmt.Errorf("add plan sku entitlement mode: %w", err)
		}
		if _, err := sqlDB.Exec(
			`UPDATE plan_skus
			    SET entitlement_mode = CASE
			      WHEN sku_type = 'traffic_pack' THEN 'traffic_addon'
			      ELSE 'plan'
			    END`,
		); err != nil {
			return fmt.Errorf("backfill plan sku entitlement mode: %w", err)
		}
	}

	var renewalEffectColumn int
	if err := sqlDB.QueryRow(
		`SELECT COUNT(*)
		   FROM information_schema.columns
		  WHERE table_schema = DATABASE()
		    AND table_name = 'plan_skus'
		    AND column_name = 'renewal_effect'`,
	).Scan(&renewalEffectColumn); err != nil {
		return fmt.Errorf("inspect plan sku renewal effect: %w", err)
	}
	if renewalEffectColumn == 0 {
		if _, err := sqlDB.Exec(
			"ALTER TABLE plan_skus ADD COLUMN renewal_effect varchar(32) NOT NULL DEFAULT '' AFTER entitlement_mode",
		); err != nil {
			return fmt.Errorf("add plan sku renewal effect: %w", err)
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
		SET renewal_effect = CASE
		  WHEN entitlement_mode = 'traffic_addon' THEN 'none'
		  WHEN NOT EXISTS (
		    SELECT 1 FROM plan_sku_operations
		     WHERE plan_sku_operations.plan_sku_id = plan_skus.id
		       AND plan_sku_operations.operation = 'renew'
		  ) THEN 'none'
		  WHEN billing_unit = 'once' THEN 'add_quota_only'
		  ELSE 'extend_only'
		END
		WHERE renewal_effect = ''`); err != nil {
		return fmt.Errorf("backfill plan sku renewal effect: %w", err)
	}

	if _, err := sqlDB.Exec(`UPDATE plan_skus
		SET traffic_bytes = CASE WHEN entitlement_mode = 'traffic_addon' THEN traffic_bytes ELSE 0 END,
		    device_limit = 0,
		    speed_limit_mbps = 0
		WHERE (entitlement_mode <> 'traffic_addon' AND traffic_bytes <> 0)
		   OR device_limit <> 0
		   OR speed_limit_mbps <> 0`); err != nil {
		return fmt.Errorf("remove duplicated plan sku entitlements: %w", err)
	}
	return nil
}

func reconcileOrderSnapshotSchema(sqlDB *sql.DB) error {
	for _, column := range commerceOrderSnapshotColumns {
		if err := ensureCommerceColumn(sqlDB, column); err != nil {
			return err
		}
	}

	// Rows created before order snapshots were introduced receive all of the new
	// columns with empty defaults. Backfill their entitlement values first while
	// the empty name/unit triplet can still be used as an unambiguous legacy marker.
	if _, err := sqlDB.Exec(`UPDATE orders
		LEFT JOIN plans ON plans.id = orders.plan_id
		LEFT JOIN plan_skus ON plan_skus.id = orders.plan_sku_id
		SET orders.billing_value = COALESCE(plan_skus.billing_value, orders.billing_value),
		    orders.traffic_bytes = CASE
		      WHEN orders.order_type = 'traffic_pack' THEN COALESCE(plan_skus.traffic_bytes, orders.traffic_bytes)
		      ELSE COALESCE(plans.traffic_bytes, orders.traffic_bytes)
		    END,
		    orders.device_limit = CASE
		      WHEN orders.order_type = 'traffic_pack' THEN 0
		      ELSE COALESCE(plans.device_limit, orders.device_limit)
		    END,
		    orders.speed_limit_mbps = CASE
		      WHEN orders.order_type = 'traffic_pack' THEN 0
		      ELSE COALESCE(plans.speed_limit_mbps, orders.speed_limit_mbps)
		    END
		WHERE orders.plan_name = ''
		  AND orders.sku_name = ''
		  AND orders.billing_unit = ''`); err != nil {
		return fmt.Errorf("backfill legacy order entitlement snapshots: %w", err)
	}

	if _, err := sqlDB.Exec(`UPDATE orders
		LEFT JOIN plans ON plans.id = orders.plan_id
		LEFT JOIN plan_skus ON plan_skus.id = orders.plan_sku_id
		SET orders.plan_name = CASE WHEN orders.plan_name = '' THEN COALESCE(plans.name, '') ELSE orders.plan_name END,
		    orders.sku_name = CASE WHEN orders.sku_name = '' THEN COALESCE(plan_skus.name, '') ELSE orders.sku_name END,
		    orders.billing_unit = CASE WHEN orders.billing_unit = '' THEN COALESCE(plan_skus.billing_unit, '') ELSE orders.billing_unit END`); err != nil {
		return fmt.Errorf("backfill legacy order identity snapshots: %w", err)
	}

	// Existing pending renewals retain the historical fulfillment contract:
	// timed renewals extended the term and added quota, while permanent renewals
	// added quota and kept the permanent end. New orders always snapshot the
	// explicit SKU value.
	if _, err := sqlDB.Exec(`UPDATE orders
		SET renewal_effect = CASE
		  WHEN order_type <> 'renewal' THEN 'none'
		  WHEN billing_unit = 'once' THEN 'add_quota_only'
		  ELSE 'extend_and_add_quota'
		END
		WHERE renewal_effect = ''`); err != nil {
		return fmt.Errorf("backfill legacy order renewal effect snapshots: %w", err)
	}
	return nil
}

func ensureCommerceColumn(sqlDB *sql.DB, column commerceColumnSpec) error {
	var count int
	if err := sqlDB.QueryRow(
		`SELECT COUNT(*)
		   FROM information_schema.columns
		  WHERE table_schema = DATABASE()
		    AND table_name = ?
		    AND column_name = ?`,
		column.table,
		column.name,
	).Scan(&count); err != nil {
		return fmt.Errorf("inspect commerce column %s.%s: %w", column.table, column.name, err)
	}
	if count > 0 {
		return nil
	}
	statement := fmt.Sprintf("ALTER TABLE `%s` ADD COLUMN `%s` %s AFTER `%s`", column.table, column.name, column.definition, column.after)
	if _, err := sqlDB.Exec(statement); err != nil {
		return fmt.Errorf("add commerce column %s.%s: %w", column.table, column.name, err)
	}
	return nil
}
