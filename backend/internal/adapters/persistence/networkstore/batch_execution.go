package networkstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type BatchResourceExecution struct{ DB *gorm.DB }

func (s BatchResourceExecution) Inspect(ctx context.Context, claim network.BatchResourceClaim) (out network.BatchResourceAction, err error) {
	err = jobstore.WithBatchLeaseTask(ctx, s.DB, claim.TaskID, claim.Token, func(tx *gorm.DB, task model.Task) error {
		var loadErr error
		out, loadErr = loadBatchResourceAction(tx, task, claim)
		return loadErr
	})
	return
}

func (s BatchResourceExecution) Commit(ctx context.Context, claim network.BatchResourceClaim, inspected network.BatchResourceAction) (out network.BatchResourceAction, err error) {
	err = jobstore.WithBatchLeaseTask(ctx, s.DB, claim.TaskID, claim.Token, func(tx *gorm.DB, task model.Task) error {
		current, loadErr := loadBatchResourceAction(tx, task, claim)
		if loadErr != nil {
			return loadErr
		}
		if !sameBatchResourceAction(current, inspected) {
			return jobs.ErrLeaseLost
		}
		switch current.Kind {
		case network.BatchNodeLifecycle:
			if err := commitNodeLifecycle(tx, current); err != nil {
				return err
			}
		case network.BatchProtocolDeploy:
			endpointID, err := firstProtocolEndpoint(tx, current.NodeID)
			if err != nil {
				return err
			}
			current.PublishEndpointID = endpointID
		case network.BatchProtocolActive:
			if err := commitProtocolActive(tx, current); err != nil {
				return err
			}
			endpointID, err := firstProtocolEndpoint(tx, current.NodeID)
			if err != nil {
				return err
			}
			current.PublishEndpointID = endpointID
		default:
			return network.ErrBatchResourceInvalid
		}
		out = current
		return nil
	})
	return
}

func loadBatchResourceAction(tx *gorm.DB, task model.Task, claim network.BatchResourceClaim) (network.BatchResourceAction, error) {
	var item model.TaskItem
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND task_id = ? AND status = ?", claim.ItemID, claim.TaskID, int16(1)).First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return network.BatchResourceAction{}, jobs.ErrLeaseLost
	}
	if err != nil {
		return network.BatchResourceAction{}, err
	}
	targetID, err := strconv.ParseUint(item.TargetID, 10, 64)
	if err != nil || targetID == 0 {
		return network.BatchResourceAction{}, network.ErrBatchResourceInvalid
	}
	var content network.BatchOperationContent
	if err := json.Unmarshal([]byte(task.Content), &content); err != nil {
		return network.BatchResourceAction{}, fmt.Errorf("decode resource batch content: %w", err)
	}
	if _, err := providerAdmin(tx, content.RequestedBy); err != nil {
		if errors.Is(err, network.ErrProviderPermission) {
			return network.BatchResourceAction{}, network.ErrResourcePermission
		}
		return network.BatchResourceAction{}, err
	}
	action := network.BatchResourceAction{Kind: task.Type, TargetType: item.TargetType, ActorID: content.RequestedBy, TaskItemID: item.ID, Lifecycle: content.LifecycleStatus, KernelVersion: content.KernelVersion, AllowDowngrade: content.AllowDowngrade}
	switch task.Type {
	case network.BatchNodeDetect, network.BatchNodeReconcile, network.BatchNodeLifecycle, network.BatchProtocolDeploy:
		if item.TargetType != "node" {
			return network.BatchResourceAction{}, network.ErrBatchResourceInvalid
		}
		action.NodeID = uint(targetID)
	case network.BatchProtocolActive:
		if item.TargetType != "node" {
			return network.BatchResourceAction{}, network.ErrBatchResourceInvalid
		}
		action.NodeID = uint(targetID)
		if content.IsActive == nil {
			return network.BatchResourceAction{}, network.ErrBatchResourceInvalid
		}
		action.Active = *content.IsActive
		action.EndpointIDs = append([]uint(nil), content.EndpointIDsByNode[strconv.FormatUint(targetID, 10)]...)
	case network.BatchNodeGroupSync:
		switch item.TargetType {
		case "node_group":
			action.NodeGroupID = uint(targetID)
			if content.NodeGroupID == 0 || action.NodeGroupID != content.NodeGroupID {
				return network.BatchResourceAction{}, network.ErrBatchResourceInvalid
			}
		case "node":
			action.NodeID = uint(targetID)
			endpointIDs := content.EndpointIDsByNode[strconv.FormatUint(targetID, 10)]
			if len(endpointIDs) > 0 {
				action.PublishEndpointID = endpointIDs[0]
			} else {
				endpointID, endpointErr := firstProtocolEndpoint(tx, action.NodeID)
				if endpointErr != nil {
					return network.BatchResourceAction{}, endpointErr
				}
				action.PublishEndpointID = endpointID
			}
		default:
			return network.BatchResourceAction{}, network.ErrBatchResourceInvalid
		}
	default:
		return network.BatchResourceAction{}, network.ErrBatchResourceInvalid
	}
	return action, nil
}

