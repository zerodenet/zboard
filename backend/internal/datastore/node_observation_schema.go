package datastore

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/observationstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const nodeObservationSchemaVersion = "0060_node_observation_handoff.up.sql"

var observationTables = []string{"node_observation_intents", "node_observation_parts", "node_observation_states"}
var observationColumns = map[string][]string{
	"node_observation_intents": {"id", "node_id", "asset_tag", "status", "available_at", "token", "lease_until", "material_bytes", "part_count", "attempts", "reason", "created_at", "updated_at"},
	"node_observation_parts":   {"intent_id", "ordinal", "data"},
	"node_observation_states":  {"node_id", "asset_tag", "latest_known", "stats_known", "latest_reference", "stats_reference", "floor_reference", "deleted", "current_known", "current_source", "counter_written_stamp", "counter_written_at", "latest_occurred_at", "latest_received_at", "stats_occurred_at", "stats_received_at", "updated_at"},
}

// Inspect before any AutoMigrate. A completed receipt or existing source facts
// cannot be converted into empty recreated evidence tables. This is structural;
// per-item damaged sources remain retained/held by the independent worker.
func observationSchema(db *gorm.DB) (tables map[string]bool, receipt bool, err error) {
	tables, err = migrationTableSet(db)
	if err != nil {
		return
	}
	if tables["schema_migrations"] {
		var n int64
		err = db.Table("schema_migrations").Where("version = ?", nodeObservationSchemaVersion).Count(&n).Error
		if err != nil {
			return
		}
		receipt = n > 0
	}
	missing := false
	authority := false
	for _, table := range observationTables {
		if !tables[table] {
			missing = true
			continue
		}
		var columns map[string]bool
		columns, err = observationColumnNames(db, table)
		if err != nil {
			return
		}
		for _, name := range observationColumns[table] {
			if !columns[name] {
				err = fmt.Errorf("observation evidence table %s lost column %s; restore before startup/copy", table, name)
				return
			}
		}
		if err = validateObservationShape(db, table); err != nil {
			return
		}
		var exists struct{ Present int }
		q := db.Table(table).Select("1 AS present").Limit(1).Scan(&exists)
		err = q.Error
		if err != nil {
			return
		}
		authority = authority || q.RowsAffected > 0
	}
	if missing && (receipt || authority) {
		err = errors.New("observation evidence tables missing; restore before startup/copy")
	}
	return
}
func observationColumnNames(db *gorm.DB, table string) (columns map[string]bool, err error) {
	if !IsSQLite(db) {
		return mysqlColumnNames(db, table)
	}
	// Only this fixed inventory may be interpolated into PRAGMA.
	allowed := false
	for _, name := range observationTables {
		allowed = allowed || name == table
	}
	if !allowed {
		return nil, errors.New("unknown observation table")
	}
	rows, err := db.Raw("PRAGMA table_info(" + table + ")").Rows()
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	columns = map[string]bool{}
	for rows.Next() {
		var id, required, pk int
		var name, kind string
		var def any
		if err := rows.Scan(&id, &name, &kind, &required, &def, &pk); err != nil {
			return nil, err
		}
		columns[strings.ToLower(name)] = true
	}
	return columns, rows.Err()
}
func preflightNodeObservationSchema(db *gorm.DB) error {
	_, _, err := observationSchema(db)
	return err
}
func reconcileNodeObservationSchema(db *gorm.DB) error {
	_, receipt, err := observationSchema(db)
	if err != nil {
		return err
	}
	if receipt {
		return ensureObservationReadyIndex(db)
	}
	asset := nodeObservationSchemaVersion
	if IsSQLite(db) {
		asset = "sqlite/0060_node_observation_handoff.sql"
	}
	raw, err := migrations.Files.ReadFile(asset)
	if err != nil {
		return err
	}
	statements, err := splitMigrationStatements(string(raw))
	if err != nil {
		return err
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			return err
		}
	}
	if err := ensureObservationReadyIndex(db); err != nil {
		return err
	}
	// Re-entry of interrupted initialization preserves every previously imported
	// source; a changed floor is an explicit failure, never fabricated Stats.
	if err := importObservationFloors(db); err != nil {
		return err
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: nodeObservationSchemaVersion, AppliedAt: time.Now().UTC()}).Error
}
func importObservationFloors(db *gorm.DB) error {
	tables, err := migrationTableSet(db)
	if err != nil {
		return err
	}
	if !tables["zero_event_node_cursors"] {
		return nil
	}
	var last uint
	for {
		var rows []struct {
			NodeID                   uint
			CoreInstanceID           string
			Sequence, ConfigRevision uint64
			OccurredAt               time.Time
		}
		// One row keeps even an old SQLite TEXT CoreID bounded by one source.
		if err := db.Table("zero_event_node_cursors").Where("node_id > ?", last).Order("node_id").Limit(1).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		row := rows[0]
		last = row.NodeID
		if err := db.Transaction(func(tx *gorm.DB) error {
			position := network.EventPosition{CoreInstanceID: row.CoreInstanceID, Sequence: row.Sequence, ConfigRevision: row.ConfigRevision, OccurredAt: row.OccurredAt}
			id, err := observationstore.Enqueue(tx, network.ObservationSource{Observation: network.NodeObservation{NodeID: row.NodeID, Latest: position}, Kind: "legacy_floor", LegacyFloor: true})
			if err != nil {
				return err
			}
			state := model.NodeObservationState{NodeID: row.NodeID, AssetTag: "", FloorReference: id, CurrentSource: "unattributed_floor", UpdatedAt: time.Now().UTC()}
			r := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&state)
			if r.Error != nil {
				return r.Error
			}
			if r.RowsAffected == 0 {
				var existing model.NodeObservationState
				if err := tx.Where("node_id = ? AND asset_tag = ?", row.NodeID, "").First(&existing).Error; err != nil {
					return err
				}
				if existing.FloorReference != id {
					return errors.New("legacy observation floor changed during initialization")
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}
}

// Complete shapes are checked on every owned entry/copy, not cached across DDL.
// In particular a nodes FK would reintroduce financial producer→node waits.
func validateObservationShape(db *gorm.DB, table string) (err error) {
	mysqlTypes := map[string]string{"id": "varchar(64)", "node_id": "bigint unsigned", "asset_tag": "varchar(64)", "status": "varchar(24)", "available_at": "datetime(3)", "token": "varchar(64)", "lease_until": "datetime(3)", "material_bytes": "bigint unsigned", "part_count": "bigint unsigned", "attempts": "bigint unsigned", "reason": "varchar(64)", "created_at": "datetime(3)", "updated_at": "datetime(3)", "intent_id": "varchar(64)", "ordinal": "bigint unsigned", "data": "blob", "latest_reference": "varchar(64)", "stats_reference": "varchar(64)", "floor_reference": "varchar(64)", "deleted": "tinyint(1)", "current_known": "tinyint(1)", "latest_known": "tinyint(1)", "stats_known": "tinyint(1)", "current_source": "varchar(32)", "counter_written_at": "datetime(3)", "counter_written_stamp": "varchar(64)", "latest_occurred_at": "datetime(3)", "latest_received_at": "datetime(3)", "stats_occurred_at": "datetime(3)", "stats_received_at": "datetime(3)"}
	primary := map[string][]string{"node_observation_intents": {"id"}, "node_observation_parts": {"intent_id", "ordinal"}, "node_observation_states": {"node_id", "asset_tag"}}
	actualPrimary := map[int]string{}
	if IsSQLite(db) {
		rows, e := db.Raw("PRAGMA table_info(" + table + ")").Rows()
		if e != nil {
			return e
		}
		for rows.Next() {
			var ordinal, required, pk int
			var name, kind string
			var def any
			if e = rows.Scan(&ordinal, &name, &kind, &required, &def, &pk); e != nil {
				break
			}
			name = strings.ToLower(name)
			expected, ok := mysqlTypes[name]
			if !ok {
				continue
			}
			switch {
			case strings.Contains(expected, "varchar"):
				expected = "text"
			case strings.HasPrefix(expected, "bigint"), strings.HasPrefix(expected, "tinyint"):
				expected = "integer"
			case strings.HasPrefix(expected, "datetime"):
				expected = "datetime"
			}
			if !strings.EqualFold(kind, expected) || (required == 0 && !observationNullable(name)) {
				e = fmt.Errorf("observation column %s.%s shape differs", table, name)
				break
			}
			if pk > 0 {
				actualPrimary[pk] = name
			}
		}
		e = errors.Join(e, rows.Err(), rows.Close())
		if e != nil {
			return e
		}
	} else {
		rows, e := db.Raw("SELECT COLUMN_NAME,COLUMN_TYPE,IS_NULLABLE,COLLATION_NAME FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?", table).Rows()
		if e != nil {
			return e
		}
		for rows.Next() {
			var name, kind, nullable string
			var collation sql.NullString
			if e = rows.Scan(&name, &kind, &nullable, &collation); e != nil {
				break
			}
			if expected, ok := mysqlTypes[strings.ToLower(name)]; ok && (!strings.EqualFold(kind, expected) || (nullable != "NO" && !observationNullable(name)) || (expected == "varchar(64)" && (!collation.Valid || collation.String != "ascii_bin"))) {
				e = fmt.Errorf("observation column %s.%s shape differs", table, name)
				break
			}
		}
		e = errors.Join(e, rows.Err(), rows.Close())
		if e != nil {
			return e
		}
		rows, e = db.Raw("SELECT SEQ_IN_INDEX,COLUMN_NAME,SUB_PART FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND INDEX_NAME = 'PRIMARY' ORDER BY SEQ_IN_INDEX", table).Rows()
		if e != nil {
			return e
		}
		for rows.Next() {
			var seq int
			var name string
			var prefix sql.NullInt64
			if e = rows.Scan(&seq, &name, &prefix); e != nil {
				break
			}
			if prefix.Valid {
				e = errors.New("observation primary key must not use prefixes")
				break
			}
			actualPrimary[seq] = strings.ToLower(name)
		}
		e = errors.Join(e, rows.Err(), rows.Close())
		if e != nil {
			return e
		}
	}
	// SQLite reports composite TEXT/INTEGER NOT NULL keys and their positions.
	if len(actualPrimary) == 0 && IsSQLite(db) {
		return errors.New("missing observation primary key")
	}
	if !IsSQLite(db) || len(actualPrimary) > 0 {
		for i, name := range primary[table] {
			if actualPrimary[i+1] != name {
				return fmt.Errorf("observation primary key differs: %s", table)
			}
		}
		if len(actualPrimary) != len(primary[table]) {
			return errors.New("extra observation primary-key columns")
		}
	}
	if IsSQLite(db) {
		rows, e := db.Raw("PRAGMA foreign_key_list(" + table + ")").Rows()
		if e != nil {
			return e
		}
		has := rows.Next()
		e = errors.Join(rows.Err(), rows.Close())
		if e != nil {
			return e
		}
		if has {
			return errors.New("observation tables must have no foreign keys")
		}
	} else {
		var n int64
		if e := db.Raw("SELECT COUNT(*) FROM INFORMATION_SCHEMA.KEY_COLUMN_USAGE WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND REFERENCED_TABLE_NAME IS NOT NULL", table).Scan(&n).Error; e != nil {
			return e
		}
		if n != 0 {
			return errors.New("observation tables must have no foreign keys")
		}
	}
	return nil
}

func ensureObservationReadyIndex(db *gorm.DB) (err error) {
	var columns []string
	if IsSQLite(db) {
		ownerRows, e := db.Raw("SELECT tbl_name FROM sqlite_master WHERE type = 'index' AND name = 'idx_node_observation_ready'").Rows()
		if e != nil {
			return e
		}
		for ownerRows.Next() {
			var owner string
			if e = ownerRows.Scan(&owner); e != nil {
				break
			}
			if owner != "node_observation_intents" {
				e = errors.New("observation ready index belongs to another table")
				break
			}
		}
		e = errors.Join(e, ownerRows.Err(), ownerRows.Close())
		if e != nil {
			return e
		}

		listRows, e := db.Raw("PRAGMA index_list(node_observation_intents)").Rows()
		if e != nil {
			return e
		}
		for listRows.Next() {
			var seq, unique, partial int
			var name, origin string
			if e = listRows.Scan(&seq, &name, &unique, &origin, &partial); e != nil {
				break
			}
			if name == "idx_node_observation_ready" && (unique != 0 || partial != 0 || origin != "c") {
				e = errors.New("observation ready index must be nonunique and complete")
				break
			}
		}
		e = errors.Join(e, listRows.Err(), listRows.Close())
		if e != nil {
			return e
		}
		rows, e := db.Raw("PRAGMA index_xinfo(idx_node_observation_ready)").Rows()
		if e != nil {
			return e
		}
		for rows.Next() {
			var seq, cid, descending, key int
			var name, collation sql.NullString
			if e = rows.Scan(&seq, &cid, &name, &descending, &collation, &key); e != nil {
				break
			}
			if key == 0 {
				continue
			}
			if descending != 0 || cid < 0 || !name.Valid || !collation.Valid || collation.String != "BINARY" {
				e = errors.New("observation ready index order/expression/collation differs")
				break
			}
			columns = append(columns, strings.ToLower(name.String))
		}
		err = errors.Join(e, rows.Err(), rows.Close())
	} else {
		rows, e := db.Raw("SELECT COLUMN_NAME,NON_UNIQUE,SUB_PART,INDEX_TYPE FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'node_observation_intents' AND INDEX_NAME = 'idx_node_observation_ready' ORDER BY SEQ_IN_INDEX").Rows()
		if e != nil {
			return e
		}
		for rows.Next() {
			var name, kind string
			var unique int
			var prefix sql.NullInt64
			if e = rows.Scan(&name, &unique, &prefix, &kind); e != nil {
				break
			}
			if unique != 1 || prefix.Valid || kind != "BTREE" {
				e = errors.New("observation ready index shape differs")
				break
			}
			columns = append(columns, strings.ToLower(name))
		}
		err = errors.Join(e, rows.Err(), rows.Close())
	}
	if err != nil {
		return err
	}
	if len(columns) == 0 {
		return db.Exec("CREATE INDEX idx_node_observation_ready ON node_observation_intents(status,available_at,id)").Error
	}
	if strings.Join(columns, ",") != "status,available_at,id" {
		return errors.New("observation ready index shape differs")
	}
	return nil
}

func observationNullable(name string) bool {
	switch strings.ToLower(name) {
	case "counter_written_at", "latest_occurred_at", "latest_received_at", "stats_occurred_at", "stats_received_at":
		return true
	}
	return false
}
