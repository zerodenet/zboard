package networkstore

import (
	"context"
	"errors"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestPublicationRequestsQueueTopologyAtomically(t *testing.T) {
	db, _ := administrationFixture(t)
	nodes := []model.Node{
		{ID: 1, Name: "landing", Address: "192.0.2.1", Config: "{}", LifecycleStatus: "active"},
		{ID: 2, Name: "front", Address: "192.0.2.2", Config: "{}", LifecycleStatus: "active"},
		{ID: 3, Name: "other", Address: "192.0.2.3", Config: "{}", LifecycleStatus: "active"},
	}
	if err := db.Create(&nodes).Error; err != nil {
		t.Fatal(err)
	}
	endpoint := model.ProtocolEndpoint{ID: 10, NodeID: 1, Name: "endpoint", RuntimeKey: "00000000-0000-4000-8000-000000000010", Protocol: "vless", Address: "edge.example", Port: 443, PublicPort: 443, MultiplierMilli: 1000, ServerConfig: "enc:{}", ClientConfig: "{}", OptionalConfig: "{}", Tags: "[]", IsActive: true}
	if err := db.Create(&endpoint).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.NetworkEntry{ID: 20, NodeID: 2, EndpointID: endpoint.ID, Name: "front", Address: "front.example", Port: 8443, PublicPort: 8443, Network: "tcp_udp", Enabled: true, Revision: 1}).Error; err != nil {
		t.Fatal(err)
	}
	store := PublicationRequests{DB: db}
	if err := store.QueuePublicationRequests(context.Background(), []network.PublicationRequest{{NodeID: 1, TriggerEndpointID: endpoint.ID}, {NodeID: 3, TriggerEndpointID: 0}}); err != nil {
		t.Fatal(err)
	}
	var publications []model.NodeConfigPublish
	if err := db.Order("node_id").Find(&publications).Error; err != nil || len(publications) != 3 || publications[0].NodeID != 1 || publications[1].NodeID != 2 || publications[2].NodeID != 3 {
		t.Fatalf("publications=%+v error=%v", publications, err)
	}
	if err := db.Where("1 = 1").Delete(&model.NodeConfigPublish{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Create().Before("gorm:create").Register("fail_second_publication_request", func(tx *gorm.DB) {
		if item, ok := tx.Statement.Dest.(*model.NodeConfigPublish); ok && item.NodeID == 3 {
			tx.AddError(errors.New("publication unavailable"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Create().Remove("fail_second_publication_request")
	if err := store.QueuePublicationRequests(context.Background(), []network.PublicationRequest{{NodeID: 1, TriggerEndpointID: endpoint.ID}, {NodeID: 3}}); err == nil {
		t.Fatal("publication failure committed")
	}
	var count int64
	if err := db.Model(&model.NodeConfigPublish{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("publications=%d error=%v", count, err)
	}
}
