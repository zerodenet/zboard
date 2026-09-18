package network

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
)

var (
	ErrNodeGroupMutationUnavailable = errors.New("node group mutation capability unavailable")
	ErrNodeGroupMutationPermission  = errors.New("node group mutation requires current administrator")
	ErrNodeGroupNotFound            = errors.New("node group not found")
)

type NodeGroupMutationValidation struct {
	Message string
	Fields  map[string]string
}

func (e *NodeGroupMutationValidation) Error() string { return e.Message }

type NodeGroupMutationConflict struct{ CurrentRevision uint64 }

func (e *NodeGroupMutationConflict) Error() string { return "node group changed" }

type NodeGroupMutationPrecondition struct{ CurrentRevision uint64 }

func (e *NodeGroupMutationPrecondition) Error() string { return "node group revision required" }

type NodeGroupRecord struct {
	ID                  uint      `json:"id"`
	Name                string    `json:"name"`
	Code                string    `json:"code"`
	Description         string    `json:"description"`
	IsEnabled           bool      `json:"is_enabled"`
	Revision            uint64    `json:"revision"`
	ProtocolEndpointIDs []uint    `json:"protocol_endpoint_ids"`
	NetworkEntryIDs     []uint    `json:"network_entry_ids"`
	PlanCount           int64     `json:"plan_count"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type NodeGroupMutationSnapshot struct {
	Group NodeGroupRecord
}

type NodeGroupCreateRequest struct {
	Name                string
	Code                string
	Description         string
	IsEnabled           *bool
	ProtocolEndpointIDs []uint
	NetworkEntryIDs     []uint
	CredentialProtocols []string
}

type NodeGroupUpdateRequest struct {
	ID                  uint
	ExpectedRevision    *uint64
	Name                *string
	Code                *string
	Description         *string
	IsEnabled           *bool
	ProtocolEndpointIDs *[]uint
	NetworkEntryIDs     *[]uint
	CredentialProtocols []string
}

type NodeGroupMutationChange struct {
	Group               NodeGroupRecord
	ReplaceEndpoints    bool
	ReplaceEntries      bool
	ReplaceEnabled      bool
	CredentialProtocols []string
	Now                 time.Time
}

type NodeGroupMutationResult struct {
	NodeGroup     NodeGroupRecord    `json:"-"`
	ReconcileTask *jobs.BatchReceipt `json:"reconcile_task,omitempty"`
}

type NodeGroupMutationRepository interface {
	LoadNodeGroupMutation(context.Context, uint, uint) (NodeGroupMutationSnapshot, error)
	CommitNodeGroupMutation(context.Context, uint, *NodeGroupMutationSnapshot, NodeGroupMutationChange) (NodeGroupMutationResult, error)
}

type NodeGroupMutations struct {
	Repository NodeGroupMutationRepository
	Now        func() time.Time
}

func (s NodeGroupMutations) Create(ctx context.Context, actor uint, request NodeGroupCreateRequest) (NodeGroupMutationResult, error) {
	if s.Repository == nil || actor == 0 {
		return NodeGroupMutationResult{}, ErrNodeGroupMutationUnavailable
	}
	name, code, description := strings.TrimSpace(request.Name), strings.ToLower(strings.TrimSpace(request.Code)), strings.TrimSpace(request.Description)
	fields := map[string]string{}
	if name == "" {
		fields["name"] = "请输入节点组名称。"
	}
	if code == "" {
		fields["code"] = "请输入节点组代码。"
	}
	if len(fields) > 0 {
		return NodeGroupMutationResult{}, &NodeGroupMutationValidation{Message: "节点组信息校验失败。", Fields: fields}
	}
	enabled := true
	if request.IsEnabled != nil {
		enabled = *request.IsEnabled
	}
	endpointIDs, entryIDs := normalizedNodeGroupIDs(request.ProtocolEndpointIDs), normalizedNodeGroupIDs(request.NetworkEntryIDs)
	if enabled && len(endpointIDs) == 0 && len(entryIDs) == 0 {
		return NodeGroupMutationResult{}, &NodeGroupMutationValidation{Message: "节点组信息校验失败。", Fields: map[string]string{"protocol_endpoint_ids": "启用的节点组至少需要一个可用协议端点。"}}
	}
	return s.Repository.CommitNodeGroupMutation(ctx, actor, nil, NodeGroupMutationChange{
		Group:            NodeGroupRecord{Name: name, Code: code, Description: description, IsEnabled: enabled, Revision: 1, ProtocolEndpointIDs: endpointIDs, NetworkEntryIDs: entryIDs},
		ReplaceEndpoints: true, ReplaceEntries: true, CredentialProtocols: normalizedProtocolSet(request.CredentialProtocols), Now: s.now(),
	})
}

func (s NodeGroupMutations) Update(ctx context.Context, actor uint, request NodeGroupUpdateRequest) (NodeGroupMutationResult, error) {
	if s.Repository == nil || actor == 0 || request.ID == 0 {
		return NodeGroupMutationResult{}, ErrNodeGroupMutationUnavailable
	}
	before, err := s.Repository.LoadNodeGroupMutation(ctx, actor, request.ID)
	if err != nil {
		return NodeGroupMutationResult{}, err
	}
	if request.ExpectedRevision == nil {
		return NodeGroupMutationResult{}, &NodeGroupMutationPrecondition{CurrentRevision: before.Group.Revision}
	}
	change := NodeGroupMutationChange{Group: before.Group, CredentialProtocols: normalizedProtocolSet(request.CredentialProtocols), Now: s.now()}
	fields := map[string]string{}
	if request.Name != nil {
		change.Group.Name = strings.TrimSpace(*request.Name)
		if change.Group.Name == "" {
			fields["name"] = "请输入节点组名称。"
		}
	}
	if request.Code != nil {
		change.Group.Code = strings.ToLower(strings.TrimSpace(*request.Code))
		if change.Group.Code == "" {
			fields["code"] = "请输入节点组代码。"
		}
	}
	if request.Description != nil {
		change.Group.Description = strings.TrimSpace(*request.Description)
	}
	if request.IsEnabled != nil {
		change.Group.IsEnabled = *request.IsEnabled
		change.ReplaceEnabled = true
	}
	if request.ProtocolEndpointIDs != nil {
		change.ReplaceEndpoints = true
		change.Group.ProtocolEndpointIDs = normalizedNodeGroupIDs(*request.ProtocolEndpointIDs)
	}
	if request.NetworkEntryIDs != nil {
		change.ReplaceEntries = true
		change.Group.NetworkEntryIDs = normalizedNodeGroupIDs(*request.NetworkEntryIDs)
	}
	if len(fields) > 0 {
		return NodeGroupMutationResult{}, &NodeGroupMutationValidation{Message: "节点组信息校验失败。", Fields: fields}
	}
	if request.Name == nil && request.Code == nil && request.Description == nil && request.IsEnabled == nil && !change.ReplaceEndpoints && !change.ReplaceEntries {
		return NodeGroupMutationResult{}, &NodeGroupMutationValidation{Message: "no valid update fields"}
	}
	if *request.ExpectedRevision != before.Group.Revision {
		return NodeGroupMutationResult{}, &NodeGroupMutationConflict{CurrentRevision: before.Group.Revision}
	}
	return s.Repository.CommitNodeGroupMutation(ctx, actor, &before, change)
}

func normalizedNodeGroupIDs(values []uint) []uint {
	seen := make(map[uint]struct{}, len(values))
	result := make([]uint, 0, len(values))
	for _, value := range values {
		if value == 0 {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func (s NodeGroupMutations) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
