package networkstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type NodeGroupMutations struct{ DB *gorm.DB }

func (s NodeGroupMutations) LoadNodeGroupMutation(ctx context.Context, actor, id uint) (out network.NodeGroupMutationSnapshot, err error) {
	if s.DB == nil {
		return out, network.ErrNodeGroupMutationUnavailable
	}
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := nodeGroupAdministrator(tx, actor); err != nil {
			return err
		}
		var group model.NodeGroup
		if err := tx.First(&group, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return network.ErrNodeGroupNotFound
			}
			return err
		}
		if err := loadNodeGroupMutationRelations(tx, &group); err != nil {
			return err
		}
		out.Group = nodeGroupRecord(group)
		return nil
	})
	return
}

func (s NodeGroupMutations) CommitNodeGroupMutation(ctx context.Context, actor uint, before *network.NodeGroupMutationSnapshot, change network.NodeGroupMutationChange) (out network.NodeGroupMutationResult, err error) {
	if s.DB == nil {
		return out, network.ErrNodeGroupMutationUnavailable
	}
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		admin, err := providerAdmin(tx, actor)
		if err != nil {
			if errors.Is(err, network.ErrProviderPermission) {
				return network.ErrNodeGroupMutationPermission
			}
			return err
		}

		row := model.NodeGroup{
			ID: change.Group.ID, Name: change.Group.Name, Code: change.Group.Code,
			Description: change.Group.Description, IsEnabled: change.Group.IsEnabled,
			Revision: change.Group.Revision, CreatedAt: change.Group.CreatedAt,
		}
		var changedEndpointIDs []uint
		membershipUpdated := change.ReplaceEndpoints || change.ReplaceEntries
		if before == nil {
			row.Revision = 1
			if err := tx.Create(&row).Error; err != nil {
				return nodeGroupConstraintError(err)
			}
		} else {
			var locked model.NodeGroup
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, before.Group.ID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return network.ErrNodeGroupNotFound
				}
				return err
			}
			if locked.Revision != before.Group.Revision {
				return &network.NodeGroupMutationConflict{CurrentRevision: locked.Revision}
			}
			if change.ReplaceEnabled && !row.IsEnabled {
				var activePlans int64
				if err := tx.Model(&model.Plan{}).Where("node_group_id = ? AND is_active = ?", locked.ID, true).Count(&activePlans).Error; err != nil {
					return err
				}
				if activePlans > 0 {
					return &network.NodeGroupMutationValidation{Message: "节点组状态校验失败。", Fields: map[string]string{"is_enabled": "请先停用使用该节点组的已发布套餐。"}}
				}
			}
			row.ID, row.Revision, row.CreatedAt = locked.ID, locked.Revision+1, locked.CreatedAt
		}

		if change.ReplaceEndpoints {
			changed, err := replaceNodeGroupMutationEndpoints(tx, row.ID, change.Group.ProtocolEndpointIDs)
			if err != nil {
				return err
			}
			changedEndpointIDs = append(changedEndpointIDs, changed...)
		}
		if change.ReplaceEntries {
			changed, err := replaceNodeGroupMutationEntries(tx, row.ID, change.Group.NetworkEntryIDs)
			if err != nil {
				return err
			}
			changedEndpointIDs = append(changedEndpointIDs, changed...)
		}
		if err := validateNodeGroupMutationAvailability(tx, row); err != nil {
			return err
		}

		if before == nil {
			if err := tx.Create(&model.AuditLog{UserID: &admin.ID, Actor: admin.Email, Action: "node_group.create", Target: fmt.Sprintf("node_group:%d", row.ID), Detail: fmt.Sprintf("endpoint_count=%d", len(change.Group.ProtocolEndpointIDs))}).Error; err != nil {
				return err
			}
		} else {
			update := tx.Model(&model.NodeGroup{}).Where("id = ? AND revision = ?", row.ID, before.Group.Revision).Updates(map[string]any{
				"name": row.Name, "code": row.Code, "description": row.Description,
				"is_enabled": row.IsEnabled, "revision": row.Revision,
			})
			if update.Error != nil {
				return nodeGroupConstraintError(update.Error)
			}
			if update.RowsAffected != 1 {
				var current model.NodeGroup
				if err := tx.First(&current, row.ID).Error; err != nil {
					return err
				}
				return &network.NodeGroupMutationConflict{CurrentRevision: current.Revision}
			}
		}

		if membershipUpdated {
			receipt, err := persistNodeGroupMutationReconcileTask(tx, admin, row, changedEndpointIDs, change.CredentialProtocols, change.Now)
			if err != nil {
				return err
			}
			out.ReconcileTask = &receipt.BatchReceipt
		}
		if before != nil {
			detail := fmt.Sprintf("revision=%d membership_updated=%t", row.Revision, membershipUpdated)
			if change.ReplaceEndpoints {
				detail += fmt.Sprintf(" endpoint_count=%d", len(change.Group.ProtocolEndpointIDs))
			}
			if err := tx.Create(&model.AuditLog{UserID: &admin.ID, Actor: admin.Email, Action: "node_group.update", Target: fmt.Sprintf("node_group:%d", row.ID), Detail: detail}).Error; err != nil {
				return err
			}
		}
		if err := tx.First(&row, row.ID).Error; err != nil {
			return err
		}
		if err := loadNodeGroupMutationRelations(tx, &row); err != nil {
			return err
		}
		out.NodeGroup = nodeGroupRecord(row)
		return nil
	})
	return
}

