package handler

import (
	"strings"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestParseNodeSSListeners(t *testing.T) {
	listeners := parseNodeSSListeners(strings.Join([]string{
		"tcp LISTEN 0 4096 0.0.0.0:443 0.0.0.0:*",
		"udp UNCONN 0 0 [::]:9443 [::]:*",
		"tcp LISTEN 0 128 [::1]:1080 [::]:*",
	}, "\n"))
	if len(listeners) != 3 {
		t.Fatalf("expected 3 listeners, got %d: %+v", len(listeners), listeners)
	}
	if listeners[0].Port != 443 || listeners[0].Network != "tcp" || listeners[0].Address != "0.0.0.0" {
		t.Fatalf("unexpected first listener: %+v", listeners[0])
	}
	if listeners[2].Port != 9443 || listeners[2].Network != "udp" || listeners[2].Address != "::" {
		t.Fatalf("unexpected UDP listener: %+v", listeners[2])
	}
}

func TestNodeDiagnosticSnapshotDetectsMissingListener(t *testing.T) {
	output := diagnosticTestOutput(`{
		"config": {
			"config_revision": 44,
			"listeners": [
				{"tag":"ss-in","protocol":"shadowsocks","listen_address":"0.0.0.0","listen_port":8388,"udp_enabled":true},
				{"tag":"hy2-in","protocol":"hysteria2","listen_address":"::","listen_port":9443,"udp_enabled":true}
			]
		},
		"runtime": {
			"core_instance_id":"core-a",
			"config_revision":44,
			"pid":123,
			"config_path":"/etc/zerodenet/current.json",
			"started_at_unix_ms":123456
		}
	}`, strings.Join([]string{
		"tcp LISTEN 0 4096 0.0.0.0:8388 0.0.0.0:*",
		"udp UNCONN 0 0 [::]:9443 [::]:*",
	}, "\n"), "ActiveState=active\nSubState=running\nMainPID=123\nExecMainStatus=0", "count=10\nsoft_limit=1024", "count=100\nmax=10000")

	snapshot := parseNodeDiagnosticSnapshot(model.Node{}, output)
	classifyNodeDiagnostic(&snapshot)
	if snapshot.Classification != "data_plane_missing" {
		t.Fatalf("classification = %q, want data_plane_missing: %+v", snapshot.Classification, snapshot)
	}
	if len(snapshot.ExpectedListeners) != 2 {
		t.Fatalf("expected 2 configured listeners, got %d", len(snapshot.ExpectedListeners))
	}
	ss := snapshot.ExpectedListeners[0]
	if ss.Present || strings.Join(ss.MissingNetworks, ",") != "udp" {
		t.Fatalf("Shadowsocks listener should report missing UDP: %+v", ss)
	}
	hy2 := snapshot.ExpectedListeners[1]
	if !hy2.Present || len(hy2.MissingNetworks) != 0 {
		t.Fatalf("Hysteria2 UDP listener should be present: %+v", hy2)
	}
	if snapshot.Runtime.CoreInstanceID != "core-a" || snapshot.Runtime.ConfigRevision != 44 || snapshot.Runtime.PID != 123 {
		t.Fatalf("unexpected runtime identity: %+v", snapshot.Runtime)
	}
}

func TestNodeDiagnosticSnapshotHealthy(t *testing.T) {
	output := diagnosticTestOutput(`{
		"config": {
			"config_revision": 7,
			"listeners": [{"tag":"trojan-in","protocol":"trojan","listen_address":"0.0.0.0","listen_port":443,"udp_enabled":false}]
		},
		"runtime": {"core_instance_id":"core-b","config_revision":7,"pid":99}
	}`, "tcp LISTEN 0 4096 0.0.0.0:443 0.0.0.0:*", "ActiveState=active\nSubState=running\nMainPID=99\nExecMainStatus=0", "count=20\nsoft_limit=4096", "count=100\nmax=10000")

	snapshot := parseNodeDiagnosticSnapshot(model.Node{}, output)
	classifyNodeDiagnostic(&snapshot)
	if snapshot.Classification != "healthy" {
		t.Fatalf("classification = %q, want healthy: %+v", snapshot.Classification, snapshot)
	}
	if !snapshot.ExpectedListeners[0].Present {
		t.Fatalf("expected listener should be present: %+v", snapshot.ExpectedListeners[0])
	}
}

func TestNodeDiagnosticSnapshotResourcePressure(t *testing.T) {
	output := diagnosticTestOutput(`{
		"config": {"listeners": [{"tag":"in","protocol":"trojan","listen_address":"0.0.0.0","listen_port":443}]},
		"runtime": {"core_instance_id":"core-c","config_revision":2,"pid":88}
	}`, "tcp LISTEN 0 4096 0.0.0.0:443 0.0.0.0:*", "ActiveState=active\nSubState=running\nMainPID=88", "count=950\nsoft_limit=1000", "count=100\nmax=10000")

	snapshot := parseNodeDiagnosticSnapshot(model.Node{}, output)
	classifyNodeDiagnostic(&snapshot)
	if snapshot.Classification != "resource_pressure" {
		t.Fatalf("classification = %q, want resource_pressure: %+v", snapshot.Classification, snapshot)
	}
	if !snapshot.Resources.ResourcePressure || snapshot.Resources.FDRatio < 0.94 {
		t.Fatalf("expected fd pressure: %+v", snapshot.Resources)
	}
}

func TestNodeDiagnosticSnapshotDoesNotFabricateHealthWithoutStatus(t *testing.T) {
	output := diagnosticTestOutput("", "tcp LISTEN 0 4096 0.0.0.0:443 0.0.0.0:*", "ActiveState=active\nSubState=running\nMainPID=99", "count=10\nsoft_limit=1000", "")
	snapshot := parseNodeDiagnosticSnapshot(model.Node{}, output)
	classifyNodeDiagnostic(&snapshot)
	if snapshot.Classification != "unknown" {
		t.Fatalf("classification = %q, want unknown", snapshot.Classification)
	}
	if snapshot.Runtime.ControlStatus != "unavailable" || snapshot.Capabilities.NativeRuntimeSnapshot {
		t.Fatalf("unexpected runtime capability: %+v", snapshot.Runtime)
	}
	if len(snapshot.Warnings) == 0 {
		t.Fatal("expected explicit unavailable warning")
	}
}

func TestRedactNodeDiagnosticText(t *testing.T) {
	value := redactNodeDiagnosticText("password=hello token:abc api_key = xyz normal=value", 1024)
	for _, secret := range []string{"hello", "abc", "xyz"} {
		if strings.Contains(value, secret) {
			t.Fatalf("secret %q was not redacted: %s", secret, value)
		}
	}
	if !strings.Contains(value, "normal=value") || !strings.Contains(value, "[REDACTED]") {
		t.Fatalf("unexpected redacted output: %s", value)
	}
}

func TestListenerAddressMatches(t *testing.T) {
	cases := []struct {
		expected string
		actual   string
		match    bool
	}{
		{"0.0.0.0", "0.0.0.0", true},
		{"127.0.0.1", "0.0.0.0", true},
		{"0.0.0.0", "127.0.0.1", false},
		{"::1", "::", true},
		{"::", "::1", false},
		{"127.0.0.1", "::", false},
	}
	for _, tc := range cases {
		if got := listenerAddressMatches(tc.expected, tc.actual); got != tc.match {
			t.Errorf("listenerAddressMatches(%q, %q) = %t, want %t", tc.expected, tc.actual, got, tc.match)
		}
	}
}

func diagnosticTestOutput(status, ss, service, fd, conntrack string) string {
	return strings.Join([]string{
		"__ZBOARD_DIAG_VERSION_BEGIN__", "zero 0.0.17", "__ZBOARD_DIAG_VERSION_END__",
		"__ZBOARD_DIAG_STATUS_BEGIN__", status, "__ZBOARD_DIAG_STATUS_END__",
		"__ZBOARD_DIAG_SS_BEGIN__", ss, "__ZBOARD_DIAG_SS_END__",
		"__ZBOARD_DIAG_SERVICE_BEGIN__", service, "__ZBOARD_DIAG_SERVICE_END__",
		"__ZBOARD_DIAG_FD_BEGIN__", fd, "__ZBOARD_DIAG_FD_END__",
		"__ZBOARD_DIAG_CONNTRACK_BEGIN__", conntrack, "__ZBOARD_DIAG_CONNTRACK_END__",
		"__ZBOARD_DIAG_JOURNAL_BEGIN__", "password=super-secret", "__ZBOARD_DIAG_JOURNAL_END__",
		"__ZBOARD_DIAG_KERNEL_BEGIN__", "oom warning", "__ZBOARD_DIAG_KERNEL_END__",
	}, "\n")
}
