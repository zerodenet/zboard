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

const discountRedemptionVersion = "0065_order_discount_redemptions.up.sql"
const discountRedemptionTable = "order_discount_redemptions"

// This runs before any startup AutoMigrate or copy. A recorded but absent intent
// table is lost external-financial authority: recreating it would invent absence
// and could strand/reuse provider stock. Require restoration, preserving all data.
func preflightDiscountRedemptionSchema(db *gorm.DB) error {
	tables, err := migrationTableSet(db)
	if err != nil {
		return err
	}
	var applied int64
	if tables["schema_migrations"] {
		if err := db.Table("schema_migrations").Where("version = ?", discountRedemptionVersion).Count(&applied).Error; err != nil {
			return err
		}
	}
	if !tables[discountRedemptionTable] {
		if applied > 0 {
			return errors.New("recorded discount reservation authority missing; restore before startup/copy")
		}
		// A newly introduced accepted obligation without its table cannot safely be
		// reconstructed from a quote: Reserve may already have executed remotely.
		if tables["orders"] && db.Migrator().HasColumn("orders", "purchase_snapshot") {
			present, err := hasAcceptedDiscountReservationObligation(db)
			if err != nil {
				return err
			}
			if present {
				return errors.New("accepted discount reservation authority missing; restore before startup/copy")
			}
		}
		return nil
	}
	columns, primary, err := projectionColumns(db, discountRedemptionTable)
	if err != nil {
		return err
	}
	if len(columns) != 15 || strings.Join(primary, ",") != "order_id" {
		return errors.New("discount reservation schema inventory differs")
	}
	for _, name := range []string{"order_id", "redemption_key", "binding_hash", "binding_json", "provider_id", "desired", "observed", "token", "revision", "lease_token", "lease_until", "next_attempt_at", "last_error", "created_at", "updated_at"} {
		kind := "varchar(16)"
		switch name {
		case "order_id", "revision":
			kind = "bigint unsigned"
		case "redemption_key", "binding_hash", "lease_token", "last_error":
			kind = "varchar(64)"
		case "binding_json":
			kind = "text"
		case "provider_id":
			kind = "varchar(160)"
		case "token":
			kind = "varchar(512)"
		case "lease_until", "next_attempt_at", "created_at", "updated_at":
			kind = "datetime(3)"
		}
		if IsSQLite(db) {
			switch name {
			case "order_id", "revision":
				kind = "integer"
			case "lease_until", "next_attempt_at", "created_at", "updated_at":
				kind = "datetime"
			default:
				kind = "text"
			}
		}
		c, ok := columns[name]
		if !ok || c.nullable || c.kind != kind {
			return fmt.Errorf("discount reservation column %s differs", name)
		}
		if !IsSQLite(db) && (name == "redemption_key" || name == "binding_hash") && c.collation != "ascii_bin" {
			return errors.New("discount reservation identity collation differs")
		}
	}
	if !tables["orders"] {
		var count int64
		if err := db.Table(discountRedemptionTable).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return errors.New("discount reservation orders missing; restore before startup/copy")
		}
	}
	return nil
}

func reconcileDiscountRedemptionSchema(db *gorm.DB) error {
	if err := preflightDiscountRedemptionSchema(db); err != nil {
		return err
	}
	if !db.Migrator().HasTable(discountRedemptionTable) {
		asset := discountRedemptionVersion
		if IsSQLite(db) {
			asset = "sqlite/0065_order_discount_redemptions.sql"
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
	}
	if err := preflightDiscountRedemptionSchema(db); err != nil {
		return err
	}
	if err := ensureDiscountRedemptionIndexes(db); err != nil {
		return err
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: discountRedemptionVersion, AppliedAt: time.Now().UTC()}).Error
}

// Validate complete key/index ownership before recording0065. Missing indexes
// after interrupted DDL may be added; an incompatible existing index is never
// dropped, renamed or weakened. Call only during migration, not financial RPC.
func ensureDiscountRedemptionIndexes(db *gorm.DB) error {
	if IsSQLite(db) {
		if err := validateSQLiteDiscountRedemptionIndexes(db); err != nil {
			return err
		}
	}
	indexes, err := db.Migrator().GetIndexes(discountRedemptionTable)
	if err != nil {
		return err
	}
	uniqueOK, dueOK, expiryOK := false, false, false
	for _, index := range indexes {
		columns := strings.Join(index.Columns(), ",")
		unique, known := index.Unique()
		if index.Name() == "idx_order_discount_redemption_key" && columns == "redemption_key" && known && unique {
			uniqueOK = true
		}
		if index.Name() == "idx_order_discount_redemption_expiry" {
			if columns != "desired,created_at,order_id" || !known || unique {
				return errors.New("discount redemption expiry index differs")
			}
			expiryOK = true
		}
		if index.Name() == "idx_order_discount_redemption_due" {
			if columns != "next_attempt_at,order_id" || !known || unique {
				return errors.New("discount redemption due index differs")
			}
			dueOK = true
		}
	}
	if !uniqueOK {
		if err := db.Exec("CREATE UNIQUE INDEX idx_order_discount_redemption_key ON order_discount_redemptions(redemption_key)").Error; err != nil {
			return err
		}
	}
	if !dueOK {
		if err := db.Exec("CREATE INDEX idx_order_discount_redemption_due ON order_discount_redemptions(next_attempt_at, order_id)").Error; err != nil {
			return err
		}
	}
	if !expiryOK {
		if err := db.Exec("CREATE INDEX idx_order_discount_redemption_expiry ON order_discount_redemptions(desired, created_at, order_id)").Error; err != nil {
			return err
		}
	}
	return nil
}

