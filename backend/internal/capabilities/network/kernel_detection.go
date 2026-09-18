package network

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const KernelDetectionFinalizeTimeout = 5 * time.Second

var (
	ErrKernelDetectionInvalid     = errors.New("kernel detection request is invalid")
	ErrKernelDetectionUnavailable = errors.New("kernel detection capability unavailable")
	ErrKernelDetectionCommit      = errors.New("kernel detection result was not committed")
	ErrKernelOperationRunning     = errors.New("another kernel operation is already running for this node")
	ErrKernelOperationLost        = errors.New("kernel operation ownership was lost")
	ErrKernelPermission           = errors.New("kernel operation permission denied")
	ErrKernelResourceDeleting     = errors.New("资源已进入删除流程，请等待删除完成或重试删除")
	ErrKernelPlatformUnsupported  = errors.New("the official Zero artifact is incompatible with this platform")
)

type KernelDetectionRequest struct {
	NodeID, ActorID uint
}

type KernelProbe struct {
	OperatingSystem, Architecture, Libc string
	Systemd, Installed                  bool
	Version, BinarySHA256, ConfigSHA256 string
	ServiceStatus, ControlStatus        string
}

type KernelAssessment struct {
	Status, RecommendedAction string
}

type KernelOperation struct {
	ID             uint       `json:"id"`
	NodeID         uint       `json:"node_id"`
	OperationType  string     `json:"operation_type"`
	Status         string     `json:"status"`
	Phase          string     `json:"phase"`
	RequestedBy    uint       `json:"requested_by"`
	DesiredVersion string     `json:"desired_version"`
	DesiredSHA256  string     `json:"desired_sha256"`
	ArtifactURL    string     `json:"artifact_url"`
	ResultSummary  string     `json:"result_summary"`
	Error          string     `json:"error"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type KernelState struct {
	NodeID              uint       `json:"node_id"`
	Status              string     `json:"status"`
	Phase               string     `json:"phase"`
	RecommendedAction   string     `json:"recommended_action"`
	PlatformOS          string     `json:"platform_os"`
	Architecture        string     `json:"architecture"`
	Libc                string     `json:"libc"`
	DesiredVersion      string     `json:"desired_version"`
	InstalledVersion    string     `json:"installed_version"`
	DesiredSHA256       string     `json:"desired_sha256"`
	InstalledSHA256     string     `json:"installed_sha256"`
	DesiredConfigSHA256 string     `json:"desired_config_sha256"`
	AppliedConfigSHA256 string     `json:"applied_config_sha256"`
	ServiceStatus       string     `json:"service_status"`
	ControlStatus       string     `json:"control_status"`
	LastError           string     `json:"last_error"`
	ActiveOperationID   *uint      `json:"active_operation_id,omitempty"`
	LastDetectedAt      *time.Time `json:"last_detected_at,omitempty"`
	LastHealthyAt       *time.Time `json:"last_healthy_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type KernelDetectionResult struct {
	State     KernelState     `json:"state"`
	Operation KernelOperation `json:"operation"`
}

type KernelDetectionRepository interface {
	BeginDetection(context.Context, KernelDetectionRequest, time.Time) (KernelOperation, error)
	CompleteDetection(context.Context, KernelOperation, KernelProbe, KernelAssessment, string, time.Time) (KernelDetectionResult, error)
	FailDetection(context.Context, KernelOperation, string, error, bool, time.Time) error
}

type KernelProbeExecutor interface {
	ValidateKernelProbeTarget(context.Context, uint) error
	ProbeKernel(context.Context, uint) (KernelProbe, error)
}

type KernelDetection struct {
	Repository        KernelDetectionRepository
	Executor          KernelProbeExecutor
	ArtifactAvailable bool
	Now               func() time.Time
}

func (s KernelDetection) Detect(ctx context.Context, request KernelDetectionRequest) (KernelDetectionResult, error) {
	if request.NodeID == 0 || request.ActorID == 0 {
		return KernelDetectionResult{}, ErrKernelDetectionInvalid
	}
	if s.Repository == nil || s.Executor == nil {
		return KernelDetectionResult{}, ErrKernelDetectionUnavailable
	}
	if err := s.Executor.ValidateKernelProbeTarget(ctx, request.NodeID); err != nil {
		return KernelDetectionResult{}, err
	}
	operation, err := s.Repository.BeginDetection(ctx, request, s.now())
	if err != nil {
		return KernelDetectionResult{}, err
	}
	probe, probeErr := s.Executor.ProbeKernel(ctx, request.NodeID)
	if probeErr != nil {
		finalizeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), KernelDetectionFinalizeTimeout)
		defer cancel()
		finalizeErr := s.Repository.FailDetection(finalizeCtx, operation, "detecting", probeErr, errors.Is(probeErr, ErrKernelPlatformUnsupported), s.now())
		if finalizeErr != nil {
			return KernelDetectionResult{}, errors.Join(probeErr, ErrKernelDetectionCommit, finalizeErr)
		}
		return KernelDetectionResult{}, probeErr
	}
	assessment := AssessKernelProbe(probe, s.ArtifactAvailable)
	finalizeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), KernelDetectionFinalizeTimeout)
	defer cancel()
	result, err := s.Repository.CompleteDetection(finalizeCtx, operation, probe, assessment, kernelProbeSummary(probe), s.now())
	if err != nil {
		return KernelDetectionResult{}, errors.Join(ErrKernelDetectionCommit, err)
	}
	return result, nil
}

func (s KernelDetection) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func AssessKernelProbe(probe KernelProbe, artifactAvailable bool) KernelAssessment {
	if !probe.Installed {
		if probe.Architecture != "x86_64" || !probe.Systemd || !artifactAvailable {
			return KernelAssessment{Status: "unsupported", RecommendedAction: "manual_review"}
		}
		return KernelAssessment{Status: "not_installed", RecommendedAction: "install"}
	}
	if probe.ServiceStatus == "active" && probe.ControlStatus == "healthy" {
		return KernelAssessment{Status: "healthy", RecommendedAction: "check_release"}
	}
	return KernelAssessment{Status: "degraded", RecommendedAction: "repair"}
}

func kernelProbeSummary(probe KernelProbe) string {
	return fmt.Sprintf("os=%s arch=%s libc=%s installed=%t version=%s service=%s control=%s", probe.OperatingSystem, probe.Architecture, probe.Libc, probe.Installed, probe.Version, probe.ServiceStatus, probe.ControlStatus)
}

func TruncateKernelError(value string) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) > 2000 {
		return string(runes[:2000]) + "…"
	}
	return value
}
