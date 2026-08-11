package handler

import "testing"

func TestNodeDiagnosticClassificationNetworkReachability(t *testing.T) {
	snapshot := nodeDiagnosticSnapshot{
		Capabilities: nodeDiagnosticCapabilities{NativeRuntimeSnapshot: true},
		Service:      nodeDiagnosticService{ActiveState: "active"},
		ExpectedListeners: []nodeDiagnosticExpectedListener{
			{
				Tag:                  "trojan-in",
				Protocol:             "trojan",
				Address:              "0.0.0.0",
				Port:                 443,
				Networks:             []string{"tcp"},
				Present:              true,
				ExternalReachability: "unreachable",
			},
		},
	}

	classifyNodeDiagnostic(&snapshot)
	if snapshot.Classification != "network_reachability" {
		t.Fatalf("classification = %q, want network_reachability: %+v", snapshot.Classification, snapshot)
	}
}

func TestNodeDiagnosticClassificationDoesNotTreatMissingServiceEvidenceAsFailure(t *testing.T) {
	snapshot := nodeDiagnosticSnapshot{
		Capabilities: nodeDiagnosticCapabilities{NativeRuntimeSnapshot: true},
		ExpectedListeners: []nodeDiagnosticExpectedListener{
			{
				Tag:                  "trojan-in",
				Protocol:             "trojan",
				Address:              "0.0.0.0",
				Port:                 443,
				Networks:             []string{"tcp"},
				Present:              true,
				ExternalReachability: "not_checked",
			},
		},
	}

	classifyNodeDiagnostic(&snapshot)
	if snapshot.Classification != "healthy" {
		t.Fatalf("classification = %q, want healthy when service evidence is absent but listener evidence is complete", snapshot.Classification)
	}
}
