package observabilitystore

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/observability"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestDashboardExcludesArchivedAndDeletedPublicationFailures(t *testing.T) {
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "visibility.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	nodes := []model.Node{
		{Name: "current", IsEnabled: true, LastSeenAt: &now, ConnectorLastSeenAt: &now, SSHVerifiedAt: &now, SSHHostKeyFingerprint: "verified", TrafficSecretPrefix: "present"},
		{Name: "archived", IsEnabled: true, LastSeenAt: &now, ConnectorLastSeenAt: &now, SSHVerifiedAt: &now, SSHHostKeyFingerprint: "verified", TrafficSecretPrefix: "present", LifecycleStatus: "archived", ArchivedAt: &now},
		{Name: "archived-marker", IsEnabled: true, LifecycleStatus: "active", ArchivedAt: &now},
	}
	if err := db.Create(&nodes).Error; err != nil {
		t.Fatal(err)
	}
	endpoints := []model.ProtocolEndpoint{
		{Name: "current failed", NodeID: nodes[0].ID, Protocol: "shadowsocks", IsActive: true},
		{Name: "archived failed", NodeID: nodes[1].ID, Protocol: "shadowsocks", IsActive: true},
		{Name: "archived marker failed", NodeID: nodes[2].ID, Protocol: "shadowsocks", IsActive: true},
		{Name: "deleted failed", NodeID: nodes[0].ID, Protocol: "shadowsocks", IsActive: true},
		{Name: "disable not applied", NodeID: nodes[0].ID, Protocol: "shadowsocks", IsActive: true},
		{Name: "recovered", NodeID: nodes[0].ID, Protocol: "shadowsocks", IsActive: true},
	}
	for i := range endpoints {
		endpoints[i].RuntimeKey = fmt.Sprintf("00000000-0000-0000-0000-%012d", i+1)
		if err := db.Create(&endpoints[i]).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.ProtocolDeployment{ProtocolEndpointID: endpoints[i].ID, NodeID: endpoints[i].NodeID, Status: "failed", Error: "historical SSH timeout"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Model(&endpoints[4]).Update("is_active", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&endpoints[3]).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ProtocolDeployment{ProtocolEndpointID: endpoints[5].ID, NodeID: nodes[0].ID, Status: "succeeded"}).Error; err != nil {
		t.Fatal(err)
	}
	// Node-wide historical attempts cannot be opened through an endpoint filter.
	if err := db.Create(&model.ProtocolDeployment{NodeID: nodes[0].ID, Status: "failed"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(datastore.RebuildTrafficHourly); err != nil {
		t.Fatal(err)
	}
	repo := Dashboard{DB: db}
	period := observability.DashboardPeriod{From: now.Add(-time.Hour), To: now, PreviousFrom: now.Add(-2 * time.Hour), PreviousTo: now.Add(-time.Hour)}
	for _, view := range []string{"current", ""} {
		result, err := repo.LoadDashboardView(context.Background(), period, now, nil, view)
		if err != nil {
			t.Fatal(view, err)
		}
		if result.Attention.UnresolvedDeployments != 2 || result.Infrastructure.NodesTotal != 1 || result.Infrastructure.ActiveProtocolEndpoints != 2 {
			t.Fatalf("%s: %+v %+v", view, result.Attention, result.Infrastructure)
		}
	}
	totals, err := repo.LoadDashboardTotals(context.Background(), now)
	if err != nil || totals.FailedDeployments != 2 || totals.Nodes != 1 || totals.OfflineNodes != 0 {
		t.Fatal(totals, err)
	}
	if err := db.Model(&nodes[0]).Updates(map[string]any{"lifecycle_status": "archived", "archived_at": now}).Error; err != nil {
		t.Fatal(err)
	}
	current, err := repo.LoadDashboardView(context.Background(), period, now, nil, "current")
	if err != nil || current.Attention.UnresolvedDeployments != 0 || current.Infrastructure.NodesTotal != 0 {
		t.Fatal(current, err)
	}
	var failed int64
	if err := db.Model(&model.ProtocolDeployment{}).Where("status='failed'").Count(&failed).Error; err != nil || failed != 7 {
		t.Fatal("historical evidence changed", failed, err)
	}
}