// Migration-only bounded keyset scan. Semantic JSON decoding recognizes compact,
// pretty and escaped property/value encodings; no raw LIKE can prove absence.
func hasAcceptedDiscountReservationObligation(db *gorm.DB) (bool, error) {
	size := "OCTET_LENGTH(purchase_snapshot)"
	if IsSQLite(db) {
		size = "length(CAST(purchase_snapshot AS BLOB))"
	}
	var cursor uint
	for {
		var page []struct {
			ID            uint
			SnapshotBytes int64
		}
		if err := db.Table("orders").Select("id, "+size+" AS snapshot_bytes").Where("id > ? AND purchase_snapshot <> ?", cursor, "").Order("id").Limit(64).Scan(&page).Error; err != nil {
			return false, err
		}
		if len(page) == 0 {
			return false, nil
		}
		for _, head := range page {
			cursor = head.ID
			if head.SnapshotBytes <= 0 {
				continue
			}
			if head.SnapshotBytes > commerce.MaxPurchaseSnapshotBytes {
				return false, errors.New("cannot prove absence of discount obligation in oversized historical snapshot; restore authority before startup/copy")
			}
			var row struct{ PurchaseSnapshot string }
			if err := db.Table("orders").Select("purchase_snapshot").Where("id = ? AND "+size+" <= ?", head.ID, commerce.MaxPurchaseSnapshotBytes).Take(&row).Error; err != nil {
				return false, err
			}
			var header struct {
				Discount *struct {
					Quote struct {
						Offer struct {
							Redemption string `json:"redemption"`
						} `json:"offer"`
					} `json:"quote"`
				} `json:"discount"`
			}
			if json.Unmarshal([]byte(row.PurchaseSnapshot), &header) == nil && header.Discount != nil && header.Discount.Quote.Offer.Redemption != "" {
				return true, nil
			}
		}
	}
}

// GetIndexes alone omits SQLite partial/expression/order/collation flags. A
// partial unique index is not an order-key uniqueness guarantee, and a partial
// due/expiry index cannot stand in for the complete recovery queue indexes.
func validateSQLiteDiscountRedemptionIndexes(db *gorm.DB) error {
	var indexes []struct {
		Name    string
		Unique  int
		Partial int
	}
	if err := db.Raw("SELECT name, \"unique\", partial FROM pragma_index_list(?)", discountRedemptionTable).Scan(&indexes).Error; err != nil {
		return err
	}
	expected := map[string]struct {
		columns string
		unique  int
	}{"idx_order_discount_redemption_key": {"redemption_key", 1}, "idx_order_discount_redemption_due": {"next_attempt_at,order_id", 0}, "idx_order_discount_redemption_expiry": {"desired,created_at,order_id", 0}}
	for _, index := range indexes {
		spec, ok := expected[index.Name]
		if !ok {
			continue
		}
		if index.Partial != 0 || index.Unique != spec.unique {
			return errors.New("discount reservation index must be complete with exact uniqueness")
		}
		var parts []struct {
			Seqno int
			Cid   int
			Name  *string
			Desc  int
			Coll  *string
			Key   int
		}
		if err := db.Raw(`SELECT seqno,cid,name,"desc",coll,"key" FROM pragma_index_xinfo(?) ORDER BY seqno`, index.Name).Scan(&parts).Error; err != nil {
			return err
		}
		var columns []string
		for _, part := range parts {
			if part.Key == 0 {
				continue
			}
			if part.Cid < 0 || part.Name == nil || part.Coll == nil || *part.Coll != "BINARY" || part.Desc != 0 {
				return errors.New("discount reservation index expression, order or collation differs")
			}
			columns = append(columns, *part.Name)
		}
		if strings.Join(columns, ",") != spec.columns {
			return errors.New("discount reservation index columns differ")
		}
	}
	return nil
}

// ValidateDiscountRedemptionBinaryRollback is a read-only feature guard. Nil
// only proves absence of this feature's new obligations; it is NOT a blanket
// rollback certificate for all other PR schema/business changes. Even terminal
// intent does not make its new accepted snapshot readable by an old strict binary.
func ValidateDiscountRedemptionBinaryRollback(db *gorm.DB) error {
	if err := preflightDiscountRedemptionSchema(db); err != nil {
		return err
	}
	if db.Migrator().HasTable(discountRedemptionTable) {
		var head struct{ OrderID uint }
		r := db.Table(discountRedemptionTable).Select("order_id").Limit(1).Find(&head)
		if r.Error != nil {
			return r.Error
		}
		if r.RowsAffected > 0 {
			return errors.New("discount reservation history requires a compatible snapshot decoder and reconciler; do not downgrade the live database, even after commit/release")
		}
	}
	if db.Migrator().HasTable("orders") && db.Migrator().HasColumn("orders", "purchase_snapshot") {
		present, err := hasAcceptedDiscountReservationObligation(db)
		if err != nil {
			return err
		}
		if present {
			return errors.New("accepted discount snapshot requires a compatible binary; terminal reservation is not old-format compatibility")
		}
	}
	return nil
}
