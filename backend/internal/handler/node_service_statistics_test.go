package handler

import (
	"fmt"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestNodeStatisticsCountFrontingEntriesAsProtocolServices(t *testing.T) {
	f, entryNode, landingEndpoint := networkEntryFixture(t)

	direct := model.ProtocolEndpoint{
		NodeID: entryNode.ID, Name: "A direct", RuntimeKey: "entry-direct", Protocol: "trojan",
		Address: entryNode.Address, Port: 10443, PublicPort: 10443, IsActive: true,
		ClientConfig: `{"type":"trojan"}`, OptionalConfig: "{}", Tags: "[]",
	}
	if err := f.h.db.Create(&direct).Error; err != nil {
		t.Fatal(err)
	}
	disabledDirect := model.ProtocolEndpoint{
		NodeID: entryNode.ID, Name: "A disabled", RuntimeKey: "entry-disabled", Protocol: "trojan",
		Address: entryNode.Address, Port: 11443, PublicPort: 11443, IsActive: false,
		ClientConfig: `{"type":"trojan"}`, OptionalConfig: "{}", Tags: "[]",
	}
	if err := f.h.db.Create(&disabledDirect).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Model(&disabledDirect).Update("is_active", false).Error; err != nil {
		t.Fatal(err)
	}
	saveEntryForTest(t, f, entryNode, landingEndpoint, "")
	disabledEntry := model.NetworkEntry{
		Name: "A disabled entry", NodeID: entryNode.ID, EndpointID: landingEndpoint.ID,
		Address: entryNode.Address, Port: 12346, PublicPort: 23457, Network: "tcp_udp", Enabled: false,
	}
	if err := f.h.db.Create(&disabledEntry).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Model(&disabledEntry).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}

	var page struct {
		Items []nodeListItem `json:"items"`
	}
	f.get(t, "/api/v1/nodes?paged=true&sort=id&direction=asc", true, f.h.NodeListHandler, &page)
	counts := make(map[uint]int64, len(page.Items))
	for _, item := range page.Items {
		counts[item.ID] = item.EnabledProtocolCount
	}
	if counts[entryNode.ID] != 2 {
		t.Fatalf("entry node enabled protocol count = %d, want direct endpoint + fronting entry", counts[entryNode.ID])
	}
	if counts[landingEndpoint.NodeID] != 1 {
		t.Fatalf("landing node enabled protocol count = %d, want landing endpoint only", counts[landingEndpoint.NodeID])
	}

	var detail nodeDetailItem
	f.get(t, fmt.Sprintf("/api/v1/nodes/%d", entryNode.ID), true, f.h.NodeDetailHandler, &detail)
	if detail.EnabledProtocolCount != 2 {
		t.Fatalf("entry node detail enabled protocol count = %d, want 2", detail.EnabledProtocolCount)
	}

	var landingDetail nodeDetailItem
	f.get(t, fmt.Sprintf("/api/v1/nodes/%d", landingEndpoint.NodeID), true, f.h.NodeDetailHandler, &landingDetail)
	if landingDetail.EnabledProtocolCount != 1 {
		t.Fatalf("landing node detail enabled protocol count = %d, want 1", landingDetail.EnabledProtocolCount)
	}
}
