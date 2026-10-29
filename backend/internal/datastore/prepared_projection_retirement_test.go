package datastore_test

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/datastore"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestSQLitePreparedProjectionRetirementAuthorityReadOnly(t *testing.T) {
	assertPreparedRetirementAuthority(t, false)
}
func TestMySQLPreparedProjectionRetirementAuthorityReadOnly(t *testing.T) {
	assertPreparedRetirementAuthority(t, true)
}

func assertPreparedRetirementAuthority(t *testing.T, mysql bool) {
	db := diagnosticSchemaDB(t, mysql)
	read := db
	if !mysql {
		var close func() error
		var err error
		read, close, err = datastore.OpenReadView(db)
		if err != nil {
			t.Fatal(err)
		}
		defer close()
		if read == db {
			t.Fatal("physical read-only fixture required")
		}
	}
	check := func(t *testing.T, want error) error {
		t.Helper()
		before := preparedRetirementSnapshot(t, db)
		validate := func(tx *gorm.DB) error {
			release := guardPreparedRetirementReadOnly(t, tx)
			defer release()
			return datastore.ValidatePreparedProjectionSchema(t.Context(), tx)
		}
		var err error
		if mysql {
			err = read.Transaction(validate, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
		} else {
			err = validate(read)
		}
		if want == nil && err != nil || want != nil && !errors.Is(err, want) {
			t.Fatal("prepared retirement dependency result", err, want)
		}
		if before != preparedRetirementSnapshot(t, db) {
			t.Fatal("read-only admission changed rows or DDL")
		}
		return err
	}
	check(t, nil)
	const receipt = "0067_node_observation_retirement_budget.up.sql"
	var version map[string]any
	if err := db.Table("schema_migrations").Where("version = ?", receipt).Take(&version).Error; err != nil {
		t.Fatal(err)
	}
	t.Run("receipt_missing", func(t *testing.T) {
		if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", receipt).Error; err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := db.Table("schema_migrations").Create(version).Error; err != nil {
				t.Fatal(err)
			}
		}()
		check(t, datastore.ErrProjectionNotPrepared)
	})
	for _, table := range []string{"node_observation_material_budget", "node_observation_retirements", "node_observation_retirement_parts"} {
		t.Run("missing_"+table, func(t *testing.T) {
			// Fixture-only rename preserves the original authority for restoration;
			// the validator must not recreate the missing recorded table.
			if err := db.Exec("ALTER TABLE " + table + " RENAME TO prepared_retirement_saved").Error; err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := db.Exec("ALTER TABLE prepared_retirement_saved RENAME TO " + table).Error; err != nil {
					t.Fatal(err)
				}
			}()
			check(t, datastore.ErrProjectionNotPrepared)
		})
	}
	for _, field := range []struct{ table, column string }{
		{"node_observation_material_budget", "retained_bytes"},
		{"node_observation_retirements", "hash_state"},
		{"node_observation_retirements", "source_summary"},
		{"node_observation_retirement_parts", "sha256"},
	} {
		t.Run("missing_"+field.table+"_"+field.column, func(t *testing.T) {
			if err := db.Exec("ALTER TABLE " + field.table + " RENAME COLUMN " + field.column + " TO prepared_wrong_column").Error; err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := db.Exec("ALTER TABLE " + field.table + " RENAME COLUMN prepared_wrong_column TO " + field.column).Error; err != nil {
					t.Fatal(err)
				}
			}()
			check(t, datastore.ErrProjectionNotPrepared)
		})
	}
	t.Run("budget_singleton_missing", func(t *testing.T) {
		var saved map[string]any
		if err := db.Table("node_observation_material_budget").Where("id = ?", 1).Take(&saved).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("DELETE FROM node_observation_material_budget WHERE id = ?", 1).Error; err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := db.Table("node_observation_material_budget").Create(saved).Error; err != nil {
				t.Fatal(err)
			}
		}()
		check(t, datastore.ErrProjectionNotPrepared)
	})
	t.Run("budget_revision_invalid", func(t *testing.T) {
		var revision uint64
		if err := db.Table("node_observation_material_budget").Select("revision").Where("id = ?", 1).Scan(&revision).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("UPDATE node_observation_material_budget SET revision = 0 WHERE id = ?", 1).Error; err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := db.Exec("UPDATE node_observation_material_budget SET revision = ? WHERE id = ?", revision, 1).Error; err != nil {
				t.Fatal(err)
			}
		}()
		check(t, datastore.ErrProjectionNotPrepared)
	})
	t.Run("budget_outside_portable_integer_bound", func(t *testing.T) {
		var original uint64
		if err := db.Table("node_observation_material_budget").Select("retained_bytes").Where("id = ?", 1).Scan(&original).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("UPDATE node_observation_material_budget SET retained_bytes = 9223372036854775808 WHERE id = 1").Error; err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := db.Exec("UPDATE node_observation_material_budget SET retained_bytes = ? WHERE id = 1", original).Error; err != nil {
				t.Fatal(err)
			}
		}()
		check(t, datastore.ErrProjectionNotPrepared)
	})
	t.Run("verification_table_read_permission", func(t *testing.T) {
		const name = "test:prepared_retirement_permission"
		denied := errors.New("verification table select unavailable")
		seen := false
		if err := read.Callback().Row().Before("gorm:row").Register(name, func(tx *gorm.DB) {
			if tx.Statement.Table != "node_observation_retirement_parts" {
				return
			}
			// RowQuery builds Statement.SQL only after this hook. Inspect the already
			// registered WHERE expression and refuse before QueryContext can open rows.
			where, ok := tx.Statement.Clauses["WHERE"].Expression.(clause.Where)
			if !ok {
				return
			}
			for _, expression := range where.Exprs {
				if expression, ok := expression.(clause.Expr); ok && expression.SQL == "1 = 0" && len(expression.Vars) == 0 {
					seen = true
					tx.AddError(denied)
					return
				}
			}
		}); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := read.Callback().Row().Remove(name); err != nil {
				t.Error(err)
			}
		}()
		err := check(t, datastore.ErrProjectionNotPrepared)
		if !seen {
			t.Fatal("zero-row verification permission probe was not reached")
		}
		if !errors.Is(err, denied) {
			t.Fatal("actual SELECT permission failure lost", err)
		}
	})
	t.Run("ready_index_missing_or_wrong", func(t *testing.T) {
		drop := "DROP INDEX idx_observation_retirement_ready"
		if mysql {
			drop += " ON node_observation_retirements"
		}
		if err := db.Exec(drop).Error; err != nil {
			t.Fatal(err)
		}
		check(t, datastore.ErrProjectionNotPrepared)
		if err := db.Exec("CREATE INDEX idx_observation_retirement_ready ON node_observation_retirements(review_at,state,intent_id)").Error; err != nil {
			t.Fatal(err)
		}
		check(t, datastore.ErrProjectionNotPrepared)
		if err := db.Exec(drop).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("CREATE INDEX idx_observation_retirement_ready ON node_observation_retirements(state,review_at,intent_id)").Error; err != nil {
			t.Fatal(err)
		}
	})
	check(t, nil)
}

