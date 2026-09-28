package entitlementstore

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestRetirementRetainsOnlyRenewableServicesForSevenDaysAndPreservesOrderHistory(t *testing.T) {
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "retirement.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { pool.Close() })
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	ended := now.Add(-time.Hour)
	subs := []model.Subscription{
		{ID: 1, UserID: 1, Lifecycle: "fixed", EndsOnQuotaExhaustion: true, Status: "active", StartAt: now.Add(-time.Hour), EndAt: entitlements.PerpetualEnd, FlowTotal: 100, FlowUsed: 100},
		{ID: 2, UserID: 1, Lifecycle: "renewable", Status: "active", StartAt: ended.Add(-time.Hour), EndAt: ended, FlowTotal: 100, FlowUsed: 80},
		{ID: 3, UserID: 1, Lifecycle: "renewable", Status: "active", StartAt: now.Add(-time.Hour), EndAt: now.Add(time.Hour), FlowTotal: 100, FlowUsed: 100, ResetPolicy: 2},
	}
	for _, sub := range subs {
		if err := db.Create(&sub).Error; err != nil {
			t.Fatal(err)
		}
	}
	order := model.Order{ID: 1, SubscriptionID: 1, UserID: 1, TradeNo: "paid", PlanName: "Trial", SKUName: "Once", Status: "paid", PaidAmount: 500}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	pending := model.Order{ID: 2, UserID: 1, TargetSubscriptionID: &subs[0].ID, TradeNo: "pending", OrderType: "renewal", Status: "pending"}
	if err := db.Create(&pending).Error; err != nil {
		t.Fatal(err)
	}
	credential := model.ProtocolCredential{SubscriptionID: 1, UserID: 1, NodeID: 1, ProtocolEndpointID: 1, CredentialID: "credential", PrincipalKey: "principal", Secret: "secret", Status: "active", ExpiresAt: entitlements.PerpetualEnd}
	if err := db.Create(&credential).Error; err != nil {
		t.Fatal(err)
	}
	failure := errors.New("publication failed")
	service := CredentialExpiry{DB: db, Publish: func(tx *gorm.DB, node, endpoint, actor uint) error { return failure }}
	if _, err := service.ExpireDueCredentials(context.Background(), now, 10); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	var first model.Subscription
	if err := db.First(&first, 1).Error; err != nil || first.EndedAt != nil || first.Status != "active" {
		t.Fatal("nonatomic termination", first, err)
	}
	service.Publish = func(tx *gorm.DB, node, endpoint, actor uint) error { return nil }
	if _, err := service.ExpireDueCredentials(context.Background(), now, 10); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&model.Subscription{}, 1).Error; !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal("single use retained", err)
	}
	if err := db.First(&model.ProtocolCredential{}, credential.ID).Error; !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal("credential retained", err)
	}
	var historical model.Order
	if err := db.First(&historical, 1).Error; err != nil || historical.SubscriptionEndReason != "exhausted" || historical.SubscriptionEndedAt == nil || historical.SubscriptionFinalFlowUsed != 100 || historical.PlanName != "Trial" || historical.PaidAmount != 500 {
		t.Fatal("order history lost", historical, err)
	}
	historical = model.Order{}
	if err := db.First(&historical, 2).Error; err != nil || historical.Status != "canceled" || historical.TargetSubscriptionID != nil || historical.SubscriptionEndedAt == nil {
		t.Fatal("pending target unsafe", historical, err)
	}
	var retained model.Subscription
	if err := db.First(&retained, 2).Error; err != nil || retained.Status != "expired" || retained.EndedAt == nil || !retained.EndedAt.Equal(ended) {
		t.Fatal("renewal window lost", retained, err)
	}
	var monthly model.Subscription
	if err := db.First(&monthly, 3).Error; err != nil || monthly.EndedAt != nil || monthly.Status != "active" {
		t.Fatal("monthly exhaustion terminated service", monthly, err)
	}
	if _, err := service.ExpireDueCredentials(context.Background(), ended.Add(entitlements.RenewalGracePeriod-time.Nanosecond), 10); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&model.Subscription{}, 2).Error; err != nil {
		t.Fatal("deleted before seven days", err)
	}
	if _, err := service.ExpireDueCredentials(context.Background(), ended.Add(entitlements.RenewalGracePeriod), 10); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&model.Subscription{}, 2).Error; !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal("retained after seven days", err)
	}
	if due, err := service.HasDueCredentials(context.Background(), ended.Add(entitlements.RenewalGracePeriod)); err != nil || due {
		t.Fatal("retirement did not converge", due, err)
	}
}
