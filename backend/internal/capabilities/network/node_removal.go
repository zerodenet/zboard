package network

import "context"

type NodeDeleteCleanup struct {
	NetworkEntries          int64 `json:"network_entries"`
	ProxyPools              int64 `json:"proxy_pools"`
	ProtocolEndpoints       int64 `json:"protocol_endpoints"`
	ProtocolDeployments     int64 `json:"protocol_deployments"`
	ProtocolCredentials     int64 `json:"protocol_credentials"`
	FlowUsage               int64 `json:"flow_usage"`
	PrincipalFlowCurrents   int64 `json:"principal_flow_currents"`
	PrincipalFlowGeneration int64 `json:"principal_flow_generation"`
	NodeGroupLinks          int64 `json:"node_group_links"`
	CertificateLinks        int64 `json:"certificate_links"`
	ManagedCertificates     int64 `json:"managed_certificates"`
	CertificateOperations   int64 `json:"certificate_operations"`
	ManagedDNSRecords       int64 `json:"managed_dns_records"`
	KernelOperations        int64 `json:"kernel_operations"`
	KernelState             int64 `json:"kernel_state"`
}

type NodeRemoved struct {
	ID                             uint              `json:"id"`
	Deleted                        bool              `json:"deleted"`
	Cleanup                        NodeDeleteCleanup `json:"cleanup"`
	TrafficRecordsRetained         int64             `json:"traffic_records_retained"`
	RemoteZeroRetained             bool              `json:"remote_zero_retained"`
	RemoteZeroStopped              bool              `json:"remote_zero_stopped"`
	RemoteCertificateFilesRetained bool              `json:"remote_certificate_files_retained"`
	ProviderDNSRecordsRetained     bool              `json:"provider_dns_records_retained"`
	RemoteCleanupRunID             string            `json:"remote_cleanup_run_id,omitempty"`
}

type NodeRemovalFacts struct {
	Cleanup                NodeDeleteCleanup
	TrafficRecordsRetained int64
	RemoteCleanupRunID     string
}
type NodeRemovalStore interface {
	RemoveNode(context.Context, uint, uint) (NodeRemovalFacts, error)
}
type NodeRemoval struct{ Store NodeRemovalStore }

func (s NodeRemoval) Remove(ctx context.Context, actor, id uint) (NodeRemoved, error) {
	if actor == 0 {
		return NodeRemoved{}, ErrResourcePermission
	}
	if id == 0 {
		return NodeRemoved{}, ErrResourceNotFound
	}
	facts, err := s.Store.RemoveNode(ctx, actor, id)
	if err != nil {
		return NodeRemoved{}, err
	}
	return NodeRemoved{ID: id, Deleted: true, Cleanup: facts.Cleanup, TrafficRecordsRetained: facts.TrafficRecordsRetained, RemoteZeroRetained: true, RemoteCertificateFilesRetained: true, ProviderDNSRecordsRetained: true, RemoteCleanupRunID: facts.RemoteCleanupRunID}, nil
}
