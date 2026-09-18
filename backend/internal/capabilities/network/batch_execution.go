package network

import (
	"context"
	"errors"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
)

const (
	BatchNodeDetect     = "node_detect"
	BatchNodeReconcile  = "node_reconcile"
	BatchNodeLifecycle  = "node_lifecycle"
	BatchProtocolDeploy = "protocol_deploy"
	BatchProtocolActive = "protocol_active"
	BatchNodeGroupSync  = "node_group_reconcile"
)

var (
	ErrBatchResourceInvalid  = errors.New("invalid resource batch operation")
	ErrBatchResourceDeleting = errors.New("资源已进入删除流程，请等待删除完成或重试删除")
)

// BatchOperationContent is the persisted resource-operation intent shared by
// request and execution adapters. Execution repositories must not trust the
// caller's copy; they reload it under the current task lease.
type BatchOperationContent struct {
	RequestedBy       uint              `json:"requested_by"`
	Actor             string            `json:"actor"`
	NodeGroupID       uint              `json:"node_group_id,omitempty"`
	LifecycleStatus   string            `json:"lifecycle_status,omitempty"`
	KernelVersion     string            `json:"kernel_version,omitempty"`
	AllowDowngrade    bool              `json:"allow_downgrade,omitempty"`
	IsActive          *bool             `json:"is_active,omitempty"`
	EndpointIDsByNode map[string][]uint `json:"endpoint_ids_by_node,omitempty"`
}

type BatchResourceClaim struct {
	TaskID, ItemID uint
	Token          string
}

type BatchResourceAction struct {
	Kind              string
	TargetType        string
	NodeID            uint
	NodeGroupID       uint
	ActorID           uint
	TaskItemID        uint
	Lifecycle         string
	KernelVersion     string
	AllowDowngrade    bool
	EndpointIDs       []uint
	Active            bool
	PublishEndpointID uint
}

type BatchResourceRepository interface {
	Inspect(context.Context, BatchResourceClaim) (BatchResourceAction, error)
	Commit(context.Context, BatchResourceClaim, BatchResourceAction) (BatchResourceAction, error)
}

type BatchResourceAdapter interface {
	DetectBatchNode(context.Context, BatchResourceAction) error
	ReconcileBatchNode(context.Context, BatchResourceAction) error
	ReconcileBatchNodeGroup(context.Context, BatchResourceAction) error
	ValidateBatchProtocolActivation(context.Context, []uint) error
	PublishBatchNodeConfig(context.Context, BatchResourceAction) error
}

// BatchResourceOperations is an explicit composition adapter. It prevents a
// transport object with unrelated authority from becoming the resource batch
// executor merely because it happens to expose the required method names.
type BatchResourceOperations struct {
	DetectNode         func(context.Context, BatchResourceAction) error
	ReconcileNode      func(context.Context, BatchResourceAction) error
	ReconcileNodeGroup func(context.Context, BatchResourceAction) error
	ValidateActivation func(context.Context, []uint) error
	PublishNodeConfig  func(context.Context, BatchResourceAction) error
}

func (a BatchResourceOperations) DetectBatchNode(ctx context.Context, action BatchResourceAction) error {
	if a.DetectNode == nil {
		return ErrBatchResourceInvalid
	}
	return a.DetectNode(ctx, action)
}

func (a BatchResourceOperations) ReconcileBatchNode(ctx context.Context, action BatchResourceAction) error {
	if a.ReconcileNode == nil {
		return ErrBatchResourceInvalid
	}
	return a.ReconcileNode(ctx, action)
}

func (a BatchResourceOperations) ReconcileBatchNodeGroup(ctx context.Context, action BatchResourceAction) error {
	if a.ReconcileNodeGroup == nil {
		return ErrBatchResourceInvalid
	}
	return a.ReconcileNodeGroup(ctx, action)
}

func (a BatchResourceOperations) ValidateBatchProtocolActivation(ctx context.Context, endpointIDs []uint) error {
	if a.ValidateActivation == nil {
		return ErrBatchResourceInvalid
	}
	return a.ValidateActivation(ctx, endpointIDs)
}

func (a BatchResourceOperations) PublishBatchNodeConfig(ctx context.Context, action BatchResourceAction) error {
	if a.PublishNodeConfig == nil {
		return ErrBatchResourceInvalid
	}
	return a.PublishNodeConfig(ctx, action)
}

type BatchResourceExecution struct {
	Repository BatchResourceRepository
	Adapter    BatchResourceAdapter
}

func (s BatchResourceExecution) Execute(ctx context.Context, claim BatchResourceClaim) error {
	if claim.TaskID == 0 || claim.ItemID == 0 || claim.Token == "" {
		return jobs.ErrLeaseLost
	}
	if s.Repository == nil || s.Adapter == nil {
		return ErrBatchResourceInvalid
	}
	action, err := s.Repository.Inspect(ctx, claim)
	if err != nil {
		return err
	}
	if err := validateBatchResourceAction(action); err != nil {
		return err
	}
	switch action.Kind {
	case BatchNodeDetect:
		return s.Adapter.DetectBatchNode(ctx, action)
	case BatchNodeReconcile:
		return s.Adapter.ReconcileBatchNode(ctx, action)
	case BatchNodeGroupSync:
		if action.TargetType == "node_group" {
			return s.Adapter.ReconcileBatchNodeGroup(ctx, action)
		}
		return s.Adapter.PublishBatchNodeConfig(ctx, action)
	}
	if action.Kind == BatchProtocolActive && action.Active {
		if err := s.Adapter.ValidateBatchProtocolActivation(ctx, append([]uint(nil), action.EndpointIDs...)); err != nil {
			return err
		}
	}
	prepared, err := s.Repository.Commit(ctx, claim, action)
	if err != nil {
		return err
	}
	if err := validateBatchResourceAction(prepared); err != nil {
		return err
	}
	if prepared.Kind == BatchNodeLifecycle {
		return nil
	}
	if prepared.PublishEndpointID == 0 {
		return ErrBatchResourceInvalid
	}
	return s.Adapter.PublishBatchNodeConfig(ctx, prepared)
}

func validateBatchResourceAction(action BatchResourceAction) error {
	if action.ActorID == 0 || action.TaskItemID == 0 {
		return ErrBatchResourceInvalid
	}
	switch action.Kind {
	case BatchNodeDetect:
		if action.TargetType != "node" || action.NodeID == 0 {
			return ErrBatchResourceInvalid
		}
	case BatchNodeReconcile:
		if action.TargetType != "node" || action.NodeID == 0 || action.KernelVersion == "" {
			return ErrBatchResourceInvalid
		}
	case BatchNodeLifecycle:
		if action.TargetType != "node" || action.NodeID == 0 || action.Lifecycle != "active" && action.Lifecycle != "maintenance" && action.Lifecycle != "retired" {
			return ErrBatchResourceInvalid
		}
	case BatchProtocolDeploy:
		if action.TargetType != "node" || action.NodeID == 0 {
			return ErrBatchResourceInvalid
		}
	case BatchProtocolActive:
		if action.TargetType != "node" || action.NodeID == 0 || len(action.EndpointIDs) == 0 {
			return ErrBatchResourceInvalid
		}
	case BatchNodeGroupSync:
		if action.TargetType == "node_group" {
			if action.NodeGroupID == 0 {
				return ErrBatchResourceInvalid
			}
		} else if action.TargetType != "node" || action.NodeID == 0 || action.PublishEndpointID == 0 {
			return ErrBatchResourceInvalid
		}
	default:
		return ErrBatchResourceInvalid
	}
	return nil
}
