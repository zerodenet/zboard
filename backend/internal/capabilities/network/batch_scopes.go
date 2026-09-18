package network

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

type BatchNodeFilter struct {
	Query           string `json:"q,omitempty"`
	LifecycleStatus string `json:"lifecycle_status,omitempty"`
	ConnectorOnline *bool  `json:"connector_online,omitempty"`
	Enabled         *bool  `json:"enabled,omitempty"`
	KernelStatus    string `json:"kernel_status,omitempty"`
}

type BatchProtocolFilter struct {
	Query            string `json:"q,omitempty"`
	NodeID           uint   `json:"node_id,omitempty"`
	Protocol         string `json:"protocol,omitempty"`
	Active           *bool  `json:"active,omitempty"`
	DeploymentStatus string `json:"deployment_status,omitempty"`
}

type BatchScopeRepository interface {
	ResolveBatchNodes(context.Context, bool, []uint, BatchNodeFilter, time.Time, int) ([]uint, error)
	ResolveBatchProtocols(context.Context, bool, []uint, BatchProtocolFilter, int) ([]uint, error)
	GroupBatchProtocols(context.Context, []uint) ([]uint, map[string][]uint, error)
}

type BatchScopes struct{ Repository BatchScopeRepository }

func (s BatchScopes) Nodes(ctx context.Context, all bool, ids []uint, filter BatchNodeFilter, onlineAfter time.Time, limit int) ([]uint, error) {
	if all && len(ids) > 0 {
		return nil, errors.New("node_ids and all_matching cannot be used together")
	}
	filter.Query = strings.ToLower(strings.TrimSpace(filter.Query))
	filter.LifecycleStatus = strings.ToLower(strings.TrimSpace(filter.LifecycleStatus))
	filter.KernelStatus = strings.TrimSpace(filter.KernelStatus)
	if len(filter.Query) > 100 {
		return nil, errors.New("search keyword is too long")
	}
	if filter.LifecycleStatus != "" && filter.LifecycleStatus != "active" && filter.LifecycleStatus != "maintenance" && filter.LifecycleStatus != "retired" {
		return nil, errors.New("invalid lifecycle_status")
	}
	return s.resolve(ctx, func() ([]uint, error) {
		return s.Repository.ResolveBatchNodes(ctx, all, uniqueBatchIDs(ids), filter, onlineAfter, limit+1)
	}, limit)
}

func (s BatchScopes) Protocols(ctx context.Context, all bool, ids []uint, filter BatchProtocolFilter, limit int) ([]uint, error) {
	if all && len(ids) > 0 {
		return nil, errors.New("protocol_endpoint_ids and all_matching cannot be used together")
	}
	filter.Query = strings.ToLower(strings.TrimSpace(filter.Query))
	filter.Protocol = strings.ToLower(strings.TrimSpace(filter.Protocol))
	filter.DeploymentStatus = strings.TrimSpace(filter.DeploymentStatus)
	if len(filter.Query) > 100 {
		return nil, errors.New("search keyword is too long")
	}
	if status := filter.DeploymentStatus; status != "" && status != "running" && status != "succeeded" && status != "failed" && status != "never" {
		return nil, errors.New("invalid deployment_status")
	}
	return s.resolve(ctx, func() ([]uint, error) {
		return s.Repository.ResolveBatchProtocols(ctx, all, uniqueBatchIDs(ids), filter, limit+1)
	}, limit)
}

func (s BatchScopes) GroupProtocols(ctx context.Context, endpointIDs []uint, limit int) ([]uint, map[string][]uint, error) {
	if s.Repository == nil {
		return nil, nil, ErrBatchResourceInvalid
	}
	nodes, groups, err := s.Repository.GroupBatchProtocols(ctx, uniqueBatchIDs(endpointIDs))
	if err != nil {
		return nil, nil, err
	}
	if err := validateBatchScopeCount(nodes, limit); err != nil {
		return nil, nil, err
	}
	return nodes, groups, nil
}

func (s BatchScopes) resolve(ctx context.Context, load func() ([]uint, error), limit int) ([]uint, error) {
	if s.Repository == nil || limit <= 0 {
		return nil, ErrBatchResourceInvalid
	}
	ids, err := load()
	if err != nil {
		return nil, err
	}
	if err := validateBatchScopeCount(ids, limit); err != nil {
		return nil, err
	}
	return ids, nil
}

func validateBatchScopeCount(ids []uint, limit int) error {
	if len(ids) == 0 {
		return errors.New("batch scope did not resolve any targets")
	}
	if len(ids) > limit {
		return fmt.Errorf("batch scope exceeds %d targets", limit)
	}
	return nil
}

func uniqueBatchIDs(ids []uint) []uint {
	seen := make(map[uint]struct{}, len(ids))
	out := make([]uint, 0, len(ids))
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
