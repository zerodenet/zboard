package networkstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
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

const networkEntryReconcileTaskType = "node_group_reconcile"

type NetworkEntryMutations struct{ DB *gorm.DB }

func (s NetworkEntryMutations) LoadNetworkEntryMutation(ctx context.Context, actor, id uint) (out network.NetworkEntryMutationSnapshot, err error) {
	if s.DB == nil {
		return out, network.ErrNetworkEntryMutationUnavailable
	}
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := networkEntryAdministrator(tx, actor); err != nil {
			return err
		}
		var row model.NetworkEntry
		if err := tx.First(&row, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return network.ErrNetworkEntryNotFound
			}
			return err
		}
		out = networkEntryMutationSnapshot(row)
		return nil
	})
	return
}

func (s NetworkEntryMutations) LoadNetworkEntryProxyPool(ctx context.Context, actor, id uint) (out network.NetworkEntryProxyPoolSnapshot, err error) {
	if s.DB == nil {
		return out, network.ErrNetworkEntryMutationUnavailable
	}
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := networkEntryAdministrator(tx, actor); err != nil {
			return err
		}
		var row model.NodeProxyPool
		if err := tx.First(&row, id).Error; err != nil {
			return &network.NetworkEntryMutationValidation{Message: "请选择存在的共享代理池"}
		}
		out = network.NetworkEntryProxyPoolSnapshot{ID: row.ID, NodeID: row.NodeID, Revision: row.Revision, ConfigCiphertext: row.Config}
		return nil
	})
	return
}

