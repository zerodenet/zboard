package network

import (
	"context"
	"errors"
	"testing"
	"time"
)

type kernelReconciliationPreparerStub struct {
	prepared PreparedKernelReconciliation
	nodeID   uint
	err      error
}

func (s *kernelReconciliationPreparerStub) PrepareKernelReconciliation(_ context.Context, nodeID uint) (PreparedKernelReconciliation, error) {
	s.nodeID = nodeID
	return s.prepared, s.err
}

type preparedKernelReconciliationStub struct {
	probe       KernelProbe
	probeErr    error
	release     PreparedKernelRelease
	releaseErr  error
	version     string
	hadDeadline bool
}

func (s *preparedKernelReconciliationStub) Probe(ctx context.Context) (KernelProbe, error) {
	_, s.hadDeadline = ctx.Deadline()
	return s.probe, s.probeErr
}

func (s *preparedKernelReconciliationStub) ResolveRelease(_ context.Context, _ KernelProbe, version string) (PreparedKernelRelease, error) {
	s.version = version
	return s.release, s.releaseErr
}

type preparedKernelReleaseStub struct {
	descriptor KernelRelease
	traffic    *KernelEncryptedCredential
	activation PreparedKernelActivation
	err        error
}

func (s *preparedKernelReleaseStub) Descriptor() KernelRelease { return s.descriptor }
func (s *preparedKernelReleaseStub) PrepareTrafficCredential(context.Context) (*KernelEncryptedCredential, error) {
	return s.traffic, s.err
}
func (s *preparedKernelReleaseStub) PrepareActivation(context.Context) (PreparedKernelActivation, error) {
	return s.activation, s.err
}

type preparedKernelActivationStub struct {
	configSHA       string
	credential      KernelEncryptedCredential
	newCredential   bool
	materialization PreparedKernelMaterialization
	verified        KernelProbe
	verifyErr       error
	connectorAt     time.Time
	connectorErr    error
	invalidations   int
}

func (s *preparedKernelActivationStub) ConfigSHA256() string { return s.configSHA }
func (s *preparedKernelActivationStub) ConnectorCredential() (KernelEncryptedCredential, bool) {
	return s.credential, s.newCredential
}
func (s *preparedKernelActivationStub) ConnectorSnapshot() KernelConnectorSnapshot {
	return KernelConnectorSnapshot{Credential: KernelEncryptedCredential{Ciphertext: "old", Prefix: "old-prefix"}}
}
func (s *preparedKernelActivationStub) Materialize(context.Context) (PreparedKernelMaterialization, error) {
	return s.materialization, nil
}
func (s *preparedKernelActivationStub) Verify(context.Context, string) (KernelProbe, error) {
	return s.verified, s.verifyErr
}
func (s *preparedKernelActivationStub) WaitConnector(context.Context, time.Time) (time.Time, error) {
	return s.connectorAt, s.connectorErr
}
func (s *preparedKernelActivationStub) InvalidateConnectorCredential() { s.invalidations++ }

type preparedKernelMaterializationStub struct {
	sha         string
	installErr  error
	rollbackErr error
	installed   bool
	rolledBack  bool
}

func (s *preparedKernelMaterializationStub) BinarySHA256() string { return s.sha }
func (s *preparedKernelMaterializationStub) Install(context.Context, uint) error {
	s.installed = true
	return s.installErr
}
func (s *preparedKernelMaterializationStub) Rollback(context.Context, uint) error {
	s.rolledBack = true
	return s.rollbackErr
}

