package datastore

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const nodeObservationMaterialVersion = "0061_node_observation_material_retention.up.sql"

type observationMaterialColumn struct {
	Kind     string
	Nullable bool
	Default  sql.NullString
}
type observationMaterialIndex struct {
	Table, Name string
	Columns     []string
}

var observationMaterialIndexes = []observationMaterialIndex{
	{"node_observation_intents", "idx_observation_material_review", []string{"status", "material_retired_at", "material_review_at", "id"}},
	{"node_observation_states", "idx_observation_latest_reference", []string{"latest_reference"}},
	{"node_observation_states", "idx_observation_stats_reference", []string{"stats_reference"}},
	{"node_observation_states", "idx_observation_floor_reference", []string{"floor_reference"}},
}

func inspectObservationMaterialSchema(db *gorm.DB) (map[string]observationMaterialColumn, bool, error) {
	tables, err := migrationTableSet(db)
	if err != nil {
		return nil, false, err
	}
	var n int64
	if tables["schema_migrations"] {
		if err := db.Table("schema_migrations").Where("version = ?", nodeObservationMaterialVersion).Count(&n).Error; err != nil {
			return nil, false, err
		}
	}
	columns := map[string]observationMaterialColumn{}
	if !tables["node_observation_intents"] {
		if n != 0 {
			return nil, true, errors.New("recorded observation material table missing; restore before startup/copy")
		}
		return columns, false, nil
	}
	if IsSQLite(db) {
		rows, err := db.Raw("PRAGMA table_info(node_observation_intents)").Rows()
		if err != nil {
			return nil, false, err
		}
		for rows.Next() {
			var ordinal, required, pk int
			var name, kind string
			var def sql.NullString
			if err = rows.Scan(&ordinal, &name, &kind, &required, &def, &pk); err != nil {
				break
			}
			if name == "material_retired_at" || name == "material_review_at" {
				columns[name] = observationMaterialColumn{kind, required == 0, def}
			}
		}
		if err = errors.Join(err, rows.Err(), rows.Close()); err != nil {
			return nil, false, err
		}
	} else {
		rows, err := db.Raw("SELECT COLUMN_NAME,COLUMN_TYPE,IS_NULLABLE,COLUMN_DEFAULT FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'node_observation_intents' AND COLUMN_NAME IN ('material_retired_at','material_review_at')").Rows()
		if err != nil {
			return nil, false, err
		}
		for rows.Next() {
			var name, kind, nullable string
			var def sql.NullString
			if err = rows.Scan(&name, &kind, &nullable, &def); err != nil {
				break
			}
			columns[name] = observationMaterialColumn{kind, nullable == "YES", def}
		}
		if err = errors.Join(err, rows.Err(), rows.Close()); err != nil {
			return nil, false, err
		}
	}
	for _, name := range []string{"material_retired_at", "material_review_at"} {
		c, present := columns[name]
		if !present {
			if n != 0 {
				return nil, true, fmt.Errorf("recorded observation material column %s missing; restore before startup/copy", name)
			}
			continue
		}
		expected := "datetime(3)"
		if IsSQLite(db) {
			expected = "datetime"
		}
		if !strings.EqualFold(c.Kind, expected) || c.Nullable != (name == "material_retired_at") {
			return nil, n != 0, fmt.Errorf("observation material column %s shape differs", name)
		}
		if name == "material_retired_at" {
			if c.Default.Valid && !strings.EqualFold(strings.Trim(c.Default.String, "() "), "NULL") {
				return nil, n != 0, errors.New("material retirement marker must default to NULL")
			}
		} else {
			value := strings.Trim(c.Default.String, "() '\"")
			at, e := time.Parse("2006-01-02 15:04:05.999999999", value)
			if !c.Default.Valid || e != nil || !at.Equal(time.Unix(0, 0).UTC()) {
				return nil, n != 0, errors.New("material review schedule must default to epoch")
			}
		}
	}
	// DDL adds the identity-bearing retirement marker first. A later review
	// column without it is evidence loss, not an interrupted first statement.
	if _, review := columns["material_review_at"]; review {
		if _, marker := columns["material_retired_at"]; !marker {
			return nil, n != 0, errors.New("observation material marker lost before receipt; restore before startup/copy")
		}
	}
	return columns, n != 0, nil
}

