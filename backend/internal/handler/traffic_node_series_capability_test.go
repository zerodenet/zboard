package handler

import (
	"context"
	"errors"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"testing"
	"time"
)

func TestTrafficNodeSeriesCapabilityBoundsScopeAndRevocation(t *testing.T) {
	f := newTrafficReadFixture(t)
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for i := 1; i <= 10; i++ {
		row := model.TrafficRecord{NodeID: uint(i), UserID: 1, UsedBytes: int64(i), At: from, ReportID: fmt.Sprint(i), Nonce: fmt.Sprint(i)}
		if err := f.h.db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := f.h.db.Create(&model.TrafficRecord{NodeID: 99, UserID: 2, UsedBytes: 999, At: from, ReportID: "foreign", Nonce: "foreign"}).Error; err != nil {
		t.Fatal(err)
	}
	service := f.h.services.NodeSeries()
	q := metering.NodeSeriesQuery{UserID: 2, Bucket: "hour", From: from, To: from.Add(24 * time.Hour)}
	out, err := service.Read(context.Background(), 1, q)
	if err != nil || !out.Truncated || len(out.Nodes) != 8 || len(out.Points) != 8 {
		t.Fatalf("bounded series: %+v %v", out, err)
	}
	var used int64
	for _, p := range out.Points {
		if p.NodeID < 3 || p.NodeID > 10 {
			t.Fatalf("unexpected node: %+v", p)
		}
		used += p.UsedBytes
	}
	if used != 52 {
		t.Fatalf("used=%d", used)
	}
	for _, n := range out.Nodes {
		if !n.Missing {
			t.Fatalf("missing reference lost: %+v", n)
		}
	}
	q.NodeID = 99
	out, err = service.Read(context.Background(), 1, q)
	if err != nil || len(out.Points) != 0 || len(out.Nodes) != 0 {
		t.Fatalf("foreign node: %+v %v", out, err)
	}
	q.NodeID = 1
	q.Bucket = "minute"
	q.To = from.Add(8 * 24 * time.Hour)
	if _, err = service.Read(context.Background(), 1, q); err == nil {
		t.Fatal("unbounded minute query accepted")
	}
	q.Bucket = "hour"
	q.Administrative = true
	if _, err = service.Read(context.Background(), 99, q); err != nil {
		t.Fatal(err)
	}
	if err = f.h.db.Model(&model.User{}).Where("id = 99").Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err = service.Read(context.Background(), 99, q); !errors.Is(err, metering.ErrTrendPermission) {
		t.Fatalf("revoked admin: %v", err)
	}
}
