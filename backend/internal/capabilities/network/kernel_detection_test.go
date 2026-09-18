package network

import (
	"context"
	"errors"
	"testing"
	"time"
)

type kernelDetectionRepositoryStub struct {
	operation   KernelOperation
	result      KernelDetectionResult
	begun       bool
	failed      error
	failCtx     error
	completeCtx error
	completed   KernelAssessment
}

func (s *kernelDetectionRepositoryStub) BeginDetection(context.Context, KernelDetectionRequest, time.Time) (KernelOperation, error) {
	s.begun = true
	return s.operation, nil
}
func (s *kernelDetectionRepositoryStub) CompleteDetection(ctx context.Context, _ KernelOperation, _ KernelProbe, assessment KernelAssessment, _ string, _ time.Time) (KernelDetectionResult, error) {
	s.completed, s.completeCtx = assessment, ctx.Err()
	return s.result, nil
}

func TestKernelDetectionFinalizesSuccessfulProbeAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	repository := &kernelDetectionRepositoryStub{operation: KernelOperation{ID: 3, NodeID: 2}}
	service := KernelDetection{Repository: repository, Executor: kernelProbeExecutorStub{probe: KernelProbe{Installed: true}, cancel: cancel}}
	if _, err := service.Detect(ctx, KernelDetectionRequest{NodeID: 2, ActorID: 1}); err != nil {
		t.Fatalf("detect error=%v", err)
	}
	if repository.completeCtx != nil {
		t.Fatalf("successful probe inherited cancellation: %v", repository.completeCtx)
	}
}
func (s *kernelDetectionRepositoryStub) FailDetection(ctx context.Context, _ KernelOperation, _ string, failure error, _ bool, _ time.Time) error {
	s.failed, s.failCtx = failure, ctx.Err()
	return nil
}

type kernelProbeExecutorStub struct {
	validateErr error
	probe       KernelProbe
	probeErr    error
	cancel      context.CancelFunc
}

func (s kernelProbeExecutorStub) ValidateKernelProbeTarget(context.Context, uint) error {
	return s.validateErr
}
func (s kernelProbeExecutorStub) ProbeKernel(context.Context, uint) (KernelProbe, error) {
	if s.cancel != nil {
		s.cancel()
	}
	return s.probe, s.probeErr
}

func TestKernelDetectionCompletesAssessedProbe(t *testing.T) {
	repository := &kernelDetectionRepositoryStub{operation: KernelOperation{ID: 3, NodeID: 2}, result: KernelDetectionResult{Operation: KernelOperation{ID: 3}}}
	service := KernelDetection{Repository: repository, Executor: kernelProbeExecutorStub{probe: KernelProbe{Installed: true, ServiceStatus: "active", ControlStatus: "healthy"}}, ArtifactAvailable: true}
	result, err := service.Detect(context.Background(), KernelDetectionRequest{NodeID: 2, ActorID: 1})
	if err != nil || result.Operation.ID != 3 {
		t.Fatalf("detect: result=%+v err=%v", result, err)
	}
	if repository.completed.Status != "healthy" || repository.completed.RecommendedAction != "check_release" {
		t.Fatalf("assessment=%+v", repository.completed)
	}
}

func TestKernelDetectionDoesNotBeginInvalidTarget(t *testing.T) {
	want := errors.New("ssh invalid")
	repository := &kernelDetectionRepositoryStub{}
	service := KernelDetection{Repository: repository, Executor: kernelProbeExecutorStub{validateErr: want}}
	if _, err := service.Detect(context.Background(), KernelDetectionRequest{NodeID: 2, ActorID: 1}); !errors.Is(err, want) || repository.begun {
		t.Fatalf("validation result: begun=%t err=%v", repository.begun, err)
	}
}

func TestKernelDetectionFinalizesProbeFailureAfterCancellation(t *testing.T) {
	want := errors.New("ssh canceled")
	ctx, cancel := context.WithCancel(context.Background())
	repository := &kernelDetectionRepositoryStub{operation: KernelOperation{ID: 3, NodeID: 2}}
	service := KernelDetection{Repository: repository, Executor: kernelProbeExecutorStub{probeErr: want, cancel: cancel}}
	if _, err := service.Detect(ctx, KernelDetectionRequest{NodeID: 2, ActorID: 1}); !errors.Is(err, want) {
		t.Fatalf("detect error=%v", err)
	}
	if !errors.Is(repository.failed, want) || repository.failCtx != nil {
		t.Fatalf("failure was not finalized independently: failed=%v ctx=%v", repository.failed, repository.failCtx)
	}
}

func TestAssessKernelProbe(t *testing.T) {
	tests := []struct {
		probe    KernelProbe
		artifact bool
		status   string
		action   string
	}{
		{KernelProbe{Architecture: "x86_64", Systemd: true}, true, "not_installed", "install"},
		{KernelProbe{Architecture: "arm64", Systemd: true}, true, "unsupported", "manual_review"},
		{KernelProbe{Architecture: "x86_64", Systemd: true}, false, "unsupported", "manual_review"},
		{KernelProbe{Installed: true, ServiceStatus: "active", ControlStatus: "healthy"}, true, "healthy", "check_release"},
		{KernelProbe{Installed: true, ServiceStatus: "failed"}, true, "degraded", "repair"},
	}
	for _, test := range tests {
		got := AssessKernelProbe(test.probe, test.artifact)
		if got.Status != test.status || got.RecommendedAction != test.action {
			t.Fatalf("probe=%+v assessment=%+v", test.probe, got)
		}
	}
}
