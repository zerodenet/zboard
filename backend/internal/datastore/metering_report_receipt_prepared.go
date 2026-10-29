package datastore

import (
	"context"
	"errors"
	"gorm.io/gorm"
	"strings"
)

// ValidatePreparedReportReceiptSchema only inspects recovery authority. It
// never creates tables, records migrations, backfills facts or initializes data.
func ValidatePreparedReportReceiptSchema(ctx context.Context, db *gorm.DB) error {
	if ctx == nil || db == nil {
		return errors.New("report receipt database is required")
	}
	if db.Dialector.Name() != "sqlite" && db.Dialector.Name() != "mysql" {
		return errors.New("unsupported report receipt database driver")
	}
	var prepared bool
	if err := inspectReportReceiptSchema(db.WithContext(ctx), &prepared); err != nil {
		return err
	}
	if !prepared {
		return errors.New("report receipt recovery authority is not prepared")
	}
	columns, primary, err := projectionColumns(db.WithContext(ctx), "traffic_records")
	if err != nil {
		return err
	}
	if strings.Join(primary, ",") != "id" {
		return errors.New("report receipt source primary key differs")
	}
	for _, name := range []string{"id", "node_id", "report_id", "protocol_endpoint_id"} {
		kind := "bigint unsigned"
		if IsSQLite(db) {
			kind = "integer"
		}
		if name == "report_id" {
			kind = "varchar(191)"
			if IsSQLite(db) {
				kind = "text"
			}
		}
		if column, ok := columns[name]; !ok || column.kind != kind {
			return errors.New("report receipt source identity column differs: " + name)
		}
	}
	return nil
}
