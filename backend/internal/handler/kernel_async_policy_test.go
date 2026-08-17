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
	if strings.Contains(source, "r.Context()") || strings.Contains(source, "reconcileNodeKernel(r.Context()") {
		t.Fatal("accepted kernel work must not inherit the initiating HTTP request context")
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
	for _, expected := range []string{"KernelVersion", "AllowDowngrade", "Version:        content.KernelVersion", "reconcileNodeKernel(ctx, node, &operation, request)"} {
		if !strings.Contains(source, expected) {
			t.Fatalf("node reconcile background task lost %q", expected)
		}
	}
}
