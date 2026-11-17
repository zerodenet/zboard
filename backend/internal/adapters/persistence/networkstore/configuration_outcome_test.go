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

func TestUnknownPublicationSurvivesRestartWithoutDuplicateDeploymentOrReadinessChanges(t *testing.T) {
	store, node, endpoint := configurationPublicationFixture(t)
	ctx, now := context.Background(), time.Now().UTC()
	request := network.ConfigurationPublicationRequest{NodeID: node.ID, TriggerEndpointID: endpoint.ID}
	start, err := store.BeginConfigurationPublication(ctx, request, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.BeginConfigurationPublication(ctx, request, now.Add(time.Second)); !errors.Is(err, network.ErrKernelOperationRunning) {
		t.Fatalf("concurrent publication admitted: %v", err)
	}
	desired := strings.Repeat("a", 64)
	deployment, err := store.SetConfigurationPublicationDesired(ctx, start.Deployment, desired)
	if err != nil {
		t.Fatal(err)
	}
	if err := EnqueuePublication(store.DB, node.ID, 99, 0); err != nil {
		t.Fatal(err)
	}
	var latest model.NodeConfigPublish
	if err := store.DB.First(&latest, node.ID).Error; err != nil {
		t.Fatal(err)
	}

	state := network.ConfigurationPublicationState{Repository: store}
	pending, err := state.Uncertain(ctx, deployment, network.ConfigurationOutcomeUnknown(context.DeadlineExceeded), "phase=reload")
	if !errors.Is(err, network.ErrConfigurationOutcomeUnknown) || pending.Status != "reconciling" || pending.FinishedAt != nil {
		t.Fatalf("pending=%+v err=%v", pending, err)
	}
	var retained model.NodeConfigPublish
	if err := store.DB.First(&retained, node.ID).Error; err != nil {
		t.Fatal(err)
	}
	if retained.Generation != latest.Generation || retained.EndpointID != 99 || !retained.Reconciling {
		t.Fatal("recovery replaced newer intent", retained)
	}

	restarted := ConfigurationPublicationState{DB: store.DB}
	for i := 0; i < 3; i++ {
		resumed, err := restarted.BeginConfigurationPublication(ctx, request, now.Add(time.Hour))
		if err != nil || resumed.Deployment.ID != pending.ID || resumed.Deployment.Status != "reconciling" {
			t.Fatalf("resumed=%+v error=%v", resumed, err)
		}
	}
	var count int64
	store.DB.Model(&model.ProtocolDeployment{}).Where("node_id = ?", node.ID).Count(&count)
	if count != 1 {
		t.Fatalf("checks created %d deployment records", count)
	}
	var storedEndpoint model.ProtocolEndpoint
	store.DB.First(&storedEndpoint, endpoint.ID)
	var storedNode model.Node
	store.DB.First(&storedNode, node.ID)
	if storedEndpoint.MieruPrincipalReady || storedNode.NodeCredential != "old" || storedNode.LastSyncAt != nil {
		t.Fatal("unconfirmed result changed credential, readiness or sync time")
	}
	if err := restarted.ResolveConfigurationPublication(ctx, pending, true, "matching receipt", now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	// The old ACK cannot change readiness of endpoints created after its request.
	store.DB.First(&storedEndpoint, endpoint.ID)
	if storedEndpoint.MieruPrincipalReady {
		t.Fatal("old receipt enabled current endpoint")
	}
	next, err := restarted.BeginConfigurationPublication(ctx, request, now.Add(time.Hour))
	if err != nil || next.Deployment.ID == pending.ID || next.Deployment.Status != "running" {
		t.Fatal(next, err)
	}
}

func TestExpiredPublicationIsReadBeforeAnyNewMutation(t *testing.T) {
	store, node, endpoint := configurationPublicationFixture(t)
	ctx, now := context.Background(), time.Now().UTC()
	request := network.ConfigurationPublicationRequest{NodeID: node.ID, TriggerEndpointID: endpoint.ID}
	start, err := store.BeginConfigurationPublication(ctx, request, now)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := store.SetConfigurationPublicationDesired(ctx, start.Deployment, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	// Simulate panel exit after sending config.apply, before any failure callback.
	resumed, err := store.BeginConfigurationPublication(ctx, request, now.Add(network.PublicationLease))
	if err != nil || resumed.Deployment.ID != pending.ID || resumed.Deployment.Status != "reconciling" {
		t.Fatal(resumed, err)
	}
}

func TestPublicationCircuitRetainsIntentAndReopensOnlyForNewRequest(t *testing.T) {
	db, _ := administrationFixture(t)
	if err := db.Create(&model.Node{ID: 1, Name: "circuit", Config: "{}"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := EnqueuePublication(db, 1, 11, 0); err != nil {
		t.Fatal(err)
	}
	ctx, now := context.Background(), time.Now().UTC().Add(time.Second)
	q := PublicationQueue{DB: db}
	for i := 0; i < network.PublicationFailureLimit; i++ {
		item, ok, err := q.Claim(ctx, now)
		if err != nil || !ok {
			t.Fatal(i, ok, err)
		}
		if err := q.Complete(ctx, item, now, errors.New("SSH unavailable")); err != nil {
			t.Fatal(err)
		}
		now = now.Add(time.Hour)
	}
	if _, ok, err := q.Claim(ctx, now.Add(24*time.Hour)); err != nil || ok {
		t.Fatalf("circuit ignored: %t %v", ok, err)
	}
	if ready, err := q.Pending(ctx, now); err != nil || ready {
		t.Fatal(ready, err)
	}
	var held model.NodeConfigPublish
	db.First(&held, 1)
	if !held.Suspended || held.EndpointID != 11 {
		t.Fatal("latest intent lost", held)
	}
	if err := EnqueuePublication(db, 1, 12, 0); err != nil {
		t.Fatal(err)
	}
	item, ok, err := q.Claim(ctx, now)
	if err != nil || !ok || item.Attempts != 0 || item.EndpointID != 12 {
		t.Fatal(item, ok, err)
	}
	if err := q.Complete(ctx, item, now, network.ConfigurationOutcomeUnknown(context.DeadlineExceeded)); err != nil {
		t.Fatal(err)
	}
	db.First(&held, 1)
	if held.Suspended || !held.Reconciling || held.Attempts != 0 {
		t.Fatal("unknown outcome consumed retry budget", held)
	}
}

func TestHealthyDetectionDoesNotConfirmUnknownConfiguration(t *testing.T) {
	store, admin, node := kernelDetectionFixture(t)
	ctx, now := context.Background(), time.Now().UTC()
	oldSHA := strings.Repeat("b", 64)
	if err := store.DB.Create(&model.NodeKernelState{NodeID: node.ID, Status: "reconciling", Phase: "awaiting_configuration", AppliedConfigSHA256: oldSHA}).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Create(&model.ProtocolDeployment{NodeID: node.ID, Status: "reconciling", DesiredConfigSHA256: strings.Repeat("a", 64), StartedAt: &now}).Error; err != nil {
		t.Fatal(err)
	}
	operation, err := store.BeginDetection(ctx, network.KernelDetectionRequest{NodeID: node.ID, ActorID: admin.ID}, now)
	if err != nil {
		t.Fatal(err)
	}
	result, err := store.CompleteDetection(ctx, operation, network.KernelProbe{Installed: true, ConfigSHA256: strings.Repeat("c", 64), ServiceStatus: "active", ControlStatus: "healthy"}, network.KernelAssessment{Status: "healthy"}, "healthy status", now)
	if err != nil || result.State.Status != "reconciling" || result.State.Phase != "awaiting_configuration" || result.State.AppliedConfigSHA256 != oldSHA {
		t.Fatal(result, err)
	}
}

func TestExpiredConfigureOwnerAfterPanelExitDoesNotBlockReceiptReconciliation(t *testing.T) {
	store, node, endpoint := configurationPublicationFixture(t)
	ctx, now := context.Background(), time.Now().UTC()
	old := now.Add(-12 * time.Minute)
	operation := model.NodeOperation{NodeID: node.ID, OperationType: "configure", Status: "running", StartedAt: &old, UpdatedAt: old}
	if err := store.DB.Create(&operation).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Create(&model.NodeKernelState{NodeID: node.ID, ActiveOperationID: &operation.ID}).Error; err != nil {
		t.Fatal(err)
	}
	pending := model.ProtocolDeployment{NodeID: node.ID, Status: "running", DesiredConfigSHA256: strings.Repeat("a", 64), StartedAt: &old}
	if err := store.DB.Create(&pending).Error; err != nil {
		t.Fatal(err)
	}
	resumed, err := store.BeginConfigurationPublication(ctx, network.ConfigurationPublicationRequest{NodeID: node.ID, TriggerEndpointID: endpoint.ID}, now)
	if err != nil || resumed.Deployment.ID != pending.ID || resumed.Deployment.Status != "reconciling" {
		t.Fatal(resumed, err)
	}
	var state model.NodeKernelState
	if err := store.DB.First(&state, "node_id = ?", node.ID).Error; err != nil {
		t.Fatal(err)
	}
	if state.ActiveOperationID != nil {
		t.Fatal("expired configure ownership remained active")
	}
}
