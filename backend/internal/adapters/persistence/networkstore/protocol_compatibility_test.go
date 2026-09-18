package networkstore

import (
	"context"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestProtocolCompatibilityLoadsOnlyEndpointIdentityInOneQuery(t *testing.T) {
	db, _ := administrationFixture(t)
	if err := db.Create(&model.Node{ID: 1, Name: "node", Config: "{}"}).Error; err != nil {
		t.Fatal(err)
	}
	endpoints := []model.ProtocolEndpoint{
		{ID: 10, NodeID: 1, Name: "a", RuntimeKey: "00000000-0000-4000-8000-000000000010", Protocol: "vless", Port: 443, OptionalConfig: "{}", Tags: "[]"},
		{ID: 11, NodeID: 1, Name: "b", RuntimeKey: "00000000-0000-4000-8000-000000000011", Protocol: "mieru", Port: 444, OptionalConfig: "{}", Tags: "[]"},
	}
	if err := db.Create(&endpoints).Error; err != nil {
		t.Fatal(err)
	}
	counter := &networkEntryQueryCounter{}
	service := network.ProtocolCompatibility{Repository: ProtocolCompatibility{DB: db.Session(&gorm.Session{Logger: counter})}}
	if err := service.ValidateEndpoints(context.Background(), []uint{11, 10}); err != nil {
		t.Fatal(err)
	}
	if got := counter.count.Load(); got != 1 {
		t.Fatalf("compatibility projection used %d queries, want 1", got)
	}
}