func preparedRetirementSnapshot(t *testing.T, db *gorm.DB) [32]byte {
	t.Helper()
	tables, err := db.Migrator().GetTables()
	if err != nil {
		t.Fatal(err)
	}
	state := map[string]any{}
	for _, table := range tables {
		if !db.Migrator().HasTable(table) {
			continue
		}
		// Scan into dynamic database/sql values. SQLite deliberately stores an
		// out-of-int64 REAL in the corrupt INTEGER-affinity budget fixture;
		// inferring an int64 destination from its declared type would prevent
		// snapshotting the invalid state before the validator can reject it.
		rows, err := db.Table(table).Rows()
		if err != nil {
			t.Fatal(err)
		}
		columns, err := rows.Columns()
		if err != nil {
			_ = rows.Close()
			t.Fatal(err)
		}
		encoded := []string{}
		for rows.Next() {
			values := make([]any, len(columns))
			pointers := make([]any, len(columns))
			for i := range values {
				pointers[i] = &values[i]
			}
			if err := rows.Scan(pointers...); err != nil {
				_ = rows.Close()
				t.Fatal(err)
			}
			row := make(map[string]any, len(columns))
			for i, name := range columns {
				row[name] = values[i]
			}
			data, err := json.Marshal(row)
			if err != nil {
				_ = rows.Close()
				t.Fatal(err)
			}
			encoded = append(encoded, string(data))
		}
		readErr := rows.Err()
		closeErr := rows.Close()
		if readErr != nil || closeErr != nil {
			t.Fatal(readErr, closeErr)
		}
		sort.Strings(encoded)
		state[table+":rows"] = encoded
		if datastore.IsSQLite(db) {
			var ddl []struct{ Type, Name, SQL string }
			if err := db.Raw("SELECT type,name,COALESCE(sql,'') AS sql FROM sqlite_master WHERE tbl_name = ? ORDER BY type,name", table).Scan(&ddl).Error; err != nil {
				t.Fatal(err)
			}
			state[table+":ddl"] = ddl
		} else {
			var name, ddl string
			if err := db.Raw("SHOW CREATE TABLE `"+strings.ReplaceAll(table, "`", "``")+"`").Row().Scan(&name, &ddl); err != nil {
				t.Fatal(err)
			}
			state[table+":ddl"] = ddl
		}
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(data)
}

func guardPreparedRetirementReadOnly(t *testing.T, db *gorm.DB) func() {
	t.Helper()
	const name = "test:prepared_retirement_read_only"
	reject := func(tx *gorm.DB) { tx.AddError(errors.New("prepared validation attempted mutation")) }
	if err := db.Callback().Create().Before("gorm:create").Register(name, reject); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Update().Before("gorm:update").Register(name, reject); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Delete().Before("gorm:delete").Register(name, reject); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Raw().Before("gorm:raw").Register(name, func(tx *gorm.DB) {
		q := strings.ToUpper(strings.TrimSpace(tx.Statement.SQL.String()))
		if !strings.HasPrefix(q, "SELECT ") && !strings.HasPrefix(q, "SHOW ") {
			reject(tx)
		}
	}); err != nil {
		t.Fatal(err)
	}
	return func() {
		if err := db.Callback().Create().Remove(name); err != nil {
			t.Fatal(err)
		}
		if err := db.Callback().Update().Remove(name); err != nil {
			t.Fatal(err)
		}
		if err := db.Callback().Delete().Remove(name); err != nil {
			t.Fatal(err)
		}
		if err := db.Callback().Raw().Remove(name); err != nil {
			t.Fatal(err)
		}
	}
}
