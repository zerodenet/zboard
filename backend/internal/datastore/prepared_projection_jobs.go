package datastore

import (
	"fmt"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

// This fixed ledger contract follows published 0007/0008/0014/0015 DDL.
// Keep it local: importing the jobstore adapter into datastore would create
// a test import cycle in jobstore's existing integration tests. No runtime or
// Store is constructed to validate a prepared database.
func projectionJobColumns(db *gorm.DB, tables map[string]bool) error {
	contracts := []struct{ table, primary, columns string }{
		{"job_execution_budget", "id", "id:u,capacity:i,revision:u,dispatch_virtual_time:u"},
		{"job_execution_groups", "id", "id:s160,capacity:i"},
		{"job_dispatch_lanes", "lane", "lane:s200,virtual_finish:u"},
		{"job_runs", "id", "id:s36,timeout_ms:n,attempt_count:j,max_attempts:j,retry_backoff_ms:n,execution_group:s160,dispatch_lane:s200,owner:s200,key:s160,handler:s320,resource:s200,payload:s,fingerprint:s64,state:s24,not_before:t,created_at:t,finished_at:t?,token:s36,worker:s160,expires_at:t?,schedule_id:s320,planned_at:t?"},
		{"job_attempts", "token", "token:s36,run_id:s36,attempt_number:j,worker:s160,state:s24,started_at:t,expires_at:t,finished_at:t?"},
		{"job_schedules", "id", "id:s320,owner:s200,name:s160,handler:s320,resource:s200,execution_group:s160,revision:s160,interval_ms:n,timeout_ms:n,max_attempts:j,retry_backoff_ms:n,timezone:s64,misfire_policy:s24,dispatch_lane:s200,anchor_at:t?,next_at:t,sequence:u,run_id:s36,runs:u,failures:u,missed_runs:u,state:s24,last_state:s24,last_started_at:t?,last_finished_at:t?,compact_history:b,compact_run_id:s36"},
	}
	for _, contract := range contracts {
		if !tables[contract.table] {
			return projectionError("job table missing: "+contract.table, nil)
		}
		columns, primary, err := projectionColumns(db, contract.table)
		if err != nil {
			return projectionError("job column metadata: "+contract.table, err)
		}
		if strings.Join(primary, ",") != contract.primary {
			return projectionError("job primary key differs: "+contract.table, nil)
		}
		for _, spec := range strings.Split(contract.columns, ",") {
			pair := strings.SplitN(spec, ":", 2)
			name, kind := pair[0], pair[1]
			column, ok := columns[name]
			if !ok {
				return projectionError("job column missing: "+contract.table+"."+name, nil)
			}
			nullable := strings.HasSuffix(kind, "?")
			kind = strings.TrimSuffix(kind, "?")
			// SQLite's legacy TEXT PRIMARY KEY metadata is nullable; the key shape is
			// checked separately. Do not reject the published SQLite schema for that.
			if !nullable && column.nullable && name != contract.primary {
				return projectionError("job column nullable: "+contract.table+"."+name, nil)
			}
			valid := false
			switch {
			case strings.HasPrefix(kind, "s"):
				valid = strings.Contains(column.kind, "char") || strings.Contains(column.kind, "text")
				if !IsSQLite(db) {
					valid = valid && column.collation == "utf8mb4_bin"
					if len(kind) > 1 {
						minimum, e := strconv.Atoi(kind[1:])
						var actual int
						_, scanErr := fmt.Sscanf(column.kind, "varchar(%d)", &actual)
						valid = valid && e == nil && scanErr == nil && actual >= minimum
					}
				}
			case kind == "t":
				valid = strings.HasPrefix(column.kind, "datetime")
				if !IsSQLite(db) {
					valid = column.kind == "datetime(3)"
				}
			case kind == "b":
				valid = column.kind == "numeric" || column.kind == "tinyint(1)"
			case kind == "n":
				valid = strings.Contains(column.kind, "int")
				if !IsSQLite(db) {
					valid = column.kind == "bigint"
				}
			case kind == "u":
				valid = strings.Contains(column.kind, "int")
				if !IsSQLite(db) {
					valid = column.kind == "bigint unsigned"
				}
			case kind == "j":
				valid = strings.Contains(column.kind, "int")
				if !IsSQLite(db) {
					valid = column.kind == "int unsigned"
				}
			case kind == "i":
				valid = strings.Contains(column.kind, "int")
				if !IsSQLite(db) {
					valid = column.kind == "int"
				}
			}
			if !valid {
				return projectionError("job column type differs: "+contract.table+"."+name, nil)
			}
		}
	}
	return nil
}
