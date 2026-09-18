package handler

import (
	"reflect"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestOrderedProtocolCredentialSubscriptions(t *testing.T) {
	input := []model.Subscription{{ID: 8}, {ID: 2}, {ID: 5}}
	ordered := orderedProtocolCredentialSubscriptions(input)
	got := []uint{ordered[0].ID, ordered[1].ID, ordered[2].ID}
	want := []uint{2, 5, 8}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ordered IDs = %v, want %v", got, want)
	}
	if input[0].ID != 8 {
		t.Fatalf("input slice was mutated: %+v", input)
	}
}

func TestProtocolCredentialsCurrentForEndpoints(t *testing.T) {
	expiresAt := time.Date(2026, time.August, 28, 12, 0, 0, 0, time.UTC)
	subscription := model.Subscription{ID: 9, UserID: 7, EndAt: expiresAt}
	endpoint := model.ProtocolEndpoint{ID: 11, NodeID: 5, Protocol: "vless", Port: 443, PublicPort: 8443}
	current := model.ProtocolCredential{
		SubscriptionID: subscription.ID, UserID: subscription.UserID,
		ProtocolEndpointID: endpoint.ID, NodeID: endpoint.NodeID,
		ListenPort: endpoint.Port, PublicPort: endpoint.PublicPort,
		Status: protocolCredentialStatusActive, ExpiresAt: subscription.EndAt,
	}
	tests := []struct {
		name        string
		endpoints   []model.ProtocolEndpoint
		credentials []model.ProtocolCredential
		want        bool
	}{
		{name: "current", endpoints: []model.ProtocolEndpoint{endpoint}, credentials: []model.ProtocolCredential{current}, want: true},
		{name: "missing", endpoints: []model.ProtocolEndpoint{endpoint}, want: false},
		{name: "stale node", endpoints: []model.ProtocolEndpoint{endpoint}, credentials: []model.ProtocolCredential{func() model.ProtocolCredential {
			stale := current
			stale.NodeID++
			return stale
		}()}, want: false},
		{name: "revoked", endpoints: []model.ProtocolEndpoint{endpoint}, credentials: []model.ProtocolCredential{func() model.ProtocolCredential {
			revoked := current
			revokedAt := expiresAt.Add(-time.Hour)
			revoked.RevokedAt = &revokedAt
			return revoked
		}()}, want: false},
		{name: "unmanaged endpoint", endpoints: []model.ProtocolEndpoint{{ID: 12, Protocol: "socks5"}}, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := protocolCredentialsCurrentForEndpoints(subscription, test.endpoints, test.credentials, false); got != test.want {
				t.Fatalf("protocolCredentialsCurrentForEndpoints() = %t, want %t", got, test.want)
			}
		})
	}
}
