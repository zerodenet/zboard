package handler

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"testing"
	"time"
)

func TestTrafficUsageCapabilityScopeAndCachedRevocation(t *testing.T) {
	f := newTrafficReadFixture(t)
	f.seedUsage(t)
	service := f.h.services.Usage(trafficStatisticsCacheAdapter{&f.h.trafficStatisticsCache}, nil)
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	q := metering.UsageQuery{RecordsQuery: metering.RecordsQuery{UserID: 2, From: from, To: from.Add(24 * time.Hour), Limit: 2}, Bucket: "hour", IncludeTotals: true}
	out, err := service.Read(context.Background(), 1, q)
	if err != nil || out.Statistics == nil || out.Statistics.Aggregates.UsedBytes != 210 || len(out.Rows) != 2 {
		t.Fatalf("own scope: %+v %v", out, err)
	}
	for _, v := range out.Rows {
		if v.UserID != 1 {
			t.Fatal("foreign row")
		}
	}
	q.IncludeTotals = false
	out, err = service.Read(context.Background(), 1, q)
	if err != nil || out.Statistics != nil || len(out.Rows) != 2 {
		t.Fatalf("omitted totals: %+v %v", out, err)
	}
	q.IncludeTotals = true
	q.SummaryOnly = true
	q.Administrative = true
	q.UserID = 0
	if _, err = service.Read(context.Background(), 99, q); err != nil {
		t.Fatal(err)
	}
	if err = f.h.db.Model(&model.User{}).Where("id = 99").Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err = service.Read(context.Background(), 99, q); !errors.Is(err, metering.ErrTrendPermission) {
		t.Fatalf("cached revoked scope: %v", err)
	}
	q.Administrative = false
	q.Limit = 201
	if _, err = service.Read(context.Background(), 1, q); err == nil {
		t.Fatal("unbounded page accepted")
	}
}
