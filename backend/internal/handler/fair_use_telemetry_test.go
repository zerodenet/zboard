package handler

import (
	"encoding/json"
	"testing"
	"time"
)

func TestFlowStartedPrincipalKeyUsesNestedAuthIdentity(t *testing.T) {
	event := zeroEventEnvelope{
		EventType:    "flow.started",
		PrincipalKey: "legacy-principal",
		Payload: json.RawMessage(`{
			"record": {
				"auth": {
					"principal_key": "principal:opaque"
				}
			}
		}`),
	}
	if got := flowStartedPrincipalKey(event); got != "principal:opaque" {
		t.Fatalf("principal = %q, want principal:opaque", got)
	}
}

func TestFlowStartedPrincipalKeyFallsBackToEnvelope(t *testing.T) {
	event := zeroEventEnvelope{
		EventType:    "flow.started",
		PrincipalKey: "principal:envelope",
		Payload:      json.RawMessage(`{"flow_id":"flow-1"}`),
	}
	if got := flowStartedPrincipalKey(event); got != "principal:envelope" {
		t.Fatalf("principal = %q, want principal:envelope", got)
	}
}

func TestParseFairUseWindowUsesSafeBounds(t *testing.T) {
	value, err := parseFairUseWindow("", fairUseDefaultConnectionStartWindowSeconds, fairUseMinConnectionStartWindowSeconds)
	if err != nil {
		t.Fatalf("default window: %v", err)
	}
	if value != fairUseDefaultConnectionStartWindowSeconds {
		t.Fatalf("default window = %d", value)
	}

	value, err = parseFairUseWindow("300", fairUseDefaultConnectionStartWindowSeconds, fairUseMinConnectionStartWindowSeconds)
	if err != nil || value != 300 {
		t.Fatalf("explicit window = %d, err=%v", value, err)
	}

	for _, raw := range []string{"abc", "0", "9", "3601"} {
		if _, err := parseFairUseWindow(raw, fairUseDefaultConnectionStartWindowSeconds, fairUseMinConnectionStartWindowSeconds); err == nil {
			t.Fatalf("window %q should be rejected", raw)
		}
	}
}

func TestParseFairUseSubscriptionIDRequiresExactMetricsPath(t *testing.T) {
	id, err := parseFairUseSubscriptionID("/api/v1/admin/subscriptions/42/fair-use/metrics")
	if err != nil {
		t.Fatalf("parse subscription id: %v", err)
	}
	if id != 42 {
		t.Fatalf("subscription id = %d, want 42", id)
	}

	for _, path := range []string{
		"/api/v1/admin/subscriptions/42",
		"/api/v1/admin/subscriptions/0/fair-use/metrics",
		"/api/v1/admin/subscriptions/not-a-number/fair-use/metrics",
		"/api/v1/admin/subscriptions/42/extra/fair-use/metrics",
	} {
		if _, err := parseFairUseSubscriptionID(path); err == nil {
			t.Fatalf("path %q should be rejected", path)
		}
	}
}

func TestFairUseObservationRetentionIsFifteenDays(t *testing.T) {
	now := time.Date(2026, 8, 18, 10, 0, 0, 0, time.FixedZone("test", 8*60*60))
	want := now.UTC().Add(-15 * 24 * time.Hour)
	if got := fairUseRawActivityCutoff(now); !got.Equal(want) {
		t.Fatalf("raw activity cutoff = %s, want %s", got, want)
	}
	if got := fairUseEvaluationEventCutoff(now); !got.Equal(want) {
		t.Fatalf("evaluation event cutoff = %s, want %s", got, want)
	}
	if fairUseObservationRetention != 15*24*time.Hour {
		t.Fatalf("observation retention = %s, want 15d", fairUseObservationRetention)
	}
}
