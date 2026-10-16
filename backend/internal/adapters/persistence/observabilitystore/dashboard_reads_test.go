package observabilitystore

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/observability"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestDashboardCurrentDoesNotWaitForReportingAndNeverWrites(t *testing.T) {
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "read.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	read, closeRead, err := datastore.OpenReadView(db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = closeRead() })
	rp, _ := read.DB()
	if rp == pool {
		t.Fatal("test requires independent WAL reporting pool")
	}
	rp.SetMaxOpenConns(1)
	held, err := rp.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	now := time.Now().UTC()
	node := model.Node{Name: "untouched", IsEnabled: true, ConnectorLastSeenAt: &now}
	sub := model.Subscription{UserID: 1, Status: "active", EndAt: now.Add(time.Hour), FlowTotal: 1000, FlowUsed: 123}
	if err := db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	var before model.Node
	db.First(&before, node.ID)
	period := observability.DashboardPeriod{From: now.Add(-time.Hour), To: now, PreviousFrom: now.Add(-2 * time.Hour), PreviousTo: now.Add(-time.Hour)}
	service := observability.Dashboard{Repository: Dashboard{DB: db, ReadDB: read}}
	for _, view := range []string{"statistics", "trend"} {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		_, err := service.View(ctx, period, now, []observability.DashboardTrendBucket{{Key: "b000", StartUTC: period.From, EndUTC: now}}, view)
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("%s: %v", view, err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	snapshot, err := service.View(ctx, period, now, nil, "current")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Service.OnlineNodes != 1 || snapshot.Business.ActiveSubscriptions != 1 || snapshot.Service.ActiveFlows != nil {
		t.Fatalf("current=%+v", snapshot)
	}
	// Current reads must not even reference historical tables, including legacy
	// full-read code paths accidentally hidden behind one HTTP request.
	if err := db.Migrator().DropTable("orders", "traffic_records", "traffic_usage_hourly"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.View(ctx, period, now, nil, "current"); err != nil {
		t.Fatal(err)
	}
	var after model.Node
	db.First(&after, node.ID)
	var afterSub model.Subscription
	db.First(&afterSub, sub.ID)
	if !reflect.DeepEqual(before, after) || afterSub.FlowUsed != 123 {
		t.Fatal("read modified node or quota state")
	}
	if err := db.Model(&model.Subscription{}).Where("id = ?", sub.ID).Update("flow_used", 1000).Error; err != nil {
		t.Fatal(err)
	}
	next, err := service.View(ctx, period, now, nil, "current")
	if err != nil || next.Business.ActiveSubscriptions != 0 || next.Subscriptions.QuotaExhausted != 1 {
		t.Fatalf("quota truth=%+v err=%v", next, err)
	}
	cancelled, stop := context.WithCancel(context.Background())
	stop()
	if _, err := service.View(cancelled, period, now, nil, "current"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled=%v", err)
	}
}
