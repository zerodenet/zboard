package handler

import (
	"fmt"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestManagedListenerFollowsSubscriptionCredentialLifecycle(t *testing.T) {
	for _, protocolName := range []string{"mieru", "hysteria2"} {
		t.Run(protocolName, func(t *testing.T) {
			f := newTrafficReadFixture(t)
			f.h.zeroNativeAccess, f.h.zeroMieruAccess = true, true
			now := time.Now().UTC()
			endpoint := model.ProtocolEndpoint{NodeID: 1, Name: "waiting", RuntimeKey: "waiting", Protocol: protocolName, Port: 443, IsActive: true, MieruPrincipalReady: true, ServerConfig: "{}", ClientConfig: "{}", OptionalConfig: "{}", Tags: "[]"}
			group := model.NodeGroup{Name: "Waiting", Code: "waiting", Revision: 1}
			for _, record := range []interface{}{&endpoint, &group} {
				if err := f.h.db.Create(record).Error; err != nil {
					t.Fatal(err)
				}
			}
			link := model.NodeGroupEndpoint{NodeGroupID: group.ID, ProtocolEndpointID: endpoint.ID}
			if err := f.h.db.Create(&link).Error; err != nil {
				t.Fatal(err)
			}
			compile := func(want int, at time.Time) {
				t.Helper()
				inbounds, err := f.h.runtimeInboundsForEndpoint(endpoint, map[string]interface{}{"type": protocolName, "users": []interface{}{}}, at, false, true, true)
				if err != nil || len(inbounds) != want {
					t.Fatalf("listeners=%d want=%d error=%v", len(inbounds), want, err)
				}
				if want > 0 {
					users := inbounds[0]["protocol"].(map[string]interface{})["users"].([]interface{})
					if len(users) != 1 || users[0].(map[string]interface{})["password"] != "subscriber-secret" {
						t.Fatal("subscriber credential missing")
					}
				}
			}
			compile(0, now)
			sub := model.Subscription{UserID: 1, NodeGroupID: group.ID, Status: subStatusActive, EndAt: now.Add(time.Hour), FlowTotal: 1000}
			if err := f.h.db.Create(&sub).Error; err != nil {
				t.Fatal(err)
			}
			// Missing credentials for a real audience remain an error, not a silent drop.
			if _, err := f.h.runtimeInboundsForEndpoint(endpoint, map[string]interface{}{"type": protocolName}, now, false, true, true); err == nil {
				t.Fatal("missing credential projection accepted")
			}
			secret, err := f.h.credentialCipher.Encrypt("subscriber-secret")
			if err != nil {
				t.Fatal(err)
			}
			credential := model.ProtocolCredential{SubscriptionID: sub.ID, ProtocolEndpointID: endpoint.ID, NodeID: 1, CredentialID: "fixture", PrincipalKey: fmt.Sprintf("sub:%d", sub.ID), Secret: secret, Status: protocolCredentialStatusActive, ExpiresAt: sub.EndAt}
			if err := f.h.db.Create(&credential).Error; err != nil {
				t.Fatal(err)
			}
			compile(1, now)
			compile(0, now.Add(2*time.Hour))
		})
	}
}

func TestEmptyAudienceKeepsListenersForProtocolsSupportingEmptyUsers(t *testing.T) {
	f := newTrafficReadFixture(t)
	for _, protocolName := range []string{"vless", "vmess", "trojan"} {
		endpoint := model.ProtocolEndpoint{ID: 7, Protocol: protocolName, Port: 443}
		inbounds, err := f.h.runtimeInboundsForEndpoint(endpoint, map[string]interface{}{"type": protocolName, "users": []interface{}{}}, time.Now().UTC(), false, true, true)
		if err != nil || len(inbounds) != 1 {
			t.Fatalf("%s listeners=%d error=%v", protocolName, len(inbounds), err)
		}
	}
}
