package handler

import (
	"strings"
	"testing"
)

func TestKernelActivationProbeHealthyRequiresStableExpectedGeneration(t *testing.T) {
	const sha = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	healthy := kernelProbe{Installed: true, BinarySHA256: sha, ServiceStatus: "active", ControlStatus: "healthy"}
	if !kernelActivationProbeHealthy(healthy, sha) {
		t.Fatal("expected active matching generation with healthy control socket to be accepted")
	}
	cases := []kernelProbe{
		{Installed: true, BinarySHA256: sha, ServiceStatus: "activating", ControlStatus: "unavailable"},
		{Installed: true, BinarySHA256: sha, ServiceStatus: "active", ControlStatus: "unavailable"},
		{Installed: true, BinarySHA256: strings.Repeat("b", 64), ServiceStatus: "active", ControlStatus: "healthy"},
	}
	for _, probe := range cases {
		if kernelActivationProbeHealthy(probe, sha) {
			t.Fatalf("unexpected healthy activation probe: %+v", probe)
		}
	}
}

func TestZeroInstallScriptLeavesReadinessVerificationToGo(t *testing.T) {
	script := buildZeroInstallScript("/tmp/stage", strings.Repeat("a", 64), 42)
	if strings.Contains(script, "healthy=0") || strings.Contains(script, "for attempt in 1 2 3 4 5 6 7 8 9 10") {
		t.Fatal("install shell must not own the Zero readiness window")
	}
	if !strings.Contains(script, "systemctl restart zero") {
		t.Fatal("install shell must still activate the service")
	}
}
