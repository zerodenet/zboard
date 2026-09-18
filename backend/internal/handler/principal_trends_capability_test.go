package handler

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"strconv"
	"testing"
	"time"
)

func TestPrincipalTrendsUseAuthorizedScopeAndExplicitDayBoundaries(t *testing.T) {
	h, token := newAnnouncementTestHandlers(t)
	actor, err := h.authFromRequest(announcementRequest("GET", "/", token, ""))
	if err != nil {
		t.Fatal(err)
	}
	// The ordinary read path must also work when the primary pool has one connection.
	pool, _ := h.db.DB()
	pool.SetMaxOpenConns(1)
	start := time.Date(2026, 9, 10, 16, 0, 0, 0, time.UTC)
	buckets := []metering.TrendBucket{{Key: "2026-09-11", StartUTC: start, EndUTC: start.Add(24 * time.Hour)}, {Key: "2026-09-12", StartUTC: start.Add(24 * time.Hour), EndUTC: start.Add(48 * time.Hour)}}
	for i, row := range []struct {
		id   uint
		at   time.Time
		peak uint64
	}{{actor.UserID, start.Add(time.Hour), 5}, {actor.UserID, start.Add(24 * time.Hour), 9}, {actor.UserID, start.Add(48 * time.Hour), 99}, {999, start.Add(time.Hour), 88}} {
		fact := principalFlowScopeObservation{ScopeType: "user", ScopeID: row.id, NodeID: uint(i + 1), EventID: "trend-" + strconv.Itoa(i), CoreInstanceID: "test", ObservedAt: row.at, ActiveFlows: row.peak}
		if err = h.db.Create(&fact).Error; err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	service := h.services.PrincipalTrends()
	query := metering.PrincipalTrendQuery{UserID: 999, Buckets: buckets}
	rows, err := service.Read(ctx, actor.UserID, query)
	if err != nil || len(rows) != 2 || rows[0].Day != "2026-09-11" || rows[0].Peak != 5 || rows[1].Peak != 9 {
		t.Fatalf("scope/day boundaries: %+v %v", rows, err)
	}
	other := model.Subscription{UserID: 999, Config: "{}", EndAt: start.Add(72 * time.Hour)}
	if err = h.db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	query.SubscriptionID = other.ID
	rows, err = service.Read(ctx, actor.UserID, query)
	if err != nil || len(rows) != 0 {
		t.Fatalf("foreign subscription: %+v %v", rows, err)
	}
	query.SubscriptionID = 0
	query.Administrative = true
	if _, err = service.Read(ctx, actor.UserID, query); !errors.Is(err, metering.ErrTrendPermission) {
		t.Fatalf("admin spoof: %v", err)
	}
	if err = h.db.Model(&model.User{}).Where("id = ?", actor.UserID).Update("is_admin", true).Error; err != nil {
		t.Fatal(err)
	}
	rows, err = service.Read(ctx, actor.UserID, query)
	if err != nil || len(rows) != 1 || rows[0].Peak != 88 {
		t.Fatalf("admin scope: %+v %v", rows, err)
	}
	if err = h.db.Model(&model.User{}).Where("id = ?", actor.UserID).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err = service.Read(ctx, actor.UserID, query); !errors.Is(err, metering.ErrTrendPermission) {
		t.Fatalf("revoked admin: %v", err)
	}
}
