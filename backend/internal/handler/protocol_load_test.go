package handler

import (
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestProtocolEndpointUsagePrefersPrincipalCurrentProjection(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	now := time.Now().UTC()
	subscription := model.Subscription{ID: 41, UserID: 1, Status: subStatusActive, EndAt: now.Add(time.Hour), FlowTotal: 1}
	if err := h.db.Create(&subscription).Error; err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		flow := model.FlowUsage{
			NodeID: 1, FlowID: string(rune('a' + index)), SubscriptionID: subscription.ID,
			ProtocolEndpointID: 81, PrincipalKey: "legacy", Status: "active",
			LastEventID: string(rune('a' + index)), LastSeenAt: now,
		}
		if err := h.db.Create(&flow).Error; err != nil {
			t.Fatal(err)
		}
	}
	endpoints := []model.ProtocolEndpoint{{ID: 81}}
	legacy, err := h.loadProtocolEndpointUsageBatch(endpoints, now)
	if err != nil {
		t.Fatal(err)
	}
	if legacy[81].ActiveFlows != 2 || legacy[81].ActiveUsers != 1 {
		t.Fatalf("legacy fallback = %+v", legacy[81])
	}

	for _, current := range []principalFlowCurrent{
		{NodeID: 1, PrincipalKey: "principal-a", CoreInstanceID: "core-a", UserID: 1, SubscriptionID: 41, ProtocolEndpointID: 81, ActiveFlows: 7, ObservedAt: now, UpdatedAt: now},
		{NodeID: 2, PrincipalKey: "principal-b", CoreInstanceID: "core-b", UserID: 2, SubscriptionID: 42, ProtocolEndpointID: 81, ActiveFlows: 4, ObservedAt: now, UpdatedAt: now},
	} {
		if err := h.db.Create(&current).Error; err != nil {
			t.Fatal(err)
		}
	}
	projected, err := h.loadProtocolEndpointUsageBatch(endpoints, now)
	if err != nil {
		t.Fatal(err)
	}
	if projected[81].ActiveFlows != 11 || projected[81].ActiveUsers != 2 || projected[81].LastUsedAt == nil {
		t.Fatalf("Principal current projection = %+v", projected[81])
	}
	if err := h.db.Model(&principalFlowCurrent{}).
		Where("protocol_endpoint_id = ?", 81).
		Updates(map[string]interface{}{"active_flows": 0, "observed_at": now.Add(time.Second)}).Error; err != nil {
		t.Fatal(err)
	}
	zero, err := h.loadProtocolEndpointUsageBatch(endpoints, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if zero[81].ActiveFlows != 0 || zero[81].ActiveUsers != 0 {
		t.Fatalf("authoritative Principal zero fell back to legacy events: %+v", zero[81])
	}
}