func TestKernelReconciliationOwnsPreparationTimeoutAndFailureFinalization(t *testing.T) {
	want := errors.New("activation failed")
	repository := &kernelReconciliationRepositoryStub{operation: KernelOperation{ID: 7, NodeID: 4, Phase: "queued"}}
	materialization := &preparedKernelMaterializationStub{sha: "binary", installErr: want}
	activation := &preparedKernelActivationStub{configSHA: "config", materialization: materialization}
	prepared := &preparedKernelReconciliationStub{
		probe:   KernelProbe{OperatingSystem: "linux", Architecture: "x86_64", Systemd: true, Installed: true, Version: "2.0.0"},
		release: &preparedKernelReleaseStub{descriptor: KernelRelease{Version: "1.2.3"}, activation: activation},
	}
	preparer := &kernelReconciliationPreparerStub{prepared: prepared}
	service := KernelReconciliation{State: KernelReconciliationState{Repository: repository}, Preparer: preparer, Timeout: time.Second}
	_, err := service.Reconcile(context.Background(), KernelReconciliationRequest{NodeID: 4, ActorID: 2, Version: "1.2.3", AllowDowngrade: true})
	if !errors.Is(err, want) || preparer.nodeID != 4 || repository.beginRequest.ActorID != 2 || !errors.Is(repository.failed, want) || repository.failedPhase != "staging" {
		t.Fatalf("prepare=%d begin=%+v failed=%v phase=%q err=%v", preparer.nodeID, repository.beginRequest, repository.failed, repository.failedPhase, err)
	}
	if !prepared.hadDeadline || prepared.version != "1.2.3" || repository.action != "downgrade" || !materialization.installed {
		t.Fatalf("prepared=%+v action=%q materialization=%+v", prepared, repository.action, materialization)
	}
}

func TestKernelReconciliationDoesNotFailAlreadyFinalizedOperation(t *testing.T) {
	repository := &kernelReconciliationRepositoryStub{operation: KernelOperation{ID: 7, NodeID: 4}}
	prepared := &preparedKernelReconciliationStub{
		probe: KernelProbe{OperatingSystem: "linux", Architecture: "x86_64", Systemd: true, Installed: true, Version: "2.0.0"},
		release: &preparedKernelReleaseStub{
			descriptor: KernelRelease{Version: "1.2.3"},
			activation: &preparedKernelActivationStub{configSHA: "config"},
		},
	}
	service := KernelReconciliation{State: KernelReconciliationState{Repository: repository}, Preparer: &kernelReconciliationPreparerStub{prepared: prepared}}
	if _, err := service.Reconcile(context.Background(), KernelReconciliationRequest{NodeID: 4, ActorID: 2, Version: "1.2.3"}); !errors.Is(err, ErrKernelOperationFinalized) {
		t.Fatalf("finalized error = %v", err)
	}
	if !repository.blocked || repository.failed != nil {
		t.Fatalf("blocked=%t already-finalized operation was failed again: %v", repository.blocked, repository.failed)
	}
}

func TestKernelReconciliationRollsBackActivationAndCredentialAfterVerificationFailure(t *testing.T) {
	want := errors.New("control socket unavailable")
	repository := &kernelReconciliationRepositoryStub{operation: KernelOperation{ID: 7, NodeID: 4}}
	materialization := &preparedKernelMaterializationStub{sha: "new-binary"}
	activation := &preparedKernelActivationStub{
		configSHA: "new-config", credential: KernelEncryptedCredential{Ciphertext: "new", Prefix: "prefix"},
		newCredential: true, materialization: materialization, verifyErr: want,
	}
	prepared := &preparedKernelReconciliationStub{
		probe: KernelProbe{OperatingSystem: "linux", Architecture: "x86_64", Systemd: true},
		release: &preparedKernelReleaseStub{
			descriptor: KernelRelease{Version: "1.2.3"},
			traffic:    &KernelEncryptedCredential{Ciphertext: "traffic", Prefix: "traffic-prefix"},
			activation: activation,
		},
	}
	service := KernelReconciliation{State: KernelReconciliationState{Repository: repository}, Preparer: &kernelReconciliationPreparerStub{prepared: prepared}}
	_, err := service.Reconcile(context.Background(), KernelReconciliationRequest{NodeID: 4, ActorID: 2, Version: "1.2.3"})
	if !errors.Is(err, want) || !materialization.installed || !materialization.rolledBack {
		t.Fatalf("installed=%t rolled_back=%t err=%v", materialization.installed, materialization.rolledBack, err)
	}
	if !repository.trafficCredential || !repository.connectorActive || !repository.connectorRestored || activation.invalidations != 2 || repository.failedPhase != "verifying" {
		t.Fatalf("repository=%+v invalidations=%d", repository, activation.invalidations)
	}
}
