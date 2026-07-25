package handler

import (
	"encoding/json"
	"fmt"

	"gorm.io/gorm"

	"github.com/zerodenet/zboard/backend/internal/model"
)

type nodeGroupMutationResponse struct {
	model.NodeGroup
	ReconcileTask *model.Task `json:"reconcile_task,omitempty"`
}

type nodeGroupReconcileScope struct {
	NodeGroupID uint   `json:"node_group_id"`
	NodeIDs     []uint `json:"node_ids"`
}

type nodeGroupPublishTarget struct {
	NodeID     uint
	EndpointID uint
}

func prepareNodeGroupReconcileTask(claims authClaims, groupID uint, revision uint64, targets []nodeGroupPublishTarget) (model.Task, []model.TaskItem, error) {
	targets = mergeNodeGroupPublishTargets(nil, targets)
	nodeIDs := make([]uint, 0, len(targets))
	endpointIDsByNode := make(map[string][]uint, len(targets))
	for _, target := range targets {
		nodeIDs = append(nodeIDs, target.NodeID)
		endpointIDsByNode[fmt.Sprintf("%d", target.NodeID)] = []uint{target.EndpointID}
	}
	scopeJSON, err := json.Marshal(nodeGroupReconcileScope{NodeGroupID: groupID, NodeIDs: nodeIDs})
	if err != nil {
		return model.Task{}, nil, err
	}
	contentJSON, err := json.Marshal(operationTaskContent{
		RequestedBy:       claims.UserID,
		Actor:             claims.Email,
		NodeGroupID:       groupID,
		EndpointIDsByNode: endpointIDsByNode,
	})
	if err != nil {
		return model.Task{}, nil, err
	}
	items := make([]model.TaskItem, 0, len(nodeIDs)+1)
	items = append(items, newTaskItem("node_group", groupID))
	for _, target := range targets {
		items = append(items, newTaskItem("node", target.NodeID))
	}
	task := model.Task{
		Type:           taskTypeNodeGroupSync,
		Scope:          string(scopeJSON),
		Content:        string(contentJSON),
		Status:         taskStatusPending,
		Total:          int64(len(items)),
		IdempotencyKey: fmt.Sprintf("node-group-reconcile:%d:%d", groupID, revision),
		MaxAttempts:    3,
	}
	return task, items, nil
}

func nodeGroupPublishTargets(tx *gorm.DB, groupID uint) ([]nodeGroupPublishTarget, error) {
	var endpoints []model.ProtocolEndpoint
	err := tx.Model(&model.ProtocolEndpoint{}).
		Select("protocol_endpoints.id, protocol_endpoints.node_id").
		Joins("JOIN node_group_endpoints ON node_group_endpoints.protocol_endpoint_id = protocol_endpoints.id").
		Where("node_group_endpoints.node_group_id = ?", groupID).
		Order("protocol_endpoints.node_id asc, protocol_endpoints.id asc").
		Find(&endpoints).Error
	if err != nil {
		return nil, err
	}
	targets := make([]nodeGroupPublishTarget, 0, len(endpoints))
	seen := make(map[uint]struct{})
	for _, endpoint := range endpoints {
		if _, exists := seen[endpoint.NodeID]; exists {
			continue
		}
		seen[endpoint.NodeID] = struct{}{}
		targets = append(targets, nodeGroupPublishTarget{NodeID: endpoint.NodeID, EndpointID: endpoint.ID})
	}
	return targets, nil
}

func mergeNodeGroupPublishTargets(previous, current []nodeGroupPublishTarget) []nodeGroupPublishTarget {
	merged := make([]nodeGroupPublishTarget, 0, len(previous)+len(current))
	indexByNode := make(map[uint]int)
	for _, group := range [][]nodeGroupPublishTarget{previous, current} {
		for _, target := range group {
			if target.NodeID == 0 || target.EndpointID == 0 {
				continue
			}
			if index, exists := indexByNode[target.NodeID]; exists {
				merged[index] = target
				continue
			}
			indexByNode[target.NodeID] = len(merged)
			merged = append(merged, target)
		}
	}
	return merged
}
