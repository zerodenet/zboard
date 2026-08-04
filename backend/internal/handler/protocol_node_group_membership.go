package handler

import (
	"fmt"
	"sort"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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

type protocolEndpointNodeGroupRevisionConflict struct {
	NodeGroupID      uint   `json:"node_group_id"`
	ExpectedRevision uint64 `json:"expected_revision"`
	CurrentRevision  uint64 `json:"current_revision"`
}

type protocolEndpointNodeGroupRevisionConflictError struct {
	Conflicts []protocolEndpointNodeGroupRevisionConflict
}

func (err *protocolEndpointNodeGroupRevisionConflictError) Error() string {
	return "node group revision conflict"
}

type protocolEndpointNodeGroupMutationResult struct {
	AddedNodeGroupIDs   []uint       `json:"added_node_group_ids,omitempty"`
	RemovedNodeGroupIDs []uint       `json:"removed_node_group_ids,omitempty"`
	AffectedNodeIDs     []uint       `json:"affected_node_ids,omitempty"`
	PublishStatus       string       `json:"publish_status"`
	ReconcileTasks      []model.Task `json:"reconcile_tasks,omitempty"`
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

func protocolEndpointNodeGroupRemovalSet(changes []protocolEndpointNodeGroupMembershipChange) map[uint]struct{} {
	removed := make(map[uint]struct{})
	for _, change := range changes {
		if !change.Member {
			removed[change.NodeGroupID] = struct{}{}
		}
	}
	return removed
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

func (h *handlers) applyProtocolEndpointNodeGroupMembershipChanges(tx *gorm.DB, claims authClaims, endpoint model.ProtocolEndpoint, changes []protocolEndpointNodeGroupMembershipChange) (*protocolEndpointNodeGroupMutationResult, error) {
	if len(changes) == 0 {
		return nil, nil
	}
	groupIDs := make([]uint, 0, len(changes))
	for _, change := range changes {
		groupIDs = append(groupIDs, change.NodeGroupID)
	}
	var groups []model.NodeGroup
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id IN ?", groupIDs).
		Order("id asc").
		Find(&groups).Error; err != nil {
		return nil, err
	}
	groupByID := make(map[uint]*model.NodeGroup, len(groups))
	for index := range groups {
		groupByID[groups[index].ID] = &groups[index]
	}
	for _, groupID := range groupIDs {
		if groupByID[groupID] == nil {
			return nil, validationError("节点组关联校验失败。", map[string]string{
				"node_group_membership_changes": fmt.Sprintf("节点组 #%d 不存在，请重新选择。", groupID),
			})
		}
	}

	conflicts := make([]protocolEndpointNodeGroupRevisionConflict, 0)
	for _, change := range changes {
		group := groupByID[change.NodeGroupID]
		if group.Revision != change.ExpectedRevision {
			conflicts = append(conflicts, protocolEndpointNodeGroupRevisionConflict{
				NodeGroupID: change.NodeGroupID, ExpectedRevision: change.ExpectedRevision, CurrentRevision: group.Revision,
			})
		}
	}
	if len(conflicts) > 0 {
		return nil, &protocolEndpointNodeGroupRevisionConflictError{Conflicts: conflicts}
	}

	var existingLinks []model.NodeGroupEndpoint
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("protocol_endpoint_id = ? AND node_group_id IN ?", endpoint.ID, groupIDs).
		Find(&existingLinks).Error; err != nil {
		return nil, err
	}
	existingByGroup := make(map[uint]model.NodeGroupEndpoint, len(existingLinks))
	for _, link := range existingLinks {
		existingByGroup[link.NodeGroupID] = link
	}

	result := &protocolEndpointNodeGroupMutationResult{PublishStatus: protocolEndpointPublishNotRequired}
	affectedNodes := make(map[uint]struct{})
	for _, change := range changes {
		group := groupByID[change.NodeGroupID]
		link, exists := existingByGroup[group.ID]
		if change.Member == exists {
			continue
		}
		if change.Member {
			if !endpoint.IsActive {
				return nil, validationError("节点组关联校验失败。", map[string]string{
					"node_group_membership_changes": "只能将已启用的协议服务加入节点组。",
				})
			}
			var lastSortOrder int
			if err := tx.Model(&model.NodeGroupEndpoint{}).
				Select("COALESCE(MAX(sort_order), -1) AS max_sort_order").
				Where("node_group_id = ?", group.ID).
				Row().Scan(&lastSortOrder); err != nil {
				return nil, err
			}
			link = model.NodeGroupEndpoint{NodeGroupID: group.ID, ProtocolEndpointID: endpoint.ID, SortOrder: lastSortOrder + 1}
			if err := tx.Create(&link).Error; err != nil {
				return nil, err
			}
			result.AddedNodeGroupIDs = append(result.AddedNodeGroupIDs, group.ID)
		} else {
			if err := tx.Delete(&model.NodeGroupEndpoint{}, link.ID).Error; err != nil {
				return nil, err
			}
			result.RemovedNodeGroupIDs = append(result.RemovedNodeGroupIDs, group.ID)
		}

		if err := validateNodeGroupMembershipAvailability(tx, *group); err != nil {
			return nil, err
		}
		group.Revision++
		if err := tx.Model(&model.NodeGroup{}).Where("id = ?", group.ID).Update("revision", group.Revision).Error; err != nil {
			return nil, err
		}
		targets, err := h.nodeGroupCredentialPublishTargets(tx, group.ID, []uint{endpoint.ID})
		if err != nil {
			return nil, err
		}
		task, items, err := prepareNodeGroupReconcileTask(claims, group.ID, group.Revision, targets)
		if err != nil {
			return nil, err
		}
		if err := persistAdminTaskRecords(tx, claims, &task, items); err != nil {
			return nil, err
		}
		result.ReconcileTasks = append(result.ReconcileTasks, task)
		for _, target := range targets {
			affectedNodes[target.NodeID] = struct{}{}
		}
		detail := fmt.Sprintf("endpoint=%d member=%t revision=%d", endpoint.ID, change.Member, group.Revision)
		if err := createAuditLog(tx, claims, "node_group.membership.update", fmt.Sprintf("node_group:%d", group.ID), detail); err != nil {
			return nil, err
		}
	}

	if len(result.AddedNodeGroupIDs) == 0 && len(result.RemovedNodeGroupIDs) == 0 {
		return nil, nil
	}
	for nodeID := range affectedNodes {
		result.AffectedNodeIDs = append(result.AffectedNodeIDs, nodeID)
	}
	sort.Slice(result.AffectedNodeIDs, func(i, j int) bool { return result.AffectedNodeIDs[i] < result.AffectedNodeIDs[j] })
	if len(result.AffectedNodeIDs) > 0 {
		result.PublishStatus = protocolEndpointPublishQueued
	}
	return result, nil
}

func validateNodeGroupMembershipAvailability(tx *gorm.DB, group model.NodeGroup) error {
	var activeEndpointCount int64
	if err := tx.Model(&model.NodeGroupEndpoint{}).
		Joins("JOIN protocol_endpoints ON protocol_endpoints.id = node_group_endpoints.protocol_endpoint_id").
		Where("node_group_endpoints.node_group_id = ? AND protocol_endpoints.is_active = ?", group.ID, true).
		Count(&activeEndpointCount).Error; err != nil {
		return err
	}
	if activeEndpointCount > 0 {
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

func (h *handlers) validateProtocolEndpointDeactivationMemberships(tx *gorm.DB, endpointID uint, removedGroupIDs map[uint]struct{}) error {
	if endpointID == 0 {
		return nil
	}
	var memberships []struct {
		NodeGroupID uint   `gorm:"column:node_group_id"`
		Name        string `gorm:"column:name"`
		IsEnabled   bool   `gorm:"column:is_enabled"`
	}
	query := tx.Table("node_group_endpoints AS membership").
		Select("membership.node_group_id, node_groups.name, node_groups.is_enabled").
		Joins("JOIN node_groups ON node_groups.id = membership.node_group_id").
		Where("membership.protocol_endpoint_id = ?", endpointID)
	if len(removedGroupIDs) > 0 {
		ids := make([]uint, 0, len(removedGroupIDs))
		for id := range removedGroupIDs {
			ids = append(ids, id)
		}
		query = query.Where("membership.node_group_id NOT IN ?", ids)
	}
	if err := query.Scan(&memberships).Error; err != nil {
		return err
	}
	for _, membership := range memberships {
		var otherActive int64
		if err := tx.Model(&model.NodeGroupEndpoint{}).
			Joins("JOIN protocol_endpoints ON protocol_endpoints.id = node_group_endpoints.protocol_endpoint_id").
			Where("node_group_endpoints.node_group_id = ? AND protocol_endpoints.id <> ? AND protocol_endpoints.is_active = ?", membership.NodeGroupID, endpointID, true).
			Count(&otherActive).Error; err != nil {
			return err
		}
		if otherActive > 0 {
			continue
		}
		var activePlanCount int64
		if err := tx.Model(&model.Plan{}).Where("node_group_id = ? AND is_active = ?", membership.NodeGroupID, true).Count(&activePlanCount).Error; err != nil {
			return err
		}
		if membership.IsEnabled || activePlanCount > 0 {
			return validationError("协议服务校验失败。", map[string]string{
				"is_active": fmt.Sprintf("节点组“%s”仍依赖此服务；请同时移除该关联或先加入其他可用服务。", membership.Name),
			})
		}
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

func (h *handlers) nodeGroupCredentialPublishTargets(tx *gorm.DB, groupID uint, endpointIDs []uint) ([]nodeGroupPublishTarget, error) {
	endpointIDs = uniqueUintIDs(endpointIDs)
	if len(endpointIDs) == 0 {
		return nil, nil
	}
	now := time.Now().UTC()
	var activeSubscriptionCount int64
	if err := tx.Model(&model.Subscription{}).
		Where("node_group_id = ? AND status = ? AND end_at > ? AND flow_used < flow_total", groupID, subStatusActive, now).
		Count(&activeSubscriptionCount).Error; err != nil {
		return nil, err
	}
	if activeSubscriptionCount == 0 {
		return nil, nil
	}
	var endpoints []model.ProtocolEndpoint
	if err := tx.Where("id IN ?", endpointIDs).Order("node_id asc, id asc").Find(&endpoints).Error; err != nil {
		return nil, err
	}
	targets := make([]nodeGroupPublishTarget, 0, len(endpoints))
	seenNodes := make(map[uint]struct{})
	for _, endpoint := range endpoints {
		if !h.protocolStoresSubscriptionCredential(endpoint.Protocol) {
			continue
		}
		if _, exists := seenNodes[endpoint.NodeID]; exists {
			continue
		}
		seenNodes[endpoint.NodeID] = struct{}{}
		targets = append(targets, nodeGroupPublishTarget{NodeID: endpoint.NodeID, EndpointID: endpoint.ID})
	}
	return targets, nil
}