func nodeGroupAdministrator(tx *gorm.DB, actor uint) error {
	if _, err := providerAdmin(tx, actor); err != nil {
		if errors.Is(err, network.ErrProviderPermission) {
			return network.ErrNodeGroupMutationPermission
		}
		return err
	}
	return nil
}

func nodeGroupRecord(group model.NodeGroup) network.NodeGroupRecord {
	return network.NodeGroupRecord{
		ID: group.ID, Name: group.Name, Code: group.Code, Description: group.Description,
		IsEnabled: group.IsEnabled, Revision: group.Revision,
		ProtocolEndpointIDs: append([]uint{}, group.ProtocolEndpointIDs...),
		NetworkEntryIDs:     append([]uint{}, group.NetworkEntryIDs...), PlanCount: group.PlanCount,
		CreatedAt: group.CreatedAt, UpdatedAt: group.UpdatedAt,
	}
}

func loadNodeGroupMutationRelations(tx *gorm.DB, group *model.NodeGroup) error {
	group.ProtocolEndpointIDs, group.NetworkEntryIDs = []uint{}, []uint{}
	if err := tx.Model(&model.NodeGroupEndpoint{}).Where("node_group_id = ?", group.ID).Order("sort_order, id").Pluck("protocol_endpoint_id", &group.ProtocolEndpointIDs).Error; err != nil {
		return err
	}
	return tx.Model(&model.NodeGroupNetworkEntry{}).Where("node_group_id = ?", group.ID).Order("sort_order, id").Pluck("network_entry_id", &group.NetworkEntryIDs).Error
}

func replaceNodeGroupMutationEndpoints(tx *gorm.DB, groupID uint, endpointIDs []uint) ([]uint, error) {
	activeIDs := make([]uint, 0, len(endpointIDs))
	for start := 0; start < len(endpointIDs); start += 500 {
		end := start + 500
		if end > len(endpointIDs) {
			end = len(endpointIDs)
		}
		var batch []uint
		if err := tx.Model(&model.ProtocolEndpoint{}).Where("id IN ? AND is_active = ?", endpointIDs[start:end], true).Pluck("id", &batch).Error; err != nil {
			return nil, err
		}
		activeIDs = append(activeIDs, batch...)
	}
	if missing, ok := firstMissingNodeGroupID(endpointIDs, activeIDs); ok {
		return nil, &network.NodeGroupMutationValidation{Message: "节点组成员校验失败。", Fields: map[string]string{"protocol_endpoint_ids": fmt.Sprintf("协议端点 #%d 不存在或已停用，请重新选择。", missing)}}
	}
	var existing []model.NodeGroupEndpoint
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("node_group_id = ?", groupID).Find(&existing).Error; err != nil {
		return nil, err
	}
	changed := changedNodeGroupEndpointIDs(existing, endpointIDs)
	desired := make(map[uint]struct{}, len(endpointIDs))
	for _, id := range endpointIDs {
		desired[id] = struct{}{}
	}
	removed := make([]uint, 0)
	for _, link := range existing {
		if _, keep := desired[link.ProtocolEndpointID]; !keep {
			removed = append(removed, link.ProtocolEndpointID)
		}
	}
	for start := 0; start < len(removed); start += 500 {
		end := start + 500
		if end > len(removed) {
			end = len(removed)
		}
		if err := tx.Where("node_group_id = ? AND protocol_endpoint_id IN ?", groupID, removed[start:end]).Delete(&model.NodeGroupEndpoint{}).Error; err != nil {
			return nil, err
		}
	}
	links := make([]model.NodeGroupEndpoint, 0, len(endpointIDs))
	for index, id := range endpointIDs {
		links = append(links, model.NodeGroupEndpoint{NodeGroupID: groupID, ProtocolEndpointID: id, SortOrder: index})
	}
	if len(links) > 0 {
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "node_group_id"}, {Name: "protocol_endpoint_id"}}, DoUpdates: clause.AssignmentColumns([]string{"sort_order"})}).CreateInBatches(&links, 500).Error; err != nil {
			return nil, err
		}
	}
	return changed, nil
}

