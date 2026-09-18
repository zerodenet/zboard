package network

import (
	"context"
	"errors"
	"time"
)

var ErrInventoryNotFound = errors.New("network inventory item not found")

type NodeKernelRecord struct {
	NodeID                                   uint `json:"node_id"`
	Status, Phase, RecommendedAction         string
	PlatformOS, Architecture, Libc           string
	DesiredVersion, InstalledVersion         string
	DesiredSHA256, InstalledSHA256           string
	DesiredConfigSHA256, AppliedConfigSHA256 string
	ServiceStatus, ControlStatus, LastError  string
	ActiveOperationID                        *uint
	LastDetectedAt, LastHealthyAt            *time.Time
	CreatedAt, UpdatedAt                     time.Time
}

type NodeInventoryItem struct {
	Node                 NodeAdministrationRecord
	KernelState          *NodeKernelRecord
	EnabledProtocolCount int64
}

type NodeInventoryQuery struct {
	ID                                            uint
	Search, Region, LifecycleStatus, KernelStatus string
	Enabled, ConnectorOnline                      *bool
	Paged                                         bool
	Offset, Limit                                 int
	Sort, Direction                               string
	OnlineCutoff                                  time.Time
}

type NodeInventoryPage struct {
	Items []NodeInventoryItem
	Total int64
}

type NodeRuntimeRecord struct {
	Node                                                                          NodeAdministrationRecord
	KernelState                                                                   *NodeKernelRecord
	NodeCredentialCiphertext, SSHPwdCiphertext, SSHPrivateKeyPassphraseCiphertext string
	SSHPrivilegePasswordCiphertext, TrafficSecretCiphertext                       string
}

type ProtocolDeploymentRecord struct {
	ID, NodeID, ProtocolEndpointID uint
	ConfigRevision                 uint64
	DesiredConfigSHA256            string
	AppliedConfigSHA256            string
	Status, Error, Output          string
	RequestedBy                    *uint
	StartedAt, FinishedAt          *time.Time
	CreatedAt, UpdatedAt           time.Time
}

type ProtocolUsageRecord struct {
	ActiveFlows, ActiveUsers, ActiveCredentials int64
	LastUsedAt                                  *time.Time
	UsedBytesToday, UsedBytesTotal              int64
}

type ProtocolEndpointInventoryItem struct {
	Endpoint             ProtocolEndpointRecord
	Node                 NodeAdministrationRecord
	ManagedCertificateID *uint
	LatestDeployment     *ProtocolDeploymentRecord
	Usage                ProtocolUsageRecord
	Memberships          []ProtocolEndpointMembership
}

type ProtocolEndpointInventoryQuery struct {
	IDs                                []uint
	NodeID                             uint
	Search, Protocol, DeploymentStatus string
	Active                             *bool
	Paged                              bool
	IncludeStatusFacets                bool
	Offset, Limit                      int
	Sort, Direction                    string
	Now                                time.Time
}

type ProtocolEndpointInventoryPage struct {
	Items  []ProtocolEndpointInventoryItem
	Total  int64
	Facets ProtocolEndpointStatusFacets
}

type ProtocolEndpointStatusFacets struct {
	All       int64 `json:"all"`
	Succeeded int64 `json:"succeeded"`
	Running   int64 `json:"running"`
	Failed    int64 `json:"failed"`
	Never     int64 `json:"never"`
}

type ProtocolDeploymentQuery struct {
	NodeID, ProtocolEndpointID uint
	Status                     string
	Offset, Limit              int
}
type ProtocolDeploymentPage struct {
	Items []ProtocolDeploymentRecord
	Total int64
}

type NodeDiagnosticEndpoint struct {
	ID, NodeID uint
	Name       string
	Protocol   string
	Port       int
}

type SubscriptionDeliveryRelation struct {
	NetworkEntryID, NodeGroupID, ProtocolEndpointID uint
	GroupSortOrder, GlobalSortOrder                 int
}

type NodeGroupInventoryQuery struct {
	ID            uint
	Search        string
	Enabled       *bool
	Paged         bool
	Offset, Limit int
}
type NodeGroupInventoryItem struct {
	Group                 NodeGroupRecord
	ProtocolEndpointCount int64
}
type NodeGroupInventoryPage struct {
	Items []NodeGroupInventoryItem
	Total int64
}

