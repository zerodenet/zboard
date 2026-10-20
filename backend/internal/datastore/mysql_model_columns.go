package datastore

import (
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

// addMissingMySQLModelColumns serves the two additive audit/phase reconcilers.
// Their fixed column lists have no primary-key/auto-increment additions, which
// require extra MySQL AddColumn handling. Use the actual model's full data types
// for these columns; this is not a general replacement for Migrator.AddColumn.
// Metadata is local to this call and completely consumed before DDL begins.
func addMissingMySQLModelColumns(db *gorm.DB, value any, names []string) error {
	stmt := &gorm.Statement{DB: db}
	if db.Statement != nil {
		stmt.Table, stmt.TableExpr = db.Statement.Table, db.Statement.TableExpr
	}
	if err := stmt.ParseWithSpecialTableName(value, stmt.Table); err != nil {
		return err
	}
	fields := make([]*schema.Field, 0, len(names))
	for _, name := range names {
		field := stmt.Schema.LookUpField(name)
		if field == nil {
			return fmt.Errorf("failed to look up field with name: %s", name)
		}
		if field.IgnoreMigration {
			continue
		}
		if field.PrimaryKey || field.AutoIncrement {
			return fmt.Errorf("cannot batch primary-key or auto-increment column %s", name)
		}
		fields = append(fields, field)
	}
	present, err := func() (present map[string]bool, err error) {
		// Unlike ColumnTypes, this does not SELECT from the target table or
		// introduce a target-table SELECT permission requirement. These two
		// startup reconcilers operate in the database selected by RunMigrations.
		rows, err := db.Raw("SELECT COLUMN_NAME FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?", stmt.Table).Rows()
		if err != nil {
			return nil, err
		}
		defer func() { err = errors.Join(err, rows.Close()) }()
		present = make(map[string]bool)
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				return nil, err
			}
			present[strings.ToLower(name)] = true
		}
		return present, rows.Err()
	}()
	if err != nil {
		return fmt.Errorf("inspect %s columns: %w", stmt.Table, err)
	}
	var table any = clause.Table{Name: stmt.Table}
	if stmt.TableExpr != nil {
		table = *stmt.TableExpr
	}
	parts := make([]string, 0, len(names))
	args := []any{table}
	for _, field := range fields {
		if present[strings.ToLower(field.DBName)] {
			continue
		}
		parts = append(parts, "ADD ? ?")
		// Keep the complete Expr, including Vars used by custom data types.
		args = append(args, clause.Column{Name: field.DBName}, db.Migrator().FullDataTypeOf(field))
	}
	if len(parts) == 0 {
		return nil
	}
	return db.Exec("ALTER TABLE ? "+strings.Join(parts, ", "), args...).Error
}
