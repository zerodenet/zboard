package datastore

import (
	"database/sql"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

type trafficReadIndexDefinition struct {
	table   string
	name    string
	columns string
	ddl     string
}

var trafficReadIndexes = []trafficReadIndexDefinition{
	{
		table:   "traffic_records",
		name:    "idx_traffic_records_user_time",
		columns: "(user_id, record_at)",
		ddl:     "ALTER TABLE traffic_records ADD KEY idx_traffic_records_user_time (user_id, record_at)",
	},
	{
		table:   "traffic_records",
		name:    "idx_traffic_records_subscription_time",
		columns: "(subscription_id, record_at)",
		ddl:     "ALTER TABLE traffic_records ADD KEY idx_traffic_records_subscription_time (subscription_id, record_at)",
	},
	{
		table:   "traffic_records",
		name:    "idx_traffic_records_subscription_created_at",
		columns: "(subscription_id, created_at)",
		ddl:     "ALTER TABLE traffic_records ADD KEY idx_traffic_records_subscription_created_at (subscription_id, created_at)",
	},
	{
		table:   "traffic_records",
		name:    "idx_traffic_records_subscription_usage",
		columns: "(subscription_id, used_bytes)",
		ddl:     "ALTER TABLE traffic_records ADD KEY idx_traffic_records_subscription_usage (subscription_id, used_bytes)",
	},
	{
		table:   "traffic_records",
		name:    "idx_traffic_records_node_time",
		columns: "(node_id, record_at)",
		ddl:     "ALTER TABLE traffic_records ADD KEY idx_traffic_records_node_time (node_id, record_at)",
	},
	{
		table:   "traffic_records",
		name:    "idx_traffic_records_endpoint_time",
		columns: "(protocol_endpoint_id, record_at)",
		ddl:     "ALTER TABLE traffic_records ADD KEY idx_traffic_records_endpoint_time (protocol_endpoint_id, record_at)",
	},
	{
		table:   "principal_flow_observations",
		name:    "idx_principal_flow_observation_user_timeline",
		columns: "(user_id, node_id, principal_key, observed_at, id)",
		ddl:     "ALTER TABLE principal_flow_observations ADD KEY idx_principal_flow_observation_user_timeline (user_id, node_id, principal_key, observed_at, id)",
	},
	{
		table:   "principal_flow_observations",
		name:    "idx_principal_flow_observation_subscription_timeline",
		columns: "(subscription_id, node_id, principal_key, observed_at, id)",
		ddl:     "ALTER TABLE principal_flow_observations ADD KEY idx_principal_flow_observation_subscription_timeline (subscription_id, node_id, principal_key, observed_at, id)",
	},
	{
		table:   "principal_flow_scope_observations",
		name:    "idx_principal_flow_scope_boundary_timeline",
		columns: "(scope_type, scope_id, source, node_id, observed_at, id)",
		ddl:     "ALTER TABLE principal_flow_scope_observations ADD KEY idx_principal_flow_scope_boundary_timeline (scope_type, scope_id, source, node_id, observed_at, id)",
	},
	{
		table:   "principal_flow_currents",
		name:    "idx_principal_flow_current_endpoint_usage",
		columns: "(protocol_endpoint_id, active_flows, user_id, observed_at)",
		ddl:     "ALTER TABLE principal_flow_currents ADD KEY idx_principal_flow_current_endpoint_usage (protocol_endpoint_id, active_flows, user_id, observed_at)",
	},
}

// ReconcileTrafficReadSchema installs the composite indexes used by bounded
// traffic and Principal-flow timelines. Existing single-column indexes remain
// useful to other read paths, while these indexes prevent trend queries from
// scanning a user's or dimension's complete history before applying time.
// The narrow subscription/usage index lets reconciliation aggregate the ledger
// without fetching every full record (including event metadata) from the table.
func ReconcileTrafficReadSchema(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database is required")
	}
	if IsSQLite(db) {
		for _, index := range trafficReadIndexes {
			if !db.Migrator().HasTable(index.table) {
				continue
			}
			statement := fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON %s %s", index.name, index.table, index.columns)
			if err := db.Exec(statement).Error; err != nil {
				return fmt.Errorf("add SQLite traffic read index %s: %w", index.name, err)
			}
		}
		if err := reconcileSQLiteTrafficBuckets(db); err != nil {
			return err
		}
		return reconcileSQLiteLedgerRevision(db)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("open traffic read schema database: %w", err)
	}
	// Keep inventory order deterministic while collecting each table's missing
	// indexes into one DDL operation. Existing tables still receive the same
	// per-index inspection and additive recovery as before.
	var tables []string
	byTable := make(map[string][]trafficReadIndexDefinition)
	for _, index := range trafficReadIndexes {
		if _, seen := byTable[index.table]; !seen {
			tables = append(tables, index.table)
		}
		byTable[index.table] = append(byTable[index.table], index)
	}
	for _, table := range tables {
		exists, err := tableExists(sqlDB, table)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		if err := ensureTrafficReadIndexes(sqlDB, table, byTable[table]); err != nil {
			return err
		}
	}
	return nil
}

func ensureTrafficReadIndexes(sqlDB *sql.DB, table string, indexes []trafficReadIndexDefinition) error {
	var additions, names []string
	for _, index := range indexes {
		var count int
		if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM information_schema.statistics
		WHERE table_schema = DATABASE() AND table_name = ? AND index_name = ?`, table, index.name).Scan(&count); err != nil {
			return fmt.Errorf("inspect traffic read index %s: %w", index.name, err)
		}
		if count > 0 {
			continue
		}
		additions = append(additions, fmt.Sprintf("ADD KEY %s %s", index.name, index.columns))
		names = append(names, index.name)
	}
	if len(additions) == 0 {
		return nil
	}
	if _, err := sqlDB.Exec("ALTER TABLE " + table + " " + strings.Join(additions, ", ")); err != nil {
		return fmt.Errorf("add traffic read indexes %s.%s: %w", table, strings.Join(names, ","), err)
	}
	return nil
}