func replaceNodeGroupMutationEntries(tx *gorm.DB, groupID uint, entryIDs []uint) ([]uint, error) {
	entries := make([]model.NetworkEntry, 0, len(entryIDs))
	if len(entryIDs) > 0 {
		if err := tx.Model(&model.NetworkEntry{}).Select("network_entries.*").
			Joins("JOIN protocol_endpoints ON protocol_endpoints.id = network_entries.endpoint_id").
			Where("network_entries.id IN ? AND network_entries.enabled = ? AND protocol_endpoints.is_active = ?", entryIDs, true, true).
			Find(&entries).Error; err != nil {
			return nil, err
		}
		if len(entries) != len(entryIDs) {
			return nil, &network.NodeGroupMutationValidation{Message: "节点组成员校验失败。", Fields: map[string]string{"network_entry_ids": "前置线路不存在、已停用或落地协议未启用，请重新选择。"}}
		}
	}
	var existing []model.NodeGroupNetworkEntry
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("node_group_id = ?", groupID).Find(&existing).Error; err != nil {
		return nil, err
	}
	oldSet, newSet := map[uint]bool{}, map[uint]bool{}
	for _, link := range existing {
		oldSet[link.NetworkEntryID] = true
	}
	for _, id := range entryIDs {
		newSet[id] = true
	}
	changedEntryIDs := make([]uint, 0)
	for _, link := range existing {
		if !newSet[link.NetworkEntryID] {
			changedEntryIDs = append(changedEntryIDs, link.NetworkEntryID)
		}
	}
	for _, id := range entryIDs {
		if !oldSet[id] {
			changedEntryIDs = append(changedEntryIDs, id)
		}
	}
	remove := tx.Where("node_group_id = ?", groupID)
	if len(entryIDs) > 0 {
		remove = remove.Where("network_entry_id NOT IN ?", entryIDs)
	}
	if err := remove.Delete(&model.NodeGroupNetworkEntry{}).Error; err != nil {
		return nil, err
	}
	links := make([]model.NodeGroupNetworkEntry, 0, len(entryIDs))
	for index, id := range entryIDs {
		links = append(links, model.NodeGroupNetworkEntry{NodeGroupID: groupID, NetworkEntryID: id, SortOrder: index})
	}
	if len(links) > 0 {
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "node_group_id"}, {Name: "network_entry_id"}}, DoUpdates: clause.AssignmentColumns([]string{"sort_order"})}).CreateInBatches(&links, 500).Error; err != nil {
			return nil, err
		}
	}
	endpointIDs := make([]uint, 0)
	if len(changedEntryIDs) > 0 {
		if err := tx.Model(&model.NetworkEntry{}).Where("id IN ?", changedEntryIDs).Distinct().Pluck("endpoint_id", &endpointIDs).Error; err != nil {
			return nil, err
		}
	}
	for _, endpointID := range endpointIDs {
		var endpoint model.ProtocolEndpoint
		if err := tx.First(&endpoint, endpointID).Error; err != nil {
			return nil, err
		}
		if err := EnqueueNodePublication(tx, endpoint.NodeID, endpoint.ID, 0); err != nil {
			return nil, err
		}
	}
	return endpointIDs, nil
}

func validateNodeGroupMutationAvailability(tx *gorm.DB, group model.NodeGroup) error {
	var activeEndpoints int64
	if err := tx.Table("node_group_endpoints").Joins("JOIN protocol_endpoints ON protocol_endpoints.id = node_group_endpoints.protocol_endpoint_id").
		Where("node_group_endpoints.node_group_id = ? AND protocol_endpoints.is_active = ?", group.ID, true).Count(&activeEndpoints).Error; err != nil {
		return err
	}
	if activeEndpoints > 0 {
		return nil
	}
	var activeEntries int64
	if err := tx.Model(&model.NodeGroupNetworkEntry{}).Joins("JOIN network_entries ON network_entries.id = node_group_network_entries.network_entry_id").
		Where("node_group_id = ? AND network_entries.enabled = ?", group.ID, true).Count(&activeEntries).Error; err != nil {
		return err
	}
	if activeEntries > 0 {
		return nil
	}
	if group.IsEnabled {
		return &network.NodeGroupMutationValidation{Message: "节点组关联校验失败。", Fields: map[string]string{"node_group_membership_changes": fmt.Sprintf("节点组“%s”已启用，必须至少保留一个可用协议服务。", group.Name)}}
	}
	var activePlans int64
	if err := tx.Model(&model.Plan{}).Where("node_group_id = ? AND is_active = ?", group.ID, true).Count(&activePlans).Error; err != nil {
		return err
	}
	if activePlans > 0 {
		return &network.NodeGroupMutationValidation{Message: "节点组关联校验失败。", Fields: map[string]string{"node_group_membership_changes": fmt.Sprintf("节点组“%s”仍被已发布套餐使用，必须至少保留一个可用协议服务。", group.Name)}}
	}
	return nil
}

