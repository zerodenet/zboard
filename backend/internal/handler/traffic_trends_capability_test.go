package handler

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"testing"
	"time"
)

func TestTrafficTrendCapabilityScopesAndRejectsRevocationBeforeCache(t *testing.T) {
	f := newTrafficReadFixture(t)
	f.seedUsage(t)
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	q := metering.TrafficTrendQuery{UserID: 2, From: from, Days: 1, Timezone: "UTC", Buckets: []metering.TrendBucket{{Key: "2026-09-01", StartUTC: from, EndUTC: from.AddDate(0, 0, 1)}}}
	service := f.h.services.TrafficTrends(trafficTrendCacheAdapter{&f.h.trafficTrendsCache})
	out, err := service.Read(context.Background(), 1, q)
	if err != nil || out.Snapshot.RecordCount != 6 {
		t.Fatalf("own scope: %+v %v", out, err)
	}
	if err = f.h.db.Model(&model.User{}).Where("id = 1").Update("is_admin", true).Error; err != nil {
		t.Fatal(err)
	}
	q.Administrative = true
	q.UserID = 0
	if _, err = service.Read(context.Background(), 1, q); err != nil {
		t.Fatal(err)
	}
	if err = f.h.db.Model(&model.User{}).Where("id = 1").Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err = service.Read(context.Background(), 1, q); !errors.Is(err, metering.ErrTrendPermission) {
		t.Fatalf("cached admin read after revocation: %v", err)
	}
}