func preflightObservationMaterialSchema(db *gorm.DB) error {
	_, _, err := inspectObservationMaterialSchema(db)
	return err
}

func reconcileObservationMaterialSchema(db *gorm.DB) error {
	columns, _, err := inspectObservationMaterialSchema(db)
	if err != nil {
		return err
	}
	if !db.Migrator().HasTable("node_observation_intents") {
		return errors.New("observation handoff must precede material retention")
	}
	path := nodeObservationMaterialVersion
	if IsSQLite(db) {
		path = "sqlite/0061_node_observation_material_retention.sql"
	}
	raw, err := migrations.Files.ReadFile(path)
	if err != nil {
		return err
	}
	statements, err := splitMigrationStatements(string(raw))
	if err != nil {
		return err
	}
	if len(statements) != 6 {
		return errors.New("unexpected observation material migration statement inventory")
	}
	for i, name := range []string{"material_retired_at", "material_review_at"} {
		if _, ok := columns[name]; !ok {
			if err := db.Exec(statements[i]).Error; err != nil {
				return err
			}
		}
	}
	if err := preflightObservationMaterialSchema(db); err != nil {
		return err
	}
	for _, spec := range observationMaterialIndexes {
		if err := ensureObservationMaterialIndex(db, spec); err != nil {
			return err
		}
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: nodeObservationMaterialVersion, AppliedAt: time.Now().UTC()}).Error
}

func ensureObservationMaterialIndex(db *gorm.DB, spec observationMaterialIndex) (err error) {
	allowed := false
	for _, known := range observationMaterialIndexes {
		if spec.Table == known.Table && spec.Name == known.Name && strings.Join(spec.Columns, ",") == strings.Join(known.Columns, ",") {
			allowed = true
		}
	}
	if !allowed {
		return errors.New("unknown observation material index")
	}
	var columns []string
	if IsSQLite(db) {
		ownerRows, e := db.Raw("SELECT tbl_name FROM sqlite_master WHERE type = 'index' AND name = ?", spec.Name).Rows()
		if e != nil {
			return e
		}
		for ownerRows.Next() {
			var owner string
			if e = ownerRows.Scan(&owner); e != nil {
				break
			}
			if owner != spec.Table {
				e = errors.New("observation material index belongs to another table")
				break
			}
		}
		e = errors.Join(e, ownerRows.Err(), ownerRows.Close())
		if e != nil {
			return e
		}

		listRows, e := db.Raw("PRAGMA index_list(" + spec.Table + ")").Rows()
		if e != nil {
			return e
		}
		for listRows.Next() {
			var seq, unique, partial int
			var name, origin string
			if e = listRows.Scan(&seq, &name, &unique, &origin, &partial); e != nil {
				break
			}
			if name == spec.Name && (unique != 0 || partial != 0 || origin != "c") {
				e = errors.New("observation material index must be nonunique and complete")
				break
			}
		}
		e = errors.Join(e, listRows.Err(), listRows.Close())
		if e != nil {
			return e
		}
		rows, e := db.Raw("PRAGMA index_xinfo(" + spec.Name + ")").Rows()
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
				e = errors.New("observation material index order/expression/collation differs")
				break
			}
			columns = append(columns, strings.ToLower(name.String))
		}
		err = errors.Join(e, rows.Err(), rows.Close())
	} else {
		rows, e := db.Raw("SELECT COLUMN_NAME,NON_UNIQUE,SUB_PART,INDEX_TYPE,COLLATION FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND INDEX_NAME = ? ORDER BY SEQ_IN_INDEX", spec.Table, spec.Name).Rows()
		if e != nil {
			return e
		}
		for rows.Next() {
			var name, kind, direction string
			var unique int
			var prefix sql.NullInt64
			if e = rows.Scan(&name, &unique, &prefix, &kind, &direction); e != nil {
				break
			}
			if unique != 1 || prefix.Valid || kind != "BTREE" || direction != "A" {
				e = errors.New("observation material index shape differs")
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
		return db.Exec("CREATE INDEX " + spec.Name + " ON " + spec.Table + "(" + strings.Join(spec.Columns, ",") + ")").Error
	}
	if strings.Join(columns, ",") != strings.Join(spec.Columns, ",") {
		return errors.New("observation material index shape differs")
	}
	return nil
}
