package handler

import (
	"encoding/json"
	"testing"
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
