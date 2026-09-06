package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type settlementDuringRead struct {
	logger.Interface
	afterSubscriptionRead func()
}

func (l *settlementDuringRead) Trace(_ context.Context, _ time.Time, sql func() (string, int64), _ error) {
	query, _ := sql()
	if l.afterSubscriptionRead != nil && strings.HasPrefix(query, "SELECT subscriptions.* FROM") {
		callback := l.afterSubscriptionRead
		l.afterSubscriptionRead = nil
		callback()
	}
}

func TestTrafficReconciliationUsesOneSnapshotDuringConcurrentSettlement(t *testing.T) {
	f := newTrafficReadFixture(t)
	sub := model.Subscription{ID: 1, UserID: 1, FlowUsed: 30, FlowTotal: 1000, Status: subStatusActive}
	if err := f.h.db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	record := model.TrafficRecord{SubscriptionID: 1, UserID: 1, NodeID: 1, ReportID: "before", Nonce: "before", UsedBytes: 30, At: time.Now().UTC()}
	if err := f.h.db.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	closeView, err := f.h.ConfigureTrafficReads()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = closeView() })
	committed := false
	trace := &settlementDuringRead{Interface: logger.Discard, afterSubscriptionRead: func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := f.h.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Model(&sub).Update("flow_used", 100).Error; err != nil {
				return err
			}
			additional := model.TrafficRecord{SubscriptionID: 1, UserID: 1, NodeID: 1, ReportID: "during", Nonce: "during", UsedBytes: 70, At: time.Now().UTC()}
			return tx.Create(&additional).Error
		})
		if err != nil {
			t.Fatalf("settlement blocked by reporting snapshot: %v", err)
		}
		committed = true
	}}
	f.h.trafficReadDB = f.h.trafficReadDB.Session(&gorm.Session{Logger: trace})
	path := "/api/v1/admin/traffic/reconciliation?paged=true"
	var first, next struct {
		Items      []trafficReconciliationItem
		Aggregates trafficReconciliationAggregates
	}
	f.get(t, path, true, f.h.TrafficReconciliationHandler, &first)
	if !committed || len(first.Items) != 1 || first.Items[0].Difference != 0 || first.Items[0].RecordedBytes != 30 || first.Aggregates.FlowUsed != 30 {
		t.Fatalf("concurrent settlement manufactured a discrepancy: committed=%t response=%+v", committed, first)
	}
	f.get(t, path, true, f.h.TrafficReconciliationHandler, &next)
	if len(next.Items) != 1 || next.Items[0].Difference != 0 || next.Items[0].RecordedBytes != 100 || next.Aggregates.FlowUsed != 100 {
		t.Fatalf("new reconciliation did not observe settlement: %+v", next)
	}
}

func TestTrafficReportingHandlersUseIsolatedReadConnection(t *testing.T) {
	f := newTrafficReadFixture(t)
	f.seedUsage(t)
	closeView, err := f.h.ConfigureTrafficReads()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = closeView() })
	for _, endpoint := range []struct {
		path string
		run  http.HandlerFunc
	}{
		{"/api/v1/admin/traffic/records?view=usage_summary&from=2026-09-01&to=2026-09-01", f.h.TrafficUsageRecordsHandler},
		{"/api/v1/admin/traffic/records?paged=true&include_totals=false&from=2026-09-01&to=2026-09-01", f.h.TrafficUsageRecordsHandler},
		{"/api/v1/admin/traffic/trends?from=2026-09-01&to=2026-09-01", f.h.TrafficTrendsSystemCalendarWithPrincipalFlowReplayHandler},
		{"/api/v1/admin/traffic/reconciliation?paged=true", f.h.TrafficReconciliationHandler},
	} {
		trace := &trafficQueryLog{Interface: logger.Discard}
		f.h.trafficReadDB = f.h.trafficReadDB.Session(&gorm.Session{Logger: trace})
		var ignored interface{}
		f.get(t, endpoint.path, true, endpoint.run, &ignored)
		if len(trace.queries) == 0 {
			t.Fatalf("reporting bypassed read connection: %s", endpoint.path)
		}
	}
}
