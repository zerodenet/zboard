package network

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type KernelReconciliationRequest struct {
	NodeID, ActorID uint
	Version         string
	AllowDowngrade  bool
}

// PreparedKernelReconciliation exposes only node-specific probe and release
// resolution. The capability owns reconciliation order, state transitions,
// downgrade policy, activation rollback, and completion semantics.
type PreparedKernelReconciliation interface {
	Probe(context.Context) (KernelProbe, error)
	ResolveRelease(context.Context, KernelProbe, string) (PreparedKernelRelease, error)
}

type PreparedKernelRelease interface {
	Descriptor() KernelRelease
	PrepareTrafficCredential(context.Context) (*KernelEncryptedCredential, error)
	PrepareActivation(context.Context) (PreparedKernelActivation, error)
}

type PreparedKernelActivation interface {
	ConfigSHA256() string
	ConnectorCredential() (KernelEncryptedCredential, bool)
	ConnectorSnapshot() KernelConnectorSnapshot
	Materialize(context.Context) (PreparedKernelMaterialization, error)
	Verify(context.Context, string) (KernelProbe, error)
	WaitConnector(context.Context, time.Time) (time.Time, error)
	InvalidateConnectorCredential()
}

type PreparedKernelMaterialization interface {
	BinarySHA256() string
	Install(context.Context, uint) error
	Rollback(context.Context, uint) error
}

type KernelReconciliationPreparer interface {
	PrepareKernelReconciliation(context.Context, uint) (PreparedKernelReconciliation, error)
}

type KernelReconciliation struct {
	State    KernelReconciliationState
	Preparer KernelReconciliationPreparer
	Timeout  time.Duration
}

func (s KernelReconciliation) Reconcile(ctx context.Context, request KernelReconciliationRequest) (KernelReconciliationResult, error) {
	if request.NodeID == 0 || request.ActorID == 0 || request.Version == "" {
		return KernelReconciliationResult{}, ErrKernelReconciliationUnavailable
	}
	if s.Preparer == nil {
		return KernelReconciliationResult{}, ErrKernelReconciliationUnavailable
	}
	prepared, err := s.Preparer.PrepareKernelReconciliation(ctx, request.NodeID)
	if err != nil {
		return KernelReconciliationResult{}, err
	}
	if prepared == nil {
		return KernelReconciliationResult{}, ErrKernelReconciliationUnavailable
	}
	operation, err := s.State.Begin(ctx, KernelDetectionRequest{NodeID: request.NodeID, ActorID: request.ActorID})
	if err != nil {
		return KernelReconciliationResult{}, err
	}
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	executionCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	result, err := s.execute(executionCtx, &operation, prepared, request)
	if err == nil || errors.Is(err, ErrKernelOperationFinalized) {
		return result, err
	}
	finalizeCtx, release := context.WithTimeout(context.WithoutCancel(executionCtx), KernelDetectionFinalizeTimeout)
	defer release()
	if finalizeErr := s.State.Fail(finalizeCtx, operation, err); finalizeErr != nil {
		err = errors.Join(err, ErrKernelDetectionCommit, finalizeErr)
	}
	return result, err
}

