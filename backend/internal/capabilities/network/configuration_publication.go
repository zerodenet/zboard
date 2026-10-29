package network

import (
	"context"
	"errors"
	"strings"
	"time"
)

var (
	ErrConfigurationPublicationUnavailable = errors.New("configuration publication state capability unavailable")
	ErrConfigurationDeploymentLost         = errors.New("configuration deployment ownership lost")
	ErrConfigurationPublicationCommit      = errors.New("configuration publication state commit failed")
	ErrConfigurationCapabilityUnavailable  = errors.New("当前内核不支持本次设置，现有运行配置保持不变")
)

type ConfigurationPublicationRequest struct {
	NodeID uint
	// TriggerEndpointID is optional for fronting-only nodes and publications
	// that remove the last runtime listener from a node.
	TriggerEndpointID uint
	RequestedBy       uint
	KernelOperationID uint
}

type ConfigurationDeployment struct {
	ID                  uint       `json:"id"`
	ProtocolEndpointID  uint       `json:"protocol_endpoint_id"`
	NodeID              uint       `json:"node_id"`
	ConfigRevision      uint64     `json:"config_revision"`
	DesiredConfigSHA256 string     `json:"desired_config_sha256"`
	AppliedConfigSHA256 string     `json:"applied_config_sha256"`
	Status              string     `json:"status"`
	RequestedBy         *uint      `json:"requested_by,omitempty"`
	Output              string     `json:"output"`
	Error               string     `json:"error"`
	StartedAt           *time.Time `json:"started_at,omitempty"`
	FinishedAt          *time.Time `json:"finished_at,omitempty"`
}

type ConfigurationPublicationStart struct {
	Deployment         ConfigurationDeployment
	MieruFallbackCount int64
}

type ConfigurationPublicationCompletion struct {
	ConfigSHA              string
	Output                 string
	LastHealthyAt          time.Time
	MieruAccess            bool
	ManagedPrincipalAccess bool
	MieruFallbackCount     int64
	SuppressMieruFallback  bool
}

type ConfigurationPublicationRepository interface {
	BeginConfigurationPublication(context.Context, ConfigurationPublicationRequest, time.Time) (ConfigurationPublicationStart, error)
	UncertainConfigurationPublication(context.Context, ConfigurationDeployment, string, string, time.Time) (ConfigurationDeployment, error)
	ResolveConfigurationPublication(context.Context, ConfigurationDeployment, bool, string, time.Time) error
	SetConfigurationPublicationDesired(context.Context, ConfigurationDeployment, string, ...string) (ConfigurationDeployment, error)
	ActivateConfigurationConnectorCredential(context.Context, ConfigurationDeployment, KernelEncryptedCredential) error
	RestoreConfigurationConnectorCredential(context.Context, ConfigurationDeployment, KernelConnectorSnapshot) error
	CompleteConfigurationPublication(context.Context, ConfigurationDeployment, ConfigurationPublicationCompletion, time.Time) (ConfigurationDeployment, error)
	FailConfigurationPublication(context.Context, ConfigurationDeployment, string, string, time.Time) (ConfigurationDeployment, error)
	DeferConfigurationPublication(context.Context, ConfigurationDeployment, string, string, time.Time) (ConfigurationDeployment, error)
}

type ConfigurationPublicationState struct {
	Repository ConfigurationPublicationRepository
	Now        func() time.Time
}

func (s ConfigurationPublicationState) Begin(ctx context.Context, request ConfigurationPublicationRequest) (ConfigurationPublicationStart, error) {
	if s.Repository == nil || request.NodeID == 0 {
		return ConfigurationPublicationStart{}, ErrConfigurationPublicationUnavailable
	}
	return s.Repository.BeginConfigurationPublication(ctx, request, s.now())
}

func (s ConfigurationPublicationState) SetDesired(ctx context.Context, deployment ConfigurationDeployment, configSHA string, targets ...[]CredentialDeliveryTarget) (ConfigurationDeployment, error) {
	if s.Repository == nil || deployment.ID == 0 || strings.TrimSpace(configSHA) == "" {
		return ConfigurationDeployment{}, ErrConfigurationPublicationUnavailable
	}
	if len(targets) > 1 {
		return ConfigurationDeployment{}, ErrCredentialDeliveryManifest
	}
	if len(targets) == 1 {
		manifest, err := EncodeCredentialDeliveryManifest(targets[0])
		if err != nil {
			return ConfigurationDeployment{}, err
		}
		return s.Repository.SetConfigurationPublicationDesired(ctx, deployment, strings.TrimSpace(configSHA), manifest)
	}
	return s.Repository.SetConfigurationPublicationDesired(ctx, deployment, strings.TrimSpace(configSHA))
}

