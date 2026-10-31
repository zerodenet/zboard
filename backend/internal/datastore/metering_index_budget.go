package datastore

import (
	"database/sql"
	"fmt"
	"gorm.io/gorm"
	"slices"
	"strings"
)

type meteringIndexDrop struct {
	columns     string
	names       []string
	replacement string
}

var meteringIndexBudget = map[string][]meteringIndexDrop{
	"flow_usages": {
		{"node_id", []string{"idx_flow_usages_node_id"}, "node_id,flow_id"},
		{"protocol_endpoint_id", []string{"idx_flow_usages_endpoint", "idx_flow_usages_protocol_endpoint_id"}, "protocol_endpoint_id,status,last_seen_at"},
		{"principal_key", []string{"idx_flow_usages_principal", "idx_flow_usages_principal_key"}, ""},
		{"status,last_seen_at", []string{"idx_flow_usages_status_seen"}, ""},
		{"status", []string{"idx_flow_usages_status"}, ""},
		{"last_seen_at", []string{"idx_flow_usages_last_seen_at"}, ""},
	},
	"traffic_records": {
		{"user_id", []string{"idx_traffic_user", "idx_traffic_records_user_id"}, "user_id,record_at"},
		{"subscription_id", []string{"idx_traffic_records_subscription_id"}, "subscription_id,record_at"},
		{"node_id", []string{"idx_traffic_node", "idx_traffic_records_node_id"}, "node_id,record_at"},
		{"protocol_endpoint_id", []string{"idx_traffic_protocol_endpoint", "idx_traffic_records_protocol_endpoint_id"}, "protocol_endpoint_id,record_at"},
		{"record_at", []string{"idx_traffic_time", "idx_traffic_records_at"}, "record_at,id"},
	},
}

// Called after scoped read indexes are prepared, before any worker starts.
// No financial row, uniqueness constraint or FK-supporting credential/subscription
// index is removed. Names and complete shapes must BOTH match this fixed budget.
// Runtime flow queries use node+flow uniqueness and endpoint+status+seen; the
// removed principal/status-only access paths have no core query owner.
func reconcileMeteringIndexBudget(db *gorm.DB) error {
	for _, table := range []string{"flow_usages", "traffic_records"} {
		if !db.Migrator().HasTable(table) {
			continue
		}
		indexes, err := meteringBudgetIndexes(db, table)
		if err != nil {
			return err
		}
		byName := map[string]meteringBudgetIndex{}
		for _, idx := range indexes {
			byName[idx.name] = idx
		}
		// SQLite's (record_at,rowid) single-column B-tree already supplies the
		// exact cursor ordering. Reuse it rather than building a large new index.
		sqliteTimeIndex := ""
		if IsSQLite(db) && table == "traffic_records" {
			for _, name := range []string{"idx_traffic_records_at", "idx_traffic_records_history_cursor", "idx_traffic_time"} {
				if idx, ok := byName[name]; ok && !idx.unique && (strings.Join(idx.columns, ",") == "record_at" || strings.Join(idx.columns, ",") == "record_at,id") {
					sqliteTimeIndex = name
					break
				}
			}
		}
		var drops []string
		for _, policy := range meteringIndexBudget[table] {
			for _, name := range policy.names {
				idx, ok := byName[name]
				if !ok {
					continue
				}
				if idx.unique || !slices.Equal(idx.columns, strings.Split(policy.columns, ",")) {
					return fmt.Errorf("metering index budget refuses unknown shape: %s.%s", table, name)
				}
				if IsSQLite(db) && table == "traffic_records" && policy.columns == "record_at" && name == sqliteTimeIndex {
					continue
				}
				if policy.replacement != "" {
					found := false
					for _, candidate := range indexes {
						if strings.Join(candidate.columns, ",") == policy.replacement || (IsSQLite(db) && table == "traffic_records" && policy.columns == "record_at" && candidate.name == sqliteTimeIndex && candidate.name != name) {
							found = true
							break
						}
					}
					if !found {
						return fmt.Errorf("metering index replacement missing: %s(%s)", table, policy.replacement)
					}
				}
				drops = append(drops, name)
			}
		}
		if len(drops) == 0 {
			continue
		}
		if IsSQLite(db) {
			for _, name := range drops {
				if err := db.Exec("DROP INDEX " + name).Error; err != nil {
					return err
				}
			}
			continue
		}
		// Use one pinned session; a busy metadata lock fails promptly instead of
		// holding upgrade shutdown indefinitely. Never mutate the pool's globals.
		if err := db.Connection(func(conn *gorm.DB) error {
			var previous int
			if err := conn.Raw("SELECT @@SESSION.lock_wait_timeout").Scan(&previous).Error; err != nil {
				return err
			}
			if err := conn.Exec("SET SESSION lock_wait_timeout = 5").Error; err != nil {
				return err
			}
			defer conn.Exec(fmt.Sprintf("SET SESSION lock_wait_timeout = %d", previous))
			clauses := make([]string, 0, len(drops))
			for _, name := range drops {
				clauses = append(clauses, "DROP INDEX "+name)
			}
			return conn.Exec("ALTER TABLE " + table + " " + strings.Join(clauses, ",") + ", ALGORITHM=INPLACE, LOCK=NONE").Error
		}); err != nil {
			return err
		}
	}
	return nil
}

type meteringBudgetIndex struct {
	name    string
	columns []string
	unique  bool
}

func meteringBudgetIndexes(db *gorm.DB, table string) ([]meteringBudgetIndex, error) {
	if !IsSQLite(db) {
		raw, err := db.Migrator().GetIndexes(table)
		if err != nil {
			return nil, err
		}
		out := make([]meteringBudgetIndex, 0, len(raw))
		for _, idx := range raw {
			unique, known := idx.Unique()
			if !known {
				return nil, fmt.Errorf("index uniqueness unavailable: %s.%s", table, idx.Name())
			}
			out = append(out, meteringBudgetIndex{idx.Name(), idx.Columns(), unique})
		}
		return out, nil
	}
	// GORM's SQLite GetIndexes cannot decode expression indexes (NULL column
	// names). Existing read models own expression indexes, so inspect nullable
	// metadata directly and never mistake a partial/expression index for coverage.
	var rows []struct {
		Name       string
		IsUnique   int
		Partial    int
		ColumnName sql.NullString
	}
	err := db.Raw(`SELECT il.name AS name,il."unique" AS is_unique,il.partial,ii.name AS column_name FROM pragma_index_list(?) il JOIN pragma_index_info(il.name) ii ORDER BY il.name,ii.seqno`, table).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	var out []meteringBudgetIndex
	for _, row := range rows {
		if len(out) == 0 || out[len(out)-1].name != row.Name {
			out = append(out, meteringBudgetIndex{name: row.Name, unique: row.IsUnique != 0})
		}
		column := row.ColumnName.String
		if !row.ColumnName.Valid || row.Partial != 0 {
			column = "<unsupported>"
		}
		out[len(out)-1].columns = append(out[len(out)-1].columns, column)
	}
	return out, nil
}
