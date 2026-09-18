package handler

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	zeroadapter "github.com/zerodenet/zboard/backend/internal/adapters/zero"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
)

const (
	nodeConfigPublishTimeout     = 2 * time.Minute
	nodeConfigPublishWorkerCount = 4
)

type contextMutex struct {
	token chan struct{}
}

func newContextMutex() *contextMutex {
	lock := &contextMutex{token: make(chan struct{}, 1)}
	lock.token <- struct{}{}
	return lock
}

func (m *contextMutex) Lock(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-m.token:
		return nil
	}
}

func (m *contextMutex) Unlock() {
	m.token <- struct{}{}
}

func (h *handlers) nodePublishLock(nodeID uint) *contextMutex {
	value, _ := h.nodePublishLocks.LoadOrStore(nodeID, newContextMutex())
	return value.(*contextMutex)
}

// Legacy request handlers still call this hook while their source is phased out.
// Credential expiry is exclusively reconciled by StartCredentialExpiryWorker.
func (h *handlers) reconcileExpiredCredentials(time.Time) {}

func (h *handlers) publishNodeConfig(ctx context.Context, endpointID, requestedBy uint) (model.ProtocolDeployment, time.Duration, error) {
	record, err := h.services.NetworkInventory.Endpoint(ctx, endpointID)
	if err != nil {
		return model.ProtocolDeployment{}, 0, err
	}
	endpoint := protocolEndpointRecordModel(record)
	return h.publishNodeConfigForNode(ctx, endpoint.NodeID, endpoint.ID, requestedBy)
}

func (h *handlers) publishNodeConfigForNode(ctx context.Context, nodeID, triggerEndpointID, requestedBy uint) (model.ProtocolDeployment, time.Duration, error) {
	started := time.Now()
	lock := h.nodePublishLock(nodeID)
	if err := lock.Lock(ctx); err != nil {
		return model.ProtocolDeployment{}, time.Since(started), fmt.Errorf("wait for node config publication lock: %w", err)
	}
	defer lock.Unlock()

	return h.publishNodeConfigForNodeLocked(ctx, nodeID, triggerEndpointID, requestedBy, false, started)
}

