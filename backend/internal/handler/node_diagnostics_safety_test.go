package handler

import (
	"strings"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestRedactNodeDiagnosticTextStructuredSecrets(t *testing.T) {
	value := strings.Join([]string{
		`{"password":"json-secret","token": "json-token"}`,
		`api_key='quoted-secret' credential = plain-secret`,
		`Authorization: Bearer bearer-secret`,
		`normal=value`,
	}, "\n")

	redacted := redactNodeDiagnosticText(value, 4096)
	for _, secret := range []string{"json-secret", "json-token", "quoted-secret", "plain-secret", "bearer-secret"} {
		if strings.Contains(redacted, secret) {
			t.Fatalf("secret %q was not redacted: %s", secret, redacted)
		}
	}
	if !strings.Contains(redacted, "normal=value") {
		t.Fatalf("non-secret diagnostic evidence should be preserved: %s", redacted)
	}
	if strings.Count(redacted, "[REDACTED]") < 5 {
		t.Fatalf("expected all structured secret forms to be redacted: %s", redacted)
	}
}

func TestListenerAddressSatisfiesConfiguredBindRejectsWiderActualBind(t *testing.T) {
	cases := []struct {
		expected string
		actual   string
		match    bool
	}{
		{"127.0.0.1", "127.0.0.1", true},
		{"127.0.0.1", "0.0.0.0", false},
		{"127.0.0.1", "*", false},
		{"0.0.0.0", "0.0.0.0", true},
		{"0.0.0.0", "*", true},
		{"::1", "::", false},
		{"::", "::", true},
	}
	for _, tc := range cases {
		if got := listenerAddressSatisfiesConfiguredBind(tc.expected, tc.actual); got != tc.match {
			t.Errorf("listenerAddressSatisfiesConfiguredBind(%q, %q) = %t, want %t", tc.expected, tc.actual, got, tc.match)
		}
	}
}

func TestNodeDiagnosticSnapshotWarnsOnWiderListenerBind(t *testing.T) {
	output := diagnosticTestOutput(`{
		"config": {
			"config_revision": 9,
			"listeners": [{"tag":"local-in","protocol":"trojan","listen_address":"127.0.0.1","listen_port":8443,"udp_enabled":false}]
		},
		"runtime": {"core_instance_id":"core-bind","config_revision":9,"pid":101}
	}`, "tcp LISTEN 0 4096 0.0.0.0:8443 0.0.0.0:*", "ActiveState=active\nSubState=running\nMainPID=101", "count=10\nsoft_limit=1024", "count=10\nmax=10000")

	snapshot := parseNodeDiagnosticSnapshot(model.Node{}, output)
	classifyNodeDiagnostic(&snapshot)
	if len(snapshot.ExpectedListeners) != 1 {
		t.Fatalf("expected one listener, got %d", len(snapshot.ExpectedListeners))
	}
	listener := snapshot.ExpectedListeners[0]
	if listener.Present || strings.Join(listener.MissingNetworks, ",") != "tcp" {
		t.Fatalf("wider actual bind must not satisfy configured loopback listener: %+v", listener)
	}
	if snapshot.Classification != "data_plane_missing" {
		t.Fatalf("classification = %q, want data_plane_missing", snapshot.Classification)
	}
	if !containsWarning(snapshot.Warnings, "监听范围扩大") {
		t.Fatalf("expected explicit wider-bind warning, got %v", snapshot.Warnings)
	}
}

func containsWarning(values []string, fragment string) bool {
	for _, value := range values {
		if strings.Contains(value, fragment) {
			return true
		}
	}
	return false
}
