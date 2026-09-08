package handler

import (
	"fmt"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Only explicit protocol membership grants credentials. A forward target is
// topology, never an implicit authorization edge.
const credentialMembershipSQL = `(SELECT node_group_id, protocol_endpoint_id FROM node_group_endpoints)`

func credentialMemberships(db *gorm.DB) *gorm.DB {
	return db.Table(credentialMembershipSQL + " AS node_group_endpoints")
}

func credentialMembershipJoin(condition string) string {
	return "JOIN " + credentialMembershipSQL + " AS node_group_endpoints ON " + condition
}

func loadNodeGroupNetworkEntryIDs(db *gorm.DB, group *model.NodeGroup) error {
	group.NetworkEntryIDs = []uint{}
	return db.Model(&model.NodeGroupNetworkEntry{}).Where("node_group_id = ?", group.ID).
		Order("sort_order, id").Pluck("network_entry_id", &group.NetworkEntryIDs).Error
}

func replaceNodeGroupNetworkEntries(tx *gorm.DB, groupID uint, ids []uint) ([]uint, error) {
	ids = uniqueUintIDs(ids)
	var entries []model.NetworkEntry
	if len(ids) > 0 {
		if err := tx.Model(&model.NetworkEntry{}).Select("network_entries.*").
			Joins("JOIN protocol_endpoints ON protocol_endpoints.id = network_entries.endpoint_id").
			Where("network_entries.id IN ? AND network_entries.enabled = ? AND protocol_endpoints.is_active = ?", ids, true, true).
			Find(&entries).Error; err != nil {
			return nil, err
		}
		if len(entries) != len(ids) {
			return nil, validationError("节点组成员校验失败。", map[string]string{"network_entry_ids": "前置线路不存在、已停用或落地协议未启用，请重新选择。"})
		}
	}
	var previous []model.NodeGroupNetworkEntry
	if err := tx.Where("node_group_id = ?", groupID).Find(&previous).Error; err != nil {
		return nil, err
	}
	oldIDs := make([]uint, 0, len(previous))
	oldSet, newSet := map[uint]bool{}, map[uint]bool{}
	for _, link := range previous {
		oldIDs = append(oldIDs, link.NetworkEntryID)
		oldSet[link.NetworkEntryID] = true
	}
	for _, id := range ids {
		newSet[id] = true
	}
	changedIDs := []uint{}
	for _, id := range oldIDs {
		if !newSet[id] {
			changedIDs = append(changedIDs, id)
		}
	}
	for _, id := range ids {
		if !oldSet[id] {
			changedIDs = append(changedIDs, id)
		}
	}
	var endpointIDs []uint
	if len(changedIDs) > 0 {
		if err := tx.Model(&model.NetworkEntry{}).Where("id IN ?", changedIDs).Distinct().Pluck("endpoint_id", &endpointIDs).Error; err != nil {
			return nil, err
		}
	}
	remove := tx.Where("node_group_id = ?", groupID)
	if len(ids) > 0 {
		remove = remove.Where("network_entry_id NOT IN ?", ids)
	}
	if err := remove.Delete(&model.NodeGroupNetworkEntry{}).Error; err != nil {
		return nil, err
	}
	links := make([]model.NodeGroupNetworkEntry, 0, len(ids))
	for index, id := range ids {
		links = append(links, model.NodeGroupNetworkEntry{NodeGroupID: groupID, NetworkEntryID: id, SortOrder: index})
	}
	if len(links) > 0 {
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "node_group_id"}, {Name: "network_entry_id"}}, DoUpdates: clause.AssignmentColumns([]string{"sort_order"})}).CreateInBatches(&links, 500).Error; err != nil {
			return nil, err
		}
	}
	for _, endpointID := range endpointIDs {
		var endpoint model.ProtocolEndpoint
		if err := tx.First(&endpoint, endpointID).Error; err != nil {
			return nil, err
		}
		if err := enqueueNodeConfigPublish(tx, endpoint.NodeID, endpoint.ID, 0); err != nil {
			return nil, err
		}
	}
	return endpointIDs, nil
}

func loadNetworkEntryMemberships(db *gorm.DB, entryID uint) ([]protocolEndpointNodeGroupMembership, error) {
	rows := []protocolEndpointNodeGroupMembership{}
	err := db.Table("node_group_network_entries membership").Select("membership.node_group_id, node_groups.name, node_groups.code, node_groups.description, node_groups.is_enabled, node_groups.revision, membership.sort_order").Joins("JOIN node_groups ON node_groups.id = membership.node_group_id").Where("membership.network_entry_id = ?", entryID).Order("node_groups.id").Scan(&rows).Error
	return rows, err
}

func (h *handlers) applyNetworkEntryMembershipChanges(tx *gorm.DB, entry model.NetworkEntry, changes []protocolEndpointNodeGroupMembershipChange, claims authClaims, tasks *[]model.Task) error {
	for _, change := range changes {
		var group model.NodeGroup
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&group, change.NodeGroupID).Error; err != nil {
			return fmt.Errorf("节点组不存在，请重新选择")
		}
		if group.Revision != change.ExpectedRevision {
			return fmt.Errorf("节点组 %s 已更新，请重新打开服务并选择", group.Name)
		}
		if err := loadNodeGroupNetworkEntryIDs(tx, &group); err != nil {
			return err
		}
		ids := []uint{}
		exists := false
		for _, id := range group.NetworkEntryIDs {
			if id == entry.ID {
				exists = true
				if !change.Member {
					continue
				}
			}
			ids = append(ids, id)
		}
		if change.Member == exists {
			continue
		}
		if change.Member {
			if !entry.Enabled {
				return fmt.Errorf("请先启用前置服务再加入节点组")
			}
			ids = append(ids, entry.ID)
		}
		if _, err := replaceNodeGroupNetworkEntries(tx, group.ID, ids); err != nil {
			return err
		}
		if err := validateNodeGroupMembershipAvailability(tx, group); err != nil {
			return err
		}
		nextRevision := group.Revision + 1
		if err := tx.Model(&group).Update("revision", nextRevision).Error; err != nil {
			return err
		}
		targets, err := h.nodeGroupCredentialPublishTargets(tx, group.ID, []uint{entry.EndpointID})
		if err != nil {
			return err
		}
		task, items, err := prepareNodeGroupReconcileTask(claims, group.ID, nextRevision, targets)
		if err != nil {
			return err
		}
		if err := persistAdminTaskRecords(tx, claims, &task, items); err != nil {
			return err
		}
		*tasks = append(*tasks, task)

	}
	return nil
}
