package network

import (
	"context"
	"errors"
	"time"
)

var (
	ErrCertificatePermission      = errors.New("certificate administration requires current administrator")
	ErrCertificateNotFound        = errors.New("certificate not found")
	ErrCertificateConflict        = errors.New("certificate revision conflict")
	ErrCertificateOperationActive = errors.New("certificate operation is already running")
	ErrCertificateDeleting        = errors.New("certificate is deleting")
	ErrCertificateDependency      = errors.New("certificate dependency is unavailable")
)

type CertificateRecord struct {
	ID                   uint
	NodeID               uint
	ProviderAccountID    *uint
	Name                 string
	Domains              string
	ContactEmail         string
	Environment          string
	ChallengeType        string
	WebrootPath          string
	Status               string
	CertPath             string
	KeyPath              string
	SerialNumber         string
	FingerprintSHA256    string
	NotBefore            *time.Time
	NotAfter             *time.Time
	LastIssuedAt         *time.Time
	LastRenewalAttemptAt *time.Time
	NextRenewalAt        *time.Time
	AutoRenew            bool
	RenewBeforeDays      int
	LastError            string
	Revision             uint64
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type CertificateOperationRecord struct {
	ID                   uint
	ManagedCertificateID uint
	NodeID               uint
	OperationType        string
	Status               string
	Phase                string
	RequestedBy          *uint
	ResultSummary        string
	Error                string
	StartedAt            *time.Time
	FinishedAt           *time.Time
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type CertificateUpdate struct {
	Name, ContactEmail, WebrootPath string
	AutoRenew                       bool
	RenewBeforeDays                 int
	ExpectedRevision                uint64
}

type CertificateIssued struct {
	CertPath, KeyPath, SerialNumber, FingerprintSHA256 string
	NotBefore, NotAfter, IssuedAt, NextRenewalAt       time.Time
}

type CertificateFailure struct {
	Phase, Message string
	Usable         bool
	At, NextRetry  time.Time
}

// CertificateExecution is the persistence-owned snapshot required by the
// remote ACME adapter. Ciphertexts stay opaque until the execution adapter
// consumes them; HTTP handlers never reconstruct this snapshot with ORM calls.
type CertificateExecution struct {
	Operation                 CertificateOperationRecord
	Certificate               CertificateRecord
	Node                      CertificateExecutionNode
	Provider                  *CertificateExecutionProvider
	BindingProtocolEndpointID uint
}

type CertificateExecutionNode struct {
	ID                                uint
	SSHHost                           string
	SSHPort                           int
	SSHUser                           string
	SSHAuthMethod                     string
	SSHPwdCiphertext                  string
	SSHPrivateKeyPassphraseCiphertext string
	SSHPrivilegeMode                  string
	SSHPrivilegePasswordCiphertext    string
	SSHHostKeyFingerprint             string
}

type CertificateExecutionProvider struct {
	ProviderKey          string
	Status               string
	Capabilities         []string
	CredentialCiphertext string
}

type CertificateLifecycleRepository interface {
	CreateCertificate(context.Context, uint, CertificateRecord) (CertificateRecord, string, error)
	UpdateCertificate(context.Context, uint, uint, CertificateUpdate) error
	UpdateCertificateRenewal(context.Context, uint, uint, bool, int, uint64) error
	StartCertificateOperation(context.Context, uint, uint, string) (CertificateOperationRecord, error)
	SetCertificateOperationPhase(context.Context, uint, string) error
	RecordCertificateIssued(context.Context, uint, uint, CertificateIssued) error
	CompleteCertificateOperation(context.Context, uint, string, time.Time) error
	FailCertificateOperation(context.Context, uint, uint, CertificateFailure) error
	DueCertificateRenewals(context.Context, time.Time, int) ([]uint, error)
	PrepareCertificateOperation(context.Context, uint) (CertificateExecution, error)
}

type CertificateLifecycle struct {
	Repository CertificateLifecycleRepository
}

func (s CertificateLifecycle) Create(ctx context.Context, actor uint, record CertificateRecord) (CertificateRecord, string, error) {
	if actor == 0 {
		return CertificateRecord{}, "", ErrCertificatePermission
	}
	return s.Repository.CreateCertificate(ctx, actor, record)
}

func (s CertificateLifecycle) Update(ctx context.Context, actor, id uint, change CertificateUpdate) error {
	if actor == 0 {
		return ErrCertificatePermission
	}
	return s.Repository.UpdateCertificate(ctx, actor, id, change)
}

func (s CertificateLifecycle) UpdateRenewal(ctx context.Context, actor, id uint, auto bool, days int, expected uint64) error {
	if actor == 0 {
		return ErrCertificatePermission
	}
	return s.Repository.UpdateCertificateRenewal(ctx, actor, id, auto, days, expected)
}

func (s CertificateLifecycle) Start(ctx context.Context, actor, id uint, operation string) (CertificateOperationRecord, error) {
	return s.Repository.StartCertificateOperation(ctx, actor, id, operation)
}

func (s CertificateLifecycle) Phase(ctx context.Context, operationID uint, phase string) error {
	return s.Repository.SetCertificateOperationPhase(ctx, operationID, phase)
}

func (s CertificateLifecycle) Issued(ctx context.Context, operationID, certificateID uint, issued CertificateIssued) error {
	return s.Repository.RecordCertificateIssued(ctx, operationID, certificateID, issued)
}

func (s CertificateLifecycle) Complete(ctx context.Context, operationID uint, summary string, at time.Time) error {
	return s.Repository.CompleteCertificateOperation(ctx, operationID, summary, at)
}

func (s CertificateLifecycle) Fail(ctx context.Context, operationID, certificateID uint, failure CertificateFailure) error {
	return s.Repository.FailCertificateOperation(context.WithoutCancel(ctx), operationID, certificateID, failure)
}

func (s CertificateLifecycle) DueRenewals(ctx context.Context, now time.Time, limit int) ([]uint, error) {
	return s.Repository.DueCertificateRenewals(ctx, now, limit)
}

func (s CertificateLifecycle) Prepare(ctx context.Context, operationID uint) (CertificateExecution, error) {
	if operationID == 0 {
		return CertificateExecution{}, ErrCertificateNotFound
	}
	return s.Repository.PrepareCertificateOperation(ctx, operationID)
}
