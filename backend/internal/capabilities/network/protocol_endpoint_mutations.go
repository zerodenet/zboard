package network

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
)

var (
	ErrProtocolEndpointMutationUnavailable = errors.New("protocol endpoint mutation unavailable")
	ErrProtocolEndpointMutationPermission  = errors.New("protocol endpoint mutation requires current administrator")
	ErrProtocolEndpointConflict            = errors.New("protocol endpoint changed")
)

type ProtocolEndpointMutationValidation struct {
	Message string
	Fields  map[string]string
}

func (e *ProtocolEndpointMutationValidation) Error() string { return e.Message }

type ProtocolEndpointRecord struct {
	ID                    uint      `json:"id"`
	NodeID                uint      `json:"node_id"`
	Name                  string    `json:"name"`
	RuntimeKey            string    `json:"-"`
	Protocol              string    `json:"protocol"`
	Address               string    `json:"address"`
	Port                  int       `json:"port"`
	PublicPort            int       `json:"public_port"`
	Cipher                int16     `json:"cipher"`
	ParentProtocolID      *uint     `json:"parent_protocol_id"`
	MultiplierMilli       int64     `json:"multiplier_milli"`
	ManagedPrincipalReady bool      `json:"managed_principal_ready"`
	MieruPrincipalReady   bool      `json:"mieru_principal_ready"`
	ServerConfig          string    `json:"-"`
	ServerCiphertext      string    `json:"-"`
	ClientConfig          string    `json:"client_config"`
	OptionalConfig        string    `json:"optional_config"`
	Tags                  string    `json:"tags"`
	IsActive              bool      `json:"is_active"`
	SortOrder             int       `json:"sort_order"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

type ProtocolEndpointMutationSnapshot struct {
	Endpoint             ProtocolEndpointRecord
	ManagedCertificateID uint
	Version              string
}

type ProtocolEndpointMembershipChange struct {
	NodeGroupID      uint
	ExpectedRevision uint64
	Member           bool
}

type ProtocolEndpointMembership struct {
	NodeGroupID uint   `json:"node_group_id"`
	Name        string `json:"name"`
	Code        string `json:"code"`
	Description string `json:"description"`
	IsEnabled   bool   `json:"is_enabled"`
	Revision    uint64 `json:"revision"`
	SortOrder   int    `json:"sort_order"`
}

type ProtocolEndpointMembershipConflict struct {
	NodeGroupID      uint   `json:"node_group_id"`
	ExpectedRevision uint64 `json:"expected_revision"`
	CurrentRevision  uint64 `json:"current_revision"`
}

type ProtocolEndpointMembershipConflictError struct {
	Conflicts []ProtocolEndpointMembershipConflict
}

func (e *ProtocolEndpointMembershipConflictError) Error() string {
	return "node group revision conflict"
}

type ProtocolEndpointMembershipMutation struct {
	AddedNodeGroupIDs   []uint              `json:"added_node_group_ids,omitempty"`
	RemovedNodeGroupIDs []uint              `json:"removed_node_group_ids,omitempty"`
	AffectedNodeIDs     []uint              `json:"affected_node_ids,omitempty"`
	PublishStatus       string              `json:"publish_status"`
	ReconcileTasks      []jobs.BatchReceipt `json:"reconcile_tasks,omitempty"`
}

type ProtocolEndpointEffect string

const (
	ProtocolEndpointEffectNone                ProtocolEndpointEffect = "none"
	ProtocolEndpointEffectManagement          ProtocolEndpointEffect = "management"
	ProtocolEndpointEffectBilling             ProtocolEndpointEffect = "billing"
	ProtocolEndpointEffectDelivery            ProtocolEndpointEffect = "delivery"
	ProtocolEndpointEffectRuntime             ProtocolEndpointEffect = "runtime"
	ProtocolEndpointEffectCredentialPlacement ProtocolEndpointEffect = "credential_placement"

	ProtocolEndpointPublishNotRequired = "not_required"
	ProtocolEndpointPublishQueued      = "queued"
)

type ProtocolEndpointChangeEffects struct {
	Effect          ProtocolEndpointEffect   `json:"effect"`
	Effects         []ProtocolEndpointEffect `json:"effects"`
	PublishStatus   string                   `json:"publish_status"`
	AffectedNodeIDs []uint                   `json:"affected_node_ids,omitempty"`
}

type ProtocolEndpointMutationRequest struct {
	ID                   uint
	NodeID               uint
	Name                 string
	Protocol             string
	Address              string
	Port                 int
	PublicPort           int
	Cipher               int16
	ParentProtocolID     *uint
	ManagedCertificateID *uint
	MultiplierMilli      int64
	IsActive             *bool
	ServerConfig         string
	ClientConfig         string
	OptionalConfig       string
	Tags                 string
	MembershipChanges    []ProtocolEndpointMembershipChange
	CredentialProtocols  []string
}

type ProtocolEndpointMutationChange struct {
	Endpoint             ProtocolEndpointRecord
	ManagedCertificateID uint
	MembershipChanges    []ProtocolEndpointMembershipChange
	CredentialProtocols  []string
	Effects              ProtocolEndpointChangeEffects
	Now                  time.Time
}

type ProtocolEndpointMutationResult struct {
	ProtocolEndpoint   ProtocolEndpointRecord              `json:"protocol_endpoint"`
	Memberships        []ProtocolEndpointMembership        `json:"node_group_memberships"`
	MembershipMutation *ProtocolEndpointMembershipMutation `json:"node_group_membership,omitempty"`
	ProtocolEndpointChangeEffects
}

type ProtocolEndpointMutationRepository interface {
	LoadProtocolEndpointMutation(context.Context, uint, uint) (ProtocolEndpointMutationSnapshot, error)
	CommitProtocolEndpointMutation(context.Context, uint, *ProtocolEndpointMutationSnapshot, ProtocolEndpointMutationChange) (ProtocolEndpointMutationResult, error)
}

type ProtocolEndpointMutations struct {
	Repository    ProtocolEndpointMutationRepository
	Cipher        ProviderCredentialCipher
	Now           func() time.Time
	NewRuntimeKey func() string
}

func (s ProtocolEndpointMutations) Load(ctx context.Context, actor, id uint) (ProtocolEndpointMutationSnapshot, error) {
	if s.Repository == nil || s.Cipher == nil || actor == 0 || id == 0 {
		return ProtocolEndpointMutationSnapshot{}, ErrProtocolEndpointMutationUnavailable
	}
	snapshot, err := s.Repository.LoadProtocolEndpointMutation(ctx, actor, id)
	if err != nil {
		return ProtocolEndpointMutationSnapshot{}, err
	}
	plain, err := s.Cipher.Decrypt(snapshot.Endpoint.ServerCiphertext)
	if err != nil {
		return ProtocolEndpointMutationSnapshot{}, err
	}
	snapshot.Endpoint.ServerConfig = plain
	return snapshot, nil
}

func (s ProtocolEndpointMutations) Save(ctx context.Context, actor uint, before *ProtocolEndpointMutationSnapshot, request ProtocolEndpointMutationRequest) (ProtocolEndpointMutationResult, error) {
	if s.Repository == nil || s.Cipher == nil || actor == 0 {
		return ProtocolEndpointMutationResult{}, ErrProtocolEndpointMutationUnavailable
	}
	if (before == nil) != (request.ID == 0) || before != nil && before.Endpoint.ID != request.ID {
		return ProtocolEndpointMutationResult{}, ErrProtocolEndpointConflict
	}
	request.Name = strings.TrimSpace(request.Name)
	request.Address = strings.TrimSpace(request.Address)
	request.Protocol = strings.ToLower(strings.TrimSpace(request.Protocol))
	if request.Protocol == "" {
		request.Protocol = "vmess"
	}
	if request.PublicPort == 0 {
		request.PublicPort = request.Port
	}
	if request.ParentProtocolID != nil && *request.ParentProtocolID == 0 {
		request.ParentProtocolID = nil
	}
	if err := validateProtocolEndpointMutationRequest(request); err != nil {
		return ProtocolEndpointMutationResult{}, err
	}
	changes, err := normalizeProtocolEndpointMembershipChanges(request.MembershipChanges, before == nil)
	if err != nil {
		return ProtocolEndpointMutationResult{}, err
	}
	ciphertext, err := s.Cipher.Encrypt(request.ServerConfig)
	if err != nil {
		return ProtocolEndpointMutationResult{}, err
	}
	record := ProtocolEndpointRecord{
		ID: request.ID, NodeID: request.NodeID, Name: request.Name,
		Protocol: request.Protocol, Address: request.Address,
		Port: request.Port, PublicPort: request.PublicPort, Cipher: request.Cipher,
		ParentProtocolID: request.ParentProtocolID, MultiplierMilli: request.MultiplierMilli,
		ServerConfig: request.ServerConfig, ServerCiphertext: ciphertext, ClientConfig: request.ClientConfig,
		OptionalConfig: normalizedEndpointJSON(request.OptionalConfig, "{}"), Tags: normalizedEndpointJSON(request.Tags, "[]"),
	}
	if before == nil {
		record.RuntimeKey = s.runtimeKey()
		if request.IsActive != nil {
			record.IsActive = *request.IsActive
		}
	} else {
		record.RuntimeKey = before.Endpoint.RuntimeKey
		record.IsActive = before.Endpoint.IsActive
		record.SortOrder = before.Endpoint.SortOrder
		record.CreatedAt = before.Endpoint.CreatedAt
		record.ManagedPrincipalReady = before.Endpoint.ManagedPrincipalReady
		record.MieruPrincipalReady = before.Endpoint.MieruPrincipalReady
		if request.IsActive != nil {
			record.IsActive = *request.IsActive
		}
	}
	if record.Protocol != "mieru" {
		record.MieruPrincipalReady = false
	}
	if record.Protocol != "trojan" && record.Protocol != "hysteria2" || before == nil || before.Endpoint.NodeID != record.NodeID || !strings.EqualFold(before.Endpoint.Protocol, record.Protocol) {
		record.ManagedPrincipalReady = false
	}
	managedCertificateID := uint(0)
	if request.ManagedCertificateID != nil {
		managedCertificateID = *request.ManagedCertificateID
	}
	var previous *ProtocolEndpointRecord
	if before != nil {
		previous = &before.Endpoint
	}
	effects := ClassifyProtocolEndpointChange(previous, record, beforeManagedCertificateID(before), managedCertificateID)
	return s.Repository.CommitProtocolEndpointMutation(ctx, actor, before, ProtocolEndpointMutationChange{
		Endpoint: record, ManagedCertificateID: managedCertificateID, MembershipChanges: changes,
		CredentialProtocols: normalizedProtocolSet(request.CredentialProtocols), Effects: effects, Now: s.now(),
	})
}

func validateProtocolEndpointMutationRequest(request ProtocolEndpointMutationRequest) error {
	fields := map[string]string{}
	if request.NodeID == 0 {
		fields["node_id"] = "请选择承载节点。"
	}
	if request.Name == "" || len(request.Name) > 80 {
		fields["name"] = "请输入 1–80 字节的服务名称。"
	}
	if request.Address == "" || len(request.Address) > 255 {
		fields["address"] = "请输入客户端可访问的对外地址。"
	}
	if request.Port < 1 || request.Port > 65535 {
		fields["port"] = "监听端口必须在 1–65535 之间。"
	}
	if request.PublicPort < 1 || request.PublicPort > 65535 {
		fields["public_port"] = "客户端连接端口必须在 1–65535 之间。"
	}
	if request.MultiplierMilli <= 0 || request.MultiplierMilli > 100000 {
		fields["multiplier_milli"] = "流量倍率必须大于 0 且不超过 100。"
	}
	switch request.Protocol {
	case "vmess", "vless", "trojan", "shadowsocks", "hysteria2", "mieru":
	default:
		fields["protocol"] = "请选择受支持的协议类型。"
	}
	if len(fields) > 0 {
		return &ProtocolEndpointMutationValidation{Message: "协议服务校验失败。", Fields: fields}
	}
	return nil
}

func ClassifyProtocolEndpointChange(before *ProtocolEndpointRecord, after ProtocolEndpointRecord, beforeCertificateID, afterCertificateID uint) ProtocolEndpointChangeEffects {
	if before == nil {
		if after.IsActive {
			return ProtocolEndpointChangeEffects{Effect: ProtocolEndpointEffectRuntime, Effects: []ProtocolEndpointEffect{ProtocolEndpointEffectRuntime}, PublishStatus: ProtocolEndpointPublishQueued, AffectedNodeIDs: []uint{after.NodeID}}
		}
		return ProtocolEndpointChangeEffects{Effect: ProtocolEndpointEffectManagement, Effects: []ProtocolEndpointEffect{ProtocolEndpointEffectManagement}, PublishStatus: ProtocolEndpointPublishNotRequired}
	}
	changed := map[ProtocolEndpointEffect]bool{}
	if canonicalEndpointJSON(before.Tags, "[]") != canonicalEndpointJSON(after.Tags, "[]") {
		changed[ProtocolEndpointEffectManagement] = true
	}
	if before.MultiplierMilli != after.MultiplierMilli {
		changed[ProtocolEndpointEffectBilling] = true
	}
	if strings.TrimSpace(before.Name) != strings.TrimSpace(after.Name) || strings.TrimSpace(before.Address) != strings.TrimSpace(after.Address) || before.PublicPort != after.PublicPort || canonicalEndpointJSON(before.ClientConfig, "{}") != canonicalEndpointJSON(after.ClientConfig, "{}") || before.SortOrder != after.SortOrder {
		changed[ProtocolEndpointEffectDelivery] = true
	}
	if !strings.EqualFold(strings.TrimSpace(before.Protocol), strings.TrimSpace(after.Protocol)) || before.Port != after.Port || before.Cipher != after.Cipher || !sameEndpointOptionalUint(before.ParentProtocolID, after.ParentProtocolID) || before.IsActive != after.IsActive || canonicalEndpointJSON(before.ServerConfig, "{}") != canonicalEndpointJSON(after.ServerConfig, "{}") || canonicalEndpointJSON(before.OptionalConfig, "{}") != canonicalEndpointJSON(after.OptionalConfig, "{}") || beforeCertificateID != afterCertificateID {
		changed[ProtocolEndpointEffectRuntime] = true
	}
	if before.NodeID != after.NodeID {
		changed[ProtocolEndpointEffectCredentialPlacement] = true
	}
	ordered := []ProtocolEndpointEffect{ProtocolEndpointEffectManagement, ProtocolEndpointEffectBilling, ProtocolEndpointEffectDelivery, ProtocolEndpointEffectRuntime, ProtocolEndpointEffectCredentialPlacement}
	result := ProtocolEndpointChangeEffects{Effect: ProtocolEndpointEffectNone, PublishStatus: ProtocolEndpointPublishNotRequired}
	for _, effect := range ordered {
		if changed[effect] {
			result.Effects = append(result.Effects, effect)
			result.Effect = effect
		}
	}
	if changed[ProtocolEndpointEffectRuntime] || changed[ProtocolEndpointEffectCredentialPlacement] {
		result.AffectedNodeIDs = protocolEndpointAffectedNodes(*before, after)
		if len(result.AffectedNodeIDs) > 0 {
			result.PublishStatus = ProtocolEndpointPublishQueued
		}
	}
	return result
}

func normalizeProtocolEndpointMembershipChanges(changes []ProtocolEndpointMembershipChange, creating bool) ([]ProtocolEndpointMembershipChange, error) {
	if len(changes) > 100 {
		return nil, endpointMutationValidation("节点组关联校验失败。", "单次最多调整 100 个节点组关联。")
	}
	seen := make(map[uint]bool, len(changes))
	result := append([]ProtocolEndpointMembershipChange(nil), changes...)
	for _, change := range result {
		if change.NodeGroupID == 0 {
			return nil, endpointMutationValidation("节点组关联校验失败。", "节点组 ID 必须为正整数。")
		}
		if change.ExpectedRevision == 0 {
			return nil, endpointMutationValidation("节点组关联校验失败。", "节点组缺少版本信息，请重新加载后再保存。")
		}
		if creating && !change.Member {
			return nil, endpointMutationValidation("节点组关联校验失败。", "创建协议服务时只能添加节点组关联。")
		}
		if seen[change.NodeGroupID] {
			return nil, endpointMutationValidation("节点组关联校验失败。", "节点组出现重复关联命令。")
		}
		seen[change.NodeGroupID] = true
	}
	sort.Slice(result, func(i, j int) bool { return result[i].NodeGroupID < result[j].NodeGroupID })
	return result, nil
}

func endpointMutationValidation(message, field string) error {
	return &ProtocolEndpointMutationValidation{Message: message, Fields: map[string]string{"node_group_membership_changes": field}}
}

func protocolEndpointAffectedNodes(before, after ProtocolEndpointRecord) []uint {
	if before.NodeID == after.NodeID {
		if before.IsActive || after.IsActive {
			return uniqueProtocolEndpointIDs(after.NodeID)
		}
		return nil
	}
	values := make([]uint, 0, 2)
	if before.IsActive {
		values = append(values, before.NodeID)
	}
	if after.IsActive {
		values = append(values, after.NodeID)
	}
	if len(values) == 0 {
		return nil
	}
	return uniqueProtocolEndpointIDs(values...)
}

func DirectProtocolEndpointPublishNodeIDs(runtimeNodeIDs, membershipNodeIDs []uint) []uint {
	membership := make(map[uint]bool, len(membershipNodeIDs))
	for _, id := range membershipNodeIDs {
		membership[id] = id != 0
	}
	seen := map[uint]bool{}
	result := make([]uint, 0, len(runtimeNodeIDs))
	for _, id := range runtimeNodeIDs {
		if id != 0 && !membership[id] && !seen[id] {
			seen[id] = true
			result = append(result, id)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func canonicalEndpointJSON(value, fallback string) string {
	value = normalizedEndpointJSON(value, fallback)
	var decoded any
	if json.Unmarshal([]byte(value), &decoded) != nil {
		return value
	}
	encoded, err := json.Marshal(decoded)
	if err != nil {
		return value
	}
	return string(encoded)
}

func normalizedEndpointJSON(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func sameEndpointOptionalUint(left, right *uint) bool {
	if left == nil || *left == 0 {
		return right == nil || *right == 0
	}
	return right != nil && *left == *right
}

func uniqueProtocolEndpointIDs(values ...uint) []uint {
	seen := map[uint]bool{}
	result := make([]uint, 0, len(values))
	for _, value := range values {
		if value != 0 && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func beforeManagedCertificateID(before *ProtocolEndpointMutationSnapshot) uint {
	if before == nil {
		return 0
	}
	return before.ManagedCertificateID
}

func (s ProtocolEndpointMutations) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s ProtocolEndpointMutations) runtimeKey() string {
	if s.NewRuntimeKey != nil {
		return s.NewRuntimeKey()
	}
	return uuid.NewString()
}
