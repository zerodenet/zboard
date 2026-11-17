package datastore

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const observationRetirementVersion = "0067_node_observation_retirement_budget.up.sql"

var observationRetirementTables = []string{"node_observation_material_budget", "node_observation_retirements", "node_observation_retirement_parts"}

// Called before any migration/copy mutation. Never seed an already recorded
// missing budget/receipt table or singleton as zero. Recovery must restore it.
func preflightObservationRetirementSchema(db *gorm.DB) error {
	tables, err := migrationTableSet(db)
	if err != nil {
		return err
	}
	var applied int64
	if tables["schema_migrations"] {
		if e := db.Table("schema_migrations").Where("version = ?", observationRetirementVersion).Count(&applied).Error; e != nil {
			return e
		}
	}

	missingTable := false
	for _, table := range observationRetirementTables {
		if !tables[table] {
			missingTable = true
		}
	}
	if missingTable && tables["node_observation_intents"] {
		var h struct{ ID string }
		q := db.Table("node_observation_intents").Select("id").Where("status = ?", "retired").Limit(1).Scan(&h)
		if q.Error != nil {
			return q.Error
		}
		if q.RowsAffected > 0 {
			return errors.New("retired observation source survives missing retirement authority; restore before startup/copy")
		}
	}
	if !tables["node_observation_material_budget"] {
		for _, table := range []string{"node_observation_retirements", "node_observation_retirement_parts"} {
			if !tables[table] {
				continue
			}
			var row struct{ IntentID string }
			q := db.Table(table).Select("intent_id").Limit(1).Scan(&row)
			if q.Error != nil {
				return q.Error
			}
			if q.RowsAffected > 0 {
				return errors.New("observation retirement evidence survives missing budget authority")
			}
		}
	}
	for _, table := range observationRetirementTables {
		if !tables[table] {
			if applied != 0 {
				return fmt.Errorf("recorded observation retirement authority %s missing; restore before startup/copy", table)
			}
			continue
		}
		columns, primary, e := projectionColumns(db, table)
		if e != nil {
			return e
		}
		key := "intent_id"
		expected := 14
		if table == "node_observation_material_budget" {
			key = "id"
			expected = 3
		}
		if table == "node_observation_retirement_parts" {
			key = "intent_id,ordinal"
			expected = 3
		}
		if len(columns) != expected || strings.Join(primary, ",") != key {
			return fmt.Errorf("observation retirement table %s columns/primary key differ", table)
		}
		names := []string{"id", "retained_bytes", "revision"}
		if table == "node_observation_retirement_parts" {
			names = []string{"intent_id", "ordinal", "sha256"}
		}
		if expected == 14 {
			names = []string{"intent_id", "header_sha256", "state", "cause", "source_summary", "next_part", "verified_bytes", "hash_state", "delete_part", "remaining_bytes", "content_verified", "review_at", "created_at", "updated_at"}
		}
		for _, name := range names {
			kind := "bigint unsigned"
			switch name {
			case "intent_id", "header_sha256", "sha256":
				kind = "varchar(64)"
			case "state":
				kind = "varchar(24)"
			case "cause":
				kind = "varchar(64)"
			case "source_summary":
				kind = "text"
			case "hash_state":
				kind = "blob"
			case "content_verified":
				kind = "tinyint(1)"
			case "review_at", "created_at", "updated_at":
				kind = "datetime(3)"
			}
			if IsSQLite(db) {
				switch kind {
				case "bigint unsigned":
					kind = "integer"
				case "tinyint(1)":
					kind = "numeric"
				case "datetime(3)":
					kind = "datetime"
				case "blob":
				default:
					kind = "text"
				}
			}
			c, ok := columns[name]
			if !ok || c.kind != kind || c.nullable {
				return fmt.Errorf("observation retirement column %s.%s differs", table, name)
			}
			if !IsSQLite(db) && (name == "intent_id" || name == "header_sha256" || name == "sha256") && c.collation != "ascii_bin" {
				return errors.New("observation retirement identity collation differs")
			}
		}
	}
	if tables["node_observation_material_budget"] {
		var rows []model.NodeObservationMaterialBudget
		if e := db.Limit(2).Find(&rows).Error; e != nil {
			return e
		}
		if len(rows) > 1 || len(rows) == 1 && (rows[0].ID != 1 || rows[0].Revision == 0 || rows[0].Revision > math.MaxInt64 || rows[0].RetainedBytes > math.MaxInt64) {
			return errors.New("observation material budget authority is inconsistent")
		}
		if len(rows) == 0 && applied != 0 {
			return errors.New("recorded observation material budget singleton missing; restore before startup/copy")
		}

		if len(rows) == 0 && tables["node_observation_retirement_parts"] {
			var part model.NodeObservationRetirementPart
			q := db.Select("intent_id").Limit(1).Find(&part)
			if q.Error != nil {
				return q.Error
			}
			if q.RowsAffected > 0 {
				return errors.New("observation budget missing with surviving per-part verification authority")
			}
		}
		if len(rows) == 0 && tables["node_observation_retirements"] {
			var row model.NodeObservationRetirement
			q := db.Select("intent_id").Limit(1).Find(&row)
			if q.Error != nil {
				return q.Error
			}
			if q.RowsAffected > 0 {
				return errors.New("observation material budget lost with surviving retirement evidence")
			}
		}
	}
	if applied != 0 && !tables["node_observation_intents"] {
		return errors.New("observation retirement source headers missing; restore before startup/copy")
	}
	return nil
}
func reconcileObservationRetirementSchema(db *gorm.DB) error {
	if e := preflightObservationRetirementSchema(db); e != nil {
		return e
	}
	if !db.Migrator().HasTable(&model.NodeObservationIntent{}) {
		return errors.New("observation handoff must precede retirement budget")
	}
	asset := observationRetirementVersion
	if IsSQLite(db) {
		asset = "sqlite/0067_node_observation_retirement_budget.sql"
	}
	raw, e := migrations.Files.ReadFile(asset)
	if e != nil {
		return e
	}
	statements, e := splitMigrationStatements(string(raw))
	if e != nil {
		return e
	}
	for _, sql := range statements {
		if e := db.Exec(sql).Error; e != nil {
			return e
		}
	}
	if e := preflightObservationRetirementSchema(db); e != nil {
		return e
	}
	if e := ensureObservationRetirementIndex(db); e != nil {
		return e
	}
	// This seed runs at owned, quiescent upgrade only. No startup/report path
	// repeatedly scans all historical material. A crash rolls back row+receipt.
	return db.Transaction(func(tx *gorm.DB) error {
		var b model.NodeObservationMaterialBudget
		e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&b, "id = ?", 1).Error
		if errors.Is(e, gorm.ErrRecordNotFound) {
			var orphan struct{ IntentID string }
			q := tx.Table("node_observation_parts AS p").Select("p.intent_id").Joins("LEFT JOIN node_observation_intents AS h ON h.id=p.intent_id").Where("h.id IS NULL").Limit(1).Scan(&orphan)
			if q.Error != nil {
				return q.Error
			}
			if q.RowsAffected > 0 {
				return errors.New("orphan observation parts require recovery")
			}
			bytes, e := measureObservationRetainedBytes(tx)
			if e != nil {
				return e
			}
			if e := tx.Create(&model.NodeObservationMaterialBudget{ID: 1, RetainedBytes: bytes, Revision: 1}).Error; e != nil {
				return e
			}
		} else if e != nil {
			return e
		}
		return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: observationRetirementVersion, AppliedAt: time.Now().UTC()}).Error
	})
}
func ensureObservationRetirementIndex(db *gorm.DB) error {
	indexes, e := db.Migrator().GetIndexes("node_observation_retirements")
	if e != nil {
		return e
	}
	for _, index := range indexes {
		if index.Name() == "idx_observation_retirement_ready" {
			unique, known := index.Unique()
			if !known || unique || strings.Join(index.Columns(), ",") != "state,review_at,intent_id" {
				return errors.New("observation retirement ready index differs")
			}
			return nil
		}
	}
	return db.Exec("CREATE INDEX idx_observation_retirement_ready ON node_observation_retirements(state,review_at,intent_id)").Error
}

