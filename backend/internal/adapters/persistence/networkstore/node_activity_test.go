package networkstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
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

func TestNodeActivityIdenticalReplayKeepsCredentialFence(t *testing.T) {
	t.Setenv("ZBOARD_TEST_MYSQL_DSN", "")
	assertIdenticalActivityReplay(t)
}

func TestMySQLNodeActivityIdenticalReplayKeepsCredentialFence(t *testing.T) {
	if os.Getenv("ZBOARD_TEST_MYSQL_DSN") == "" {
		t.Skip("actual MySQL DSN required")
	}
	assertIdenticalActivityReplay(t)
}

func assertIdenticalActivityReplay(t *testing.T) {
	db, other := administrationFixture(t)
	node := model.Node{Name: "identical-replay", Config: "{}", IsEnabled: true, NodeCredential: "opaque"}
	if err := db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	at := time.Unix(1700000000, 123000000).UTC()
	fixed := db.Session(&gorm.Session{NowFunc: func() time.Time { return at }})
	zeroWrites := 0
	const hook = "test:activity_unchanged_update"
	if err := fixed.Callback().Update().After("gorm:update").Register(hook, func(tx *gorm.DB) {
		if tx.Statement.Table == "nodes" && tx.Error == nil && tx.RowsAffected == 0 {
			zeroWrites++
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fixed.Callback().Update().Remove(hook) })
	flows := uint64(7)
	update := network.NodeActivityUpdate{At: at, Online: true, ConnectorSeen: true, CounterWriter: network.CounterWriterHeartbeat, ActiveFlows: &flows}
	service := network.NodeActivity{Repository: NodeActivity{DB: fixed}}
	for i := 0; i < 2; i++ {
		if err := service.Record(context.Background(), node.ID, "opaque", update); err != nil {
			t.Fatal("identical valid heartbeat rejected", i, err)
		}
	}
	if db.Dialector.Name() == "mysql" && zeroWrites == 0 {
		t.Fatal("fixture did not exercise actual MySQL unchanged-row semantics")
	}
	var state model.NodeObservationState
	if err := other.First(&state, "node_id = ? AND asset_tag = ?", node.ID, node.AssetTag).Error; err != nil || state.CurrentKnown || state.CurrentSource != "heartbeat" || state.CounterWrittenStamp != at.Format(time.RFC3339Nano) {
		t.Fatal("identical replay lost counter evidence", state, err)
	}
	if err := other.Model(&model.Node{}).Where("id = ?", node.ID).Update("node_credential", "rotated").Error; err != nil {
		t.Fatal(err)
	}
	if err := service.Record(context.Background(), node.ID, "opaque", update); !errors.Is(err, network.ErrNodeActivityCredential) {
		t.Fatal("rotated credential replay accepted", err)
	}
}
