package handler

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	zeroadapter "github.com/zerodenet/zboard/backend/internal/adapters/zero"
)

func TestKernelActivationWaitsForRecoveryAndResetsHealthySamples(t *testing.T) {
	if zeroActivationVerificationTimeout <= 45*time.Second {
		t.Fatal("activation must allow durable replay beyond the old 45 second window")
	}
	const sha = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	healthy := kernelProbe{Installed: true, BinarySHA256: sha, ServiceStatus: "active", ControlStatus: "healthy"}
	calls := 0
	probe, err := verifyKernelActivation(context.Background(), sha, time.Second, 0,
		func(context.Context) (kernelProbe, error) {
			calls++
			if calls <= 47 {
				return kernelProbe{Installed: true, BinarySHA256: sha, ServiceStatus: "active", ControlStatus: "unavailable"}, nil
			}
			if calls == 50 {
				return kernelProbe{}, errors.New("transient SSH failure")
			}
			return healthy, nil
		}, func() string { t.Fatal("healthy activation captured a failure"); return "" })
	if err != nil || probe != healthy || calls != 53 {
		t.Fatalf("probe=%+v calls=%d err=%v", probe, calls, err)
	}
}

func TestKernelActivationHonorsCancellationEvenOnHealthyProbe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	const sha = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	calls, captures := 0, 0
	_, err := verifyKernelActivation(ctx, sha, time.Second, 0,
		func(context.Context) (kernelProbe, error) {
			calls++
			if calls == zeroActivationHealthySamples {
				cancel()
			}
			return kernelProbe{Installed: true, BinarySHA256: sha, ServiceStatus: "active", ControlStatus: "healthy"}, nil
		}, func() string { captures++; return "diagnostic summary" })
	if err == nil || captures != 1 || !strings.Contains(err.Error(), "diagnostic summary") {
		t.Fatalf("captures=%d err=%v", captures, err)
	}
}

func TestKernelActivationTimesOutWhenControlNeverBecomesReady(t *testing.T) {
	_, err := verifyKernelActivation(context.Background(), "expected", 5*time.Millisecond, time.Millisecond,
		func(ctx context.Context) (kernelProbe, error) {
			<-ctx.Done()
			return kernelProbe{}, ctx.Err()
		}, func() string { return "still starting" })
	if err == nil || !strings.Contains(err.Error(), "last probe error") || !strings.Contains(err.Error(), "still starting") {
		t.Fatalf("err=%v", err)
	}
}

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
	script := zeroadapter.BuildKernelInstallScript("/tmp/stage", strings.Repeat("a", 64), 42)
	if strings.Contains(script, "healthy=0") || strings.Contains(script, "for attempt in 1 2 3 4 5 6 7 8 9 10") {
		t.Fatal("install shell must not own the Zero readiness window")
	}
	if !strings.Contains(script, "systemctl restart zero") {
		t.Fatal("install shell must still activate the service")
	}
}
