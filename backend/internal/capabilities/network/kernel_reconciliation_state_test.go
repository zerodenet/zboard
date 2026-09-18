package network

import (
	"context"
	"errors"
	"testing"
	"time"
)

type kernelReconciliationRepositoryStub struct {
	operation         KernelOperation
	result            KernelReconciliationResult
	beginRequest      KernelDetectionRequest
	phase             string
	probeRecorded     bool
	releaseRecorded   bool
	action            string
	blocked           bool
	completed         bool
	changed           bool
	connectorVerified bool
	trafficCredential bool
	connectorActive   bool
	connectorRestored bool
	failed            error
	failedPhase       string
}

func (s *kernelReconciliationRepositoryStub) BeginReconciliation(_ context.Context, request KernelDetectionRequest, _ time.Time) (KernelOperation, error) {
	s.beginRequest = request
	return s.operation, nil
}

func (s *kernelReconciliationRepositoryStub) SetKernelPhase(_ context.Context, operation KernelOperation, phase string) (KernelOperation, error) {
	s.phase = phase
	operation.Phase = phase
	return operation, nil
}

func (s *kernelReconciliationRepositoryStub) RecordKernelProbe(context.Context, KernelOperation, KernelProbe, time.Time) error {
	s.probeRecorded = true
	return nil
}

func (s *kernelReconciliationRepositoryStub) RecordKernelRelease(_ context.Context, operation KernelOperation, _ KernelRelease) (KernelOperation, error) {
	s.releaseRecorded = true
	return operation, nil
}

func (s *kernelReconciliationRepositoryStub) RecordKernelAction(_ context.Context, operation KernelOperation, action string) (KernelOperation, error) {
	s.action = action
	operation.OperationType = action
	return operation, nil
}

func (s *kernelReconciliationRepositoryStub) EnsureKernelTrafficCredential(context.Context, KernelOperation, KernelEncryptedCredential) error {
	s.trafficCredential = true
	return nil
}

func (s *kernelReconciliationRepositoryStub) ActivateKernelConnectorCredential(context.Context, KernelOperation, KernelEncryptedCredential) error {
	s.connectorActive = true
	return nil
}

func (s *kernelReconciliationRepositoryStub) RestoreKernelConnectorCredential(context.Context, KernelOperation, KernelConnectorSnapshot) error {
	s.connectorRestored = true
	return nil
}

func (s *kernelReconciliationRepositoryStub) BlockKernelDowngrade(context.Context, KernelOperation, KernelProbe, KernelRelease, string, string, time.Time) error {
	s.blocked = true
	return nil
}

func (s *kernelReconciliationRepositoryStub) CompleteKernelReconciliation(_ context.Context, _ KernelOperation, _ KernelProbe, _ KernelRelease, _, _, _ string, changed, connectorVerified bool, _ string, _ time.Time) (KernelReconciliationResult, error) {
	s.completed, s.changed, s.connectorVerified = true, changed, connectorVerified
	return s.result, nil
}

func (s *kernelReconciliationRepositoryStub) FailKernelReconciliation(_ context.Context, operation KernelOperation, phase string, failure error, _ bool, _ time.Time) error {
	s.failed, s.failedPhase = failure, phase
	if phase != operation.Phase {
		return errors.New("failure phase does not match operation")
	}
	return nil
}

func TestKernelReconciliationStateDelegatesLifecycle(t *testing.T) {
	repository := &kernelReconciliationRepositoryStub{
		operation: KernelOperation{ID: 7, NodeID: 4, Phase: "queued"},
		result:    KernelReconciliationResult{Changed: true},
	}
	state := KernelReconciliationState{Repository: repository, Now: func() time.Time { return time.Unix(100, 0) }}
	operation, err := state.Begin(context.Background(), KernelDetectionRequest{NodeID: 4, ActorID: 2})
	if err != nil {
		t.Fatal(err)
	}
	operation, err = state.SetPhase(context.Background(), operation, "detecting")
	if err != nil {
		t.Fatal(err)
	}
	probe := KernelProbe{Installed: true}
	release := KernelRelease{Version: "0.0.15"}
	if err := state.RecordProbe(context.Background(), operation, probe); err != nil {
		t.Fatal(err)
	}
	if operation, err = state.RecordRelease(context.Background(), operation, release); err != nil {
		t.Fatal(err)
	}
	if operation, err = state.RecordAction(context.Background(), operation, "upgrade"); err != nil {
		t.Fatal(err)
	}
	credential := KernelEncryptedCredential{Ciphertext: "encrypted", Prefix: "prefix"}
	if err := state.EnsureTrafficCredential(context.Background(), operation, credential); err != nil {
		t.Fatal(err)
	}
	if err := state.ActivateConnectorCredential(context.Background(), operation, credential); err != nil {
		t.Fatal(err)
	}
	if err := state.RestoreConnectorCredential(context.Background(), operation, KernelConnectorSnapshot{}); err != nil {
		t.Fatal(err)
	}
	if _, err := state.Complete(context.Background(), operation, probe, release, "binary", "config", "done", true, true, ""); err != nil {
		t.Fatal(err)
	}
	if repository.beginRequest.ActorID != 2 || repository.phase != "detecting" || !repository.probeRecorded || !repository.releaseRecorded || repository.action != "upgrade" || !repository.trafficCredential || !repository.connectorActive || !repository.connectorRestored || !repository.completed || !repository.changed || !repository.connectorVerified {
		t.Fatalf("repository calls=%+v", repository)
	}
}

func TestKernelReconciliationStateMarksPersistedDowngradeBlockFinal(t *testing.T) {
	repository := &kernelReconciliationRepositoryStub{}
	state := KernelReconciliationState{Repository: repository}
	want := errors.New("downgrade requires confirmation")
	err := state.BlockDowngrade(context.Background(), KernelOperation{ID: 1, NodeID: 2}, KernelProbe{}, KernelRelease{Version: "0.0.14"}, "config", want)
	if !errors.Is(err, want) || !errors.Is(err, ErrKernelOperationFinalized) || !repository.blocked {
		t.Fatalf("blocked=%t err=%v", repository.blocked, err)
	}
}

func TestKernelReconciliationStateRejectsIncompleteBoundaryValues(t *testing.T) {
	state := KernelReconciliationState{}
	if _, err := state.Begin(context.Background(), KernelDetectionRequest{NodeID: 1, ActorID: 1}); !errors.Is(err, ErrKernelReconciliationUnavailable) {
		t.Fatalf("begin error=%v", err)
	}
	state.Repository = &kernelReconciliationRepositoryStub{}
	if _, err := state.SetPhase(context.Background(), KernelOperation{}, "detecting"); !errors.Is(err, ErrKernelReconciliationUnavailable) {
		t.Fatalf("phase error=%v", err)
	}
	if _, err := state.Complete(context.Background(), KernelOperation{ID: 1}, KernelProbe{}, KernelRelease{}, "", "", "", false, false, ""); !errors.Is(err, ErrKernelReconciliationUnavailable) {
		t.Fatalf("complete error=%v", err)
	}
}
