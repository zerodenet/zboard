package networkstore

import (
	"context"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestPublicationTargetResolutionOwnsWorkerPreconditions(t *testing.T) {
	db, _ := administrationFixture(t)
	user := model.User{Email: "publisher@example.test", Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	active := model.Node{Name: "active", LifecycleStatus: "active"}
	fronting := model.Node{Name: "fronting", LifecycleStatus: "active"}
	cleanup := model.Node{Name: "cleanup", LifecycleStatus: "active"}
	unconfigured := model.Node{Name: "unconfigured", LifecycleStatus: "active"}
	deleting := model.Node{Name: "deleting", LifecycleStatus: "deleting"}
	for _, node := range []*model.Node{&active, &fronting, &cleanup, &unconfigured, &deleting} {
		if err := db.Create(node).Error; err != nil {
			t.Fatal(err)
		}
	}
	endpoint := model.ProtocolEndpoint{NodeID: active.ID, Name: "first", Protocol: "vless", Address: "127.0.0.1", Port: 443}
	if err := db.Create(&endpoint).Error; err != nil {
		t.Fatal(err)
	}
	entry := model.NetworkEntry{
		Name: "fronting-entry", NodeID: fronting.ID, EndpointID: endpoint.ID,
		Address: "127.0.0.1", Port: 8443, PublicPort: 8443, Enabled: true,
	}
	if err := db.Create(&entry).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ProtocolDeployment{NodeID: cleanup.ID, ConfigRevision: 1, Status: "succeeded"}).Error; err != nil {
		t.Fatal(err)
	}
	resolver := PublicationTargets{DB: db}
	target, ok, err := resolver.Resolve(context.Background(), network.Publication{NodeID: active.ID, RequestedBy: user.ID})
	if err != nil || !ok || target.EndpointID != endpoint.ID || target.RequestedBy != user.ID {
		t.Fatalf("active target: %+v ok=%t err=%v", target, ok, err)
	}
	target, ok, err = resolver.Resolve(context.Background(), network.Publication{NodeID: active.ID, RequestedBy: user.ID + 100})
	if err != nil || !ok || target.RequestedBy != 0 {
		t.Fatalf("missing actor was not normalized: %+v ok=%t err=%v", target, ok, err)
	}
	target, ok, err = resolver.Resolve(context.Background(), network.Publication{NodeID: fronting.ID})
	if err != nil || !ok || target.EndpointID != 0 {
		t.Fatalf("fronting target: %+v ok=%t err=%v", target, ok, err)
	}
	target, ok, err = resolver.Resolve(context.Background(), network.Publication{NodeID: cleanup.ID})
	if err != nil || !ok || target.EndpointID != 0 {
		t.Fatalf("cleanup target: %+v ok=%t err=%v", target, ok, err)
	}
	if _, ok, err = resolver.Resolve(context.Background(), network.Publication{NodeID: unconfigured.ID}); err != nil || ok {
		t.Fatalf("unconfigured node remained publishable: ok=%t err=%v", ok, err)
	}
	if _, ok, err = resolver.Resolve(context.Background(), network.Publication{NodeID: deleting.ID}); err != nil || ok {
		t.Fatalf("deleting node remained publishable: ok=%t err=%v", ok, err)
	}
	if _, ok, err = resolver.Resolve(context.Background(), network.Publication{NodeID: deleting.ID + 100}); err != nil || ok {
		t.Fatalf("missing node remained publishable: ok=%t err=%v", ok, err)
	}
}
