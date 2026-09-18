package networkstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func seedRemovalTopology(t *testing.T, db *gorm.DB) []model.NetworkEntry {
	t.Helper()
	prepareProviderDirectory(t, db)
	for _, row := range []any{
		&model.Node{ID: 2, Name: "survivor", Config: "{}"},
		&model.ProtocolEndpoint{ID: 31, NodeID: 1, Name: "landing", RuntimeKey: "00000000-0000-4000-8000-000000000031", Protocol: "trojan", Port: 443, OptionalConfig: "{}", Tags: "[]"},
		&model.ProtocolEndpoint{ID: 32, NodeID: 2, Name: "surviving", RuntimeKey: "00000000-0000-4000-8000-000000000032", Protocol: "trojan", Port: 443, OptionalConfig: "{}", Tags: "[]"},
		&model.NodeGroup{ID: 1, Name: "group", Code: "topology", Revision: 1},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	entries := []model.NetworkEntry{{Name: "first", NodeID: 2, EndpointID: 31, Port: 1001, Enabled: true}, {Name: "second", NodeID: 2, EndpointID: 31, Port: 1002, Enabled: true}, {Name: "owned", NodeID: 1, EndpointID: 32, Port: 1003, Enabled: true}}
	for i := range entries {
		if err := db.Create(&entries[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, entry := range entries[:2] {
		if err := db.Create(&model.NodeGroupNetworkEntry{NodeGroupID: 1, NetworkEntryID: entry.ID}).Error; err != nil {
			t.Fatal(err)
		}
	}
	return entries
}
func TestTopologyRemovalCoalescesWithdrawalAndPreservesPublicationLease(t *testing.T) {
	db, _ := administrationFixture(t)
	seedRemovalTopology(t, db)
	until := time.Now().UTC().Truncate(time.Millisecond).Add(time.Minute)
	pending := model.NodeConfigPublish{NodeID: 2, Generation: 5, Attempts: 2, LastError: "previous", NextAttemptAt: until, LeaseUntil: until, LeaseToken: "current-worker"}
	if err := db.Create(&pending).Error; err != nil {
		t.Fatal(err)
	}
	result, err := nodeRemovalFixtureService(db).Remove(context.Background(), 1, 1)
	if err != nil || result.Cleanup.NetworkEntries != 3 {
		t.Fatal(result, err)
	}
	var publication model.NodeConfigPublish
	if err := db.First(&publication, "node_id = ?", 2).Error; err != nil {
		t.Fatal(err)
	}
	if publication.Generation != 6 || publication.LeaseToken != "current-worker" || !publication.LeaseUntil.Equal(until) || publication.Attempts != 0 {
		t.Fatal("withdrawal replaced lease or was duplicated", publication.Generation, publication.LeaseToken)
	}
	var group model.NodeGroup
	if err := db.First(&group, 1).Error; err != nil {
		t.Fatal(err)
	}
	if group.Revision != 2 {
		t.Fatal("group was not invalidated once", group.Revision)
	}
	var endpoint model.ProtocolEndpoint
	if err := db.First(&endpoint, 32).Error; err != nil {
		t.Fatal("survivor endpoint removed", err)
	}
	var count int64
	if err := db.Model(&model.NodeConfigPublish{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatal("deleted node queued", count, err)
	}
}
func TestTopologyEntryRemovalIsAuthorizedAndAtomic(t *testing.T) {
	db, _ := administrationFixture(t)
	entries := seedRemovalTopology(t, db)
	service := network.TopologyRemoval{Store: TopologyRemoval{DB: db}}
	if err := db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := service.Entry(context.Background(), 1, entries[0].ID); !errors.Is(err, network.ErrResourcePermission) {
		t.Fatal(err)
	}
	if err := db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", true).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Create().Before("gorm:create").Register("fail_topology_audit", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "audit_logs" {
			tx.AddError(errors.New("audit failed"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.Entry(context.Background(), 1, entries[0].ID); err == nil {
		t.Fatal("unaudited deletion")
	}
	db.Callback().Create().Remove("fail_topology_audit")
	var group model.NodeGroup
	if err := db.First(&group, 1).Error; err != nil || group.Revision != 1 {
		t.Fatal(group.Revision, err)
	}
	var queued int64
	if err := db.Model(&model.NodeConfigPublish{}).Count(&queued).Error; err != nil || queued != 0 {
		t.Fatal("failed deletion queued publication", queued, err)
	}
	if err := service.Entry(context.Background(), 1, entries[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := service.Entry(context.Background(), 1, entries[0].ID); !errors.Is(err, network.ErrResourceNotFound) {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&model.NetworkEntry{}).Count(&count).Error; err != nil || count != 2 {
		t.Fatal("wrong selection scope", count, err)
	}
}
