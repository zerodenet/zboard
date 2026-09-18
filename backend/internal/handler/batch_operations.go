package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
)

type nodeBatchFilters = network.BatchNodeFilter
type protocolBatchFilters = network.BatchProtocolFilter

type nodeBatchOperationRequest struct {
	Action         string           `json:"action"`
	NodeIDs        []uint           `json:"node_ids,omitempty"`
	AllMatching    bool             `json:"all_matching,omitempty"`
	Filters        nodeBatchFilters `json:"filters,omitempty"`
	Version        string           `json:"version,omitempty"`
	AllowDowngrade bool             `json:"allow_downgrade,omitempty"`
	IdempotencyKey string           `json:"idempotency_key,omitempty"`
}

type protocolBatchDeployRequest struct {
	ProtocolEndpointIDs []uint               `json:"protocol_endpoint_ids,omitempty"`
	AllMatching         bool                 `json:"all_matching,omitempty"`
	Filters             protocolBatchFilters `json:"filters,omitempty"`
	IdempotencyKey      string               `json:"idempotency_key,omitempty"`
}

type protocolBatchActiveRequest struct {
	ProtocolEndpointIDs []uint               `json:"protocol_endpoint_ids,omitempty"`
	AllMatching         bool                 `json:"all_matching,omitempty"`
	Filters             protocolBatchFilters `json:"filters,omitempty"`
	IdempotencyKey      string               `json:"idempotency_key,omitempty"`
	IsActive            *bool                `json:"is_active"`
}

type operationTaskContent = network.BatchOperationContent

func (h *handlers) NodeBatchOperationHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	var req nodeBatchOperationRequest
	if err := decodeBody(r, &req); err != nil {
		BadRequest(w, err.Error())
		return
	}
	req.Action = strings.ToLower(strings.TrimSpace(req.Action))
	taskType := ""
	content := operationTaskContent{RequestedBy: claims.UserID, Actor: claims.Email}
	switch req.Action {
	case "detect":
		taskType = taskTypeNodeDetect
	case "reconcile":
		taskType = taskTypeNodeReconcile
		content.KernelVersion = strings.TrimSpace(req.Version)
		content.AllowDowngrade = req.AllowDowngrade
		if content.KernelVersion == "" {
			BadRequest(w, "version is required for kernel reconcile")
			return
		}
	case "activate":
		taskType = taskTypeNodeLifecycle
		content.LifecycleStatus = "active"
	case "maintenance":
		taskType = taskTypeNodeLifecycle
		content.LifecycleStatus = "maintenance"
	case "retire":
		taskType = taskTypeNodeLifecycle
		content.LifecycleStatus = "retired"
	default:
		BadRequest(w, "action must be detect, reconcile, activate, maintenance or retire")
		return
	}
	ids, scope, err := h.resolveNodeBatchScope(r.Context(), req)
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	task, err := h.createOperationTask(r.Context(), claims, taskType, scope, content, "node", ids, req.IdempotencyKey)
	if err != nil {
		writeOperationTaskError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, "batch task accepted", task)
}

func (h *handlers) ProtocolBatchDeployHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	var req protocolBatchDeployRequest
	if err := decodeBody(r, &req); err != nil {
		BadRequest(w, err.Error())
		return
	}
	ids, scope, err := h.resolveProtocolBatchScope(r.Context(), req)
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	if err := h.validateProtocolEndpointKernelSupport(r.Context(), ids); err != nil {
		BadRequest(w, err.Error())
		return
	}
	nodeIDs, _, err := h.groupProtocolEndpointsByNode(r.Context(), ids)
	if err != nil {
		writeOperationTaskError(w, err)
		return
	}
	content := operationTaskContent{RequestedBy: claims.UserID, Actor: claims.Email}
	task, err := h.createOperationTask(r.Context(), claims, taskTypeProtocolDeploy, scope, content, "node", nodeIDs, req.IdempotencyKey)
	if err != nil {
		writeOperationTaskError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, "batch task accepted", task)
}

func (h *handlers) ProtocolBatchActiveHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	var req protocolBatchActiveRequest
	if err := decodeBody(r, &req); err != nil {
		BadRequest(w, err.Error())
		return
	}
	if req.IsActive == nil {
		BadRequest(w, "is_active is required")
		return
	}
	ids, scope, err := h.resolveProtocolBatchScope(r.Context(), protocolBatchDeployRequest{
		ProtocolEndpointIDs: req.ProtocolEndpointIDs,
		AllMatching:         req.AllMatching,
		Filters:             req.Filters,
		IdempotencyKey:      req.IdempotencyKey,
	})
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	if *req.IsActive {
		if err := h.validateProtocolEndpointKernelSupport(r.Context(), ids); err != nil {
			BadRequest(w, err.Error())
			return
		}
	}
	nodeIDs, endpointIDsByNode, err := h.groupProtocolEndpointsByNode(r.Context(), ids)
	if err != nil {
		writeOperationTaskError(w, err)
		return
	}
	content := operationTaskContent{RequestedBy: claims.UserID, Actor: claims.Email, IsActive: req.IsActive, EndpointIDsByNode: endpointIDsByNode}
	task, err := h.createOperationTask(r.Context(), claims, taskTypeProtocolActive, scope, content, "node", nodeIDs, req.IdempotencyKey)
	if err != nil {
		writeOperationTaskError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, "batch task accepted", task)
}

func (h *handlers) resolveNodeBatchScope(ctx context.Context, req nodeBatchOperationRequest) ([]uint, interface{}, error) {
	if req.AllMatching && len(req.NodeIDs) > 0 {
		return nil, nil, errors.New("node_ids and all_matching cannot be used together")
	}
	ids, err := h.services.BatchScopes().Nodes(ctx, req.AllMatching, req.NodeIDs, req.Filters, time.Now().UTC().Add(-nodeOnlineWindow), maxTaskTargets)
	if err != nil {
		return nil, nil, err
	}
	if req.AllMatching {
		return ids, map[string]interface{}{"all_matching": true, "filters": req.Filters}, nil
	}
	return ids, map[string]interface{}{"node_ids": ids}, nil
}

func (h *handlers) resolveProtocolBatchScope(ctx context.Context, req protocolBatchDeployRequest) ([]uint, interface{}, error) {
	if req.AllMatching && len(req.ProtocolEndpointIDs) > 0 {
		return nil, nil, errors.New("protocol_endpoint_ids and all_matching cannot be used together")
	}
	if req.Filters.Protocol != "" && !h.isProtocolSupported(strings.ToLower(strings.TrimSpace(req.Filters.Protocol))) {
		return nil, nil, errors.New("invalid protocol")
	}
	ids, err := h.services.BatchScopes().Protocols(ctx, req.AllMatching, req.ProtocolEndpointIDs, req.Filters, maxTaskTargets)
	if err != nil {
		return nil, nil, err
	}
	if req.AllMatching {
		return ids, map[string]interface{}{"all_matching": true, "filters": req.Filters}, nil
	}
	return ids, map[string]interface{}{"protocol_endpoint_ids": ids}, nil
}

func (h *handlers) groupProtocolEndpointsByNode(ctx context.Context, endpointIDs []uint) ([]uint, map[string][]uint, error) {
	return h.services.BatchScopes().GroupProtocols(ctx, endpointIDs, maxTaskTargets)
}

func validateBatchTargetCount(ids []uint) error {
	if len(ids) == 0 {
		return errors.New("batch scope did not resolve any targets")
	}
	if len(ids) > maxTaskTargets {
		return fmt.Errorf("batch scope exceeds %d targets", maxTaskTargets)
	}
	return nil
}

