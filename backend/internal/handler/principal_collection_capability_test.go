package handler

import (
	"context"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"testing"
	"time"
)

func TestPrincipalCollectionPreservesGenerationAndRevisionBoundaries(t *testing.T) {
	h, token := newAnnouncementTestHandlers(t)
	actor, err := h.authFromRequest(announcementRequest("GET", "/", token, ""))
	if err != nil {
		t.Fatal(err)
	}
	credential := model.ProtocolCredential{UserID: actor.UserID, SubscriptionID: 31, ProtocolEndpointID: 17, NodeID: 9, CredentialID: "projection-credential", PrincipalKey: "projection-principal", Secret: "opaque", Status: "active", ExpiresAt: time.Now().Add(time.Hour)}
	if err = h.db.Create(&credential).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	ctx := context.Background()
	event := metering.PrincipalEvent{CoreInstanceID: "instance-a", EventID: "first", Sequence: 1, ObservedAt: now}
	obs := metering.PrincipalObservation{PrincipalKey: credential.PrincipalKey, ActiveFlows: 5, SessionRegistryRevision: 5, ObservedAt: now}
	apply := func() {
		t.Helper()
		if err := h.services.PrincipalCollection.Observe(ctx, 9, event, obs); err != nil {
			t.Fatal(err)
		}
	}
	expect := func(want uint64) {
		t.Helper()
		var scope principalFlowScopeCurrent
		if err := h.db.Where("scope_type = ? AND scope_id = ?", "subscription", 31).First(&scope).Error; err != nil || scope.ActiveFlows != want {
			t.Fatalf("scope = %+v err=%v want=%d", scope, err, want)
		}
	}
	apply()
	expect(5)
	apply()
	expect(5)
	event.EventID = "older-revision"
	obs.SessionRegistryRevision = 4
	obs.ActiveFlows = 99
	apply()
	expect(5)
	event.EventID = "stop"
	event.ObservedAt = now.Add(time.Second)
	if err = h.services.PrincipalCollection.Boundary(ctx, 9, event, true); err != nil {
		t.Fatal(err)
	}
	expect(0)
	event.EventID = "late-closed"
	obs.SessionRegistryRevision = 6
	apply()
	expect(0)
	event = metering.PrincipalEvent{CoreInstanceID: "instance-b", EventID: "restart", Sequence: 1, ObservedAt: now.Add(2 * time.Second)}
	if err = h.services.PrincipalCollection.Boundary(ctx, 9, event, false); err != nil {
		t.Fatal(err)
	}
	event.EventID = "new-instance-flow"
	obs.ObservedAt = event.ObservedAt
	obs.SessionRegistryRevision = 1
	obs.ActiveFlows = 2
	apply()
	expect(2)
	event.CoreInstanceID = "instance-a"
	event.EventID = "late-old-instance"
	obs.ObservedAt = now
	obs.SessionRegistryRevision = 7
	obs.ActiveFlows = 100
	apply()
	expect(2)
}
