package commercestore

import (
	"errors"
	"strings"

	"github.com/go-sql-driver/mysql"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"gorm.io/gorm"
	"modernc.org/sqlite"
	sqlitelib "modernc.org/sqlite/lib"
)

// Only SKU and plan writes classify unique violations as identifier conflicts.
// Operation and audit errors remain persistence failures.
func skuWriteError(err error) error {
	if uniqueViolation(err) {
		return commerce.ErrSKUCodeConflict
	}
	return err
}
func uniqueViolation(err error) bool {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
		return true
	}
	var sqliteErr *sqlite.Error
	return errors.As(err, &sqliteErr) && sqliteErr.Code() == sqlitelib.SQLITE_CONSTRAINT_UNIQUE
}

// MySQL's initial schema also has a legacy unique name index. Inspect only a
// typed duplicate error's constraint suffix, never arbitrary response text.
func planWriteError(err error) error {
	if !uniqueViolation(err) {
		return err
	}
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) {
		const marker = " for key '"
		if index := strings.LastIndex(mysqlErr.Message, marker); index >= 0 {
			key := strings.TrimSuffix(mysqlErr.Message[index+len(marker):], "'")
			if key == "name" || key == "plans.name" {
				return commerce.ErrPlanNameConflict
			}
			if key == "uk_plans_slug" || key == "plans.uk_plans_slug" {
				return commerce.ErrPlanSlugConflict
			}
		}
		return err
	}
	var sqliteErr *sqlite.Error
	if errors.As(err, &sqliteErr) {
		if strings.Contains(sqliteErr.Error(), "UNIQUE constraint failed: plans.name (") {
			return commerce.ErrPlanNameConflict
		}
		if strings.Contains(sqliteErr.Error(), "UNIQUE constraint failed: plans.slug (") {
			return commerce.ErrPlanSlugConflict
		}
		return err
	}
	// A translated GORM error no longer retains the unique-index identity.
	return err
}
