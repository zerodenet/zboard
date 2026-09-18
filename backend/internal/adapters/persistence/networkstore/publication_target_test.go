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
	deleting := model.Node{Name: "deleting", LifecycleStatus: "deleting"}
	if err := db.Create(&active).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&deleting).Error; err != nil {
		t.Fatal(err)
	}
	endpoint := model.ProtocolEndpoint{NodeID: active.ID, Name: "first", Protocol: "vless", Address: "127.0.0.1", Port: 443}
	if err := db.Create(&endpoint).Error; err != nil {
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
	if _, ok, err = resolver.Resolve(context.Background(), network.Publication{NodeID: deleting.ID}); err != nil || ok {
		t.Fatalf("deleting node remained publishable: ok=%t err=%v", ok, err)
	}
	if _, ok, err = resolver.Resolve(context.Background(), network.Publication{NodeID: deleting.ID + 100}); err != nil || ok {
		t.Fatalf("missing node remained publishable: ok=%t err=%v", ok, err)
	}
}
