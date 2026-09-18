package networkstore

import (
	"context"
	"errors"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestMieruEndpointConfigurationsCommitBatchAtomically(t *testing.T) {
	db, _ := administrationFixture(t)
	rows := []model.ProtocolEndpoint{
		{ID: 1, NodeID: 10, Name: "one", RuntimeKey: "00000000-0000-4000-8000-000000000001", Protocol: "mieru", Address: "one.example", Port: 1001, PublicPort: 1001, MultiplierMilli: 1000, ServerConfig: "enc:one", ClientConfig: "client-one", OptionalConfig: "{}", Tags: "[]", IsActive: true},
		{ID: 2, NodeID: 20, Name: "two", RuntimeKey: "00000000-0000-4000-8000-000000000002", Protocol: "mieru", Address: "two.example", Port: 1002, PublicPort: 1002, MultiplierMilli: 1000, ServerConfig: "enc:two", ClientConfig: "client-two", OptionalConfig: "{}", Tags: "[]", IsActive: true},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	store := MieruEndpointConfigurations{DB: db}
	listed, err := store.ListMieruEndpointConfigurations(context.Background())
	if err != nil || len(listed) != 2 || listed[0].ID != 1 || listed[1].ID != 2 {
		t.Fatalf("listed=%+v error=%v", listed, err)
	}
	updates := []network.MieruEndpointConfigurationUpdate{
		{ID: 1, ExpectedServerCiphertext: "enc:one", ExpectedClientConfig: "client-one", ServerCiphertext: "enc:new-one", ClientConfig: "new-client-one"},
		{ID: 2, ExpectedServerCiphertext: "stale", ExpectedClientConfig: "client-two", ServerCiphertext: "enc:new-two", ClientConfig: "new-client-two"},
	}
	if err := store.CommitMieruEndpointConfigurations(context.Background(), updates); !errors.Is(err, network.ErrMieruEndpointConfigurationConflict) {
		t.Fatalf("error=%v", err)
	}
	var first model.ProtocolEndpoint
	if err := db.First(&first, 1).Error; err != nil || first.ServerConfig != "enc:one" || first.ClientConfig != "client-one" {
		t.Fatalf("first=%+v error=%v", first, err)
	}
	updates[1].ExpectedServerCiphertext = "enc:two"
	if err := store.CommitMieruEndpointConfigurations(context.Background(), updates); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&first, 1).Error; err != nil || first.ServerConfig != "enc:new-one" || first.ClientConfig != "new-client-one" {
		t.Fatalf("first=%+v error=%v", first, err)
	}
}

func TestManagedPublicationInventoryReturnsStableTargets(t *testing.T) {
	db, _ := administrationFixture(t)
	rows := []model.ProtocolEndpoint{
		{ID: 3, NodeID: 20, Name: "hysteria", RuntimeKey: "00000000-0000-4000-8000-000000000003", Protocol: "Hysteria2", Address: "h.example", Port: 443, PublicPort: 443, OptionalConfig: "{}", Tags: "[]", IsActive: true},
		{ID: 2, NodeID: 10, Name: "trojan", RuntimeKey: "00000000-0000-4000-8000-000000000002", Protocol: "trojan", Address: "t.example", Port: 443, PublicPort: 443, OptionalConfig: "{}", Tags: "[]", IsActive: true},
		{ID: 4, NodeID: 30, Name: "vless", RuntimeKey: "00000000-0000-4000-8000-000000000004", Protocol: "vless", Address: "v.example", Port: 443, PublicPort: 443, OptionalConfig: "{}", Tags: "[]", IsActive: true},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	targets, err := (ManagedPublicationInventory{DB: db}).ListManagedPublicationTargets(context.Background(), []string{"trojan", "hysteria2"})
	if err != nil || len(targets) != 2 || targets[0].TriggerEndpointID != 2 || targets[1].TriggerEndpointID != 3 {
		t.Fatalf("targets = %+v err=%v", targets, err)
	}
}