func (s NetworkEntryMutations) CommitNetworkEntryMutation(ctx context.Context, actor uint, before *network.NetworkEntryMutationSnapshot, change network.NetworkEntryMutationChange) (out network.NetworkEntryMutationResult, err error) {
	if s.DB == nil {
		return out, network.ErrNetworkEntryMutationUnavailable
	}
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		admin, err := providerAdmin(tx, actor)
		if err != nil {
			if errors.Is(err, network.ErrProviderPermission) {
				return network.ErrNetworkEntryPermission
			}
			return err
		}

		var previous model.NetworkEntry
		if before != nil {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&previous, before.Entry.ID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return network.ErrNetworkEntryNotFound
				}
				return err
			}
			if previous.Revision != before.Entry.Revision {
				return network.ErrNetworkEntryConflict
			}
		}

		var endpoint model.ProtocolEndpoint
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&endpoint, change.Entry.EndpointID).Error; err != nil {
			return &network.NetworkEntryMutationValidation{Message: "请选择存在的落地协议"}
		}
		var nodes []model.Node
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id IN ?", []uint{change.Entry.NodeID, endpoint.NodeID}).Order("id").Find(&nodes).Error; err != nil {
			return err
		}
		if len(nodes) != 2 {
			return &network.NetworkEntryMutationValidation{Message: "A 与 B 必须是两个存在的不同节点"}
		}
		for _, node := range nodes {
			if change.Entry.Enabled && (!node.IsEnabled || node.LifecycleStatus == "deleting") {
				return &network.NetworkEntryMutationValidation{Message: "A、B 节点必须启用且未在删除"}
			}
		}
		if change.Pool != nil {
			var pool model.NodeProxyPool
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&pool, change.Pool.ID).Error; err != nil {
				return &network.NetworkEntryMutationValidation{Message: "请选择存在的共享代理池"}
			}
			if pool.NodeID != change.Entry.NodeID || pool.Revision != change.Pool.Revision || pool.Config != change.Pool.ConfigCiphertext {
				return network.ErrNetworkEntryConflict
			}
			if change.Entry.Network != "tcp" && !change.PoolSupportsDatagram {
				message := strings.TrimSpace(change.PoolDatagramError)
				if message == "" {
					message = "代理池不支持 UDP 转发"
				}
				return &network.NetworkEntryMutationValidation{Message: message}
			}
		}
		if change.Entry.Network == "tcp" && strings.EqualFold(endpoint.Protocol, "hysteria2") {
			return &network.NetworkEntryMutationValidation{Message: "Hysteria2 使用 UDP，请选择 TCP/UDP 转发"}
		}
		if change.Entry.Enabled && !endpoint.IsActive {
			return &network.NetworkEntryMutationValidation{Message: "落地协议必须启用"}
		}
		var legacy int64
		if err := tx.Model(&model.ProtocolCredential{}).Where("protocol_endpoint_id = ? AND status = ? AND listen_port <> ?", endpoint.ID, "active", endpoint.Port).Count(&legacy).Error; err != nil {
			return err
		}
		if legacy > 0 {
			return &network.NetworkEntryMutationValidation{Message: "请先重新发布 B 并将旧凭据迁移到统一协议端口"}
		}
		var duplicateName int64
		if err := tx.Model(&model.NetworkEntry{}).Where("endpoint_id = ? AND name = ? AND id <> ?", change.Entry.EndpointID, change.Entry.Name, change.Entry.ID).Count(&duplicateName).Error; err != nil {
			return err
		}
		if duplicateName > 0 {
			return &network.NetworkEntryMutationValidation{Message: "同一个落地协议的入口名称不能重复"}
		}
		if err := networkEntryMutationPortAvailable(tx, change.Entry.NodeID, change.Entry.Port, change.Entry.ID); err != nil {
			return err
		}

		row := networkEntryMutationModel(change.Entry, change.PathCiphertext)
		if before == nil {
			row.Revision = 1
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
			if err := enqueueNetworkEntryMutationPublishes(tx, row, actor); err != nil {
				return err
			}
		} else {
			row.Revision = previous.Revision + 1
			row.CreatedAt = previous.CreatedAt
			update := tx.Model(&model.NetworkEntry{}).Where("id = ? AND revision = ?", previous.ID, previous.Revision).
				Select("*").Omit("delivery_sort_order").Updates(&row)
			if update.Error != nil {
				return update.Error
			}
			if update.RowsAffected != 1 {
				return network.ErrNetworkEntryConflict
			}
			if previous.EndpointID != row.EndpointID {
				if err := enqueueNetworkEntryMutationPublishes(tx, previous, actor); err != nil {
					return err
				}
			}
			if previous.NodeID != row.NodeID {
				if err := EnqueuePublication(tx, previous.NodeID, 0, actor); err != nil {
					return err
				}
			}
		}

		taskIDs, err := applyNetworkEntryMembershipMutations(tx, admin, row, change)
		if err != nil {
			return err
		}
		action := "network_entry.create"
		if before != nil {
			action = "network_entry.update"
		}
		if err := tx.Create(&model.AuditLog{UserID: &admin.ID, Actor: admin.Email, Action: action, Target: fmt.Sprintf("network_entry:%d", row.ID), Detail: fmt.Sprintf("node=%d endpoint=%d", row.NodeID, row.EndpointID)}).Error; err != nil {
			return err
		}
		if before != nil {
			if err := enqueueNetworkEntryMutationPublishes(tx, row, actor); err != nil {
				return err
			}
		}
		out = network.NetworkEntryMutationResult{Entry: networkEntryRecordView(row), HasPath: row.PathConfig != "", ReconcileTaskIDs: taskIDs}
		return nil
	})
	return
}

func networkEntryAdministrator(tx *gorm.DB, actor uint) error {
	if _, err := providerAdmin(tx, actor); err != nil {
		if errors.Is(err, network.ErrProviderPermission) {
			return network.ErrNetworkEntryPermission
		}
		return err
	}
	return nil
}

func networkEntryMutationSnapshot(row model.NetworkEntry) network.NetworkEntryMutationSnapshot {
	return network.NetworkEntryMutationSnapshot{Entry: networkEntryRecordView(row), PathCiphertext: row.PathConfig}
}

