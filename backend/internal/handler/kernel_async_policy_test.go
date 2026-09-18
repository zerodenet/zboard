package handler

import (
	"os"
	"strings"
	"testing"
)

func TestKernelReconcileHTTPHandlerQueuesPersistedTask(t *testing.T) {
	payload, err := os.ReadFile("kernel_async.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(payload)
	if !strings.Contains(source, "createOperationTask") || !strings.Contains(source, "http.StatusAccepted") {
		t.Fatal("single-node kernel reconcile must enqueue a persisted task and return accepted")
	}
	if !strings.Contains(source, "createOperationTask(r.Context()") || strings.Contains(source, "context.Background()") {
		t.Fatal("accepted kernel work must preserve request values while task submission detaches cancellation")
	}
	if !strings.Contains(source, "version is required for kernel reconcile") {
		t.Fatal("kernel reconcile must pin an explicit target version at submission")
	}
}

func TestKernelBatchTaskCarriesPinnedVersion(t *testing.T) {
	payload, err := os.ReadFile("batch_operations.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(payload)
	for _, expected := range []string{"context.WithoutCancel(ctx)", "KernelVersion", "AllowDowngrade", "Version: action.KernelVersion", "AllowDowngrade: action.AllowDowngrade", "KernelReconciliation(h).Reconcile"} {
		if !strings.Contains(source, expected) {
			t.Fatalf("node reconcile background task lost %q", expected)
		}
	}
	adapter, err := os.ReadFile("kernel_automation.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"PrepareKernelReconciliation", "ResolveRelease", "PrepareActivation", "Materialize", "Install", "Rollback"} {
		if !strings.Contains(string(adapter), expected) {
			t.Fatalf("kernel execution adapter lost %q", expected)
		}
	}
	if strings.Contains(string(adapter), "reconcileNodeKernel") {
		t.Fatal("kernel stage and rollback orchestration returned to the HTTP adapter")
	}
}
