package network

import (
	"context"
	"errors"
)

var (
	ErrNetworkEntryQueryUnavailable = errors.New("network entry query capability unavailable")
	ErrNetworkEntryQueryPermission  = errors.New("network entry query requires current administrator")
)

type NetworkEntryMembership struct {
	NodeGroupID uint   `json:"node_group_id"`
	Name        string `json:"name"`
	Code        string `json:"code"`
	Description string `json:"description"`
	IsEnabled   bool   `json:"is_enabled"`
	Revision    uint64 `json:"revision"`
	SortOrder   int    `json:"sort_order"`
}

// NetworkEntryListItem is an administrator projection. It deliberately
// exposes only whether a private path exists, never its encrypted contents.
type NetworkEntryListItem struct {
	NetworkEntryRecord
	ServiceKind      string                   `json:"service_kind"`
	ParentProtocolID uint                     `json:"parent_protocol_id"`
	Memberships      []NetworkEntryMembership `json:"node_group_memberships"`
	LandingNodeID    uint                     `json:"landing_node_id"`
	NodeGroupNames   []string                 `json:"node_group_names"`
	HasPath          bool                     `json:"has_path"`
	NodeName         string                   `json:"node_name"`
	EndpointName     string                   `json:"endpoint_name"`
	Pending          bool                     `json:"pending"`
	LastError        string                   `json:"last_error"`
}

type NetworkEntryQueryRepository interface {
	ListNetworkEntries(context.Context, uint) ([]NetworkEntryListItem, error)
}

type NetworkEntryQueries struct {
	Repository NetworkEntryQueryRepository
}

func (q NetworkEntryQueries) List(ctx context.Context, actor uint) ([]NetworkEntryListItem, error) {
	if q.Repository == nil || actor == 0 {
		return nil, ErrNetworkEntryQueryUnavailable
	}
	return q.Repository.ListNetworkEntries(ctx, actor)
}
