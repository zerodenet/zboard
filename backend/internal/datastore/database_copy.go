package datastore

import (
	"fmt"
	"gorm.io/gorm"
	"strings"
)

// CopyApplicationData copies the authoritative table inventory into a prepared,
// empty destination. The caller owns source quiescence, destination transaction
// and foreign-key ordering. Missing legacy source tables may be absent; an
// existing source table must never be silently dropped from the destination.
func CopyApplicationData(source, target *gorm.DB) error {
	tables, err := MigrationTables(source)
	if err != nil {
		return err
	}
	sourceTables, err := migrationTableSet(source)
	if err != nil {
		return fmt.Errorf("inspect source tables: %w", err)
	}
	targetTables, err := migrationTableSet(target)
	if err != nil {
		return fmt.Errorf("inspect target tables: %w", err)
	}
	for _, table := range tables {
		if !sourceTables[table] {
			continue
		}
		if !targetTables[table] {
			return fmt.Errorf("migration destination is missing source table %s", table)
		}
		if err := copyTableRows(source, target, table); err != nil {
			return err
		}
	}
	return nil
}

func copyTableRows(source, target *gorm.DB, table string) error {
	rows, err := source.Table(table).Rows()
	if err != nil {
		return fmt.Errorf("read source table %s: %w", table, err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return fmt.Errorf("read source columns %s: %w", table, err)
	}
	types, err := rows.ColumnTypes()
	if err != nil {
		return fmt.Errorf("read source column types %s: %w", table, err)
	}
	batch := make([]map[string]interface{}, 0, 200)
	for rows.Next() {
		values := make([]interface{}, len(columns))
		pointers := make([]interface{}, len(columns))
		for index := range values {
			pointers[index] = &values[index]
		}
		if err := rows.Scan(pointers...); err != nil {
			return fmt.Errorf("scan source table %s: %w", table, err)
		}
		record := make(map[string]interface{}, len(columns))
		for index, column := range columns {
			if raw, ok := values[index].([]byte); ok {
				kind := strings.ToUpper(types[index].DatabaseTypeName())
				switch {
				case strings.Contains(kind, "BLOB"), kind == "BINARY", kind == "VARBINARY", kind == "BIT":
					record[column] = append([]byte(nil), raw...)
				default:
					// MySQL returns JSON/text/decimal as bytes; inserting them
					// as []byte into SQLite would change their storage class.
					record[column] = string(raw)
				}
			} else {
				record[column] = values[index]
			}
		}
		batch = append(batch, record)
		if len(batch) == cap(batch) {
			if err := target.Table(table).Create(&batch).Error; err != nil {
				return fmt.Errorf("write target table %s: %w", table, err)
			}
			batch = make([]map[string]interface{}, 0, 200)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate source table %s: %w", table, err)
	}
	if len(batch) > 0 {
		if err := target.Table(table).Create(&batch).Error; err != nil {
			return fmt.Errorf("write target table %s: %w", table, err)
		}
	}
	var sourceCount, targetCount int64
	if err := source.Table(table).Count(&sourceCount).Error; err != nil {
		return err
	}
	if err := target.Table(table).Count(&targetCount).Error; err != nil {
		return err
	}
	if sourceCount != targetCount {
		return fmt.Errorf("verify table %s: source=%d target=%d", table, sourceCount, targetCount)
	}
	return nil
}

func migrationTableSet(db *gorm.DB) (map[string]bool, error) {
	tables, err := db.Migrator().GetTables()
	if err != nil {
		return nil, err
	}
	result := make(map[string]bool, len(tables))
	for _, table := range tables {
		result[table] = true
	}
	return result, nil
}
