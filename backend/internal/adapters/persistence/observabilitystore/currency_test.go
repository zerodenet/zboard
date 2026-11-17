package observabilitystore

import (
	"github.com/zerodenet/zboard/backend/internal/capabilities/observability"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"path/filepath"
	"testing"
	"time"
)

func TestDashboardDefaultCurrencyOnlyForEmptyFacts(t *testing.T) {
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "currency.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.SystemConfig{ConfigKey: "default_currency", Value: "USD", ValueType: "string", IsPublic: true}).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 5, 8, 0, 0, 0, time.UTC)
	period := observability.DashboardPeriod{Range: "today", From: now.Add(-time.Hour), To: now, PreviousFrom: now.Add(-2 * time.Hour), PreviousTo: now.Add(-time.Hour), Bucket: "hour", Timezone: "UTC"}
	buckets := []observability.DashboardTrendBucket{{Key: "b0", StartUTC: period.From, EndUTC: period.To}}
	service := observability.Dashboard{Repository: Dashboard{DB: db}}
	result, err := service.View(t.Context(), period, now, buckets, "statistics")
	if err != nil || result.Business.Currency != "USD" {
		t.Fatal(result, err)
	}
	_, currency, mixed, err := loadDashboardTrendFacts(db, period, buckets)
	if err != nil || currency != "USD" || mixed {
		t.Fatal(currency, mixed, err)
	}
	paid := now.Add(-time.Minute)
	if err := db.Create(&model.Order{TradeNo: "historical-cny", Status: "paid", PaidAt: &paid, PaidAmount: 500, Currency: "CNY", OrderType: "new"}).Error; err != nil {
		t.Fatal(err)
	}
	result, err = service.View(t.Context(), period, now, buckets, "statistics")
	if err != nil || result.Business.Currency != "CNY" || result.Business.RevenueCents != 500 {
		t.Fatal(result, err)
	}
	_, currency, mixed, err = loadDashboardTrendFacts(db, period, buckets)
	if err != nil || currency != "CNY" || mixed {
		t.Fatal(currency, mixed, err)
	}
	if err := db.Create(&model.Order{TradeNo: "new-usd", Status: "paid", PaidAt: &paid, PaidAmount: 200, Currency: "USD", OrderType: "new"}).Error; err != nil {
		t.Fatal(err)
	}
	result, err = service.View(t.Context(), period, now, buckets, "statistics")
	if err != nil || !result.Business.MixedCurrency || result.Business.Currency != "" {
		t.Fatal(result, err)
	}
	_, currency, mixed, err = loadDashboardTrendFacts(db, period, buckets)
	if err != nil || !mixed || currency != "" {
		t.Fatal(currency, mixed, err)
	}
}
