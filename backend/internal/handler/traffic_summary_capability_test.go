package handler

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"testing"
	"time"
)

func TestTrafficSummaryCapabilityScopesAndRevocation(t *testing.T) {
	f := newTrafficReadFixture(t)
	f.seedReconciliation(t)
	if err := f.h.db.Model(&model.Subscription{}).Where("id = 1").Updates(map[string]interface{}{"status": "active", "flow_total": 1000, "end_at": time.Now().UTC().Add(time.Hour)}).Error; err != nil {
		t.Fatal(err)
	}
	service := f.h.services.UsageSummary()
	result, err := service.Read(context.Background(), 1, metering.UsageSummaryQuery{UserID: 2})
	// Reporting totals preserve ledger ownership, including orphaned records;
	// reconciliation separately counts wrong-owner rows by subscription.
	if err != nil || result.ScopeUser != 1 || result.TotalUsedBytes != 927 || result.RemainingBytes != 900 || result.UsedBytesToday != 927 {
		t.Fatalf("own totals: %+v %v", result, err)
	}
	result, err = service.Read(context.Background(), 99, metering.UsageSummaryQuery{Administrative: true, UserID: 2})
	if err != nil || result.TotalUsedBytes != 10040 {
		t.Fatalf("admin filter: %+v %v", result, err)
	}
	if err = f.h.db.Model(&model.User{}).Where("id = 99").Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err = service.Read(context.Background(), 99, metering.UsageSummaryQuery{Administrative: true}); !errors.Is(err, metering.ErrTrendPermission) {
		t.Fatalf("revoked admin: %v", err)
	}
}
