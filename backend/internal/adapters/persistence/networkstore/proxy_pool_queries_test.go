package networkstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestProxyPoolQueriesScopeAggregateAndRedact(t *testing.T) {
	db, _ := administrationFixture(t)
	seedLegacyResources(t, db)
	if err := db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", true).Error; err != nil {
		t.Fatal(err)
	}
	other := model.Node{ID: 2, Name: "other", Config: "{}"}
	if err := db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	pools := []model.NodeProxyPool{
		{NodeID: 1, Name: "visible", Config: "encrypted-config", SubscriptionURL: "encrypted-url", Revision: 2},
		{NodeID: 2, Name: "other", Config: "other-secret", Revision: 1},
	}
	if err := db.Create(&pools).Error; err != nil {
		t.Fatal(err)
	}
	endpoint := model.ProtocolEndpoint{NodeID: 2, Name: "landing", RuntimeKey: "pool-query-endpoint", Protocol: "vless", Port: 443, PublicPort: 443, ServerConfig: "{}", ClientConfig: "{}", OptionalConfig: "{}", Tags: "[]"}
	if err := db.Create(&endpoint).Error; err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		poolID := pools[0].ID
		entry := model.NetworkEntry{Name: fmt.Sprintf("entry-%d", index), NodeID: 1, EndpointID: endpoint.ID, Port: 10000 + index, PublicPort: 10000 + index, Enabled: true, ProxyPoolID: &poolID}
		if err := db.Create(&entry).Error; err != nil {
			t.Fatal(err)
		}
	}
	service := network.ProxyPoolQueries{Repository: ProxyPoolQueries{DB: db}, Details: ProxyPoolQueries{DB: db}}
	items, err := service.List(context.Background(), 1, 1)
	if err != nil || len(items) != 1 || items[0].EntryCount != 2 || !items[0].SubscriptionConfigured {
		t.Fatalf("items=%+v error=%v", items, err)
	}
	payload, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "encrypted-config") || strings.Contains(string(payload), "encrypted-url") {
		t.Fatalf("secret leaked: %s", payload)
	}
	detail, err := service.Get(context.Background(), 1, pools[0].ID)
	if err != nil || detail.ConfigCiphertext != "encrypted-config" || detail.SubscriptionURLCiphertext != "encrypted-url" {
		t.Fatalf("detail=%+v error=%v", detail, err)
	}
	if err := db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.List(context.Background(), 1, 1); !errors.Is(err, network.ErrProxyPoolQueryPermission) {
		t.Fatalf("revoked authority error=%v", err)
	}
	if _, err := service.Get(context.Background(), 1, pools[0].ID); !errors.Is(err, network.ErrProxyPoolQueryPermission) {
		t.Fatalf("revoked detail authority error=%v", err)
	}
}
