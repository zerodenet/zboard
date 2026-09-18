package network

import (
	"context"
	"errors"
	"time"
)

var (
	ErrKernelReconciliationUnavailable = errors.New("kernel reconciliation state capability unavailable")
	ErrKernelOperationFinalized        = errors.New("kernel operation was already finalized")
)

type KernelRelease struct {
	Version, ArtifactURL, ArtifactSHA256 string
	ArtifactSize                         int64
}

type KernelReconciliationResult struct {
	State             KernelState     `json:"state"`
	Operation         KernelOperation `json:"operation"`
	Changed           bool            `json:"changed"`
	Action            string          `json:"action,omitempty"`
	ConnectorVerified bool            `json:"connector_verified,omitempty"`
	ConnectorWarning  string          `json:"connector_warning,omitempty"`
}

type KernelEncryptedCredential struct {
	Ciphertext string
	Prefix     string
}

type KernelConnectorSnapshot struct {
	Credential          KernelEncryptedCredential
	RevokedAt           *time.Time
	ConnectorLastSeenAt *time.Time
	LastSeenAt          *time.Time
	IsOnline            bool
	Status              int16
	Version             string
	UptimeSeconds       uint64
	ActiveFlows         uint64
	BytesUp             uint64
	BytesDown           uint64
}

type KernelReconciliationRepository interface {
	BeginReconciliation(context.Context, KernelDetectionRequest, time.Time) (KernelOperation, error)
	SetKernelPhase(context.Context, KernelOperation, string) (KernelOperation, error)
	RecordKernelProbe(context.Context, KernelOperation, KernelProbe, time.Time) error
	RecordKernelRelease(context.Context, KernelOperation, KernelRelease) (KernelOperation, error)
	RecordKernelAction(context.Context, KernelOperation, string) (KernelOperation, error)
	EnsureKernelTrafficCredential(context.Context, KernelOperation, KernelEncryptedCredential) error
	ActivateKernelConnectorCredential(context.Context, KernelOperation, KernelEncryptedCredential) error
	RestoreKernelConnectorCredential(context.Context, KernelOperation, KernelConnectorSnapshot) error
	BlockKernelDowngrade(context.Context, KernelOperation, KernelProbe, KernelRelease, string, string, time.Time) error
	CompleteKernelReconciliation(context.Context, KernelOperation, KernelProbe, KernelRelease, string, string, string, bool, bool, string, time.Time) (KernelReconciliationResult, error)
	FailKernelReconciliation(context.Context, KernelOperation, string, error, bool, time.Time) error
}

type KernelReconciliationState struct {
	Repository KernelReconciliationRepository
	Now        func() time.Time
}

func (s KernelReconciliationState) Begin(ctx context.Context, request KernelDetectionRequest) (KernelOperation, error) {
	if s.Repository == nil {
		return KernelOperation{}, ErrKernelReconciliationUnavailable
	}
	if request.NodeID == 0 || request.ActorID == 0 {
		return KernelOperation{}, ErrKernelDetectionInvalid
	}
	return s.Repository.BeginReconciliation(ctx, request, s.now())
}

func (s KernelReconciliationState) SetPhase(ctx context.Context, operation KernelOperation, phase string) (KernelOperation, error) {
	if s.Repository == nil || operation.ID == 0 || operation.NodeID == 0 || phase == "" {
		return KernelOperation{}, ErrKernelReconciliationUnavailable
	}
	return s.Repository.SetKernelPhase(ctx, operation, phase)
}

func (s KernelReconciliationState) RecordProbe(ctx context.Context, operation KernelOperation, probe KernelProbe) error {
	if s.Repository == nil || operation.ID == 0 || operation.NodeID == 0 {
		return ErrKernelReconciliationUnavailable
	}
	return s.Repository.RecordKernelProbe(ctx, operation, probe, s.now())
}

func (s KernelReconciliationState) RecordRelease(ctx context.Context, operation KernelOperation, release KernelRelease) (KernelOperation, error) {
	if s.Repository == nil || operation.ID == 0 || release.Version == "" {
		return KernelOperation{}, ErrKernelReconciliationUnavailable
	}
	return s.Repository.RecordKernelRelease(ctx, operation, release)
}

func (s KernelReconciliationState) RecordAction(ctx context.Context, operation KernelOperation, action string) (KernelOperation, error) {
	if s.Repository == nil || operation.ID == 0 || action == "" {
		return KernelOperation{}, ErrKernelReconciliationUnavailable
	}
	return s.Repository.RecordKernelAction(ctx, operation, action)
}

func (s KernelReconciliationState) EnsureTrafficCredential(ctx context.Context, operation KernelOperation, credential KernelEncryptedCredential) error {
	if s.Repository == nil || operation.ID == 0 || credential.Ciphertext == "" || credential.Prefix == "" {
		return ErrKernelReconciliationUnavailable
	}
	return s.Repository.EnsureKernelTrafficCredential(ctx, operation, credential)
}

func (s KernelReconciliationState) ActivateConnectorCredential(ctx context.Context, operation KernelOperation, credential KernelEncryptedCredential) error {
	if s.Repository == nil || operation.ID == 0 || credential.Ciphertext == "" || credential.Prefix == "" {
		return ErrKernelReconciliationUnavailable
	}
	return s.Repository.ActivateKernelConnectorCredential(ctx, operation, credential)
}

func (s KernelReconciliationState) RestoreConnectorCredential(ctx context.Context, operation KernelOperation, snapshot KernelConnectorSnapshot) error {
	if s.Repository == nil || operation.ID == 0 {
		return ErrKernelReconciliationUnavailable
	}
	return s.Repository.RestoreKernelConnectorCredential(ctx, operation, snapshot)
}

func (s KernelReconciliationState) BlockDowngrade(ctx context.Context, operation KernelOperation, probe KernelProbe, release KernelRelease, configSHA string, cause error) error {
	if s.Repository == nil || cause == nil {
		return ErrKernelReconciliationUnavailable
	}
	if err := s.Repository.BlockKernelDowngrade(ctx, operation, probe, release, configSHA, TruncateKernelError(cause.Error()), s.now()); err != nil {
		return errors.Join(cause, ErrKernelDetectionCommit, err)
	}
	return errors.Join(cause, ErrKernelOperationFinalized)
}

func (s KernelReconciliationState) Complete(ctx context.Context, operation KernelOperation, probe KernelProbe, release KernelRelease, binarySHA, configSHA, summary string, changed, connectorVerified bool, connectorWarning string) (KernelReconciliationResult, error) {
	if s.Repository == nil || operation.ID == 0 || release.Version == "" {
		return KernelReconciliationResult{}, ErrKernelReconciliationUnavailable
	}
	return s.Repository.CompleteKernelReconciliation(ctx, operation, probe, release, binarySHA, configSHA, summary, changed, connectorVerified, connectorWarning, s.now())
}

func (s KernelReconciliationState) Fail(ctx context.Context, operation KernelOperation, failure error) error {
	if s.Repository == nil || operation.ID == 0 || failure == nil {
		return ErrKernelReconciliationUnavailable
	}
	return s.Repository.FailKernelReconciliation(ctx, operation, operation.Phase, failure, errors.Is(failure, ErrKernelPlatformUnsupported), s.now())
}

func (s KernelReconciliationState) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
