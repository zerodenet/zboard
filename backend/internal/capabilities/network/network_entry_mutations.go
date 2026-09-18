package network

import (
	"context"
	"errors"
	"net"
	"sort"
	"strings"
	"time"
)

var (
	ErrNetworkEntryMutationUnavailable = errors.New("network entry mutation capability unavailable")
	ErrNetworkEntryPermission          = errors.New("network entry mutation requires current administrator")
	ErrNetworkEntryNotFound            = errors.New("network entry not found")
	ErrNetworkEntryConflict            = errors.New("network entry changed")
)

type NetworkEntryMutationValidation struct{ Message string }

func (e *NetworkEntryMutationValidation) Error() string { return e.Message }

type NetworkEntryRecord struct {
	ID                uint      `json:"id"`
	ProxyPoolID       *uint     `json:"proxy_pool_id"`
	DeliverySortOrder *int      `json:"delivery_sort_order,omitempty"`
	Network           string    `json:"network"`
	Name              string    `json:"name"`
	NodeID            uint      `json:"node_id"`
	EndpointID        uint      `json:"endpoint_id"`
	Address           string    `json:"address"`
	Port              int       `json:"port"`
	PublicPort        int       `json:"public_port"`
	Enabled           bool      `json:"enabled"`
	Revision          uint64    `json:"revision"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type NetworkEntryMutationSnapshot struct {
	Entry          NetworkEntryRecord
	PathCiphertext string
}

type NetworkEntryProxyPoolSnapshot struct {
	ID               uint
	NodeID           uint
	Revision         uint64
	ConfigCiphertext string
}

type NetworkEntryMembershipChange struct {
	NodeGroupID      uint
	ExpectedRevision uint64
	Member           bool
}

type NetworkEntryMutationRequest struct {
	ID                  uint
	ExpectedRevision    uint64
	NodeID              uint
	EndpointID          uint
	Name                string
	Network             string
	Address             string
	Port                int
	PublicPort          int
	Enabled             bool
	ReplaceProxyPool    bool
	ProxyPoolID         *uint
	ReplacePath         bool
	Path                string
	MembershipChanges   []NetworkEntryMembershipChange
	CredentialProtocols []string
}

type NetworkEntryMutationChange struct {
	Entry                NetworkEntryRecord
	PathCiphertext       string
	Pool                 *NetworkEntryProxyPoolSnapshot
	PoolSupportsDatagram bool
	PoolDatagramError    string
	MembershipChanges    []NetworkEntryMembershipChange
	CredentialProtocols  []string
	Now                  time.Time
}

type NetworkEntryMutationResult struct {
	Entry            NetworkEntryRecord `json:"entry"`
	HasPath          bool               `json:"has_path"`
	ReconcileTaskIDs []uint             `json:"reconcile_task_ids,omitempty"`
}

type NetworkEntryMutationRepository interface {
	LoadNetworkEntryMutation(context.Context, uint, uint) (NetworkEntryMutationSnapshot, error)
	LoadNetworkEntryProxyPool(context.Context, uint, uint) (NetworkEntryProxyPoolSnapshot, error)
	CommitNetworkEntryMutation(context.Context, uint, *NetworkEntryMutationSnapshot, NetworkEntryMutationChange) (NetworkEntryMutationResult, error)
}

type NetworkEntryMutations struct {
	Repository NetworkEntryMutationRepository
	Cipher     ProviderCredentialCipher
	Inspector  ProxyPoolConfigurationInspector
	Now        func() time.Time
}

func (s NetworkEntryMutations) CheckRevision(ctx context.Context, actor, id uint, expected uint64) error {
	if s.Repository == nil || actor == 0 || id == 0 {
		return ErrNetworkEntryMutationUnavailable
	}
	snapshot, err := s.Repository.LoadNetworkEntryMutation(ctx, actor, id)
	if err != nil {
		return err
	}
	if snapshot.Entry.Revision != expected {
		return ErrNetworkEntryConflict
	}
	return nil
}

func (s NetworkEntryMutations) Save(ctx context.Context, actor uint, request NetworkEntryMutationRequest) (NetworkEntryMutationResult, error) {
	if s.Repository == nil || s.Cipher == nil || s.Inspector == nil || actor == 0 {
		return NetworkEntryMutationResult{}, ErrNetworkEntryMutationUnavailable
	}
	request.Name = strings.TrimSpace(request.Name)
	request.Address = strings.TrimSpace(request.Address)
	if request.Network == "" {
		request.Network = "tcp_udp"
	}
	if request.PublicPort == 0 {
		request.PublicPort = request.Port
	}
	if err := validateNetworkEntryMutationRequest(request); err != nil {
		return NetworkEntryMutationResult{}, err
	}
	var before *NetworkEntryMutationSnapshot
	if request.ID != 0 {
		snapshot, err := s.Repository.LoadNetworkEntryMutation(ctx, actor, request.ID)
		if err != nil {
			return NetworkEntryMutationResult{}, err
		}
		before = &snapshot
		if snapshot.Entry.Revision != request.ExpectedRevision {
			return NetworkEntryMutationResult{}, ErrNetworkEntryConflict
		}
	}

	poolID := request.ProxyPoolID
	pathCiphertext := ""
	if before != nil {
		if !request.ReplaceProxyPool {
			poolID = before.Entry.ProxyPoolID
		}
		pathCiphertext = before.PathCiphertext
	}
	if request.ReplacePath {
		pathCiphertext = ""
		plain := strings.TrimSpace(request.Path)
		if plain != "" {
			if _, err := s.Inspector.InspectProxyPoolConfiguration(ctx, plain, false); err != nil {
				return NetworkEntryMutationResult{}, err
			}
			ciphertext, err := s.Cipher.Encrypt(plain)
			if err != nil {
				return NetworkEntryMutationResult{}, err
			}
			pathCiphertext = ciphertext
		}
	}
	if poolID != nil && *poolID == 0 {
		poolID = nil
	}
	if poolID != nil {
		if request.ReplacePath && pathCiphertext != "" {
			return NetworkEntryMutationResult{}, &NetworkEntryMutationValidation{Message: "共享代理池和独立代理路径只能选择一个"}
		}
		pathCiphertext = ""
	}

	change := NetworkEntryMutationChange{
		Entry: NetworkEntryRecord{
			ID: request.ID, ProxyPoolID: poolID, Network: request.Network, Name: request.Name,
			NodeID: request.NodeID, EndpointID: request.EndpointID, Address: request.Address,
			Port: request.Port, PublicPort: request.PublicPort, Enabled: request.Enabled,
		},
		PathCiphertext: pathCiphertext, MembershipChanges: append([]NetworkEntryMembershipChange(nil), request.MembershipChanges...),
		CredentialProtocols: normalizedProtocolSet(request.CredentialProtocols), Now: s.now(),
	}
	sort.Slice(change.MembershipChanges, func(i, j int) bool {
		return change.MembershipChanges[i].NodeGroupID < change.MembershipChanges[j].NodeGroupID
	})
	if before != nil {
		change.Entry.DeliverySortOrder = before.Entry.DeliverySortOrder
		change.Entry.CreatedAt = before.Entry.CreatedAt
	}
	if poolID != nil {
		pool, err := s.Repository.LoadNetworkEntryProxyPool(ctx, actor, *poolID)
		if err != nil {
			return NetworkEntryMutationResult{}, err
		}
		plain, err := s.Cipher.Decrypt(pool.ConfigCiphertext)
		if err != nil {
			return NetworkEntryMutationResult{}, errors.New("代理池配置无法解密")
		}
		facts, err := s.Inspector.InspectProxyPoolConfiguration(ctx, plain, false)
		if err != nil {
			return NetworkEntryMutationResult{}, err
		}
		change.Pool = &pool
		change.PoolSupportsDatagram, change.PoolDatagramError = facts.SupportsDatagram, facts.DatagramError
	} else if request.Network != "tcp" && pathCiphertext != "" {
		plain, err := s.Cipher.Decrypt(pathCiphertext)
		if err != nil {
			return NetworkEntryMutationResult{}, errors.New("代理路径无法解密")
		}
		facts, err := s.Inspector.InspectProxyPoolConfiguration(ctx, plain, false)
		if err != nil {
			return NetworkEntryMutationResult{}, err
		}
		if !facts.SupportsDatagram {
			message := strings.TrimSpace(facts.DatagramError)
			if message == "" {
				message = "代理路径不支持 UDP 转发"
			}
			return NetworkEntryMutationResult{}, &NetworkEntryMutationValidation{Message: message}
		}
	}
	return s.Repository.CommitNetworkEntryMutation(ctx, actor, before, change)
}

func validateNetworkEntryMutationRequest(request NetworkEntryMutationRequest) error {
	if request.Network != "tcp" && request.Network != "tcp_udp" {
		return &NetworkEntryMutationValidation{Message: "请选择 TCP 或 TCP/UDP 转发"}
	}
	if request.Name == "" || len(request.Name) > 80 {
		return &NetworkEntryMutationValidation{Message: "请输入 1–80 字节的入口名称"}
	}
	if !networkEntryAddressValid(request.Address) {
		return &NetworkEntryMutationValidation{Message: "请输入客户端连接 A 的 IP 或域名，不包含端口或 URL"}
	}
	if request.Port < 1 || request.Port > 65535 || request.PublicPort < 1 || request.PublicPort > 65535 {
		return &NetworkEntryMutationValidation{Message: "监听端口和对外端口必须为 1–65535"}
	}
	if request.NodeID == 0 || request.EndpointID == 0 {
		return &NetworkEntryMutationValidation{Message: "请选择入口节点 A 和 B 的落地协议"}
	}
	if len(request.MembershipChanges) > 100 {
		return &NetworkEntryMutationValidation{Message: "单次最多调整 100 个节点组关联"}
	}
	seen := map[uint]bool{}
	for _, item := range request.MembershipChanges {
		if item.NodeGroupID == 0 || item.ExpectedRevision == 0 || seen[item.NodeGroupID] {
			return &NetworkEntryMutationValidation{Message: "节点组关联缺少有效 ID、版本或包含重复命令"}
		}
		if request.ID == 0 && !item.Member {
			return &NetworkEntryMutationValidation{Message: "创建前置服务时只能添加节点组关联"}
		}
		seen[item.NodeGroupID] = true
	}
	return nil
}

func networkEntryAddressValid(value string) bool {
	if net.ParseIP(value) != nil {
		return true
	}
	if len(value) == 0 || len(value) > 253 {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-') {
				return false
			}
		}
	}
	return true
}

func normalizedProtocolSet(values []string) []string {
	set := map[string]bool{}
	for _, value := range values {
		if value = strings.ToLower(strings.TrimSpace(value)); value != "" {
			set[value] = true
		}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func (s NetworkEntryMutations) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
