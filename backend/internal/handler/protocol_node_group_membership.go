package handler

import (
	"fmt"
	"sort"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type protocolEndpointNodeGroupMembershipChange struct {
	NodeGroupID      uint   `json:"node_group_id"`
	ExpectedRevision uint64 `json:"expected_revision"`
	Member           bool   `json:"member"`
}

type protocolEndpointNodeGroupMembership struct {
	NodeGroupID uint   `json:"node_group_id" gorm:"column:node_group_id"`
	Name        string `json:"name" gorm:"column:name"`
	Code        string `json:"code" gorm:"column:code"`
	Description string `json:"description" gorm:"column:description"`
	IsEnabled   bool   `json:"is_enabled" gorm:"column:is_enabled"`
	Revision    uint64 `json:"revision" gorm:"column:revision"`
	SortOrder   int    `json:"sort_order" gorm:"column:sort_order"`
}

func normalizeProtocolEndpointNodeGroupMembershipChanges(changes []protocolEndpointNodeGroupMembershipChange, creating bool) ([]protocolEndpointNodeGroupMembershipChange, error) {
	if len(changes) == 0 {
		return nil, nil
	}
	if len(changes) > 100 {
		return nil, validationError("节点组关联校验失败。", map[string]string{
			"node_group_membership_changes": "单次最多调整 100 个节点组关联。",
		})
	}
	seen := make(map[uint]struct{}, len(changes))
	normalized := make([]protocolEndpointNodeGroupMembershipChange, 0, len(changes))
	for _, change := range changes {
		if change.NodeGroupID == 0 {
			return nil, validationError("节点组关联校验失败。", map[string]string{
				"node_group_membership_changes": "节点组 ID 必须为正整数。",
			})
		}
		if change.ExpectedRevision == 0 {
			return nil, validationError("节点组关联校验失败。", map[string]string{
				"node_group_membership_changes": fmt.Sprintf("节点组 #%d 缺少版本信息，请重新加载后再保存。", change.NodeGroupID),
			})
		}
		if creating && !change.Member {
			return nil, validationError("节点组关联校验失败。", map[string]string{
				"node_group_membership_changes": "创建协议服务时只能添加节点组关联。",
			})
		}
		if _, exists := seen[change.NodeGroupID]; exists {
			return nil, validationError("节点组关联校验失败。", map[string]string{
				"node_group_membership_changes": fmt.Sprintf("节点组 #%d 出现重复关联命令。", change.NodeGroupID),
			})
		}
		seen[change.NodeGroupID] = struct{}{}
		normalized = append(normalized, change)
	}
	sort.Slice(normalized, func(i, j int) bool { return normalized[i].NodeGroupID < normalized[j].NodeGroupID })
	return normalized, nil
}

func loadProtocolEndpointNodeGroupMemberships(tx *gorm.DB, endpointID uint) ([]protocolEndpointNodeGroupMembership, error) {
	if endpointID == 0 {
		return []protocolEndpointNodeGroupMembership{}, nil
	}
	var memberships []protocolEndpointNodeGroupMembership
	err := tx.Table("node_group_endpoints AS membership").
		Select("membership.node_group_id, node_groups.name, node_groups.code, node_groups.description, node_groups.is_enabled, node_groups.revision, membership.sort_order").
		Joins("JOIN node_groups ON node_groups.id = membership.node_group_id").
		Where("membership.protocol_endpoint_id = ?", endpointID).
		Order("node_groups.name asc, node_groups.id asc").
		Scan(&memberships).Error
	if memberships == nil {
		memberships = []protocolEndpointNodeGroupMembership{}
	}
	return memberships, err
}

func validateNodeGroupMembershipAvailability(tx *gorm.DB, group model.NodeGroup) error {
	var activeEndpointCount int64
	if err := credentialMemberships(tx).
		Joins("JOIN protocol_endpoints ON protocol_endpoints.id = node_group_endpoints.protocol_endpoint_id").
		Where("node_group_endpoints.node_group_id = ? AND protocol_endpoints.is_active = ?", group.ID, true).
		Count(&activeEndpointCount).Error; err != nil {
		return err
	}
	if activeEndpointCount > 0 {
		return nil
	}
	// A group may describe entry access without granting its landing protocol.
	var entries int64
	if err := tx.Model(&model.NodeGroupNetworkEntry{}).Joins("JOIN network_entries ON network_entries.id = node_group_network_entries.network_entry_id").Where("node_group_id = ? AND network_entries.enabled = ?", group.ID, true).Count(&entries).Error; err != nil {
		return err
	}
	if entries > 0 {
		return nil
	}

	if group.IsEnabled {
		return validationError("节点组关联校验失败。", map[string]string{
			"node_group_membership_changes": fmt.Sprintf("节点组“%s”已启用，必须至少保留一个可用协议服务。", group.Name),
		})
	}
	var activePlanCount int64
	if err := tx.Model(&model.Plan{}).Where("node_group_id = ? AND is_active = ?", group.ID, true).Count(&activePlanCount).Error; err != nil {
		return err
	}
	if activePlanCount > 0 {
		return validationError("节点组关联校验失败。", map[string]string{
			"node_group_membership_changes": fmt.Sprintf("节点组“%s”仍被已发布套餐使用，必须至少保留一个可用协议服务。", group.Name),
		})
	}
	return nil
}

func protocolEndpointDirectPublishNodeIDs(runtimeNodeIDs, membershipNodeIDs []uint) []uint {
	membershipNodes := make(map[uint]struct{}, len(membershipNodeIDs))
	for _, nodeID := range membershipNodeIDs {
		if nodeID > 0 {
			membershipNodes[nodeID] = struct{}{}
		}
	}
	direct := make([]uint, 0, len(runtimeNodeIDs))
	seen := make(map[uint]struct{}, len(runtimeNodeIDs))
	for _, nodeID := range runtimeNodeIDs {
		if nodeID == 0 {
			continue
		}
		if _, sequencedAfterCredentialReconcile := membershipNodes[nodeID]; sequencedAfterCredentialReconcile {
			continue
		}
		if _, exists := seen[nodeID]; exists {
			continue
		}
		seen[nodeID] = struct{}{}
		direct = append(direct, nodeID)
	}
	return direct
}

func nodeGroupMembershipChangedEndpointIDs(existing []model.NodeGroupEndpoint, desired []uint) []uint {
	desiredSet := make(map[uint]struct{}, len(desired))
	for _, id := range desired {
		desiredSet[id] = struct{}{}
	}
	existingSet := make(map[uint]struct{}, len(existing))
	changed := make([]uint, 0)
	for _, link := range existing {
		existingSet[link.ProtocolEndpointID] = struct{}{}
		if _, keep := desiredSet[link.ProtocolEndpointID]; !keep {
			changed = append(changed, link.ProtocolEndpointID)
		}
	}
	for _, id := range desired {
		if _, exists := existingSet[id]; !exists {
			changed = append(changed, id)
		}
	}
	return uniqueUintIDs(changed)
}