func (s KernelReconciliation) execute(ctx context.Context, operation *KernelOperation, prepared PreparedKernelReconciliation, request KernelReconciliationRequest) (KernelReconciliationResult, error) {
	setPhase := func(phase string) error {
		next, err := s.State.SetPhase(ctx, *operation, phase)
		if err == nil {
			*operation = next
		}
		return err
	}
	if err := setPhase("detecting"); err != nil {
		return KernelReconciliationResult{}, err
	}
	probe, err := prepared.Probe(ctx)
	if err != nil {
		return KernelReconciliationResult{}, err
	}
	if err := s.State.RecordProbe(ctx, *operation, probe); err != nil {
		return KernelReconciliationResult{}, err
	}
	if probe.Architecture != "x86_64" || !probe.Systemd {
		return KernelReconciliationResult{}, fmt.Errorf("%w: automatic installation requires Linux x86_64 with systemd (os=%s arch=%s systemd=%t)", ErrKernelPlatformUnsupported, probe.OperatingSystem, probe.Architecture, probe.Systemd)
	}
	if err := setPhase("resolving_release"); err != nil {
		return KernelReconciliationResult{}, err
	}
	preparedRelease, err := prepared.ResolveRelease(ctx, probe, request.Version)
	if err != nil {
		return KernelReconciliationResult{}, fmt.Errorf("resolve Zero release: %w", err)
	}
	if preparedRelease == nil {
		return KernelReconciliationResult{}, ErrKernelReconciliationUnavailable
	}
	release := preparedRelease.Descriptor()
	if release.Version == "" {
		return KernelReconciliationResult{}, ErrKernelReconciliationUnavailable
	}
	if next, recordErr := s.State.RecordRelease(ctx, *operation, release); recordErr != nil {
		return KernelReconciliationResult{}, recordErr
	} else {
		*operation = next
	}

	if err := setPhase("preparing_connector_credential"); err != nil {
		return KernelReconciliationResult{}, err
	}
	trafficCredential, err := preparedRelease.PrepareTrafficCredential(ctx)
	if err != nil {
		return KernelReconciliationResult{}, err
	}
	if trafficCredential != nil {
		if err := s.State.EnsureTrafficCredential(ctx, *operation, *trafficCredential); err != nil {
			return KernelReconciliationResult{}, fmt.Errorf("persist traffic report credential: %w", err)
		}
	}
	activation, err := preparedRelease.PrepareActivation(ctx)
	if err != nil {
		return KernelReconciliationResult{}, err
	}
	if activation == nil || activation.ConfigSHA256() == "" {
		return KernelReconciliationResult{}, ErrKernelReconciliationUnavailable
	}
	configSHA := activation.ConfigSHA256()

	if CompareKernelVersions(probe.Version, release.Version) > 0 && !request.AllowDowngrade {
		cause := fmt.Errorf("installed Zero %s is newer than selected release %s; set allow_downgrade only after explicit operator confirmation", probe.Version, release.Version)
		return KernelReconciliationResult{}, s.State.BlockDowngrade(ctx, *operation, probe, release, configSHA, cause)
	}

	if err := setPhase("downloading"); err != nil {
		return KernelReconciliationResult{}, err
	}
	materialization, err := activation.Materialize(ctx)
	if err != nil {
		return KernelReconciliationResult{}, err
	}
	if materialization == nil || materialization.BinarySHA256() == "" {
		return KernelReconciliationResult{}, ErrKernelReconciliationUnavailable
	}
	binarySHA := materialization.BinarySHA256()
	action := ClassifyKernelAction(probe, release.Version, binarySHA, configSHA)
	if action == "manual_review" && request.AllowDowngrade {
		action = "downgrade"
	}
	if next, recordErr := s.State.RecordAction(ctx, *operation, action); recordErr != nil {
		return KernelReconciliationResult{}, recordErr
	} else {
		*operation = next
	}
	if action == "none" {
		finalizeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), KernelDetectionFinalizeTimeout)
		defer cancel()
		return s.State.Complete(finalizeCtx, *operation, probe, release, binarySHA, configSHA, "Zero is already at the desired binary and configuration", false, false, "")
	}

	if err := setPhase("staging"); err != nil {
		return KernelReconciliationResult{}, err
	}
	credentialActivated := false
	if credential, ok := activation.ConnectorCredential(); ok {
		if err := s.State.ActivateConnectorCredential(ctx, *operation, credential); err != nil {
			return KernelReconciliationResult{}, fmt.Errorf("prepare generated connector credential before Zero startup: %w", err)
		}
		activation.InvalidateConnectorCredential()
		credentialActivated = true
	}
	restoreCredential := func() error {
		if !credentialActivated {
			return nil
		}
		restoreCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), KernelDetectionFinalizeTimeout)
		defer cancel()
		err := s.State.RestoreConnectorCredential(restoreCtx, *operation, activation.ConnectorSnapshot())
		if err == nil {
			activation.InvalidateConnectorCredential()
		}
		return err
	}
	activationStartedAt := time.Now().UTC()
	if err := materialization.Install(ctx, operation.ID); err != nil {
		if credentialErr := restoreCredential(); credentialErr != nil {
			return KernelReconciliationResult{}, fmt.Errorf("%w; generated connector credential rollback failed: %v", err, credentialErr)
		}
		if credentialActivated {
			return KernelReconciliationResult{}, fmt.Errorf("%w; the generated connector credential was rolled back because Zero activation did not complete", err)
		}
		return KernelReconciliationResult{}, err
	}
	rollbackAfterActivation := func(cause error) error {
		rollbackErr := materialization.Rollback(context.WithoutCancel(ctx), operation.ID)
		credentialErr := restoreCredential()
		if rollbackErr != nil || credentialErr != nil {
			return fmt.Errorf("%w; automatic rollback incomplete (kernel=%v credential=%v)", cause, rollbackErr, credentialErr)
		}
		if credentialActivated {
			return fmt.Errorf("%w; the activated generation and generated connector credential were rolled back", cause)
		}
		return fmt.Errorf("%w; the activated generation was rolled back", cause)
	}
	if err := setPhase("verifying"); err != nil {
		return KernelReconciliationResult{}, rollbackAfterActivation(err)
	}
	verified, err := activation.Verify(ctx, binarySHA)
	if err != nil {
		return KernelReconciliationResult{}, rollbackAfterActivation(fmt.Errorf("post-install verification failed: %w", err))
	}
	if err := setPhase("waiting_connector_event"); err != nil {
		return KernelReconciliationResult{}, rollbackAfterActivation(err)
	}
	connectorEventAt, connectorEventErr := activation.WaitConnector(ctx, activationStartedAt)
	summary := fmt.Sprintf("Zero %s %s and passed systemd and control-socket health checks", release.Version, action)
	if connectorEventErr == nil {
		summary += fmt.Sprintf("; Connector activity observed at %s", connectorEventAt.Format(time.RFC3339))
	} else {
		summary += fmt.Sprintf("; Connector activity is not yet observable (%s)", TruncateKernelError(connectorEventErr.Error()))
	}
	warning := ""
	if connectorEventErr != nil {
		warning = TruncateKernelError(connectorEventErr.Error())
	}
	finalizeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), KernelDetectionFinalizeTimeout)
	defer cancel()
	result, err := s.State.Complete(finalizeCtx, *operation, verified, release, binarySHA, configSHA, summary, true, connectorEventErr == nil, warning)
	if err != nil {
		return KernelReconciliationResult{}, rollbackAfterActivation(fmt.Errorf("persist successful Zero operation: %w", err))
	}
	return result, nil
}

