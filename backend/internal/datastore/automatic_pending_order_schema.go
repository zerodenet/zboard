package datastore

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const automaticPendingOrderVersion = "0066_automatic_renewal_pending_orders.up.sql"

var automaticPendingColumns = []string{"pending_order_id", "pending_consent_revision", "pending_end_at", "pending_epoch", "pending_attempt"}

// Run before AutoMigrate/copy. Recorded authority must never be replaced by
// default zero/null links. Unrecorded empty interrupted DDL can resume safely.
func preflightAutomaticPendingOrders(db *gorm.DB) error {
	if db == nil {
		return errors.New("database is required")
	}
	var applied int64
	if db.Migrator().HasTable("schema_migrations") {
		if err := db.Table("schema_migrations").Where("version = ?", automaticPendingOrderVersion).Count(&applied).Error; err != nil {
			return err
		}
	}
	const table = "subscription_automatic_renewals"
	if !db.Migrator().HasTable(table) {
		if applied > 0 {
			return errors.New("automatic pending order authority missing; restore before startup/copy")
		}
		return nil
	}
	present, err := automaticPendingColumnPresence(db)
	if err != nil {
		return err
	}
	missing := false
	for _, column := range automaticPendingColumns {
		missing = missing || !present[column]
	}
	if missing {
		if applied > 0 {
			return errors.New("recorded automatic pending order authority column missing; restore before startup/copy")
		}
		if db.Migrator().HasTable("audit_logs") {
			var event struct{ ID uint }
			r := db.Table("audit_logs").Select("id").Where("action = ?", "commerce.auto_renew.stage_created").Limit(1).Find(&event)
			if r.Error != nil {
				return r.Error
			}
			if r.RowsAffected > 0 {
				return errors.New("automatic staged history exists without complete pending authority; restore before startup/copy")
			}
		}

		var conditions []string
		for _, column := range automaticPendingColumns {
			if !present[column] {
				continue
			}
			if column == "pending_order_id" || column == "pending_end_at" {
				conditions = append(conditions, column+" IS NOT NULL")
			} else {
				conditions = append(conditions, column+" <> 0")
			}
		}
		if len(conditions) > 0 {
			var head struct{ SubscriptionID uint }
			r := db.Table(table).Select("subscription_id").Where(strings.Join(conditions, " OR ")).Limit(1).Find(&head)
			if r.Error != nil {
				return r.Error
			}
			if r.RowsAffected > 0 {
				return errors.New("incomplete automatic pending columns contain recovery authority; restore before startup/copy")
			}
		}
		return nil
	}
	columns, _, err := projectionColumns(db, table)
	if err != nil {
		return err
	}
	for _, column := range automaticPendingColumns {
		kind := "bigint unsigned"
		nullable := column == "pending_order_id" || column == "pending_end_at"
		if column == "pending_end_at" {
			kind = "datetime(3)"
		}
		if IsSQLite(db) {
			kind = "integer"
			if column == "pending_end_at" {
				kind = "datetime"
			}
		}
		c, ok := columns[column]
		if !ok || c.kind != kind || c.nullable != nullable {
			return fmt.Errorf("automatic pending column %s differs", column)
		}
	}
	return nil
}

func reconcileAutomaticPendingOrders(db *gorm.DB) error {
	if err := preflightAutomaticPendingOrders(db); err != nil {
		return err
	}
	if !db.Migrator().HasTable("subscription_automatic_renewals") {
		return errors.New("automatic consent table missing before pending-order migration")
	}
	asset := automaticPendingOrderVersion
	if IsSQLite(db) {
		asset = "sqlite/0066_automatic_renewal_pending_orders.sql"
	}
	raw, err := migrations.Files.ReadFile(asset)
	if err != nil {
		return err
	}
	statements, err := splitMigrationStatements(string(raw))
	if err != nil || len(statements) != 7 {
		return errors.New("unexpected automatic pending migration inventory")
	}
	// The complete-table fast path issues no DDL and shares one presence read.
	// Interrupted/missing-column recovery keeps the original per-column checks;
	// no snapshot is reused across any ALTER. The original final full preflight
	// remains mandatory in both paths.
	complete := false
	if !IsSQLite(db) {
		present, e := automaticPendingColumnPresence(db)
		if e != nil {
			return e
		}
		complete = true
		for _, column := range automaticPendingColumns {
			complete = complete && present[column]
		}
	}
	for i, column := range automaticPendingColumns {
		if complete {
			break
		}
		if !db.Migrator().HasColumn("subscription_automatic_renewals", column) {
			if err := db.Exec(statements[i]).Error; err != nil {
				return err
			}
		}
	}
	if err := preflightAutomaticPendingOrders(db); err != nil {
		return err
	}
	if err := ensureAutomaticPendingIndexes(db, statements[5:]); err != nil {
		return err
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: automaticPendingOrderVersion, AppliedAt: time.Now().UTC()}).Error
}

