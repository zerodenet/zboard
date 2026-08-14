package handler

import (
	"encoding/json"
	"testing"
)

func TestParseZeroPrincipalFlowObservationKeepsLegacyEventsCompatible(t *testing.T) {
	event := zeroEventEnvelope{
		EventType:      "flow.started",
		PrincipalKey:   "subscription:1:endpoint:4",
		CoreInstanceID: "core-a",
		Payload:        json.RawMessage(`{"flow_id":"flow-1","traffic":{"bytes_up":0,"bytes_down":0}}`),
	}
	observation, present, err := parseZeroPrincipalFlowObservation(event)
	if err != nil {
		t.Fatalf("legacy event returned error: %v", err)
	}
	if present {
		t.Fatalf("legacy event unexpectedly produced an observation: %+v", observation)
	}
}

func TestParseZeroPrincipalFlowObservationAcceptsFinalZero(t *testing.T) {
	event := zeroEventEnvelope{
		EventType:      "flow.completed",
		PrincipalKey:   "subscription:1:endpoint:4",
		CoreInstanceID: "core-a",
		Payload: json.RawMessage(`{
			"principal_active_flows": 0,
			"session_registry_revision": 42,
			"observed_at_unix_ms": 1786669200000
		}`),
	}
	observation, present, err := parseZeroPrincipalFlowObservation(event)
	if err != nil {
		t.Fatalf("new observation returned error: %v", err)
	}
	if !present {
		t.Fatal("new observation was not detected")
	}
	if observation.ActiveFlows != 0 {
		t.Fatalf("final zero was lost: %+v", observation)
	}
	if observation.SessionRegistryRevision != 42 {
		t.Fatalf("unexpected registry revision: %d", observation.SessionRegistryRevision)
	}
	if observation.PrincipalKey != "subscription:1:endpoint:4" {
		t.Fatalf("unexpected principal: %q", observation.PrincipalKey)
	}
}

func TestParseZeroPrincipalFlowObservationRejectsPartialContract(t *testing.T) {
	event := zeroEventEnvelope{
		EventType:      "flow.started",
		PrincipalKey:   "subscription:1:endpoint:4",
		CoreInstanceID: "core-a",
		Payload: json.RawMessage(`{
			"principal_active_flows": 2,
			"session_registry_revision": 41
		}`),
	}
	_, present, err := parseZeroPrincipalFlowObservation(event)
	if !present {
		t.Fatal("partial new contract must be recognized as present")
	}
	if err == nil {
		t.Fatal("partial new contract must be rejected")
	}
}

func TestApplyPrincipalFlowTrendRowsLeavesUnsupportedRangeNull(t *testing.T) {
	response := trafficTrendResponse{
		Points: []trafficTrendPoint{{Date: "2026-08-13"}, {Date: "2026-08-14"}},
	}
	applyPrincipalFlowTrendRows(&response, nil)
	if response.ConnectionSampleCount != 0 || response.PeakConnections != nil {
		t.Fatalf("legacy/no-observation range changed: %+v", response)
	}
	for _, point := range response.Points {
		if point.PeakConnections != nil {
			t.Fatalf("legacy point unexpectedly received a peak: %+v", point)
		}
	}
}

func TestApplyPrincipalFlowTrendRowsUsesObservedScopeMaximum(t *testing.T) {
	response := trafficTrendResponse{
		Points: []trafficTrendPoint{{Date: "2026-08-13"}, {Date: "2026-08-14"}},
	}
	applyPrincipalFlowTrendRows(&response, []principalFlowScopeTrendRow{
		{Day: "2026-08-13", Peak: 7, SampleCount: 3},
		{Day: "2026-08-14", Peak: 11, SampleCount: 4},
	})
	if response.ConnectionSampleCount != 7 {
		t.Fatalf("unexpected sample count: %d", response.ConnectionSampleCount)
	}
	if response.PeakConnections == nil || *response.PeakConnections != 11 {
		t.Fatalf("unexpected overall peak: %+v", response.PeakConnections)
	}
	if response.Points[0].PeakConnections == nil || *response.Points[0].PeakConnections != 7 {
		t.Fatalf("unexpected first-day peak: %+v", response.Points[0].PeakConnections)
	}
	if response.Points[1].PeakConnections == nil || *response.Points[1].PeakConnections != 11 {
		t.Fatalf("unexpected second-day peak: %+v", response.Points[1].PeakConnections)
	}
}