func persistNodeGroupMutationReconcileTask(tx *gorm.DB, admin model.User, group model.NodeGroup, endpointIDs []uint, supported []string, now time.Time) (jobs.BatchSubmissionReceipt, error) {
	endpointIDs = uniqueSortedIDs(endpointIDs)
	protocolSet := make(map[string]bool, len(supported))
	for _, protocol := range supported {
		protocolSet[strings.ToLower(strings.TrimSpace(protocol))] = true
	}
	targets := make([]struct{ NodeID, EndpointID uint }, 0)
	var activeSubscriptions int64
	if err := tx.Model(&model.Subscription{}).Where("node_group_id = ? AND status = ? AND end_at > ? AND flow_used < flow_total", group.ID, "active", now).Count(&activeSubscriptions).Error; err != nil {
		return jobs.BatchSubmissionReceipt{}, err
	}
	if activeSubscriptions > 0 && len(endpointIDs) > 0 {
		var endpoints []model.ProtocolEndpoint
		if err := tx.Where("id IN ?", endpointIDs).Order("node_id, id").Find(&endpoints).Error; err != nil {
			return jobs.BatchSubmissionReceipt{}, err
		}
		seenNodes := map[uint]bool{}
		for _, endpoint := range endpoints {
			if !protocolSet[strings.ToLower(strings.TrimSpace(endpoint.Protocol))] || seenNodes[endpoint.NodeID] {
				continue
			}
			seenNodes[endpoint.NodeID] = true
			targets = append(targets, struct{ NodeID, EndpointID uint }{endpoint.NodeID, endpoint.ID})
		}
	}
	nodeIDs := make([]uint, 0, len(targets))
	endpointIDsByNode := make(map[string][]uint, len(targets))
	for _, target := range targets {
		nodeIDs = append(nodeIDs, target.NodeID)
		endpointIDsByNode[strconv.FormatUint(uint64(target.NodeID), 10)] = []uint{target.EndpointID}
	}
	scope, err := json.Marshal(struct {
		NodeGroupID uint   `json:"node_group_id"`
		NodeIDs     []uint `json:"node_ids"`
	}{group.ID, nodeIDs})
	if err != nil {
		return jobs.BatchSubmissionReceipt{}, err
	}
	content, err := json.Marshal(network.BatchOperationContent{RequestedBy: admin.ID, Actor: admin.Email, NodeGroupID: group.ID, EndpointIDsByNode: endpointIDsByNode})
	if err != nil {
		return jobs.BatchSubmissionReceipt{}, err
	}
	batchTargets := []jobs.BatchSubmissionTarget{{Type: "node_group", ID: group.ID}}
	for _, target := range targets {
		batchTargets = append(batchTargets, jobs.BatchSubmissionTarget{Type: "node", ID: target.NodeID})
	}
	return jobstore.PersistBatchSubmission(tx, jobs.BatchSubmissionActor{ID: admin.ID, Email: admin.Email}, jobs.BatchSubmission{
		Type: networkEntryReconcileTaskType, Scope: string(scope), Content: string(content),
		IdempotencyKey: fmt.Sprintf("node-group-reconcile:%d:%d", group.ID, group.Revision),
		MaxAttempts:    3, ScheduledAt: now, Targets: batchTargets,
	})
}

func changedNodeGroupEndpointIDs(existing []model.NodeGroupEndpoint, desired []uint) []uint {
	desiredSet, existingSet := map[uint]bool{}, map[uint]bool{}
	for _, id := range desired {
		desiredSet[id] = true
	}
	changed := make([]uint, 0)
	for _, link := range existing {
		existingSet[link.ProtocolEndpointID] = true
		if !desiredSet[link.ProtocolEndpointID] {
			changed = append(changed, link.ProtocolEndpointID)
		}
	}
	for _, id := range desired {
		if !existingSet[id] {
			changed = append(changed, id)
		}
	}
	return uniqueSortedIDs(changed)
}

func firstMissingNodeGroupID(requested, existing []uint) (uint, bool) {
	available := make(map[uint]bool, len(existing))
	for _, id := range existing {
		available[id] = true
	}
	for _, id := range requested {
		if !available[id] {
			return id, true
		}
	}
	return 0, false
}

func nodeGroupConstraintError(err error) error {
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "duplicate") || strings.Contains(message, "unique constraint") {
		return &network.NodeGroupMutationValidation{Message: "节点组信息校验失败。", Fields: map[string]string{"code": "节点组代码已存在，请更换后重试。"}}
	}
	return err
}
