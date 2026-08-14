package handler

import (
	"testing"
	"time"
)

func TestApplyPrincipalFlowReplayOrdersByRegistryObservationNotDelivery(t *testing.T) {
	from := time.Date(2026, time.August, 14, 0, 0, 0, 0, time.UTC)
	response := trafficTrendResponse{Points: []trafficTrendPoint{{Date: "2026-08-14"}}}

	// Deliberately pass the newer registry transition first to model an event
	// delivery order that differs from SessionRegistry transition order.
	events := []principalFlowHistoryRow{
		{ID: 2, NodeID: 1, CoreInstanceID: "core-a", SessionRegistryRevision: 42, PrincipalKey: "p", ActiveFlows: 7, ObservedAt: from.Add(2 * time.Second)},
		{ID: 1, NodeID: 1, CoreInstanceID: "core-a", SessionRegistryRevision: 41, PrincipalKey: "p", ActiveFlows: 8, ObservedAt: from.Add(time.Second)},
	}

	applyPrincipalFlowReplay(&response, from, nil, events, nil)

	if response.PeakConnections == nil || *response.PeakConnections != 8 {
		t.Fatalf("historical peak must retain rev41=8 even when rev42 arrived first: %+v", response.PeakConnections)
	}
	if response.ConnectionSampleCount != 2 {
		t.Fatalf("unexpected sample count: %d", response.ConnectionSampleCount)
	}
}

func TestApplyPrincipalFlowReplayAggregatesPrincipalsBeforeTakingPeak(t *testing.T) {
	from := time.Date(2026, time.August, 14, 0, 0, 0, 0, time.UTC)
	response := trafficTrendResponse{Points: []trafficTrendPoint{{Date: "2026-08-14"}}}
	events := []principalFlowHistoryRow{
		{ID: 1, NodeID: 1, CoreInstanceID: "core-a", SessionRegistryRevision: 1, PrincipalKey: "a", ActiveFlows: 5, ObservedAt: from.Add(time.Second)},
		{ID: 2, NodeID: 1, CoreInstanceID: "core-a", SessionRegistryRevision: 2, PrincipalKey: "b", ActiveFlows: 7, ObservedAt: from.Add(2 * time.Second)},
		{ID: 3, NodeID: 1, CoreInstanceID: "core-a", SessionRegistryRevision: 3, PrincipalKey: "a", ActiveFlows: 2, ObservedAt: from.Add(3 * time.Second)},
	}

	applyPrincipalFlowReplay(&response, from, nil, events, nil)

	if response.PeakConnections == nil || *response.PeakConnections != 12 {
		t.Fatalf("expected aggregate user/subscription peak 12, got %+v", response.PeakConnections)
	}
}

func TestApplyPrincipalFlowReplayGenerationBoundaryClearsOldNodeState(t *testing.T) {
	from := time.Date(2026, time.August, 14, 0, 0, 0, 0, time.UTC)
	response := trafficTrendResponse{Points: []trafficTrendPoint{{Date: "2026-08-14"}}}
	baseline := []principalFlowHistoryRow{
		{ID: 1, NodeID: 1, CoreInstanceID: "old", SessionRegistryRevision: 20, PrincipalKey: "a", ActiveFlows: 9, ObservedAt: from.Add(-time.Second)},
		{ID: 2, NodeID: 2, CoreInstanceID: "other", SessionRegistryRevision: 8, PrincipalKey: "b", ActiveFlows: 3, ObservedAt: from.Add(-time.Second)},
	}
	boundaries := []principalFlowBoundaryRow{
		{ID: 10, NodeID: 1, CoreInstanceID: "new", Source: "generation_reset", ObservedAt: from.Add(time.Second)},
	}
	events := []principalFlowHistoryRow{
		{ID: 11, NodeID: 1, CoreInstanceID: "new", SessionRegistryRevision: 1, PrincipalKey: "a", ActiveFlows: 2, ObservedAt: from.Add(time.Second)},
	}

	applyPrincipalFlowReplay(&response, from, baseline, events, boundaries)

	// Start-of-day was 12, then node 1 reset to zero and new generation became
	// 2 while node 2 remained 3. The historical maximum must remain 12.
	if response.PeakConnections == nil || *response.PeakConnections != 12 {
		t.Fatalf("unexpected peak across generation reset: %+v", response.PeakConnections)
	}
}

func TestApplyPrincipalFlowReplayKeepsUnsupportedRangeNull(t *testing.T) {
	from := time.Date(2026, time.August, 14, 0, 0, 0, 0, time.UTC)
	response := trafficTrendResponse{Points: []trafficTrendPoint{{Date: "2026-08-14"}}}

	applyPrincipalFlowReplay(&response, from, nil, nil, nil)

	if response.PeakConnections != nil || response.Points[0].PeakConnections != nil || response.ConnectionSampleCount != 0 {
		t.Fatalf("unsupported/old Core range must stay null, got %+v", response)
	}
}
