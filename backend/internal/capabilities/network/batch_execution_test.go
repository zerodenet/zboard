package network

import (
	"context"
	"errors"
	"testing"
)

type batchResourceRepositoryStub struct {
	action     BatchResourceAction
	inspectErr error
	commitErr  error
	commits    int
	lastClaim  BatchResourceClaim
	lastAction BatchResourceAction
}

func (s *batchResourceRepositoryStub) Inspect(_ context.Context, claim BatchResourceClaim) (BatchResourceAction, error) {
	s.lastClaim = claim
	return s.action, s.inspectErr
}
func (s *batchResourceRepositoryStub) Commit(_ context.Context, claim BatchResourceClaim, action BatchResourceAction) (BatchResourceAction, error) {
	s.commits++
	s.lastClaim = claim
	s.lastAction = action
	return s.action, s.commitErr
}

type batchResourceAdapterStub struct {
	validationErr error
	publishErr    error
	validated     [][]uint
	published     []BatchResourceAction
	detected      []BatchResourceAction
	reconciled    []BatchResourceAction
	groups        []BatchResourceAction
}

func (s *batchResourceAdapterStub) DetectBatchNode(_ context.Context, action BatchResourceAction) error {
	s.detected = append(s.detected, action)
	return nil
}
func (s *batchResourceAdapterStub) ReconcileBatchNode(_ context.Context, action BatchResourceAction) error {
	s.reconciled = append(s.reconciled, action)
	return nil
}
func (s *batchResourceAdapterStub) ReconcileBatchNodeGroup(_ context.Context, action BatchResourceAction) error {
	s.groups = append(s.groups, action)
	return nil
}
func (s *batchResourceAdapterStub) ValidateBatchProtocolActivation(_ context.Context, ids []uint) error {
	s.validated = append(s.validated, ids)
	return s.validationErr
}
func (s *batchResourceAdapterStub) PublishBatchNodeConfig(_ context.Context, action BatchResourceAction) error {
	s.published = append(s.published, action)
	return s.publishErr
}

func TestBatchResourceExecutionSeparatesLocalAndExternalEffects(t *testing.T) {
	claim := BatchResourceClaim{TaskID: 1, ItemID: 2, Token: "lease"}
	for _, test := range []struct {
		name       string
		action     BatchResourceAction
		commits    int
		validates  int
		publishes  int
		detects    int
		reconciles int
		groups     int
	}{
		{name: "detect", action: BatchResourceAction{Kind: BatchNodeDetect, TargetType: "node", NodeID: 3, ActorID: 4, TaskItemID: 2}, detects: 1},
		{name: "reconcile", action: BatchResourceAction{Kind: BatchNodeReconcile, TargetType: "node", NodeID: 3, ActorID: 4, TaskItemID: 2, KernelVersion: "1.2.3"}, reconciles: 1},
		{name: "group", action: BatchResourceAction{Kind: BatchNodeGroupSync, TargetType: "node_group", NodeGroupID: 7, ActorID: 4, TaskItemID: 2}, groups: 1},
		{name: "group publication", action: BatchResourceAction{Kind: BatchNodeGroupSync, TargetType: "node", NodeID: 3, ActorID: 4, TaskItemID: 2, PublishEndpointID: 5}, publishes: 1},
		{name: "lifecycle", action: BatchResourceAction{Kind: BatchNodeLifecycle, TargetType: "node", NodeID: 3, ActorID: 4, TaskItemID: 2, Lifecycle: "maintenance"}, commits: 1},
		{name: "deploy", action: BatchResourceAction{Kind: BatchProtocolDeploy, TargetType: "node", NodeID: 3, ActorID: 4, TaskItemID: 2, PublishEndpointID: 5}, commits: 1, publishes: 1},
		{name: "disable", action: BatchResourceAction{Kind: BatchProtocolActive, TargetType: "node", NodeID: 3, ActorID: 4, TaskItemID: 2, EndpointIDs: []uint{5}, PublishEndpointID: 5}, commits: 1, publishes: 1},
		{name: "activate", action: BatchResourceAction{Kind: BatchProtocolActive, TargetType: "node", NodeID: 3, ActorID: 4, TaskItemID: 2, EndpointIDs: []uint{5}, Active: true, PublishEndpointID: 5}, commits: 1, validates: 1, publishes: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := &batchResourceRepositoryStub{action: test.action}
			adapter := &batchResourceAdapterStub{}
			service := BatchResourceExecution{Repository: repository, Adapter: adapter}
			if err := service.Execute(context.Background(), claim); err != nil {
				t.Fatal(err)
			}
			if repository.commits != test.commits || len(adapter.validated) != test.validates || len(adapter.published) != test.publishes || len(adapter.detected) != test.detects || len(adapter.reconciled) != test.reconciles || len(adapter.groups) != test.groups {
				t.Fatalf("commits=%d validations=%d publications=%d detects=%d reconciles=%d groups=%d", repository.commits, len(adapter.validated), len(adapter.published), len(adapter.detected), len(adapter.reconciled), len(adapter.groups))
			}
		})
	}
}

func TestBatchResourceExecutionDoesNotCommitAfterActivationValidationFailure(t *testing.T) {
	repository := &batchResourceRepositoryStub{action: BatchResourceAction{Kind: BatchProtocolActive, TargetType: "node", NodeID: 3, ActorID: 4, TaskItemID: 2, EndpointIDs: []uint{5}, Active: true}}
	want := errors.New("unsupported protocol")
	adapter := &batchResourceAdapterStub{validationErr: want}
	err := (BatchResourceExecution{Repository: repository, Adapter: adapter}).Execute(context.Background(), BatchResourceClaim{TaskID: 1, ItemID: 2, Token: "lease"})
	if !errors.Is(err, want) || repository.commits != 0 || len(adapter.published) != 0 {
		t.Fatalf("validation failure crossed commit boundary: err=%v commits=%d publications=%d", err, repository.commits, len(adapter.published))
	}
}
