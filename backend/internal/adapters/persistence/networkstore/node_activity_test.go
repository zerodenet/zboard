package networkstore

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestNodeActivityFencesCredentialAndProjectsState(t *testing.T) {
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "node-activity.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	node := model.Node{Name: "activity", Config: "{}", IsEnabled: true, NodeCredential: "opaque"}
	if err := db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	nodeID := node.ID
	service := network.NodeActivity{Repository: NodeActivity{DB: db}}
	now, version := time.Now().UTC(), "build-1"
	flows, up, down, uptime := uint64(3), uint64(5), uint64(7), uint64(11)
	if err := service.Record(context.Background(), node.ID, "opaque", network.NodeActivityUpdate{At: now, Online: true, ConnectorSeen: true, Version: &version, ActiveFlows: &flows, BytesUp: &up, BytesDown: &down, UptimeSeconds: &uptime}); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&node, node.ID).Error; err != nil || !node.IsOnline || node.ConnectorLastSeenAt == nil || node.Version != version || node.ActiveFlows != flows {
		t.Fatalf("node activity = %+v err=%v", node, err)
	}
	if err := service.Record(context.Background(), node.ID, "wrong", network.NodeActivityUpdate{At: now, Online: true, ConnectorSeen: true}); !errors.Is(err, network.ErrNodeActivityCredential) {
		t.Fatalf("credential error = %v", err)
	}
	if err := service.Record(context.Background(), nodeID, "opaque", network.NodeActivityUpdate{At: now.Add(time.Second), Online: false}); err != nil {
		t.Fatal(err)
	}
	node = model.Node{}
	if err := db.First(&node, nodeID).Error; err != nil || node.IsOnline || node.ConnectorLastSeenAt != nil {
		t.Fatalf("stopped node activity = %+v err=%v", node, err)
	}
}
