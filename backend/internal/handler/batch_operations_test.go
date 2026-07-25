package handler

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestProtocolBatchActiveRequestDecodesScopeAndRequiredState(t *testing.T) {
	payload := `{
		"protocol_endpoint_ids": [9, 4, 9],
		"all_matching": false,
		"filters": {"q": "tokyo", "protocol": "vless", "active": true},
		"idempotency_key": "batch-42",
		"is_active": false
	}`
	var request protocolBatchActiveRequest
	if err := json.Unmarshal([]byte(payload), &request); err != nil {
		t.Fatalf("decode protocol batch request: %v", err)
	}
	if request.IsActive == nil || *request.IsActive {
		t.Fatalf("is_active = %v, want an explicit false pointer", request.IsActive)
	}
	if len(request.ProtocolEndpointIDs) != 3 || request.ProtocolEndpointIDs[0] != 9 {
		t.Fatalf("protocol_endpoint_ids = %v, want decoded scope", request.ProtocolEndpointIDs)
	}
	if request.Filters.Query != "tokyo" || request.Filters.Protocol != "vless" || request.Filters.Active == nil || !*request.Filters.Active {
		t.Fatalf("filters = %+v, want decoded workbench filters", request.Filters)
	}
	if request.IdempotencyKey != "batch-42" {
		t.Fatalf("idempotency_key = %q, want batch-42", request.IdempotencyKey)
	}
}

func TestValidateBatchTargetCountEnforcesNonEmptyAndMaximum(t *testing.T) {
	if err := validateBatchTargetCount(nil); err == nil || !strings.Contains(err.Error(), "did not resolve") {
		t.Fatalf("empty scope error = %v, want did not resolve", err)
	}
	if err := validateBatchTargetCount([]uint{1}); err != nil {
		t.Fatalf("single target error = %v, want nil", err)
	}
	tooMany := make([]uint, maxTaskTargets+1)
	if err := validateBatchTargetCount(tooMany); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized scope error = %v, want exceeds", err)
	}
}

func TestBatchScopeRejectsExplicitIDsWithAllMatching(t *testing.T) {
	h := &handlers{}
	if _, _, err := h.resolveNodeBatchScope(nodeBatchOperationRequest{NodeIDs: []uint{1}, AllMatching: true}); err == nil {
		t.Fatal("node scope accepted node_ids with all_matching")
	}
	if _, _, err := h.resolveProtocolBatchScope(protocolBatchDeployRequest{ProtocolEndpointIDs: []uint{1}, AllMatching: true}); err == nil {
		t.Fatal("protocol scope accepted protocol_endpoint_ids with all_matching")
	}
}