func ClassifyKernelAction(probe KernelProbe, desiredVersion, desiredBinarySHA, desiredConfigSHA string) string {
	if !probe.Installed {
		return "install"
	}
	switch CompareKernelVersions(probe.Version, desiredVersion) {
	case -1:
		return "upgrade"
	case 1:
		return "manual_review"
	}
	if probe.BinarySHA256 != desiredBinarySHA {
		return "repair"
	}
	if probe.ConfigSHA256 != desiredConfigSHA {
		return "configure"
	}
	if probe.ServiceStatus != "active" || probe.ControlStatus != "healthy" {
		return "repair"
	}
	return "none"
}

func CompareKernelVersions(left, right string) int {
	type parsedVersion struct {
		core       [3]int
		prerelease []string
	}
	parse := func(raw string) (parsedVersion, bool) {
		var result parsedVersion
		versionParts := strings.SplitN(strings.TrimPrefix(strings.TrimSpace(raw), "v"), "-", 2)
		parts := strings.Split(versionParts[0], ".")
		if len(parts) != 3 {
			return result, false
		}
		for index, part := range parts {
			value, err := strconv.Atoi(part)
			if err != nil || value < 0 {
				return result, false
			}
			result.core[index] = value
		}
		if len(versionParts) == 2 {
			result.prerelease = strings.Split(versionParts[1], ".")
		}
		return result, true
	}
	l, lok := parse(left)
	r, rok := parse(right)
	if !lok || !rok {
		return 0
	}
	for index := range l.core {
		if l.core[index] < r.core[index] {
			return -1
		}
		if l.core[index] > r.core[index] {
			return 1
		}
	}
	if len(l.prerelease) == 0 && len(r.prerelease) == 0 {
		return 0
	}
	if len(l.prerelease) == 0 {
		return 1
	}
	if len(r.prerelease) == 0 {
		return -1
	}
	for index := 0; index < len(l.prerelease) && index < len(r.prerelease); index++ {
		if l.prerelease[index] == r.prerelease[index] {
			continue
		}
		leftNumber, leftErr := strconv.Atoi(l.prerelease[index])
		rightNumber, rightErr := strconv.Atoi(r.prerelease[index])
		switch {
		case leftErr == nil && rightErr == nil:
			if leftNumber < rightNumber {
				return -1
			}
			return 1
		case leftErr == nil:
			return -1
		case rightErr == nil:
			return 1
		case l.prerelease[index] < r.prerelease[index]:
			return -1
		default:
			return 1
		}
	}
	if len(l.prerelease) < len(r.prerelease) {
		return -1
	}
	if len(l.prerelease) > len(r.prerelease) {
		return 1
	}
	return 0
}
