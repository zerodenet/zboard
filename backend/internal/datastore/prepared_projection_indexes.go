package datastore

import (
	"database/sql"
	"errors"
	"gorm.io/gorm"
	"strings"
)

type projectionIndex struct {
	columns string
	unique  bool
}

// Required access/dedup shapes, not a replacement for deployment migrations.
// Accept equivalent auto-generated SQLite names for inline UNIQUE constraints.
var projectionRequiredIndexes = map[string][]projectionIndex{
	"job_execution_groups":              {{"id", true}},
	"job_dispatch_lanes":                {{"lane", true}},
	"job_schedules":                     {{"id", true}},
	"protocol_endpoint_usage_daily":     {{"protocol_endpoint_id,usage_date", true}},
	"traffic_usage_hourly":              {{"record_at,user_id,subscription_id,node_id,protocol_endpoint_id,protocol_multiplier_milli", true}},
	"job_runs":                          {{"id", true}, {"owner,key", true}, {"state,not_before", false}, {"resource,state", false}, {"execution_group,state", false}, {"schedule_id", false}, {"dispatch_lane,state,not_before", false}, {"schedule_id,planned_at", true}},
	"job_attempts":                      {{"token", true}, {"run_id,attempt_number", true}},
	"system_configs":                    {{"config_key", true}},
	"metering_usage_projection_pending": {{"protocol_endpoint_id,traffic_record_id", false}, {"hourly_pending,record_at", false}, {"hourly_pending,user_id,record_at", false}, {"hourly_pending,subscription_id,record_at", false}, {"hourly_pending,node_id,record_at", false}, {"hourly_pending,protocol_endpoint_id,record_at", false}},
	"node_observation_intents":          {{"status,available_at,id", false}},
	"node_observation_retirements":      {{"state,review_at,intent_id", false}},
}

func projectionIndexes(db *gorm.DB) error {
	for _, table := range []string{"job_execution_groups", "job_dispatch_lanes", "job_schedules", "job_runs", "job_attempts", "system_configs", "metering_usage_projection_pending", "protocol_endpoint_usage_daily", "traffic_usage_hourly", "node_observation_intents", "node_observation_retirements"} {
		actual, err := projectionTableIndexes(db, table)
		if err != nil {
			return projectionError("index metadata: "+table, err)
		}
		for _, want := range projectionRequiredIndexes[table] {
			found := false
			for _, got := range actual {
				found = found || got == want
			}
			if !found {
				return projectionError("required index shape missing: "+table+"("+want.columns+")", nil)
			}
		}
	}
	if !IsSQLite(db) {
		// The pending queue must protect immutable source facts from deletion.
		rows, err := db.Raw(`SELECT k.COLUMN_NAME,k.REFERENCED_TABLE_SCHEMA,k.REFERENCED_TABLE_NAME,k.REFERENCED_COLUMN_NAME,r.DELETE_RULE,r.UPDATE_RULE
FROM INFORMATION_SCHEMA.KEY_COLUMN_USAGE k JOIN INFORMATION_SCHEMA.REFERENTIAL_CONSTRAINTS r
ON r.CONSTRAINT_SCHEMA=k.CONSTRAINT_SCHEMA AND r.TABLE_NAME=k.TABLE_NAME AND r.CONSTRAINT_NAME=k.CONSTRAINT_NAME
WHERE k.TABLE_SCHEMA=DATABASE() AND k.TABLE_NAME='metering_usage_projection_pending' AND k.REFERENCED_TABLE_NAME IS NOT NULL AND k.REFERENCED_TABLE_SCHEMA=DATABASE()`).Rows()
		if err != nil {
			return projectionError("usage foreign key metadata", err)
		}
		found := false
		for rows.Next() {
			var column, schema, table, target, del, update string
			if err = rows.Scan(&column, &schema, &table, &target, &del, &update); err != nil {
				break
			}
			_ = schema // constrained to DATABASE() by the same metadata query.
			if column == "traffic_record_id" && table == "traffic_records" && target == "id" && del == "RESTRICT" && (update == "RESTRICT" || update == "NO ACTION") {
				found = true
			}
		}
		err = errors.Join(err, rows.Err(), rows.Close())
		if err != nil {
			return projectionError("usage foreign key metadata", err)
		}
		if !found {
			return projectionError("usage source protection missing", nil)
		}
	}
	return nil
}
func projectionTableIndexes(db *gorm.DB, table string) (result []projectionIndex, err error) {
	if IsSQLite(db) {
		rows, e := db.Raw("PRAGMA index_list(" + table + ")").Rows()
		if e != nil {
			return nil, e
		}
		type entry struct {
			name   string
			unique bool
		}
		var entries []entry
		for rows.Next() {
			var seq, unique, partial int
			var name, origin string
			if e = rows.Scan(&seq, &name, &unique, &origin, &partial); e != nil {
				break
			}
			if partial == 0 {
				entries = append(entries, entry{name, unique != 0})
			}
		}
		err = errors.Join(e, rows.Err(), rows.Close())
		if err != nil {
			return nil, err
		}
		for _, index := range entries {
			// Index identifiers originate in SQLite metadata, not request input.
			quoted := "\"" + strings.ReplaceAll(index.name, "\"", "\"\"") + "\""
			rows, e = db.Raw("PRAGMA index_xinfo(" + quoted + ")").Rows()
			if e != nil {
				return nil, e
			}
			var columns []string
			valid := true
			for rows.Next() {
				var seq, cid, descending, key int
				var name, collation sql.NullString
				if e = rows.Scan(&seq, &cid, &name, &descending, &collation, &key); e != nil {
					break
				}
				if key == 0 {
					continue
				}
				if cid < 0 || !name.Valid || descending != 0 || !collation.Valid || !strings.EqualFold(collation.String, "BINARY") {
					valid = false
				}
				columns = append(columns, strings.ToLower(name.String))
			}
			err = errors.Join(e, rows.Err(), rows.Close())
			if err != nil {
				return nil, err
			}
			if valid {
				result = append(result, projectionIndex{strings.Join(columns, ","), index.unique})
			}
		}
		return result, nil
	}
	rows, e := db.Raw("SELECT INDEX_NAME,SEQ_IN_INDEX,COLUMN_NAME,NON_UNIQUE,SUB_PART,INDEX_TYPE,COLLATION,IS_VISIBLE FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? ORDER BY INDEX_NAME,SEQ_IN_INDEX", table).Rows()
	if e != nil {
		return nil, e
	}
	var columns []string
	last := ""
	unique, valid := false, true
	flush := func() {
		if last != "" && valid {
			result = append(result, projectionIndex{strings.Join(columns, ","), unique})
		}
	}
	for rows.Next() {
		var index, kind, visible string
		var seq, nonunique int
		var name, order sql.NullString
		var prefix sql.NullInt64
		if e = rows.Scan(&index, &seq, &name, &nonunique, &prefix, &kind, &order, &visible); e != nil {
			break
		}
		if index != last {
			flush()
			last = index
			columns = nil
			unique = nonunique == 0
			valid = true
		}
		valid = valid && seq == len(columns)+1 && name.Valid && !prefix.Valid && kind == "BTREE" && order.Valid && order.String == "A" && visible == "YES"
		columns = append(columns, strings.ToLower(name.String))
	}
	flush()
	err = errors.Join(e, rows.Err(), rows.Close())
	return
}