func (h *handlers) publishNodeConfigForNodeLocked(ctx context.Context, nodeID, triggerEndpointID, requestedBy uint, suppressMieruFallback bool, started time.Time) (model.ProtocolDeployment, time.Duration, error) {
	node, err := h.loadNode(nodeID)
	if err != nil {
		return model.ProtocolDeployment{}, time.Since(started), err
	}
	if node.LifecycleStatus == resourceStatusDeleting {
		return model.ProtocolDeployment{}, time.Since(started), errResourceDeleting
	}
	state := h.services.ConfigurationPublicationState()
	publication, err := state.Begin(ctx, network.ConfigurationPublicationRequest{NodeID: node.ID, TriggerEndpointID: triggerEndpointID, RequestedBy: requestedBy})
	if err != nil {
		return model.ProtocolDeployment{}, time.Since(started), err
	}
	deploymentState := publication.Deployment
	deployment := configurationDeploymentModel(deploymentState)
	mieruFallbackCount := publication.MieruFallbackCount
	credentialActivated, remoteActivated := false, false
	restoreCredential := func() error { return nil }
	fail := func(cause error, output string) (model.ProtocolDeployment, time.Duration, error) {
		if credentialActivated && !remoteActivated {
			if restoreErr := restoreCredential(); restoreErr != nil {
				cause = errors.Join(cause, fmt.Errorf("restore generated connector credential: %w", restoreErr))
			}
		}
		finalizeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), network.KernelDetectionFinalizeTimeout)
		defer cancel()
		failed, failureErr := state.Fail(finalizeCtx, deploymentState, cause, output)
		if failed.ID != 0 {
			deploymentState = failed
			deployment = configurationDeploymentModel(failed)
		}
		return deployment, time.Since(started), failureErr
	}
	if err := h.validateNodeSSH(node); err != nil {
		return fail(err, "")
	}
	probe, err := h.probeNodeKernelContext(ctx, node)
	if err != nil {
		return fail(fmt.Errorf("detect installed Zero before publishing config: %w", err), "")
	}
	if !probe.Installed || strings.TrimSpace(probe.Version) == "" {
		return fail(fmt.Errorf("detect installed Zero before publishing config: Zero is not installed"), "")
	}
	// Readiness commits only after the target accepts and activates this config.
	mieruAccess, managedAccess := true, true
	credential, err := h.nodeConnectorCredential(node)
	if err != nil {
		return fail(err, "")
	}
	if credential.IsNew {
		if err := state.ActivateConnectorCredential(ctx, deploymentState, network.KernelEncryptedCredential{Ciphertext: credential.Encrypted, Prefix: credential.Prefix}); err != nil {
			return fail(err, "")
		}
		h.invalidateZeroEventCredential(node.ID)
		credentialActivated = true
	}
	restoreCredential = func() error {
		if !credential.IsNew {
			return nil
		}
		restoreCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), network.KernelDetectionFinalizeTimeout)
		defer cancel()
		err := state.RestoreConnectorCredential(restoreCtx, deploymentState, kernelConnectorSnapshot(node))
		if err == nil {
			h.invalidateZeroEventCredential(node.ID)
		}
		return err
	}
	if err := h.services.NodeCredentialReconciliation(h.credentialCipher, true).ReconcileNode(ctx, node.ID); err != nil {
		return fail(fmt.Errorf("reconcile node subscription credentials: %w", err), "")
	}
	runtimeConfig, configSHA, err := h.compileNodeRuntimeConfigWithOptionsContext(ctx, node, credential.Raw, probe.Version, suppressMieruFallback)
	if err != nil {
		return fail(err, "")
	}
	deploymentState, err = state.SetDesired(ctx, deploymentState, configSHA)
	if err != nil {
		return fail(err, "")
	}
	deployment = configurationDeploymentModel(deploymentState)

	remote, err := (zeroadapter.ConfigurationPublisher{Dialer: zeroKernelRemoteDialer{h: h, node: node}}).Publish(ctx, zeroadapter.ConfigurationPublishRequest{
		DeploymentID: deployment.ID, RuntimeConfig: runtimeConfig, ConfigSHA256: configSHA,
		ConnectorKey: credential.Raw,
	})
	if err != nil {
		return fail(err, remote.Output)
	}
	remoteActivated = true
	output := remote.Output
	connectorEventAt, connectorErr := h.services.ConnectorActivityObserver().Wait(ctx, node.ID, remote.ActivatedAt)
	finished := time.Now().UTC()
	output, lastHealthyAt := finalizeConnectorConfirmation(output, connectorEventAt, connectorErr, finished)
	finalizeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), network.KernelDetectionFinalizeTimeout)
	defer cancel()
	deploymentState, err = state.Complete(finalizeCtx, deploymentState, network.ConfigurationPublicationCompletion{
		ConfigSHA: configSHA, Output: output, LastHealthyAt: lastHealthyAt,
		MieruAccess: mieruAccess, ManagedPrincipalAccess: managedAccess,
		MieruFallbackCount: mieruFallbackCount, SuppressMieruFallback: suppressMieruFallback,
	})
	if err != nil {
		return fail(err, output)
	}
	deployment = configurationDeploymentModel(deploymentState)
	if mieruAccess && mieruFallbackCount > 0 && !suppressMieruFallback {
		return h.publishNodeConfigForNodeLocked(ctx, node.ID, triggerEndpointID, requestedBy, true, started)
	}
	return deployment, time.Since(started), nil
}

func finalizeConnectorConfirmation(output string, connectorEventAt time.Time, connectorErr error, fallback time.Time) (string, time.Time) {
	output = strings.TrimSpace(output)
	if connectorErr == nil {
		return output, connectorEventAt
	}
	message := strings.ReplaceAll(strings.TrimSpace(connectorErr.Error()), "\n", " ")
	warning := "ZBOARD_CONNECTOR_CONFIRMATION_PENDING=" + message
	if output == "" {
		return warning, fallback
	}
	return output + "\n" + warning, fallback
}

func mieruReadinessCanCommit(enabled bool, fallbackCount int64, suppressFallback bool) bool {
	return network.ConfigurationMieruReadinessCanCommit(enabled, fallbackCount, suppressFallback)
}

func configurationDeploymentModel(value network.ConfigurationDeployment) model.ProtocolDeployment {
	return model.ProtocolDeployment{
		ID: value.ID, ProtocolEndpointID: value.ProtocolEndpointID, NodeID: value.NodeID, ConfigRevision: value.ConfigRevision,
		DesiredConfigSHA256: value.DesiredConfigSHA256, AppliedConfigSHA256: value.AppliedConfigSHA256,
		Status: value.Status, RequestedBy: value.RequestedBy, Output: value.Output, Error: value.Error,
		StartedAt: value.StartedAt, FinishedAt: value.FinishedAt,
	}
}
