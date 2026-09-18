package networkstore

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func kernelDetectionFixture(t *testing.T) (*KernelDetection, model.User, model.Node) {
	t.Helper()
	db, _ := administrationFixture(t)
	admin := model.User{ID: 1, Email: "kernel-admin@example.test", Password: "hash", IsAdmin: true, Status: "active"}
	node := model.Node{ID: 1, Name: "kernel-node", LifecycleStatus: "active"}
	for _, record := range []interface{}{&admin, &node} {
		if err := db.Create(record).Error; err != nil {
			t.Fatal(err)
		}
	}
	return &KernelDetection{DB: db}, admin, node
}

func TestKernelDetectionPersistsFencedLifecycle(t *testing.T) {
	store, admin, node := kernelDetectionFixture(t)
	ctx := context.Background()
	started := time.Now().UTC()
	request := network.KernelDetectionRequest{NodeID: node.ID, ActorID: admin.ID}
	operation, err := store.BeginDetection(ctx, request, started)
	if err != nil {
		t.Fatal(err)
	}
	if operation.Status != "running" || operation.Phase != "detecting" || operation.RequestedBy != admin.ID {
		t.Fatalf("operation=%+v", operation)
	}
	if _, err := store.BeginDetection(ctx, request, started.Add(time.Second)); !errors.Is(err, network.ErrKernelOperationRunning) {
		t.Fatalf("second operation error=%v", err)
	}
	probe := network.KernelProbe{
		OperatingSystem: "debian 12", Architecture: "x86_64", Libc: "glibc 2.36", Systemd: true,
		Installed: true, Version: "0.0.15", BinarySHA256: strings.Repeat("a", 64), ConfigSHA256: strings.Repeat("b", 64),
		ServiceStatus: "active", ControlStatus: "healthy",
	}
	finished := started.Add(2 * time.Second)
	result, err := store.CompleteDetection(ctx, operation, probe, network.KernelAssessment{Status: "healthy", RecommendedAction: "check_release"}, "healthy probe", finished)
	if err != nil {
		t.Fatal(err)
	}
	if result.State.Status != "healthy" || result.State.ActiveOperationID != nil || result.State.LastDetectedAt == nil || !result.State.LastDetectedAt.Equal(finished) {
		t.Fatalf("state=%+v", result.State)
	}
	if result.Operation.Status != "succeeded" || result.Operation.Phase != "completed" || result.Operation.ResultSummary != "healthy probe" {
		t.Fatalf("completed operation=%+v", result.Operation)
	}
	var audits int64
	if err := store.DB.Model(&model.AuditLog{}).Where("action = ? AND target = ?", "node.kernel.detect", "node:1").Count(&audits).Error; err != nil || audits != 1 {
		t.Fatalf("audit count=%d err=%v", audits, err)
	}
}

func TestKernelDetectionFailureAndStaleCompletionAreFenced(t *testing.T) {
	store, admin, node := kernelDetectionFixture(t)
	ctx := context.Background()
	request := network.KernelDetectionRequest{NodeID: node.ID, ActorID: admin.ID}
	operation, err := store.BeginDetection(ctx, request, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New(strings.Repeat("界", 2100))
	if err := store.FailDetection(ctx, operation, "detecting", failure, false, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	var state model.NodeKernelState
	var row model.NodeOperation
	if err := store.DB.First(&state, "node_id = ?", node.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB.First(&row, operation.ID).Error; err != nil {
		t.Fatal(err)
	}
	if state.ActiveOperationID != nil || state.Status != "failed" || row.Status != "failed" || len([]rune(row.Error)) != 2001 {
		t.Fatalf("failed state=%+v operation=%+v", state, row)
	}
	next, err := store.BeginDetection(ctx, request, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Model(&model.NodeKernelState{}).Where("node_id = ?", node.ID).Update("active_operation_id", nil).Error; err != nil {
		t.Fatal(err)
	}
	_, err = store.CompleteDetection(ctx, next, network.KernelProbe{}, network.KernelAssessment{}, "stale", time.Now().UTC())
	if !errors.Is(err, network.ErrKernelOperationLost) {
		t.Fatalf("stale completion error=%v", err)
	}
}

func TestKernelDetectionRechecksActorAndNodeAvailability(t *testing.T) {
	store, admin, node := kernelDetectionFixture(t)
	ctx := context.Background()
	request := network.KernelDetectionRequest{NodeID: node.ID, ActorID: admin.ID}
	if err := store.DB.Model(&model.User{}).Where("id = ?", admin.ID).Update("status", "disabled").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := store.BeginDetection(ctx, request, time.Now().UTC()); !errors.Is(err, network.ErrKernelPermission) {
		t.Fatalf("revoked actor error=%v", err)
	}
	if err := store.DB.Model(&model.User{}).Where("id = ?", admin.ID).Update("status", "active").Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Model(&model.Node{}).Where("id = ?", node.ID).Update("lifecycle_status", "deleting").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := store.BeginDetection(ctx, request, time.Now().UTC()); !errors.Is(err, network.ErrKernelResourceDeleting) {
		t.Fatalf("deleting node error=%v", err)
	}
}
