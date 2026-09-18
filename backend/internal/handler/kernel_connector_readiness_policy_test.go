package handler

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestZeroStatsProjectionAcceptsIdleNode(t *testing.T) {
	stats, err := parseZeroStatsProjection(json.RawMessage(`{"active_sessions":0,"bytes_up":0,"bytes_down":0}`))
	if err != nil {
		t.Fatalf("idle stats must remain a valid online telemetry sample: %v", err)
	}
	if stats.ActiveSessions != 0 || stats.BytesUp != 0 || stats.BytesDown != 0 {
		t.Fatalf("unexpected idle stats projection: %+v", stats)
	}
}

func TestKernelReadinessDoesNotRollbackForMissingConnectorActivity(t *testing.T) {
	payload, err := os.ReadFile("../capabilities/network/kernel_reconciliation.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(payload)
	if strings.Contains(source, `rollbackAfterActivation(fmt.Errorf("connector event verification failed:`) {
		t.Fatal("Connector observation must not roll back a locally healthy Zero generation")
	}
	for _, expected := range []string{
		`summary := fmt.Sprintf("Zero %s %s and passed systemd and control-socket health checks"`,
		`connectorEventErr == nil, warning`,
		`warning = TruncateKernelError(connectorEventErr.Error())`,
	} {
		if !strings.Contains(source, expected) {
			t.Fatalf("kernel/Connector readiness must remain independently observable: missing %q", expected)
		}
	}
}

func TestManagedZeroOnboardingInitializesCompatibilityTrafficCredential(t *testing.T) {
	payload, err := os.ReadFile("kernel_automation.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), `PrepareTrafficCredential`) || !strings.Contains(string(payload), `prepareNodeTrafficReportCredential`) {
		t.Fatal("managed Zero adapter must prepare the node traffic-report credential")
	}
	capabilitySource, err := os.ReadFile("../capabilities/network/kernel_reconciliation.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(capabilitySource), `s.State.EnsureTrafficCredential`) {
		t.Fatal("kernel reconciliation capability must own traffic credential persistence order")
	}
	credentialSource, err := os.ReadFile("kernel_traffic_credential.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(credentialSource)
	for _, expected := range []string{
		`node.TrafficSecret != "" || node.TrafficSecretRevokedAt != nil`,
		`&network.KernelEncryptedCredential{Ciphertext: encrypted, Prefix: prefix}`,
	} {
		if !strings.Contains(source, expected) {
			t.Fatalf("traffic credential bootstrap policy is incomplete: missing %q", expected)
		}
	}
}
