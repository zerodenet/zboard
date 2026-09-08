package handler

import (
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/model"
	"net/http/httptest"
	"testing"
)

func TestNodeDeletionRollsBackAssociationsWhenDatabaseDeletionFails(t *testing.T) {
	f, a, b := networkEntryFixture(t)
	entry := saveEntryForTest(t, f, a, b, "")
	group := model.NodeGroup{Name: "rollback", Code: "rollback"}
	if err := f.h.db.Create(&group).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Create(&model.NodeGroupNetworkEntry{NodeGroupID: group.ID, NetworkEntryID: entry.ID}).Error; err != nil {
		t.Fatal(err)
	}
	f.h.db.Where("1 = 1").Delete(&model.NodeConfigPublish{})
	if err := f.h.db.Exec(fmt.Sprintf("CREATE TRIGGER deny_node_delete BEFORE DELETE ON nodes WHEN OLD.id = %d BEGIN SELECT RAISE(ABORT, 'test delete failure'); END", b.NodeID)).Error; err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	f.h.NodeCascadeDeleteHandler(w, announcementRequest("DELETE", fmt.Sprintf("/api/v1/nodes/%d", b.NodeID), f.admin, ""))
	if w.Code != 500 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if err := f.h.db.First(&entry, entry.ID).Error; err != nil {
		t.Fatal("failed deletion lost entry")
	}
	var links int64
	f.h.db.Model(&model.NodeGroupNetworkEntry{}).Where("network_entry_id = ?", entry.ID).Count(&links)
	if links != 1 {
		t.Fatal("failed deletion lost group link")
	}
	if err := f.h.db.First(&b, b.ID).Error; err != nil {
		t.Fatal("failed deletion lost landing protocol")
	}
	var queued int64
	f.h.db.Model(&model.NodeConfigPublish{}).Count(&queued)
	if queued != 0 {
		t.Fatal("failed deletion committed runtime withdrawal")
	}
}

func TestActiveProtocolDeletionClearsReferencesAndQueuesWithoutSSH(t *testing.T) {
	f, a, b := networkEntryFixture(t)
	entry := saveEntryForTest(t, f, a, b, "")
	group := model.NodeGroup{Name: "published", Code: "published"}
	if err := f.h.db.Create(&group).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Create(&model.NodeGroupEndpoint{NodeGroupID: group.ID, ProtocolEndpointID: b.ID}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Create(&model.NodeGroupNetworkEntry{NodeGroupID: group.ID, NetworkEntryID: entry.ID}).Error; err != nil {
		t.Fatal(err)
	}
	f.h.db.Where("1 = 1").Delete(&model.NodeConfigPublish{})
	w := httptest.NewRecorder()
	f.h.ProtocolEndpointDeleteHandler(w, announcementRequest("DELETE", fmt.Sprintf("/api/v1/admin/protocol-endpoints/%d", b.ID), f.admin, ""))
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var entries, links, queued int64
	f.h.db.Model(&model.NetworkEntry{}).Where("id = ?", entry.ID).Count(&entries)
	f.h.db.Model(&model.NodeGroupEndpoint{}).Where("node_group_id = ?", group.ID).Count(&links)
	f.h.db.Model(&model.NodeConfigPublish{}).Where("node_id IN ?", []uint{a.ID, b.NodeID}).Count(&queued)
	if entries != 0 || links != 0 || queued != 2 {
		t.Fatalf("entries=%d links=%d queued=%d", entries, links, queued)
	}
	var node model.Node
	if err := f.h.db.First(&node, b.NodeID).Error; err != nil {
		t.Fatal("protocol deletion deleted VPS")
	}
}
