package zero

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func eventStateConfig(receiver, source string) []byte {
	data, _ := json.Marshal(map[string]any{"api": ConnectorAPIConfig(receiver, 8, "secret", false)})
	return []byte(strings.ReplaceAll(string(data), "node-8", source))
}

func TestEventStateOwnerSeparatesNodesAndPanelsButSurvivesKeyRotation(t *testing.T) {
	first := eventStateConfig("https://one.example.test", "node-8")
	owner, err := kernelEventStateOwner(first)
	if err != nil || len(owner) != 64 {
		t.Fatalf("owner=%q err=%v", owner, err)
	}
	for _, config := range [][]byte{
		eventStateConfig("https://one.example.test", "node-2"),
		eventStateConfig("https://two.example.test", "node-8"),
	} {
		other, err := kernelEventStateOwner(config)
		if err != nil || other == owner {
			t.Fatalf("different installation shares event state: %q %v", other, err)
		}
	}
	rotated, err := kernelEventStateOwner([]byte(strings.ReplaceAll(string(first), "Bearer secret", "Bearer rotated")))
	if err != nil || rotated != owner {
		t.Fatal("credential rotation changed durable event ownership")
	}
}

func TestKernelInstallerIsolatesForeignStateBeforeCompatibilityInspection(t *testing.T) {
	for _, mode := range []string{"reset", "preserve"} {
		t.Run(mode, func(t *testing.T) {
			session := &kernelRemoteSessionStub{eventStateMode: mode}
			err := (KernelInstaller{Dialer: &kernelRemoteDialerStub{session: session}}).Install(context.Background(), KernelInstallRequest{
				OperationID: 42, Binary: []byte("binary"), BinarySHA256: strings.Repeat("a", 64),
				RuntimeConfig: eventStateConfig("https://panel.example.test", "node-8"), ConnectorKey: "secret",
			})
			if err != nil {
				t.Fatal(err)
			}
			inspected := false
			for _, run := range session.runs {
				inspected = inspected || strings.Contains(run.command, " connector state ")
			}
			if inspected != (mode == "preserve") {
				t.Fatal("compatibility inspection must apply only to the queue being retained")
			}
			var plan string
			for _, upload := range session.uploads {
				if strings.HasSuffix(upload.path, "/event-state.plan") {
					plan = string(upload.data)
				}
			}
			if !strings.HasSuffix(plan, " 0 "+mode+"\n") {
				t.Fatalf("initialization plan=%q", plan)
			}
		})
	}
}

func TestKernelEventStateOwnershipFailureNeverActivates(t *testing.T) {
	session := &kernelRemoteSessionStub{eventStateMode: "unexpected response"}
	err := (KernelInstaller{Dialer: &kernelRemoteDialerStub{session: session}}).Install(context.Background(), KernelInstallRequest{
		OperationID: 42, Binary: []byte("binary"), BinarySHA256: strings.Repeat("a", 64),
		RuntimeConfig: eventStateConfig("https://panel.example.test", "node-8"), ConnectorKey: "secret",
	})
	if err == nil || !strings.Contains(err.Error(), "ownership response") {
		t.Fatalf("error=%v", err)
	}
	for _, run := range session.runs {
		if strings.Contains(run.command, "ZBOARD_KERNEL_ACTIVATED") {
			t.Fatal("ownership failure activated a kernel")
		}
	}
}
