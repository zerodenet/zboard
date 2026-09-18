package datastore

import (
	"fmt"
	"gorm.io/gorm"
	"strings"
)

// ClearCopyDestination removes schema defaults only after the caller has
// established that the destination contains no user data. It must never be
// used to turn an occupied destination into an empty migration target.
func ClearCopyDestination(target *gorm.DB) error {
	tables, err := MigrationTables(target)
	if err != nil {
		return err
	}
	return WithCopyDestination(target.Statement.Context, target, func(tx *gorm.DB) error {
		return ClearCopyDestinationRows(tx, tables)
	})
}

// ClearCopyDestinationRows participates in the caller's pinned transaction.
func ClearCopyDestinationRows(tx *gorm.DB, tables []string) error {
	present, err := migrationTableSet(tx)
	if err != nil {
		return fmt.Errorf("inspect target tables: %w", err)
	}
	for index := len(tables) - 1; index >= 0; index-- {
		if present[tables[index]] {
			if err := tx.Exec("DELETE FROM " + quoteIdentifier(tx, tables[index])).Error; err != nil {
				return fmt.Errorf("clear target table %s: %w", tables[index], err)
			}
		}
	}
	return nil
}

func quoteIdentifier(db *gorm.DB, value string) string {
	if IsSQLite(db) {
		return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
	}
	return "`" + strings.ReplaceAll(value, "`", "``") + "`"
}
