package handler

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestNodeDiagnosticChecksOnlyAssignedEndpoints(t *testing.T) {
	endpoints := []model.ProtocolEndpoint{{
		ID: 1, NodeID: 9, Name: "SS 主入口", Protocol: "shadowsocks", Port: 14588, IsActive: true,
	}}
	output := diagnosticTestOutput(`{"runtime":{"core_instance_id":"core-a","config_revision":7}}`, strings.Join([]string{
		"tcp LISTEN 0 4096 *:22 *:*",
		"tcp LISTEN 0 4096 *:443 *:*",
		"tcp LISTEN 0 4096 *:14588 *:*",
		"udp UNCONN 0 0 *:14588 *:*",
		"tcp LISTEN 0 4096 127.0.0.1:3306 *:*",
	}, "\n"))

	snapshot := newNodeDiagnosticSnapshot(9, endpoints)
	evaluateNodeDiagnosticSnapshot(&snapshot, endpoints, output)
	if snapshot.Status != nodeDiagnosticHealthy || snapshot.Checks.SSH != nodeDiagnosticHealthy || snapshot.Checks.Zero != nodeDiagnosticHealthy {
		t.Fatalf("unexpected node status: %+v", snapshot)
	}
	if len(snapshot.Protocols) != 1 {
		t.Fatalf("expected only the assigned endpoint, got %+v", snapshot.Protocols)
	}
	if snapshot.Protocols[0].Name != "SS 主入口" || snapshot.Protocols[0].Protocol != "shadowsocks" || snapshot.Protocols[0].Status != nodeDiagnosticHealthy {
		t.Fatalf("unexpected protocol result: %+v", snapshot.Protocols[0])
	}
}

func TestNodeDiagnosticShadowsocksRequiresAssignedTCPAndUDPListener(t *testing.T) {
	endpoints := []model.ProtocolEndpoint{{Name: "SS", Protocol: "shadowsocks", Port: 8388}}
	output := diagnosticTestOutput(`{"core_instance_id":"core-b","config_revision":3}`, "tcp LISTEN 0 4096 *:8388 *:*")

	snapshot := newNodeDiagnosticSnapshot(1, endpoints)
	evaluateNodeDiagnosticSnapshot(&snapshot, endpoints, output)
	if snapshot.Status != nodeDiagnosticError || snapshot.Protocols[0].Status != nodeDiagnosticError || snapshot.Protocols[0].Reason != "listener_unavailable" {
		t.Fatalf("missing assigned UDP listener must be an abstract protocol error: %+v", snapshot)
	}
}

func TestNodeDiagnosticHysteria2ChecksUDPOnly(t *testing.T) {
	endpoints := []model.ProtocolEndpoint{{Name: "HY2", Protocol: "hysteria2", Port: 9443}}
	output := diagnosticTestOutput(`{"runtime":{"core_instance_id":"core-c"}}`, "udp UNCONN 0 0 [::]:9443 [::]:*")

	snapshot := newNodeDiagnosticSnapshot(1, endpoints)
	evaluateNodeDiagnosticSnapshot(&snapshot, endpoints, output)
	if snapshot.Status != nodeDiagnosticHealthy || snapshot.Protocols[0].Status != nodeDiagnosticHealthy {
		t.Fatalf("Hysteria2 should be healthy with its assigned UDP listener: %+v", snapshot)
	}
}

func TestNodeDiagnosticZeroUnavailableDoesNotTrustUnrelatedSockets(t *testing.T) {
	endpoints := []model.ProtocolEndpoint{{Name: "SS", Protocol: "shadowsocks", Port: 8388}}
	output := diagnosticTestOutput("", "tcp LISTEN 0 4096 *:8388 *:*\nudp UNCONN 0 0 *:8388 *:*")

	snapshot := newNodeDiagnosticSnapshot(1, endpoints)
	evaluateNodeDiagnosticSnapshot(&snapshot, endpoints, output)
	if snapshot.Checks.SSH != nodeDiagnosticHealthy || snapshot.Checks.Zero != nodeDiagnosticError {
		t.Fatalf("unexpected check status: %+v", snapshot.Checks)
	}
	if snapshot.Protocols[0].Reason != "zero_unavailable" {
		t.Fatalf("protocol must not be called healthy when Zero runtime cannot be queried: %+v", snapshot.Protocols[0])
	}
}

func TestNodeDiagnosticRuntimeAvailabilitySupportsSocketResponseShapes(t *testing.T) {
	cases := []struct {
		value string
		want  bool
	}{
		{`{"runtime":{"core_instance_id":"a"}}`, true},
		{`{"core_instance_id":"a","stats":{}}`, true},
		{`{"stats":{"active_sessions":0}}`, true},
		{`{}`, false},
		{`not-json`, false},
		{"", false},
	}
	for _, tc := range cases {
		if got := nodeDiagnosticRuntimeAvailable(tc.value); got != tc.want {
			t.Errorf("nodeDiagnosticRuntimeAvailable(%q) = %t, want %t", tc.value, got, tc.want)
		}
	}
}

func TestNodeDiagnosticResponseDoesNotExposeHostEvidence(t *testing.T) {
	endpoints := []model.ProtocolEndpoint{{Name: "SS", Protocol: "shadowsocks", Port: 14588}}
	snapshot := newNodeDiagnosticSnapshot(1, endpoints)
	evaluateNodeDiagnosticSnapshot(&snapshot, endpoints, diagnosticTestOutput(`{"runtime":{"core_instance_id":"core-a"}}`, "tcp LISTEN 0 4096 *:14588 *:*\nudp UNCONN 0 0 *:14588 *:*"))

	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	body := string(encoded)
	for _, forbidden := range []string{`"port"`, `"address"`, `"pid"`, `"config_path"`, `"actual_listeners"`, `"expected_listeners"`, `"recent_zero_logs"`, `"recent_kernel_logs"`, `"conntrack"`, `"fd_count"`} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("diagnostic API leaked host/runtime implementation field %s: %s", forbidden, body)
		}
	}
	if !strings.Contains(body, `"protocol":"shadowsocks"`) || !strings.Contains(body, `"status":"healthy"`) {
		t.Fatalf("diagnostic API must retain business protocol status: %s", body)
	}
}

func TestNodeDiagnosticEmptyProtocolsSerializesAsArray(t *testing.T) {
	snapshot := newNodeDiagnosticSnapshot(1, nil)
	evaluateNodeDiagnosticSnapshot(&snapshot, nil, diagnosticTestOutput(`{"runtime":{"core_instance_id":"core-a"}}`, "tcp LISTEN 0 4096 *:22 *:*"))
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"protocols":[]`) {
		t.Fatalf("empty protocol set must be an array, got %s", encoded)
	}
}

func diagnosticTestOutput(status, ss string) string {
	return strings.Join([]string{
		"__ZBOARD_DIAG_STATUS_BEGIN__", status, "__ZBOARD_DIAG_STATUS_END__",
		"__ZBOARD_DIAG_SS_BEGIN__", ss, "__ZBOARD_DIAG_SS_END__",
	}, "\n")
}
