package datastore

import (
	"database/sql"
	"fmt"

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
}

// ReconcileTrafficReadSchema installs the composite indexes used by bounded
// traffic and Principal-flow timelines. Existing single-column indexes remain
// useful to other read paths, while these indexes prevent trend queries from
// scanning a user's or dimension's complete history before applying time.
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
		return nil
	}
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("open traffic read schema database: %w", err)
	}
	for _, index := range trafficReadIndexes {
		exists, err := tableExists(sqlDB, index.table)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		if err := ensureTrafficReadIndex(sqlDB, index); err != nil {
			return err
		}
	}
	return nil
}

func ensureTrafficReadIndex(sqlDB *sql.DB, index trafficReadIndexDefinition) error {
	var count int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM information_schema.statistics
		WHERE table_schema = DATABASE() AND table_name = ? AND index_name = ?`, index.table, index.name).Scan(&count); err != nil {
		return fmt.Errorf("inspect traffic read index %s: %w", index.name, err)
	}
	if count > 0 {
		return nil
	}
	if _, err := sqlDB.Exec(index.ddl); err != nil {
		return fmt.Errorf("add traffic read index %s: %w", index.name, err)
	}
	return nil
}
