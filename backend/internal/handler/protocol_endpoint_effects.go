package handler

import (
	"encoding/json"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/model"
)

type protocolEndpointEffect string

const (
	protocolEndpointEffectNone                protocolEndpointEffect = "none"
	protocolEndpointEffectManagement          protocolEndpointEffect = "management"
	protocolEndpointEffectBilling             protocolEndpointEffect = "billing"
	protocolEndpointEffectDelivery            protocolEndpointEffect = "delivery"
	protocolEndpointEffectRuntime             protocolEndpointEffect = "runtime"
	protocolEndpointEffectCredentialPlacement protocolEndpointEffect = "credential_placement"

	protocolEndpointPublishNotRequired = "not_required"
	protocolEndpointPublishQueued      = "queued"

	// Runtime compilation is ordered by endpoint identity. SortOrder belongs to subscription delivery.
	protocolEndpointRuntimeOrder = "id asc"
)

type protocolEndpointEffectSnapshot struct {
	NodeID               uint
	Name                 string
	Protocol             string
	Address              string
	Port                 int
	PublicPort           int
	Cipher               int16
	ParentProtocolID     *uint
	MultiplierMilli      int64
	ServerConfig         string
	ClientConfig         string
	OptionalConfig       string
	Tags                 string
	IsActive             bool
	SortOrder            int
	ManagedCertificateID uint
}

type protocolEndpointChangeEffects struct {
	Effect          protocolEndpointEffect   `json:"effect"`
	Effects         []protocolEndpointEffect `json:"effects"`
	PublishStatus   string                   `json:"publish_status"`
	AffectedNodeIDs []uint                   `json:"affected_node_ids,omitempty"`
}

type protocolEndpointMutationResponse struct {
	ProtocolEndpoint model.ProtocolEndpoint `json:"protocol_endpoint"`
	protocolEndpointChangeEffects
}

func classifyProtocolEndpointChange(before *protocolEndpointEffectSnapshot, after protocolEndpointEffectSnapshot) protocolEndpointChangeEffects {
	if before == nil {
		if after.IsActive {
			return protocolEndpointChangeEffects{
				Effect:          protocolEndpointEffectRuntime,
				Effects:         []protocolEndpointEffect{protocolEndpointEffectRuntime},
				PublishStatus:   protocolEndpointPublishQueued,
				AffectedNodeIDs: []uint{after.NodeID},
			}
		}
		return protocolEndpointChangeEffects{
			Effect:        protocolEndpointEffectManagement,
			Effects:       []protocolEndpointEffect{protocolEndpointEffectManagement},
			PublishStatus: protocolEndpointPublishNotRequired,
		}
	}

	changed := make(map[protocolEndpointEffect]bool)
	if canonicalJSON(before.Tags, "[]") != canonicalJSON(after.Tags, "[]") {
		changed[protocolEndpointEffectManagement] = true
	}
	if before.MultiplierMilli != after.MultiplierMilli {
		changed[protocolEndpointEffectBilling] = true
	}
	if strings.TrimSpace(before.Name) != strings.TrimSpace(after.Name) ||
		strings.TrimSpace(before.Address) != strings.TrimSpace(after.Address) ||
		before.PublicPort != after.PublicPort ||
		canonicalJSON(before.ClientConfig, "{}") != canonicalJSON(after.ClientConfig, "{}") ||
		before.SortOrder != after.SortOrder {
		changed[protocolEndpointEffectDelivery] = true
	}
	if !strings.EqualFold(strings.TrimSpace(before.Protocol), strings.TrimSpace(after.Protocol)) ||
		before.Port != after.Port ||
		before.Cipher != after.Cipher ||
		!sameOptionalUint(before.ParentProtocolID, after.ParentProtocolID) ||
		before.IsActive != after.IsActive ||
		canonicalJSON(before.ServerConfig, "{}") != canonicalJSON(after.ServerConfig, "{}") ||
		canonicalJSON(before.OptionalConfig, "{}") != canonicalJSON(after.OptionalConfig, "{}") ||
		before.ManagedCertificateID != after.ManagedCertificateID {
		changed[protocolEndpointEffectRuntime] = true
	}
	if before.NodeID != after.NodeID {
		changed[protocolEndpointEffectCredentialPlacement] = true
	}

	ordered := []protocolEndpointEffect{
		protocolEndpointEffectManagement,
		protocolEndpointEffectBilling,
		protocolEndpointEffectDelivery,
		protocolEndpointEffectRuntime,
		protocolEndpointEffectCredentialPlacement,
	}
	effects := make([]protocolEndpointEffect, 0, len(ordered))
	primary := protocolEndpointEffectNone
	for _, effect := range ordered {
		if !changed[effect] {
			continue
		}
		effects = append(effects, effect)
		primary = effect
	}

	result := protocolEndpointChangeEffects{
		Effect:        primary,
		Effects:       effects,
		PublishStatus: protocolEndpointPublishNotRequired,
	}
	if changed[protocolEndpointEffectRuntime] || changed[protocolEndpointEffectCredentialPlacement] {
		result.PublishStatus = protocolEndpointPublishQueued
		if changed[protocolEndpointEffectCredentialPlacement] {
			result.AffectedNodeIDs = uniqueNodeIDs(before.NodeID, after.NodeID)
		} else {
			result.AffectedNodeIDs = uniqueNodeIDs(after.NodeID)
		}
	}
	return result
}

func canonicalJSON(value, fallback string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		trimmed = fallback
	}
	var decoded interface{}
	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
		return trimmed
	}
	normalized, err := json.Marshal(decoded)
	if err != nil {
		return trimmed
	}
	return string(normalized)
}

func sameOptionalUint(left, right *uint) bool {
	if left == nil || *left == 0 {
		return right == nil || *right == 0
	}
	return right != nil && *left == *right
}

func uniqueNodeIDs(values ...uint) []uint {
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