// Only the existing owned destination-copy transaction may seed a truly legacy
// source into a prepared target. Source0067 budgets/progress are copied exactly.
func initializeCopiedObservationBudget(target *gorm.DB, sourceHasBudget, sourceHasMaterial bool) error {
	if sourceHasBudget {
		return preflightObservationRetirementSchema(target)
	}
	if !target.Migrator().HasTable("node_observation_material_budget") {
		if sourceHasMaterial {
			return errors.New("legacy observation material requires a prepared current budget destination")
		}
		return nil // Truly pre-observation narrow source/destination contract.
	}
	proof, err := requireCopiedObservationBudgetProof(target)
	if err != nil {
		return err
	}
	var n int64
	if e := target.Model(&model.NodeObservationMaterialBudget{}).Count(&n).Error; e != nil {
		return e
	}
	if n != 0 {
		return errors.New("legacy observation copy requires an empty target budget")
	}
	if e := target.Model(&model.NodeObservationRetirement{}).Count(&n).Error; e != nil {
		return e
	}
	if n != 0 {
		return errors.New("legacy observation copy contains unexplained retirement authority")
	}
	bytes, e := measureObservationRetainedBytes(target)
	if e != nil {
		return e
	}
	if err := target.Create(&model.NodeObservationMaterialBudget{ID: 1, RetainedBytes: bytes, Revision: 1}).Error; err != nil {
		return err
	}
	proof.Consumed = true
	return nil
}

// Empty is known only from an actual aggregate row whose COUNT is zero and
// whose SUMs are NULL. A missing result, SQL error, NULL body, invalid part size
// or overflowing/contradictory sum is lost authority, never an invented zero.
func measureObservationRetainedBytes(db *gorm.DB) (uint64, error) {
	length := "OCTET_LENGTH(data)"
	if IsSQLite(db) {
		length = "length(CAST(data AS BLOB))"
	}
	var aggregate struct {
		RowCount     int64
		ByteCount    int64
		Bytes        sql.NullInt64
		InvalidParts sql.NullInt64
	}
	q := db.Table("node_observation_parts").Select("COUNT(*) AS row_count,COUNT(" + length + ") AS byte_count,SUM(" + length + ") AS bytes,SUM(CASE WHEN " + length + " BETWEEN 1 AND 61440 THEN 0 ELSE 1 END) AS invalid_parts").Scan(&aggregate)
	if q.Error != nil {
		return 0, fmt.Errorf("measure observation retained-byte authority: %w", q.Error)
	}
	if q.RowsAffected != 1 || aggregate.RowCount < 0 || aggregate.ByteCount != aggregate.RowCount {
		return 0, errors.New("observation retained-byte aggregate is incomplete")
	}
	if aggregate.RowCount == 0 {
		if aggregate.Bytes.Valid || aggregate.InvalidParts.Valid {
			return 0, errors.New("empty observation material has contradictory aggregate")
		}
		return 0, nil
	}
	if !aggregate.Bytes.Valid || aggregate.Bytes.Int64 <= 0 || !aggregate.InvalidParts.Valid || aggregate.InvalidParts.Int64 != 0 {
		return 0, errors.New("observation retained-byte aggregate has invalid/unknown parts")
	}
	return uint64(aggregate.Bytes.Int64), nil
}
