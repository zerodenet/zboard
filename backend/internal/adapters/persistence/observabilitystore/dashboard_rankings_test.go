package observabilitystore

import (
	"context"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/capabilities/observability"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"path/filepath"
	"testing"
	"time"
)

func TestDashboardRankingsExactPartialHoursAndBoundedQueries(t *testing.T) {
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "rank.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 28, 10, 25, 0, 0, time.UTC)
	period := observability.DashboardPeriod{From: now.Add(-24 * time.Hour), To: now, PreviousFrom: now.Add(-48 * time.Hour), PreviousTo: now.Add(-24 * time.Hour)}
	for i := 1; i <= 12; i++ {
		if err := db.Create(&model.Node{ID: uint(i), Name: fmt.Sprintf("node-%d", i)}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.User{ID: uint(i), Email: fmt.Sprintf("user-%d@example.test", i), Password: "unused", Status: "active"}).Error; err != nil {
			t.Fatal(err)
		}
		for _, r := range []model.TrafficRecord{{UserID: uint(i), NodeID: uint(i), UsedBytes: int64(i) * 10, At: now.Add(-time.Hour)}, {UserID: uint(i), NodeID: uint(i), UsedBytes: int64(i) * 3, At: period.PreviousFrom.Add(time.Hour)}} {
			r.ReportID = fmt.Sprintf("%d-%d", r.NodeID, r.At.UnixNano())
			r.Nonce = r.ReportID
			if err := db.Create(&r).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	// Same UTC hour but outside the previous window must not leak into comparisons.
	for _, r := range []model.TrafficRecord{{UserID: 12, NodeID: 12, UsedBytes: 999, At: period.From.Add(5 * time.Minute)}, {UserID: 12, NodeID: 12, UsedBytes: 9999, At: now.Add(time.Minute)}, {UserID: 99, NodeID: 99, UsedBytes: 9000, At: period.PreviousFrom.Add(time.Hour)}} {
		r.ReportID = fmt.Sprintf("%d-%d", r.NodeID, r.At.UnixNano())
		r.Nonce = r.ReportID
		if err := db.Create(&r).Error; err != nil {
			t.Fatal(err)
		}
	}
	counter := &dashboardQueryCounter{Interface: db.Config.Logger}
	read := db.Session(&gorm.Session{Logger: counter})
	result, err := (Dashboard{DB: read}).LoadTrafficRankings(context.Background(), period)
	if err != nil {
		t.Fatal(err)
	}
	if counter.queries.Load() != 2 {
		t.Fatalf("queries %d", counter.queries.Load())
	}
	if len(result.Nodes) != 10 || len(result.Users) != 10 {
		t.Fatalf("rank lengths %d %d", len(result.Nodes), len(result.Users))
	}
	for _, items := range [][]observability.DashboardTrafficRanking{result.Nodes, result.Users} {
		if items[0].ID != 12 || items[0].TrafficBytes != 1119 || items[0].PreviousTrafficBytes != 36 {
			t.Fatalf("top %+v", items[0])
		}
		if items[9].ID != 3 {
			t.Fatalf("cutoff %+v", items[9])
		}
	}
	counter.queries.Store(0)
	nodes, err := (Dashboard{DB: read}).LoadTrafficRankings(context.Background(), period, "nodes")
	if err != nil {
		t.Fatal(err)
	}
	if counter.queries.Load() != 1 || len(nodes.Nodes) != 10 || len(nodes.Users) != 0 {
		t.Fatalf("nodes=%+v queries=%d", nodes, counter.queries.Load())
	}

}