func ensureAutomaticPendingIndexes(db *gorm.DB, ddl []string) error {
	const table = "subscription_automatic_renewals"
	expected := []struct {
		name, columns string
		unique        bool
	}{{"idx_auto_pending_order", "pending_order_id", true}, {"idx_auto_pending_due", "pending_order_id,next_attempt_at,user_id", false}}
	indexes, err := db.Migrator().GetIndexes(table)
	if err != nil {
		return err
	}
	for i, spec := range expected {
		found := false
		for _, index := range indexes {
			if index.Name() != spec.name {
				continue
			}
			found = true
			unique, known := index.Unique()
			if !known || unique != spec.unique || strings.Join(index.Columns(), ",") != spec.columns {
				return errors.New("automatic pending index differs")
			}
		}
		if found && IsSQLite(db) {
			var list []struct {
				Name    string
				Unique  int
				Partial int
			}
			if err := db.Raw("SELECT name, \"unique\", partial FROM pragma_index_list(?)", table).Scan(&list).Error; err != nil {
				return err
			}
			for _, index := range list {
				if index.Name == spec.name && index.Partial != 0 {
					return errors.New("automatic pending index must cover all rows")
				}
			}
			var parts []struct {
				Cid  int
				Name *string
				Desc int
				Coll *string
				Key  int
			}
			if err := db.Raw(`SELECT cid,name,"desc",coll,"key" FROM pragma_index_xinfo(?) ORDER BY seqno`, spec.name).Scan(&parts).Error; err != nil {
				return err
			}
			var columns []string
			for _, part := range parts {
				if part.Key == 0 {
					continue
				}
				if part.Cid < 0 || part.Name == nil || part.Coll == nil || *part.Coll != "BINARY" || part.Desc != 0 {
					return errors.New("automatic pending index expression/order/collation differs")
				}
				columns = append(columns, *part.Name)
			}
			if strings.Join(columns, ",") != spec.columns {
				return errors.New("automatic pending index columns differ")
			}
		}
		if !found {
			if err := db.Exec(ddl[i]).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

// Even before the first stage exists, a new explicit finite automatic approval
// is unreadable by the old strict consent decoder. Terminal rows are not an
// old-binary compatibility certificate. This guard never rewrites authority.
func ValidateAutomaticPendingBinaryRollback(db *gorm.DB) error {
	if err := preflightAutomaticPendingOrders(db); err != nil {
		return err
	}
	const table = "subscription_automatic_renewals"
	if !db.Migrator().HasTable(table) {
		return nil
	}
	if db.Migrator().HasColumn(table, "pending_order_id") {
		var head struct{ SubscriptionID uint }
		r := db.Table(table).Select("subscription_id").Where("pending_order_id IS NOT NULL OR pending_attempt <> 0").Limit(1).Find(&head)
		if r.Error != nil {
			return r.Error
		}
		if r.RowsAffected > 0 {
			return errors.New("automatic staged renewal history requires a compatible binary and reconciler; retain pending links and snapshots")
		}
	}
	if !db.Migrator().HasColumn(table, "approval_snapshot") {
		return nil
	}
	size := "OCTET_LENGTH(approval_snapshot)"
	if IsSQLite(db) {
		size = "length(CAST(approval_snapshot AS BLOB))"
	}
	var cursor uint
	for {
		var page []struct {
			SubscriptionID uint
			SnapshotBytes  int64
		}
		if err := db.Table(table).Select("subscription_id,"+size+" AS snapshot_bytes").Where("subscription_id > ? AND approval_snapshot <> ?", cursor, "").Order("subscription_id").Limit(64).Scan(&page).Error; err != nil {
			return err
		}
		if len(page) == 0 {
			return nil
		}
		for _, head := range page {
			cursor = head.SubscriptionID
			if head.SnapshotBytes > commerce.MaxPurchaseSnapshotBytes {
				return errors.New("cannot prove automatic approval rollback compatibility within evidence budget")
			}
			var row struct{ ApprovalSnapshot string }
			if err := db.Table(table).Select("approval_snapshot").Where("subscription_id = ? AND "+size+" <= ?", head.SubscriptionID, commerce.MaxPurchaseSnapshotBytes).Take(&row).Error; err != nil {
				return err
			}
			var obligation struct {
				Redemption string `json:"redemption"`
			}
			if json.Unmarshal([]byte(row.ApprovalSnapshot), &obligation) == nil && obligation.Redemption != "" {
				return errors.New("finite automatic approval requires the new strict decoder even before any staged order exists")
			}
		}
	}
}

// Read only the fixed consent table. MySQL HasColumn otherwise performs a
// database-name probe, schema resolver and COUNT for EACH of the five names.
// SQLite retains its original HasColumn path. No state survives this call.
func automaticPendingColumnPresence(db *gorm.DB) (map[string]bool, error) {
	if !IsSQLite(db) {
		return mysqlColumnNames(db, "subscription_automatic_renewals")
	}
	present := map[string]bool{}
	for _, column := range automaticPendingColumns {
		present[column] = db.Migrator().HasColumn("subscription_automatic_renewals", column)
	}
	return present, nil
}
