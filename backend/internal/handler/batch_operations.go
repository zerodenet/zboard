package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type nodeBatchFilters struct {
	Query           string `json:"q,omitempty"`
	LifecycleStatus string `json:"lifecycle_status,omitempty"`
	ConnectorOnline *bool  `json:"connector_online,omitempty"`
	Enabled         *bool  `json:"enabled,omitempty"`
	KernelStatus    string `json:"kernel_status,omitempty"`
}

type protocolBatchFilters struct {
	Query            string `json:"q,omitempty"`
	NodeID           uint   `json:"node_id,omitempty"`
	Protocol         string `json:"protocol,omitempty"`
	Active           *bool  `json:"active,omitempty"`
	DeploymentStatus string `json:"deployment_status,omitempty"`
}

type nodeBatchOperationRequest struct {
	Action         string           `json:"action"`
	NodeIDs        []uint           `json:"node_ids,omitempty"`
	AllMatching    bool             `json:"all_matching,omitempty"`
	Filters        nodeBatchFilters `json:"filters,omitempty"`
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

type operationTaskContent struct {
	RequestedBy       uint              `json:"requested_by"`
	Actor             string            `json:"actor"`
	NodeGroupID       uint              `json:"node_group_id,omitempty"`
	LifecycleStatus   string            `json:"lifecycle_status,omitempty"`
	IsActive          *bool             `json:"is_active,omitempty"`
	EndpointIDsByNode map[string][]uint `json:"endpoint_ids_by_node,omitempty"`
}

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
	ids, scope, err := h.resolveNodeBatchScope(req)
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	task, err := h.createOperationTask(claims, taskType, scope, content, "node", ids, req.IdempotencyKey)
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
	ids, scope, err := h.resolveProtocolBatchScope(req)
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	if err := h.validateProtocolEndpointKernelSupport(ids); err != nil {
		BadRequest(w, err.Error())
		return
	}
	nodeIDs, _, err := h.groupProtocolEndpointsByNode(ids)
	if err != nil {
		writeOperationTaskError(w, err)
		return
	}
	content := operationTaskContent{RequestedBy: claims.UserID, Actor: claims.Email}
	task, err := h.createOperationTask(claims, taskTypeProtocolDeploy, scope, content, "node", nodeIDs, req.IdempotencyKey)
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
	ids, scope, err := h.resolveProtocolBatchScope(protocolBatchDeployRequest{
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
		if err := h.validateProtocolEndpointKernelSupport(ids); err != nil {
			BadRequest(w, err.Error())
			return
		}
	}
	nodeIDs, endpointIDsByNode, err := h.groupProtocolEndpointsByNode(ids)
	if err != nil {
		writeOperationTaskError(w, err)
		return
	}
	content := operationTaskContent{RequestedBy: claims.UserID, Actor: claims.Email, IsActive: req.IsActive, EndpointIDsByNode: endpointIDsByNode}
	task, err := h.createOperationTask(claims, taskTypeProtocolActive, scope, content, "node", nodeIDs, req.IdempotencyKey)
	if err != nil {
		writeOperationTaskError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, "batch task accepted", task)
}

func (h *handlers) resolveNodeBatchScope(req nodeBatchOperationRequest) ([]uint, interface{}, error) {
	if req.AllMatching && len(req.NodeIDs) > 0 {
		return nil, nil, errors.New("node_ids and all_matching cannot be used together")
	}
	if req.AllMatching {
		query := h.db.Model(&model.Node{}).Select("nodes.id")
		var err error
		query, err = applyNodeBatchFilters(query, req.Filters)
		if err != nil {
			return nil, nil, err
		}
		var ids []uint
		if err := query.Order("nodes.id asc").Limit(maxTaskTargets+1).Pluck("nodes.id", &ids).Error; err != nil {
			return nil, nil, err
		}
		if err := validateBatchTargetCount(ids); err != nil {
			return nil, nil, err
		}
		return ids, map[string]interface{}{"all_matching": true, "filters": req.Filters}, nil
	}
	ids := uniqueUintIDs(req.NodeIDs)
	if err := validateBatchTargetCount(ids); err != nil {
		return nil, nil, err
	}
	var existing []uint
	if err := h.db.Model(&model.Node{}).Where("id IN ?", ids).Order("id asc").Pluck("id", &existing).Error; err != nil {
		return nil, nil, err
	}
	if len(existing) != len(ids) {
		return nil, nil, errors.New("one or more nodes do not exist")
	}
	return existing, map[string]interface{}{"node_ids": existing}, nil
}

func applyNodeBatchFilters(query *gorm.DB, filters nodeBatchFilters) (*gorm.DB, error) {
	keyword := strings.ToLower(strings.TrimSpace(filters.Query))
	if len(keyword) > 100 {
		return nil, errors.New("search keyword is too long")
	}
	if keyword != "" {
		pattern := "%" + keyword + "%"
		query = query.Where("LOWER(nodes.name) LIKE ? OR LOWER(nodes.address) LIKE ? OR LOWER(nodes.region) LIKE ?", pattern, pattern, pattern)
	}
	if filters.LifecycleStatus != "" {
		status := strings.ToLower(strings.TrimSpace(filters.LifecycleStatus))
		if status != "active" && status != "maintenance" && status != "retired" {
			return nil, errors.New("invalid lifecycle_status")
		}
		query = query.Where("nodes.lifecycle_status = ?", status)
	}
	if filters.ConnectorOnline != nil {
		cutoff := time.Now().UTC().Add(-nodeOnlineWindow)
		if *filters.ConnectorOnline {
			query = query.Where("nodes.connector_last_seen_at >= ?", cutoff)
		} else {
			query = query.Where("nodes.connector_last_seen_at IS NULL OR nodes.connector_last_seen_at < ?", cutoff)
		}
	}
	if filters.Enabled != nil {
		query = query.Where("nodes.is_enabled = ?", *filters.Enabled)
	}
	if filters.KernelStatus != "" {
		query = query.Joins("JOIN node_kernel_states ON node_kernel_states.node_id = nodes.id").Where("node_kernel_states.status = ?", strings.TrimSpace(filters.KernelStatus))
	}
	return query, nil
}

func (h *handlers) resolveProtocolBatchScope(req protocolBatchDeployRequest) ([]uint, interface{}, error) {
	if req.AllMatching && len(req.ProtocolEndpointIDs) > 0 {
		return nil, nil, errors.New("protocol_endpoint_ids and all_matching cannot be used together")
	}
	if req.AllMatching {
		query := h.db.Model(&model.ProtocolEndpoint{}).Select("protocol_endpoints.id")
		var err error
		query, err = h.applyProtocolBatchFilters(query, req.Filters)
		if err != nil {
			return nil, nil, err
		}
		var ids []uint
		if err := query.Order("protocol_endpoints.id asc").Limit(maxTaskTargets+1).Pluck("protocol_endpoints.id", &ids).Error; err != nil {
			return nil, nil, err
		}
		if err := validateBatchTargetCount(ids); err != nil {
			return nil, nil, err
		}
		return ids, map[string]interface{}{"all_matching": true, "filters": req.Filters}, nil
	}
	ids := uniqueUintIDs(req.ProtocolEndpointIDs)
	if err := validateBatchTargetCount(ids); err != nil {
		return nil, nil, err
	}
	var existing []uint
	if err := h.db.Model(&model.ProtocolEndpoint{}).Where("id IN ?", ids).Order("id asc").Pluck("id", &existing).Error; err != nil {
		return nil, nil, err
	}
	if len(existing) != len(ids) {
		return nil, nil, errors.New("one or more protocol endpoints do not exist")
	}
	return existing, map[string]interface{}{"protocol_endpoint_ids": existing}, nil
}

func (h *handlers) groupProtocolEndpointsByNode(endpointIDs []uint) ([]uint, map[string][]uint, error) {
	type endpointNode struct {
		ID     uint
		NodeID uint
	}
	rows := make([]endpointNode, 0, len(endpointIDs))
	if err := h.db.Model(&model.ProtocolEndpoint{}).
		Select("id, node_id").Where("id IN ?", endpointIDs).Order("node_id asc, id asc").Scan(&rows).Error; err != nil {
		return nil, nil, err
	}
	groups := make(map[string][]uint)
	nodeIDs := make([]uint, 0)
	for _, row := range rows {
		key := strconv.FormatUint(uint64(row.NodeID), 10)
		if _, exists := groups[key]; !exists {
			nodeIDs = append(nodeIDs, row.NodeID)
		}
		groups[key] = append(groups[key], row.ID)
	}
	if err := validateBatchTargetCount(nodeIDs); err != nil {
		return nil, nil, err
	}
	return nodeIDs, groups, nil
}

func (h *handlers) applyProtocolBatchFilters(query *gorm.DB, filters protocolBatchFilters) (*gorm.DB, error) {
	keyword := strings.ToLower(strings.TrimSpace(filters.Query))
	if len(keyword) > 100 {
		return nil, errors.New("search keyword is too long")
	}
	if keyword != "" {
		pattern := "%" + keyword + "%"
		query = query.Where("LOWER(protocol_endpoints.name) LIKE ? OR LOWER(protocol_endpoints.address) LIKE ?", pattern, pattern)
	}
	if filters.NodeID != 0 {
		query = query.Where("protocol_endpoints.node_id = ?", filters.NodeID)
	}
	if filters.Protocol != "" {
		protocol := strings.ToLower(strings.TrimSpace(filters.Protocol))
		if !h.isProtocolSupported(protocol) {
			return nil, errors.New("invalid protocol")
		}
		query = query.Where("protocol_endpoints.protocol = ?", protocol)
	}
	if filters.Active != nil {
		query = query.Where("protocol_endpoints.is_active = ?", *filters.Active)
	}
	if filters.DeploymentStatus != "" {
		status := strings.TrimSpace(filters.DeploymentStatus)
		if status != "running" && status != "succeeded" && status != "failed" && status != "never" {
			return nil, errors.New("invalid deployment_status")
		}
		latestIDs := h.db.Model(&model.ProtocolDeployment{}).Select("MAX(id)").Group("protocol_endpoint_id")
		if status == "never" {
			deployedIDs := h.db.Model(&model.ProtocolDeployment{}).Select("DISTINCT protocol_endpoint_id")
			query = query.Where("protocol_endpoints.id NOT IN (?)", deployedIDs)
		} else {
			matchingIDs := h.db.Model(&model.ProtocolDeployment{}).Select("protocol_endpoint_id").Where("id IN (?) AND status = ?", latestIDs, status)
			query = query.Where("protocol_endpoints.id IN (?)", matchingIDs)
		}
	}
	return query, nil
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

func (h *handlers) createOperationTask(claims authClaims, taskType string, scope interface{}, content operationTaskContent, targetType string, ids []uint, idempotencyKey string) (model.Task, error) {
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
	items := make([]model.TaskItem, 0, len(ids))
	for _, id := range ids {
		items = append(items, newTaskItem(targetType, id))
	}
	task := model.Task{
		Type: taskType, Scope: string(scopeJSON), Content: string(contentJSON), Status: taskStatusPending,
		Total: int64(len(items)), IdempotencyKey: idempotencyKey, MaxAttempts: 3,
	}
	return h.persistAdminTask(claims, task, items, true)
}

func writeOperationTaskError(w http.ResponseWriter, err error) {
	if isDuplicateError(err) {
		writeJSON(w, http.StatusConflict, "task idempotency key already exists", nil)
		return
	}
	ServerError(w, err)
}

func (h *handlers) executeOperationTaskItem(task model.Task, item model.TaskItem) error {
	var content operationTaskContent
	if err := json.Unmarshal([]byte(task.Content), &content); err != nil {
		return fmt.Errorf("decode operation task content: %w", err)
	}
	claims := authClaims{UserID: content.RequestedBy, Email: content.Actor, IsAdmin: true}
	targetID, err := strconv.ParseUint(item.TargetID, 10, 64)
	if err != nil || targetID == 0 {
		return errors.New("invalid operation target")
	}
	switch task.Type {
	case taskTypeNodeDetect:
		return h.executeNodeDetectTask(uint(targetID), claims)
	case taskTypeNodeReconcile:
		return h.executeNodeReconcileTask(uint(targetID), claims)
	case taskTypeNodeLifecycle:
		return h.executeNodeLifecycleTask(uint(targetID), content.LifecycleStatus, claims, item.ID)
	case taskTypeProtocolDeploy:
		return h.executeProtocolDeployTask(uint(targetID), content.RequestedBy)
	case taskTypeProtocolActive:
		if content.IsActive == nil {
			return errors.New("protocol active task is missing is_active")
		}
		endpointIDs := content.EndpointIDsByNode[strconv.FormatUint(targetID, 10)]
		if len(endpointIDs) == 0 {
			return errors.New("protocol active task is missing endpoint scope for node")
		}
		return h.executeProtocolActiveTask(uint(targetID), endpointIDs, *content.IsActive, claims, item.ID)
	case taskTypeNodeGroupSync:
		switch item.TargetType {
		case "node_group":
			if content.NodeGroupID == 0 || uint(targetID) != content.NodeGroupID {
				return errors.New("node group reconcile task has an invalid group target")
			}
			return h.reconcileNodeGroupCredentials(content.NodeGroupID)
		case "node":
			endpointIDs := content.EndpointIDsByNode[strconv.FormatUint(targetID, 10)]
			triggerEndpointID := uint(0)
			if len(endpointIDs) > 0 {
				triggerEndpointID = endpointIDs[0]
			}
			if triggerEndpointID == 0 {
				if err := h.db.Model(&model.ProtocolEndpoint{}).
					Where("node_id = ?", uint(targetID)).
					Order("id asc").
					Limit(1).
					Pluck("id", &triggerEndpointID).Error; err != nil {
					return err
				}
			}
			if triggerEndpointID == 0 {
				return errors.New("node group reconcile task cannot resolve a protocol endpoint trigger")
			}
			ctx, cancel := context.WithTimeout(context.Background(), nodeConfigPublishTimeout)
			defer cancel()
			_, _, err := h.publishNodeConfigForNode(ctx, uint(targetID), triggerEndpointID, content.RequestedBy)
			return err
		default:
			return errors.New("node group reconcile task has an invalid target type")
		}
	default:
		return errors.New("unsupported operation task type")
	}
}

func (h *handlers) executeNodeDetectTask(nodeID uint, claims authClaims) error {
	node, err := h.loadNode(nodeID)
	if err != nil {
		return err
	}
	if err := h.validateNodeSSH(node); err != nil {
		return err
	}
	operation, err := h.beginKernelOperation(node.ID, claims, "detect")
	if err != nil {
		return err
	}
	probe, err := h.probeNodeKernel(node)
	if err != nil {
		_ = h.failKernelOperation(operation.ID, node.ID, "detecting", err)
		return err
	}
	_, err = h.completeKernelDetection(operation, probe)
	return err
}

func (h *handlers) executeNodeReconcileTask(nodeID uint, claims authClaims) error {
	node, err := h.loadNode(nodeID)
	if err != nil {
		return err
	}
	if err := h.validateNodeSSH(node); err != nil {
		return err
	}
	operation, err := h.beginKernelOperation(node.ID, claims, "reconcile")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	_, err = h.reconcileNodeKernel(ctx, node, &operation, kernelReconcileRequest{})
	if err != nil {
		_ = h.failKernelOperation(operation.ID, node.ID, operation.Phase, err)
	}
	return err
}

func (h *handlers) executeNodeLifecycleTask(nodeID uint, lifecycle string, claims authClaims, taskItemID uint) error {
	if lifecycle != "active" && lifecycle != "maintenance" && lifecycle != "retired" {
		return errors.New("invalid lifecycle status")
	}
	return h.db.Transaction(func(tx *gorm.DB) error {
		var node model.Node
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&node, nodeID).Error; err != nil {
			return err
		}
		enabled := lifecycle == "active"
		if node.LifecycleStatus == lifecycle && node.IsEnabled == enabled {
			return nil
		}
		if err := tx.Model(&node).Updates(map[string]interface{}{"lifecycle_status": lifecycle, "is_enabled": enabled}).Error; err != nil {
			return err
		}
		return createAuditLog(tx, claims, "node.lifecycle.batch", fmt.Sprintf("node:%d", node.ID), fmt.Sprintf("lifecycle=%s task_item=%d", lifecycle, taskItemID))
	})
}

func (h *handlers) executeProtocolDeployTask(nodeID, requestedBy uint) error {
	var endpointID uint
	if err := h.db.Model(&model.ProtocolEndpoint{}).Where("node_id = ?", nodeID).Order("id asc").Limit(1).Pluck("id", &endpointID).Error; err != nil {
		return err
	}
	if endpointID == 0 {
		return errors.New("node no longer has a protocol endpoint to publish")
	}
	ctx, cancel := context.WithTimeout(context.Background(), nodeConfigPublishTimeout)
	defer cancel()
	_, _, err := h.publishNodeConfig(ctx, endpointID, requestedBy)
	return err
}

func (h *handlers) executeProtocolActiveTask(nodeID uint, endpointIDs []uint, active bool, claims authClaims, taskItemID uint) error {
	if active {
		if err := h.validateProtocolEndpointKernelSupport(endpointIDs); err != nil {
			return err
		}
	}
	if !active {
		var activePlanCount int64
		if err := h.db.Table("node_group_endpoints").
			Joins("JOIN plans ON plans.node_group_id = node_group_endpoints.node_group_id").
			Where("node_group_endpoints.protocol_endpoint_id IN ? AND plans.is_active = ?", endpointIDs, true).
			Count(&activePlanCount).Error; err != nil {
			return err
		}
		if activePlanCount > 0 {
			return errors.New("unbind this endpoint from active plans before disabling it")
		}
	}
	if err := h.db.Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&model.ProtocolEndpoint{}).Where("node_id = ? AND id IN ?", nodeID, endpointIDs).Count(&count).Error; err != nil {
			return err
		}
		if count != int64(len(endpointIDs)) {
			return errors.New("one or more protocol endpoints no longer belong to this node")
		}
		if err := tx.Model(&model.ProtocolEndpoint{}).Where("node_id = ? AND id IN ? AND is_active <> ?", nodeID, endpointIDs, active).Update("is_active", active).Error; err != nil {
			return err
		}
		return createAuditLog(tx, claims, "protocol_endpoint.active.batch", fmt.Sprintf("node:%d", nodeID), fmt.Sprintf("active=%t endpoints=%d task_item=%d", active, len(endpointIDs), taskItemID))
	}); err != nil {
		return err
	}
	return h.executeProtocolDeployTask(nodeID, claims.UserID)
}

func (h *handlers) validateProtocolEndpointKernelSupport(endpointIDs []uint) error {
	if len(endpointIDs) == 0 {
		return nil
	}
	var endpoints []model.ProtocolEndpoint
	if err := h.db.Select("id", "protocol").Where("id IN ?", uniqueUintIDs(endpointIDs)).Find(&endpoints).Error; err != nil {
		return err
	}
	for _, endpoint := range endpoints {
		if supported, reason := h.protocolKernelSupport(endpoint.Protocol); !supported {
			return fmt.Errorf("protocol endpoint %d cannot be used: %s", endpoint.ID, reason)
		}
	}
	return nil
}