func networkEntryRecordView(row model.NetworkEntry) network.NetworkEntryRecord {
	return network.NetworkEntryRecord{
		ID: row.ID, ProxyPoolID: row.ProxyPoolID, DeliverySortOrder: row.DeliverySortOrder,
		Network: row.Network, Name: row.Name, NodeID: row.NodeID, EndpointID: row.EndpointID,
		Address: row.Address, Port: row.Port, PublicPort: row.PublicPort, Enabled: row.Enabled,
		Revision: row.Revision, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func networkEntryMutationModel(row network.NetworkEntryRecord, path string) model.NetworkEntry {
	return model.NetworkEntry{
		DeliverySortOrder: row.DeliverySortOrder, ProxyPoolID: row.ProxyPoolID, ID: row.ID,
		Network: row.Network, Name: row.Name, NodeID: row.NodeID, EndpointID: row.EndpointID,
		Address: row.Address, Port: row.Port, PublicPort: row.PublicPort, Enabled: row.Enabled,
		PathConfig: path, Revision: row.Revision, CreatedAt: row.CreatedAt,
	}
}

func networkEntryMutationPortAvailable(tx *gorm.DB, nodeID uint, port int, entryID uint) error {
	for _, query := range []*gorm.DB{
		tx.Model(&model.NetworkEntry{}).Where("node_id = ? AND port = ? AND id <> ?", nodeID, port, entryID),
		tx.Model(&model.ProtocolEndpoint{}).Where("node_id = ? AND port = ?", nodeID, port),
		tx.Model(&model.ProtocolCredential{}).Where("node_id = ? AND listen_port = ?", nodeID, port),
	} {
		var count int64
		if err := query.Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return &network.NetworkEntryMutationValidation{Message: fmt.Sprintf("A 的端口 %d 已被协议、凭据或其他入口占用", port)}
		}
	}
	return nil
}

func enqueueNetworkEntryMutationPublishes(tx *gorm.DB, entry model.NetworkEntry, actor uint) error {
	var endpoint model.ProtocolEndpoint
	if err := tx.First(&endpoint, entry.EndpointID).Error; err != nil {
		return err
	}
	if err := EnqueueNodePublication(tx, endpoint.NodeID, endpoint.ID, actor); err != nil {
		return err
	}
	return EnqueuePublication(tx, entry.NodeID, 0, actor)
}

func applyNetworkEntryMembershipMutations(tx *gorm.DB, admin model.User, entry model.NetworkEntry, change network.NetworkEntryMutationChange) ([]uint, error) {
	taskIDs := make([]uint, 0, len(change.MembershipChanges))
	for _, item := range change.MembershipChanges {
		var group model.NodeGroup
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&group, item.NodeGroupID).Error; err != nil {
			return nil, &network.NetworkEntryMutationValidation{Message: "节点组不存在，请重新选择"}
		}
		if group.Revision != item.ExpectedRevision {
			return nil, network.ErrNetworkEntryConflict
		}
		var existing model.NodeGroupNetworkEntry
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("node_group_id = ? AND network_entry_id = ?", group.ID, entry.ID).First(&existing).Error
		exists := err == nil
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		if exists == item.Member {
			continue
		}
		if item.Member && !entry.Enabled {
			return nil, &network.NetworkEntryMutationValidation{Message: "请先启用前置服务再加入节点组"}
		}
		if err := replaceNetworkEntryGroupMembership(tx, group.ID, entry, item.Member); err != nil {
			return nil, err
		}
		if err := validateNetworkEntryGroupAvailability(tx, group); err != nil {
			return nil, err
		}
		group.Revision++
		if err := tx.Model(&model.NodeGroup{}).Where("id = ? AND revision = ?", group.ID, item.ExpectedRevision).Update("revision", group.Revision).Error; err != nil {
			return nil, err
		}
		taskID, err := persistNetworkEntryReconcileTask(tx, admin, group, entry.EndpointID, change.CredentialProtocols, change.Now)
		if err != nil {
			return nil, err
		}
		taskIDs = append(taskIDs, taskID)
	}
	return taskIDs, nil
}

func replaceNetworkEntryGroupMembership(tx *gorm.DB, groupID uint, entry model.NetworkEntry, member bool) error {
	var links []model.NodeGroupNetworkEntry
	if err := tx.Where("node_group_id = ?", groupID).Order("sort_order, id").Find(&links).Error; err != nil {
		return err
	}
	ids := make([]uint, 0, len(links)+1)
	for _, link := range links {
		if link.NetworkEntryID != entry.ID {
			ids = append(ids, link.NetworkEntryID)
		}
	}
	if member {
		ids = append(ids, entry.ID)
	}
	remove := tx.Where("node_group_id = ?", groupID)
	if len(ids) > 0 {
		remove = remove.Where("network_entry_id NOT IN ?", ids)
	}
	if err := remove.Delete(&model.NodeGroupNetworkEntry{}).Error; err != nil {
		return err
	}
	desired := make([]model.NodeGroupNetworkEntry, 0, len(ids))
	for index, id := range ids {
		desired = append(desired, model.NodeGroupNetworkEntry{NodeGroupID: groupID, NetworkEntryID: id, SortOrder: index})
	}
	if len(desired) > 0 {
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "node_group_id"}, {Name: "network_entry_id"}}, DoUpdates: clause.AssignmentColumns([]string{"sort_order"})}).CreateInBatches(&desired, 500).Error; err != nil {
			return err
		}
	}
	var endpoint model.ProtocolEndpoint
	if err := tx.First(&endpoint, entry.EndpointID).Error; err != nil {
		return err
	}
	return EnqueueNodePublication(tx, endpoint.NodeID, endpoint.ID, 0)
}