func (h *handlers) createOperationTask(ctx context.Context, claims authClaims, taskType string, scope interface{}, content operationTaskContent, targetType string, ids []uint, idempotencyKey string) (model.Task, error) {
	scopeJSON, err := json.Marshal(scope)
	if err != nil {
		return model.Task{}, err
	}
	contentJSON, err := json.Marshal(content)
	if err != nil {
		return model.Task{}, err
	}
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" {
		idempotencyKey = uuid.NewString()
	}
	if len(idempotencyKey) > 128 {
		return model.Task{}, errors.New("idempotency_key is too long")
	}
	targets := make([]jobs.BatchSubmissionTarget, 0, len(ids))
	for _, id := range ids {
		targets = append(targets, jobs.BatchSubmissionTarget{Type: targetType, ID: id})
	}
	now := time.Now().UTC()
	receipt, err := h.services.BatchRequests.Submit(context.WithoutCancel(ctx), claims.UserID, jobs.BatchSubmission{
		Type: taskType, Scope: string(scopeJSON), Content: string(contentJSON), IdempotencyKey: idempotencyKey,
		MaxAttempts: 3, ScheduledAt: now, Targets: targets,
	})
	if err != nil {
		return model.Task{}, err
	}
	h.StartAdminTaskWorker()
	return taskFromBatchReceipt(receipt.BatchReceipt), nil
}

func writeOperationTaskError(w http.ResponseWriter, err error) {
	if isDuplicateError(err) {
		writeJSON(w, http.StatusConflict, "task idempotency key already exists", nil)
		return
	}
	ServerError(w, err)
}

func taskFromBatchReceipt(receipt jobs.BatchReceipt) model.Task {
	return model.Task{ID: receipt.ID, Type: receipt.Type, Scope: receipt.Scope, Content: receipt.Content, Status: receipt.Status, Errors: receipt.Errors, Total: receipt.Total, Current: receipt.Current, IdempotencyKey: receipt.IdempotencyKey, Priority: receipt.Priority, ScheduledAt: receipt.ScheduledAt, StartedAt: receipt.StartedAt, FinishedAt: receipt.FinishedAt, Attempts: receipt.Attempts, MaxAttempts: receipt.MaxAttempts, LockedBy: receipt.LockedBy, LockedUntil: receipt.LockedUntil, CreatedAt: receipt.CreatedAt, UpdatedAt: receipt.UpdatedAt}
}

func (h *handlers) executeOperationTaskItem(ctx context.Context, task model.Task, item model.TaskItem) error {
	operations := network.BatchResourceOperations{
		DetectNode:         h.detectBatchNode,
		ReconcileNode:      h.reconcileBatchNode,
		ReconcileNodeGroup: h.reconcileBatchNodeGroup,
		ValidateActivation: h.validateBatchProtocolActivation,
		PublishNodeConfig:  h.publishBatchNodeConfig,
	}
	return h.services.ResourceBatchExecution(operations).Execute(ctx, network.BatchResourceClaim{TaskID: task.ID, ItemID: item.ID, Token: task.LockedBy})
}

func (h *handlers) detectBatchNode(ctx context.Context, action network.BatchResourceAction) error {
	_, err := h.services.KernelDetection(h, h.hasConfiguredNativeZeroArtifact()).Detect(ctx, network.KernelDetectionRequest{NodeID: action.NodeID, ActorID: action.ActorID})
	return err
}

func (h *handlers) reconcileBatchNode(ctx context.Context, action network.BatchResourceAction) error {
	_, err := h.services.KernelReconciliation(h).Reconcile(ctx, network.KernelReconciliationRequest{NodeID: action.NodeID, ActorID: action.ActorID, Version: action.KernelVersion, AllowDowngrade: action.AllowDowngrade})
	return err
}

func (h *handlers) reconcileBatchNodeGroup(ctx context.Context, action network.BatchResourceAction) error {
	return h.reconcileNodeGroupCredentialsContext(ctx, action.NodeGroupID)
}

func (h *handlers) validateProtocolEndpointKernelSupport(ctx context.Context, endpointIDs []uint) error {
	return h.services.ProtocolCompatibility().ValidateEndpoints(ctx, endpointIDs)
}

func (h *handlers) validateBatchProtocolActivation(ctx context.Context, endpointIDs []uint) error {
	return h.services.ProtocolCompatibility().ValidateEndpoints(ctx, endpointIDs)
}

func (h *handlers) publishBatchNodeConfig(ctx context.Context, action network.BatchResourceAction) error {
	ctx, cancel := context.WithTimeout(ctx, nodeConfigPublishTimeout)
	defer cancel()
	_, _, err := h.publishNodeConfigForNode(ctx, action.NodeID, action.PublishEndpointID, action.ActorID)
	return err
}