func (s ConfigurationPublicationState) SetDesiredWithTargets(ctx context.Context, deployment ConfigurationDeployment, configSHA string, targets ConfigurationDeliveryTargets) (ConfigurationDeployment, error) {
	if s.Repository == nil || deployment.ID == 0 || strings.TrimSpace(configSHA) == "" {
		return ConfigurationDeployment{}, ErrConfigurationPublicationUnavailable
	}
	manifest, err := EncodeCredentialDeliveryManifest(targets.Credentials, targets.NetworkEntries)
	if err != nil {
		return ConfigurationDeployment{}, err
	}
	return s.Repository.SetConfigurationPublicationDesired(ctx, deployment, strings.TrimSpace(configSHA), manifest)
}

func (s ConfigurationPublicationState) ActivateConnectorCredential(ctx context.Context, deployment ConfigurationDeployment, credential KernelEncryptedCredential) error {
	if s.Repository == nil || deployment.ID == 0 || credential.Ciphertext == "" || credential.Prefix == "" {
		return ErrConfigurationPublicationUnavailable
	}
	return s.Repository.ActivateConfigurationConnectorCredential(ctx, deployment, credential)
}

func (s ConfigurationPublicationState) RestoreConnectorCredential(ctx context.Context, deployment ConfigurationDeployment, snapshot KernelConnectorSnapshot) error {
	if s.Repository == nil || deployment.ID == 0 {
		return ErrConfigurationPublicationUnavailable
	}
	return s.Repository.RestoreConfigurationConnectorCredential(ctx, deployment, snapshot)
}

func (s ConfigurationPublicationState) Complete(ctx context.Context, deployment ConfigurationDeployment, completion ConfigurationPublicationCompletion) (ConfigurationDeployment, error) {
	if s.Repository == nil || deployment.ID == 0 || strings.TrimSpace(completion.ConfigSHA) == "" || completion.LastHealthyAt.IsZero() {
		return ConfigurationDeployment{}, ErrConfigurationPublicationUnavailable
	}
	completion.ConfigSHA = strings.TrimSpace(completion.ConfigSHA)
	completion.Output = strings.TrimSpace(completion.Output)
	return s.Repository.CompleteConfigurationPublication(ctx, deployment, completion, s.now())
}

func (s ConfigurationPublicationState) Fail(ctx context.Context, deployment ConfigurationDeployment, cause error, output string) (ConfigurationDeployment, error) {
	if s.Repository == nil || deployment.ID == 0 || cause == nil {
		return ConfigurationDeployment{}, ErrConfigurationPublicationUnavailable
	}
	failed, err := s.Repository.FailConfigurationPublication(ctx, deployment, TruncateKernelError(cause.Error()), strings.TrimSpace(output), s.now())
	if err != nil {
		return failed, errors.Join(cause, ErrConfigurationPublicationCommit, err)
	}
	return failed, cause
}

func (s ConfigurationPublicationState) Uncertain(ctx context.Context, deployment ConfigurationDeployment, cause error, output string) (ConfigurationDeployment, error) {
	if s.Repository == nil || deployment.ID == 0 || !errors.Is(cause, ErrConfigurationOutcomeUnknown) {
		return ConfigurationDeployment{}, ErrConfigurationPublicationUnavailable
	}
	pending, err := s.Repository.UncertainConfigurationPublication(ctx, deployment, TruncateKernelError(cause.Error()), strings.TrimSpace(output), s.now())
	if err != nil {
		return pending, errors.Join(cause, ErrConfigurationPublicationCommit, err)
	}
	return pending, cause
}

func (s ConfigurationPublicationState) Resolve(ctx context.Context, deployment ConfigurationDeployment, applied bool, output string) error {
	if s.Repository == nil || deployment.ID == 0 || deployment.Status != "reconciling" {
		return ErrConfigurationPublicationUnavailable
	}
	return s.Repository.ResolveConfigurationPublication(ctx, deployment, applied, strings.TrimSpace(output), s.now())
}

func (s ConfigurationPublicationState) Defer(ctx context.Context, deployment ConfigurationDeployment, cause error, output string) (ConfigurationDeployment, error) {
	if s.Repository == nil || deployment.ID == 0 || !errors.Is(cause, ErrConfigurationCapabilityUnavailable) {
		return ConfigurationDeployment{}, ErrConfigurationPublicationUnavailable
	}
	deferred, err := s.Repository.DeferConfigurationPublication(ctx, deployment, TruncateKernelError(cause.Error()), strings.TrimSpace(output), s.now())
	if err != nil {
		return deferred, errors.Join(cause, ErrConfigurationPublicationCommit, err)
	}
	return deferred, cause
}

func IsConfigurationCapabilityDeferral(err error) bool {
	return errors.Is(err, ErrConfigurationCapabilityUnavailable) &&
		!errors.Is(err, ErrConfigurationPublicationCommit) && !errors.Is(err, ErrPublicationLeaseLost) && !errors.Is(err, ErrPublicationFinalize)
}

func (s ConfigurationPublicationState) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func ConfigurationMieruReadinessCanCommit(enabled bool, fallbackCount int64, suppressFallback bool) bool {
	return !enabled || fallbackCount == 0 || suppressFallback
}
