package networkstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type networkEntryQueryCounter struct {
	logger.Interface
	count atomic.Int64
}

func (c *networkEntryQueryCounter) Trace(context.Context, time.Time, func() (string, int64), error) {
	c.count.Add(1)
}

func TestNetworkEntryQueriesAggregateRedactAndRemainBounded(t *testing.T) {
	db, _ := administrationFixture(t)
	endpoint, firstGroup := seedNetworkEntryMutationFixture(t, db)
	secondGroup := model.NodeGroup{Name: "Alpha", Code: "alpha", Description: "second", IsEnabled: true, Revision: 3}
	if err := db.Create(&secondGroup).Error; err != nil {
		t.Fatal(err)
	}
	entries := []model.NetworkEntry{
		{Name: "newer", Network: "tcp_udp", NodeID: 1, EndpointID: endpoint.ID, Address: "newer.example.test", Port: 1443, PublicPort: 2443, Enabled: true, PathConfig: "encrypted-path-secret", Revision: 2},
		{Name: "older", Network: "tcp", NodeID: 1, EndpointID: endpoint.ID, Address: "older.example.test", Port: 1444, PublicPort: 2444, Enabled: true, Revision: 1},
	}
	if err := db.Create(&entries).Error; err != nil {
		t.Fatal(err)
	}
	links := []model.NodeGroupNetworkEntry{
		{NodeGroupID: firstGroup.ID, NetworkEntryID: entries[0].ID, SortOrder: 8},
		{NodeGroupID: secondGroup.ID, NetworkEntryID: entries[0].ID, SortOrder: 2},
	}
	if err := db.Create(&links).Error; err != nil {
		t.Fatal(err)
	}
	publications := []model.NodeConfigPublish{
		{NodeID: 1, EndpointID: endpoint.ID, RequestedBy: 1, Generation: 1, LastError: "alpha"},
		{NodeID: endpoint.NodeID, EndpointID: endpoint.ID, RequestedBy: 1, Generation: 1, LastError: "zeta"},
	}
	if err := db.Create(&publications).Error; err != nil {
		t.Fatal(err)
	}

	counter := &networkEntryQueryCounter{Interface: logger.Discard}
	service := network.NetworkEntryQueries{Repository: NetworkEntryQueries{DB: db.Session(&gorm.Session{Logger: counter})}}
	items, err := service.List(context.Background(), 1)
	if err != nil || len(items) != 2 {
		t.Fatalf("items=%+v error=%v", items, err)
	}
	if items[0].ID <= items[1].ID {
		t.Fatalf("entries are not newest first: %d, %d", items[0].ID, items[1].ID)
	}
	byID := make(map[uint]network.NetworkEntryListItem, len(items))
	for _, item := range items {
		byID[item.ID] = item
	}
	projected := byID[entries[0].ID]
	if projected.ServiceKind != "forward" || projected.ParentProtocolID != endpoint.ID || projected.LandingNodeID != endpoint.NodeID || projected.NodeName != "test" || projected.EndpointName != endpoint.Name {
		t.Fatalf("projection=%+v", projected)
	}
	if !projected.HasPath || !projected.Pending || projected.LastError != "zeta" {
		t.Fatalf("path/publication=%+v", projected)
	}
	if got := projected.NodeGroupNames; len(got) != 2 || got[0] != "Alpha" || got[1] != firstGroup.Name {
		t.Fatalf("group names=%v", got)
	}
	if got := projected.Memberships; len(got) != 2 || got[0].NodeGroupID != firstGroup.ID || got[0].SortOrder != 8 || got[1].NodeGroupID != secondGroup.ID || got[1].Revision != 3 {
		t.Fatalf("memberships=%+v", got)
	}
	empty := byID[entries[1].ID]
	if empty.Memberships == nil || empty.NodeGroupNames == nil || !empty.Pending {
		t.Fatalf("empty projections are not stable arrays: %+v", empty)
	}
	payload, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "encrypted-path-secret") || strings.Contains(string(payload), "path_config") {
		t.Fatalf("private path leaked: %s", payload)
	}
	firstQueryCount := counter.count.Load()
	if firstQueryCount > 4 {
		t.Fatalf("unexpected query count=%d", firstQueryCount)
	}

	for index := 0; index < 12; index++ {
		entry := model.NetworkEntry{Name: fmt.Sprintf("bulk-%02d", index), Network: "tcp", NodeID: 1, EndpointID: endpoint.ID, Address: "bulk.example.test", Port: 15000 + index, PublicPort: 15000 + index, Enabled: true, Revision: 1}
		if err := db.Create(&entry).Error; err != nil {
			t.Fatal(err)
		}
	}
	counter.count.Store(0)
	items, err = service.List(context.Background(), 1)
	if err != nil || len(items) != 14 || counter.count.Load() != firstQueryCount {
		t.Fatalf("items=%d bounded queries=%d want=%d error=%v", len(items), counter.count.Load(), firstQueryCount, err)
	}
}

func TestNetworkEntryQueriesRecheckCurrentAdministrator(t *testing.T) {
	db, _ := administrationFixture(t)
	seedNetworkEntryMutationFixture(t, db)
	service := network.NetworkEntryQueries{Repository: NetworkEntryQueries{DB: db}}
	if err := db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.List(context.Background(), 1); !errors.Is(err, network.ErrNetworkEntryQueryPermission) {
		t.Fatalf("error=%v", err)
	}
}
