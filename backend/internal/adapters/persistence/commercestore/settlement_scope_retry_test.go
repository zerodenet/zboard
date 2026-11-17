package commercestore

import (
	"database/sql"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
)

func pendingScopeSQLiteFixture(t *testing.T) (*gorm.DB, model.Order, model.Subscription) {
	db, service, _, _, sub := recurringSQLiteFixture(t)
	// The unit fixture predates funding scope discovery. Use its formal empty
	// funding table, without changing the shared fixture or production schema.
	ddl, err := migrations.Files.ReadFile("sqlite/0040_order_funding.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(string(ddl)).Error; err != nil {
		t.Fatal(err)
	}
	request := commerce.OrderCreateRequest{PlanSKUID: sub.PlanSKUID, TargetSubscriptionID: sub.ID}
	preview, err := service.Preview(t.Context(), sub.UserID, request)
	if err != nil {
		t.Fatal(err)
	}
	request.QuoteFingerprint = preview.QuoteFingerprint
	order, err := service.Create(t.Context(), sub.UserID, request)
	if err != nil {
		t.Fatal(err)
	}
	return db, model.Order(order), sub
}

func TestSettlementScopeRetryIsBoundedBeforeFinancialCallback(t *testing.T) {
	db, order, sub := pendingScopeSQLiteFixture(t)
	const callback = "test:settlement-scope-changes-every-preparation"
	var preparations atomic.Int32
	if err := db.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Error != nil || tx.Statement.Table != "orders" || !strings.Contains(strings.Join(tx.Statement.Selects, " "), "evidence_bytes") || tx.RowsAffected == 0 {
			return
		}
		if _, inTransaction := tx.Statement.ConnPool.(*sql.Tx); inTransaction {
			return
		}
		n := preparations.Add(1)
		// Model another committed writer advancing the source boundary after
		// each unlocked snapshot. Neither authority attempt may enter apply.
		next := sub.StartAt.Add(time.Duration(n) * time.Minute)
		if err := db.Model(&model.Subscription{}).Where("id = ?", sub.ID).Update("start_at", next).Error; err != nil {
			tx.AddError(err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Query().Remove(callback)
	applyCalls := 0
	err := withSubscriptionSettlementScope(db.WithContext(t.Context()), order.ID, sub.UserID, sub.UserID, false, func(*gorm.DB, *subscriptionSettlementIdentity) error {
		applyCalls++
		return nil
	})
	if !errors.Is(err, errSubscriptionSettlementScopeChanged) || !errors.Is(err, commerce.ErrSubscriptionQuoteChanged) || preparations.Load() != 2 || applyCalls != 0 {
		t.Fatalf("source drift retried financial work or lost its bound: preparations=%d apply=%d err=%v", preparations.Load(), applyCalls, err)
	}
}

func TestSettlementScopeNeverRetriesBusinessQuoteConflict(t *testing.T) {
	db, order, sub := pendingScopeSQLiteFixture(t)
	applyCalls := 0
	err := withSubscriptionSettlementScope(db.WithContext(t.Context()), order.ID, sub.UserID, sub.UserID, false, func(*gorm.DB, *subscriptionSettlementIdentity) error {
		applyCalls++
		return commerce.ErrSubscriptionQuoteChanged
	})
	if !errors.Is(err, commerce.ErrSubscriptionQuoteChanged) || errors.Is(err, errSubscriptionSettlementScopeChanged) || applyCalls != 1 {
		t.Fatalf("business quote conflict was treated as preparation drift: apply=%d err=%v", applyCalls, err)
	}
}