func validateNetworkEntryGroupAvailability(tx *gorm.DB, group model.NodeGroup) error {
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
		return &network.NetworkEntryMutationValidation{Message: fmt.Sprintf("节点组“%s”已启用，必须至少保留一个可用协议服务", group.Name)}
	}
	var activePlans int64
	if err := tx.Model(&model.Plan{}).Where("node_group_id = ? AND is_active = ?", group.ID, true).Count(&activePlans).Error; err != nil {
		return err
	}
	if activePlans > 0 {
		return &network.NetworkEntryMutationValidation{Message: fmt.Sprintf("节点组“%s”仍被已发布套餐使用，必须至少保留一个可用协议服务", group.Name)}
	}
	return nil
}

func persistNetworkEntryReconcileTask(tx *gorm.DB, admin model.User, group model.NodeGroup, endpointID uint, supported []string, now time.Time) (uint, error) {
	protocolSet := map[string]bool{}
	for _, protocol := range supported {
		protocolSet[strings.ToLower(strings.TrimSpace(protocol))] = true
	}
	var targets []struct {
		NodeID     uint
		EndpointID uint
	}
	var activeSubscriptions int64
	if err := tx.Model(&model.Subscription{}).Where("node_group_id = ? AND status = ? AND end_at > ? AND flow_used < flow_total", group.ID, "active", now).Count(&activeSubscriptions).Error; err != nil {
		return 0, err
	}
	if activeSubscriptions > 0 {
		var endpoint model.ProtocolEndpoint
		if err := tx.First(&endpoint, endpointID).Error; err != nil {
			return 0, err
		}
		if protocolSet[strings.ToLower(strings.TrimSpace(endpoint.Protocol))] {
			targets = append(targets, struct {
				NodeID     uint
				EndpointID uint
			}{endpoint.NodeID, endpoint.ID})
		}
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].NodeID < targets[j].NodeID })
	nodeIDs := make([]uint, 0, len(targets))
	endpointIDsByNode := map[string][]uint{}
	for _, target := range targets {
		nodeIDs = append(nodeIDs, target.NodeID)
		endpointIDsByNode[strconv.FormatUint(uint64(target.NodeID), 10)] = []uint{target.EndpointID}
	}
	scope, err := json.Marshal(struct {
		NodeGroupID uint   `json:"node_group_id"`
		NodeIDs     []uint `json:"node_ids"`
	}{group.ID, nodeIDs})
	if err != nil {
		return 0, err
	}
	content, err := json.Marshal(network.BatchOperationContent{RequestedBy: admin.ID, Actor: admin.Email, NodeGroupID: group.ID, EndpointIDsByNode: endpointIDsByNode})
	if err != nil {
		return 0, err
	}
	batchTargets := make([]jobs.BatchSubmissionTarget, 0, len(nodeIDs)+1)
	batchTargets = append(batchTargets, jobs.BatchSubmissionTarget{Type: "node_group", ID: group.ID})
	for _, nodeID := range nodeIDs {
		batchTargets = append(batchTargets, jobs.BatchSubmissionTarget{Type: "node", ID: nodeID})
	}
	receipt, err := jobstore.PersistBatchSubmission(tx, jobs.BatchSubmissionActor{ID: admin.ID, Email: admin.Email}, jobs.BatchSubmission{
		Type: networkEntryReconcileTaskType, Scope: string(scope), Content: string(content),
		IdempotencyKey: fmt.Sprintf("node-group-reconcile:%d:%d", group.ID, group.Revision), MaxAttempts: 3,
		ScheduledAt: now, Targets: batchTargets,
	})
	return receipt.ID, err
}