type InventoryRepository interface {
	ListNodes(context.Context, NodeInventoryQuery) (NodeInventoryPage, error)
	Node(context.Context, uint) (NodeInventoryItem, error)
	RuntimeNode(context.Context, uint) (NodeRuntimeRecord, error)
	RuntimeNodes(context.Context, []uint) (map[uint]NodeRuntimeRecord, error)
	Endpoint(context.Context, uint) (ProtocolEndpointRecord, error)
	NodeDiagnosticEndpoints(context.Context, uint) ([]NodeDiagnosticEndpoint, error)
	AuthorizedProtocolEndpoints(context.Context, uint, time.Time) ([]ProtocolEndpointRecord, error)
	SubscriptionDeliveryRelations(context.Context, []uint) ([]SubscriptionDeliveryRelation, error)
	ListProtocolEndpoints(context.Context, ProtocolEndpointInventoryQuery) (ProtocolEndpointInventoryPage, error)
	ProtocolEndpoint(context.Context, uint, time.Time) (ProtocolEndpointInventoryItem, error)
	SelectProtocolEndpointIDs(context.Context, ProtocolEndpointInventoryQuery) ([]uint, int64, error)
	ListProtocolDeployments(context.Context, ProtocolDeploymentQuery) (ProtocolDeploymentPage, error)
	ListNodeGroups(context.Context, NodeGroupInventoryQuery) (NodeGroupInventoryPage, error)
	NodeGroup(context.Context, uint) (NodeGroupRecord, error)
	ProtocolUsage(context.Context, []uint, time.Time) (map[uint]ProtocolUsageRecord, error)
}

type Inventory struct{ Repository InventoryRepository }

func (s Inventory) Nodes(ctx context.Context, q NodeInventoryQuery) (NodeInventoryPage, error) {
	return s.Repository.ListNodes(ctx, q)
}
func (s Inventory) Node(ctx context.Context, id uint) (NodeInventoryItem, error) {
	if id == 0 {
		return NodeInventoryItem{}, ErrInventoryNotFound
	}
	return s.Repository.Node(ctx, id)
}
func (s Inventory) RuntimeNode(ctx context.Context, id uint) (NodeRuntimeRecord, error) {
	if id == 0 {
		return NodeRuntimeRecord{}, ErrInventoryNotFound
	}
	return s.Repository.RuntimeNode(ctx, id)
}
func (s Inventory) RuntimeNodes(ctx context.Context, ids []uint) (map[uint]NodeRuntimeRecord, error) {
	if len(ids) == 0 {
		return map[uint]NodeRuntimeRecord{}, nil
	}
	return s.Repository.RuntimeNodes(ctx, ids)
}
func (s Inventory) Endpoint(ctx context.Context, id uint) (ProtocolEndpointRecord, error) {
	if id == 0 {
		return ProtocolEndpointRecord{}, ErrInventoryNotFound
	}
	return s.Repository.Endpoint(ctx, id)
}
func (s Inventory) DiagnosticEndpoints(ctx context.Context, nodeID uint) ([]NodeDiagnosticEndpoint, error) {
	if nodeID == 0 {
		return nil, ErrInventoryNotFound
	}
	return s.Repository.NodeDiagnosticEndpoints(ctx, nodeID)
}
func (s Inventory) AuthorizedProtocolEndpoints(ctx context.Context, userID uint, now time.Time) ([]ProtocolEndpointRecord, error) {
	if userID == 0 {
		return nil, ErrInventoryNotFound
	}
	return s.Repository.AuthorizedProtocolEndpoints(ctx, userID, now)
}
func (s Inventory) DeliveryRelations(ctx context.Context, groupIDs []uint) ([]SubscriptionDeliveryRelation, error) {
	if len(groupIDs) == 0 {
		return []SubscriptionDeliveryRelation{}, nil
	}
	return s.Repository.SubscriptionDeliveryRelations(ctx, groupIDs)
}
func (s Inventory) ProtocolEndpoints(ctx context.Context, q ProtocolEndpointInventoryQuery) (ProtocolEndpointInventoryPage, error) {
	return s.Repository.ListProtocolEndpoints(ctx, q)
}
func (s Inventory) ProtocolEndpoint(ctx context.Context, id uint, now time.Time) (ProtocolEndpointInventoryItem, error) {
	if id == 0 {
		return ProtocolEndpointInventoryItem{}, ErrInventoryNotFound
	}
	return s.Repository.ProtocolEndpoint(ctx, id, now)
}
func (s Inventory) SelectProtocolEndpointIDs(ctx context.Context, q ProtocolEndpointInventoryQuery) ([]uint, int64, error) {
	return s.Repository.SelectProtocolEndpointIDs(ctx, q)
}
func (s Inventory) ProtocolDeployments(ctx context.Context, q ProtocolDeploymentQuery) (ProtocolDeploymentPage, error) {
	return s.Repository.ListProtocolDeployments(ctx, q)
}
func (s Inventory) NodeGroups(ctx context.Context, q NodeGroupInventoryQuery) (NodeGroupInventoryPage, error) {
	return s.Repository.ListNodeGroups(ctx, q)
}
func (s Inventory) NodeGroup(ctx context.Context, id uint) (NodeGroupRecord, error) {
	if id == 0 {
		return NodeGroupRecord{}, ErrInventoryNotFound
	}
	return s.Repository.NodeGroup(ctx, id)
}
func (s Inventory) Usage(ctx context.Context, ids []uint, now time.Time) (map[uint]ProtocolUsageRecord, error) {
	if len(ids) == 0 {
		return map[uint]ProtocolUsageRecord{}, nil
	}
	return s.Repository.ProtocolUsage(ctx, ids, now)
}