func commitNodeLifecycle(tx *gorm.DB, action network.BatchResourceAction) error {
	if action.Lifecycle != "active" && action.Lifecycle != "maintenance" && action.Lifecycle != "retired" {
		return network.ErrBatchResourceInvalid
	}
	var node model.Node
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&node, action.NodeID).Error; err != nil {
		return err
	}
	if node.LifecycleStatus == "deleting" {
		return network.ErrBatchResourceDeleting
	}
	enabled := action.Lifecycle == "active"
	if node.LifecycleStatus == action.Lifecycle && node.IsEnabled == enabled {
		return nil
	}
	if err := tx.Model(&node).Updates(map[string]any{"lifecycle_status": action.Lifecycle, "is_enabled": enabled}).Error; err != nil {
		return err
	}
	return createBatchResourceAudit(tx, action, "node.lifecycle.batch", fmt.Sprintf("node:%d", node.ID), fmt.Sprintf("lifecycle=%s task_item=%d", action.Lifecycle, action.TaskItemID))
}

func commitProtocolActive(tx *gorm.DB, action network.BatchResourceAction) error {
	if len(action.EndpointIDs) == 0 {
		return network.ErrBatchResourceInvalid
	}
	if !action.Active {
		var activePlanCount int64
		if err := tx.Table("node_group_endpoints").Joins("JOIN plans ON plans.node_group_id = node_group_endpoints.node_group_id").Where("node_group_endpoints.protocol_endpoint_id IN ? AND plans.is_active = ?", action.EndpointIDs, true).Count(&activePlanCount).Error; err != nil {
			return err
		}
		if activePlanCount > 0 {
			return errors.New("unbind this endpoint from active plans before disabling it")
		}
	}
	var count int64
	if err := tx.Model(&model.ProtocolEndpoint{}).Where("node_id = ? AND id IN ?", action.NodeID, action.EndpointIDs).Count(&count).Error; err != nil {
		return err
	}
	if count != int64(len(action.EndpointIDs)) {
		return errors.New("one or more protocol endpoints no longer belong to this node")
	}
	if err := tx.Model(&model.ProtocolEndpoint{}).Where("node_id = ? AND id IN ? AND is_active <> ?", action.NodeID, action.EndpointIDs, action.Active).Update("is_active", action.Active).Error; err != nil {
		return err
	}
	return createBatchResourceAudit(tx, action, "protocol_endpoint.active.batch", fmt.Sprintf("node:%d", action.NodeID), fmt.Sprintf("active=%t endpoints=%d task_item=%d", action.Active, len(action.EndpointIDs), action.TaskItemID))
}

func createBatchResourceAudit(tx *gorm.DB, action network.BatchResourceAction, name, target, detail string) error {
	var actor model.User
	if err := tx.Where("id = ? AND is_admin = ? AND status = ?", action.ActorID, true, "active").First(&actor).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return network.ErrResourcePermission
		}
		return err
	}
	return tx.Create(&model.AuditLog{UserID: &actor.ID, Actor: actor.Email, Action: name, Target: target, Detail: detail}).Error
}

func firstProtocolEndpoint(tx *gorm.DB, nodeID uint) (uint, error) {
	var endpointID uint
	if err := tx.Model(&model.ProtocolEndpoint{}).Where("node_id = ?", nodeID).Order("id asc").Limit(1).Pluck("id", &endpointID).Error; err != nil {
		return 0, err
	}
	if endpointID == 0 {
		return 0, errors.New("node no longer has a protocol endpoint to publish")
	}
	return endpointID, nil
}

func sameBatchResourceAction(left, right network.BatchResourceAction) bool {
	return left.Kind == right.Kind && left.TargetType == right.TargetType && left.NodeID == right.NodeID && left.NodeGroupID == right.NodeGroupID && left.ActorID == right.ActorID && left.TaskItemID == right.TaskItemID && left.Lifecycle == right.Lifecycle && left.KernelVersion == right.KernelVersion && left.AllowDowngrade == right.AllowDowngrade && left.Active == right.Active && left.PublishEndpointID == right.PublishEndpointID && slices.Equal(left.EndpointIDs, right.EndpointIDs)
}
