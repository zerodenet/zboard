package handler

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

// This observes executed reads, including the private preselection/current-read
// and automatic quote paths. Every historical body batch needs its own prior
// length/identity result; an earlier scan cannot license a later unbounded read.
func guardRenewalSourceReads(t *testing.T, db *gorm.DB, refuseBody bool) (func(), *int) {
	t.Helper()
	const name = "test:renewal_source_byte_preflight"
	const beforeName, afterName = name + ":before", name + ":after"
	var mu sync.Mutex
	tickets := map[uint]int{}
	bodyReads := 0
	isBody := func(tx *gorm.DB) bool {
		if tx.Statement.Table != "orders" {
			return false
		}
		switch tx.Statement.Dest.(type) {
		case *model.Order, *[]model.Order:
		default:
			return false
		}
		if len(tx.Statement.Selects) == 0 {
			return true
		}
		selected := strings.Join(tx.Statement.Selects, ",")
		return strings.Contains(selected, "*") || strings.Contains(selected, "purchase_snapshot") || strings.Contains(selected, "entitlement_value_snapshot") || strings.Contains(selected, "change_snapshot")
	}
	if err := db.Callback().Query().Before("gorm:query").Register(beforeName, func(tx *gorm.DB) {
		if !isBody(tx) {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		bodyReads++
		if refuseBody {
			tx.AddError(errors.New("over-budget renewal source body was requested"))
			return
		}
		if len(tx.Statement.Selects) == 0 {
			omitted := false
			for _, field := range tx.Statement.Omits {
				omitted = omitted || field == "raw_callback"
			}
			if !omitted {
				tx.AddError(errors.New("renewal evidence read included channel callback"))
			}
		}
		if len(tickets) == 0 {
			tx.AddError(errors.New("renewal source body preceded byte preflight"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Query().After("gorm:query").Register(afterName, func(tx *gorm.DB) {
		if tx.Error != nil || tx.Statement.Table != "orders" {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if strings.Contains(strings.Join(tx.Statement.Selects, " "), "evidence_bytes") {
			value := reflect.ValueOf(tx.Statement.Dest)
			if value.Kind() == reflect.Pointer && !value.IsNil() {
				value = value.Elem()
			}
			if value.Kind() != reflect.Slice {
				tx.AddError(errors.New("source metadata was not a bounded page"))
				return
			}
			for i := 0; i < value.Len(); i++ {
				id := uint(value.Index(i).FieldByName("ID").Uint())
				tickets[id]++
			}
			return
		}
		if !isBody(tx) {
			return
		}
		var rows []model.Order
		switch dest := tx.Statement.Dest.(type) {
		case *model.Order:
			if dest.ID != 0 {
				rows = append(rows, *dest)
			}
		case *[]model.Order:
			rows = *dest
		}
		for _, row := range rows {
			if row.RawCallback != "" || tickets[row.ID] == 0 {
				tx.AddError(fmt.Errorf("source %d had callback or no matching byte preflight", row.ID))
				return
			}
			tickets[row.ID]--
			if tickets[row.ID] == 0 {
				delete(tickets, row.ID)
			}
		}
	}); err != nil {
		db.Callback().Query().Remove(beforeName)
		t.Fatal(err)
	}
	var once sync.Once
	remove := func() {
		once.Do(func() {
			db.Callback().Query().Remove(beforeName)
			db.Callback().Query().Remove(afterName)
		})
	}
	t.Cleanup(remove)
	return remove, &bodyReads
}

func TestRenewalSourceBudgetPrecedesEveryBodyAndOmitsCallback(t *testing.T) {
	assertRenewalSourceBudgetNormal(t, newOrderFixture(t))
}

func TestMySQLRenewalSourceBudgetPrecedesEveryBodyAndOmitsCallback(t *testing.T) {
	assertRenewalSourceBudgetNormal(t, newMySQLBusinessOrderFixture(t))
}

func assertRenewalSourceBudgetNormal(t *testing.T, base orderFixture) {
	t.Helper()
	f := newAutomaticRecurringFixture(t, base, 50000)
	request := commerce.OrderCreateRequest{PlanSKUID: f.base.skuRecord.ID, TargetSubscriptionID: f.sub.ID}
	before, err := f.orders.Preview(t.Context(), 1, request)
	if err != nil {
		t.Fatal(err)
	}
	// MySQL's existing callback TEXT column accepts this diagnostic payload.
	// It is irrelevant even when all financial evidence remains within budget.
	if err := f.db.Model(&model.Order{}).Where("id = ?", f.root.ID).Update("raw_callback", strings.Repeat("x", 60<<10)).Error; err != nil {
		t.Fatal(err)
	}
	remove, reads := guardRenewalSourceReads(t, f.db, false)
	after, err := f.orders.Preview(t.Context(), 1, request)
	if err != nil || after.BasePriceMinor != before.BasePriceMinor || after.DiscountAmount != before.DiscountAmount || after.PayableAmount != before.PayableAmount || after.QuoteFingerprint != before.QuoteFingerprint {
		t.Fatalf("irrelevant callback changed purchased quote: before=%+v after=%+v err=%v", before, after, err)
	}
	view := f.enable(t)
	remove()
	if !view.Enabled || *reads == 0 {
		t.Fatal("source body guard did not exercise manual and automatic quote paths", *reads)
	}
}

func TestRenewalSourceByteBudgetRejectsBeforeBodyOrMoneyChange(t *testing.T) {
	assertRenewalSourceOversized(t, func(t *testing.T) orderFixture { return newOrderFixture(t) })
}

func TestMySQLRenewalSourceByteBudgetRejectsBeforeBodyOrMoneyChange(t *testing.T) {
	assertRenewalSourceOversized(t, newMySQLBusinessOrderFixture)
}

func assertRenewalSourceOversized(t *testing.T, fixture func(*testing.T) orderFixture) {
	t.Helper()
	for _, column := range []string{"change_snapshot", "entitlement_value_snapshot"} {
		t.Run(column, func(t *testing.T) {
			f := newAutomaticRecurringFixture(t, fixture(t), 50000)
			request := commerce.OrderCreateRequest{PlanSKUID: f.base.skuRecord.ID, TargetSubscriptionID: f.sub.ID}
			preview, err := f.orders.Preview(t.Context(), 1, request)
			if err != nil {
				t.Fatal(err)
			}
			request.QuoteFingerprint = preview.QuoteFingerprint
			view, err := f.auto.Read(t.Context(), 1, f.sub.ID, f.base.skuRecord.ID)
			if err != nil || view.Offer == nil {
				t.Fatal("missing original automatic offer", view, err)
			}
			var wallet model.WalletAccount
			if err := f.db.Where("user_id = ? AND currency = ?", 1, f.root.Currency).First(&wallet).Error; err != nil {
				t.Fatal(err)
			}
			var count int64
			if err := f.db.Model(&model.Order{}).Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			// Less than 1Mi characters but more than 1Mi bytes. Both engines must
			// count persisted bytes before legacy LONGTEXT reaches the process.
			if err := f.db.Model(&model.Order{}).Where("id = ?", f.root.ID).Update(column, strings.Repeat("界", 350000)).Error; err != nil {
				t.Fatal(err)
			}
			calls := f.quotes.calls.Load()
			remove, reads := guardRenewalSourceReads(t, f.db, true)
			if _, err := f.orders.Preview(t.Context(), 1, request); !errors.Is(err, commerce.ErrSubscriptionEvidenceBudget) {
				t.Fatal("oversized source returned manual quote or loaded text", err)
			}
			if _, err := f.orders.Create(t.Context(), 1, request); !errors.Is(err, commerce.ErrSubscriptionEvidenceBudget) {
				t.Fatal("oversized source created order or loaded text", err)
			}
			if blocked, err := f.auto.Read(t.Context(), 1, f.sub.ID, f.base.skuRecord.ID); err != nil || blocked.Offer != nil || blocked.Enabled {
				t.Fatal("oversized automatic source exposed an offer", blocked, err)
			}
			_, err = f.auto.Set(t.Context(), 1, f.sub.ID, commerce.AutomaticRenewalRequest{Enabled: true, PlanSKUID: f.base.skuRecord.ID, QuoteFingerprint: view.Offer.QuoteFingerprint, ExpectedRevision: view.Revision})
			remove()
			if !errors.Is(err, commerce.ErrSubscriptionEvidenceBudget) || *reads != 0 || f.quotes.calls.Load() != calls {
				t.Fatalf("budget was checked after body/provider/consent: body=%d calls=%d err=%v", *reads, f.quotes.calls.Load()-calls, err)
			}
			var afterWallet model.WalletAccount
			if err := f.db.Where("user_id = ? AND currency = ?", wallet.UserID, wallet.Currency).First(&afterWallet).Error; err != nil || afterWallet.BalanceMinor != wallet.BalanceMinor || afterWallet.ReservedMinor != wallet.ReservedMinor || afterWallet.Revision != wallet.Revision {
				t.Fatal("budget rejection changed wallet", afterWallet, err)
			}
			var after model.Subscription
			if err := f.db.First(&after, f.sub.ID).Error; err != nil || after.Status != f.sub.Status || !after.StartAt.Equal(f.sub.StartAt) || !after.EndAt.Equal(f.sub.EndAt) || after.EntitlementEpoch != f.sub.EntitlementEpoch || after.FlowTotal != f.sub.FlowTotal || after.FlowUsed != f.sub.FlowUsed {
				t.Fatal("budget rejection changed rights", after, err)
			}
			var afterCount int64
			if err := f.db.Model(&model.Order{}).Count(&afterCount).Error; err != nil || afterCount != count {
				t.Fatal("budget rejection left an order", afterCount, count, err)
			}
		})
	}
}
