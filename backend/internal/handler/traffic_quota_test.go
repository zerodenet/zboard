package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestTrafficQuotaReadsAllOwnedEntitlementsWithoutHistory(t *testing.T) {
	f := newTrafficReadFixture(t)
	end := time.Now().UTC().Add(time.Hour)
	for i := 1; i <= 6; i++ {
		sub := model.Subscription{ID: uint(i), UserID: 1, Status: "active", EndAt: end, FlowTotal: 1000, FlowUsed: int64(i * 10)}
		if err := f.h.db.Create(&sub).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, sub := range []model.Subscription{
		{UserID: 1, Status: "expired", EndAt: end, FlowTotal: 1000000},
		{UserID: 1, Status: "active", EndAt: end.Add(-2 * time.Hour), FlowTotal: 1000000},
		{UserID: 1, Status: "active", EndAt: end, FlowTotal: 100, FlowUsed: 100},
		{UserID: 2, Status: "active", EndAt: end, FlowTotal: 5000, FlowUsed: 500},
	} {
		if err := f.h.db.Create(&sub).Error; err != nil {
			t.Fatal(err)
		}
	}
	f.capture()
	var wire map[string]json.RawMessage
	f.get(t, "/api/v1/traffic/summary?view=quota&user_id=2", false, f.h.TrafficSummaryHandler, &wire)
	var remaining int64
	if err := json.Unmarshal(wire["remaining_bytes"], &remaining); err != nil || remaining != 5790 {
		t.Fatalf("remaining=%d err=%v", remaining, err)
	}
	if len(wire) != 3 || wire["total_used_bytes"] != nil || wire["used_bytes_today"] != nil {
		t.Fatalf("quota response pretended to include history: %v", wire)
	}
	aggregates := 0
	for _, query := range f.log.queries {
		if strings.Contains(query, "traffic_records") || strings.Contains(query, "traffic_usage_hourly") {
			t.Fatalf("quota waited for history: %s", query)
		}
		if strings.Contains(query, "SUM(flow_total - flow_used)") {
			aggregates++
		}
	}
	if aggregates != 1 {
		t.Fatalf("quota aggregate count=%d", aggregates)
	}
	service := f.h.services.UsageQuota()
	admin, err := service.Read(context.Background(), 99, metering.UsageSummaryQuery{Administrative: true, UserID: 2})
	if err != nil || admin.RemainingBytes != 4500 || admin.ScopeUser != 2 {
		t.Fatalf("admin scope=%+v err=%v", admin, err)
	}
	if err := f.h.db.Model(&model.Subscription{}).Where("id = 1").Update("flow_used", 20).Error; err != nil {
		t.Fatal(err)
	}
	next, err := service.Read(context.Background(), 1, metering.UsageSummaryQuery{UserID: 2})
	if err != nil || next.RemainingBytes != 5780 || next.AsOf == "" {
		t.Fatalf("live quota=%+v err=%v", next, err)
	}
	if err := f.h.db.Model(&model.User{}).Where("id = 99").Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.Read(context.Background(), 99, metering.UsageSummaryQuery{Administrative: true}); !errors.Is(err, metering.ErrTrendPermission) {
		t.Fatalf("revoked admin: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.Read(ctx, 1, metering.UsageSummaryQuery{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled quota=%v", err)
	}
}

func TestTrafficQuotaDoesNotWaitForOccupiedHistoryPool(t *testing.T) {
	f := newTrafficReadFixture(t)
	sub := model.Subscription{UserID: 1, Status: "active", EndAt: time.Now().UTC().Add(time.Hour), FlowTotal: 1000, FlowUsed: 100}
	if err := f.h.db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	closeRead, err := f.h.ConfigureTrafficReads()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = closeRead() })
	reporting := f.h.services.UsageSummary().Repository.(meteringstore.UsageSummary).ReadDB
	pool, err := reporting.DB()
	if err != nil {
		t.Fatal(err)
	}
	authority, err := f.h.db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if pool == authority {
		t.Fatal("fixture requires an independent WAL reporting pool")
	}
	pool.SetMaxOpenConns(1)
	held, err := pool.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	blocked, stop := context.WithTimeout(context.Background(), 50*time.Millisecond)
	_, historyErr := f.h.services.UsageSummary().Read(blocked, 1, metering.UsageSummaryQuery{})
	stop()
	if !errors.Is(historyErr, context.DeadlineExceeded) {
		t.Fatalf("fixture did not block history pool: %v", historyErr)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, err := f.h.services.UsageQuota().Read(ctx, 1, metering.UsageSummaryQuery{})
	if err != nil || got.RemainingBytes != 900 {
		t.Fatalf("quota waited behind history: %+v %v", got, err)
	}
}

func TestTrafficQuotaRejectsUnknownViewAndUnauthenticatedRequests(t *testing.T) {
	f := newTrafficReadFixture(t)
	for _, item := range []struct {
		token, view string
		code        int
	}{{f.token, "unknown", 400}, {"", "quota", 401}} {
		response := httptest.NewRecorder()
		f.h.TrafficSummaryHandler(response, announcementRequest(http.MethodGet, "/api/v1/traffic/summary?view="+item.view, item.token, ""))
		if response.Code != item.code {
			t.Fatalf("status=%d want=%d", response.Code, item.code)
		}
	}
}

func TestMySQLTrafficQuotaMatchesFullSummaryWithoutHistoryDependency(t *testing.T) {
	h, credential := accountingFixtureFromOrder(t, newMySQLOrderFixture(t))
	var subscription model.Subscription
	if err := h.db.First(&subscription, credential.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	quota, err := h.services.UsageQuota().Read(context.Background(), subscription.UserID, metering.UsageSummaryQuery{})
	if err != nil {
		t.Fatal(err)
	}
	full, err := h.services.UsageSummary().Read(context.Background(), subscription.UserID, metering.UsageSummaryQuery{})
	if err != nil || quota.RemainingBytes != full.RemainingBytes {
		t.Fatalf("quota=%+v full=%+v err=%v", quota, full, err)
	}
	if err := h.projectZeroNodeEvents(context.Background(), accountingBenchmarkEvents(credential, 0, 1)); err != nil {
		t.Fatal(err)
	}
	next, err := h.services.UsageQuota().Read(context.Background(), subscription.UserID, metering.UsageSummaryQuery{})
	if err != nil || next.RemainingBytes != quota.RemainingBytes-30 {
		t.Fatalf("live MySQL quota=%+v err=%v", next, err)
	}
}
