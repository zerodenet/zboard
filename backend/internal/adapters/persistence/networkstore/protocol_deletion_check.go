package networkstore

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s ProtocolEndpointRemoval) CheckProtocolEndpointDeletion(ctx context.Context, actor, id uint) (out network.DeletionCheck, err error) {
	if s.DB == nil {
		return out, network.ErrProtocolEndpointRemovalUnavailable
	}
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := deletionAdministrator(tx, actor); err != nil {
			return err
		}
		var endpoint model.ProtocolEndpoint
		if err := tx.Unscoped().First(&endpoint, id).Error; err != nil {
			return removalError(err)
		}
		var node model.Node
		if err := tx.First(&node, endpoint.NodeID).Error; err != nil {
			return removalError(err)
		}
		out, err = protocolDeletionCheck(tx, endpoint, node, false)
		return err
	})
	return
}
func deletionAdministrator(tx *gorm.DB, actor uint) (model.User, error) {
	admin, err := providerAdmin(tx, actor)
	if errors.Is(err, network.ErrProviderPermission) {
		return admin, network.ErrResourcePermission
	}
	return admin, err
}
func protocolDeletionCheck(tx *gorm.DB, endpoint model.ProtocolEndpoint, node model.Node, lock bool) (network.DeletionCheck, error) {
	check := network.DeletionCheck{ID: endpoint.ID, Name: endpoint.Name, Blockers: []network.DeletionBlocker{}, Warnings: []network.DeletionBlocker{}, Effects: []string{}}
	if endpoint.DeletedAt.Valid || node.LifecycleStatus == "archived" {
		check.AlreadyDeleted = true
		check.Effects = append(check.Effects, "该协议已删除，历史流量和账本保留，无需再次删除。")
		return check, nil
	}
	if network.NodeManagementClosed(node.LifecycleStatus) {
		check.Blockers = append(check.Blockers, network.DeletionBlocker{Code: "node_deleting", Message: "所属服务器正在删除，请查看服务器删除任务。", Href: fmt.Sprintf("/admin/nodes?deletion=%d", node.ID)})
		return check, nil
	}
	var children int64
	if err := tx.Model(&model.ProtocolEndpoint{}).Where("parent_protocol_id = ?", endpoint.ID).Count(&children).Error; err != nil {
		return check, err
	}
	if children > 0 {
		check.Warnings = append(check.Warnings, network.DeletionBlocker{Code: "related_protocols", Message: fmt.Sprintf("解除 %d 个协议的父级关联；这些协议及其服务器继续保留。", children), Href: fmt.Sprintf("/admin/protocols?node=%d", node.ID)})
	}
	groups, err := topologyDeletionGroupWarnings(tx, endpoint.ID, 0, lock)
	if err != nil {
		return check, err
	}
	check.Warnings = append(check.Warnings, groups...)
	var entries []model.NetworkEntry
	if err := tx.Where("endpoint_id = ?", endpoint.ID).Find(&entries).Error; err != nil {
		return check, err
	}
	nodes := []uint{node.ID}
	for _, entry := range entries {
		if entry.NodeID != 0 {
			nodes = append(nodes, entry.NodeID)
		}
	}
	publication, err := deletionPublicationWarnings(tx, uniqueSortedIDs(nodes), lock)
	if err != nil {
		return check, err
	}
	check.Warnings = append(check.Warnings, publication...)
	var grants int64
	if err := tx.Model(&model.ProtocolCredential{}).Where("protocol_endpoint_id = ? AND revoked_at IS NULL", endpoint.ID).Count(&grants).Error; err != nil {
		return check, err
	}
	check.Effects = append(check.Effects, fmt.Sprintf("删除此协议、%d 个前置入口及当前权限组和证书关联，撤销 %d 条当前授权。", len(entries), grants), "历史流量、计费账本和发布记录保留，不影响已结算数据。", "服务器运行配置撤除将进入发布队列；远端完成更新前可能仍保留旧配置。")
	return check, nil
}

// Check both direct and forwarded alternatives. Removing a landing endpoint
// removes all of its entries; none can count as a surviving delivery path.
func topologyDeletionGroupWarnings(tx *gorm.DB, endpointID, entryID uint, lock bool) ([]network.DeletionBlocker, error) {
	var groups []model.NodeGroup
	q := tx.Model(&model.NodeGroup{})
	if endpointID != 0 {
		q = q.Where("id IN (?) OR id IN (?)", tx.Model(&model.NodeGroupEndpoint{}).Select("node_group_id").Where("protocol_endpoint_id = ?", endpointID), tx.Table("node_group_network_entries AS m").Select("m.node_group_id").Joins("JOIN network_entries AS e ON e.id = m.network_entry_id").Where("e.endpoint_id = ?", endpointID))
	} else {
		q = q.Where("id IN (?)", tx.Model(&model.NodeGroupNetworkEntry{}).Select("node_group_id").Where("network_entry_id = ?", entryID))
	}
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := q.Order("id").Find(&groups).Error; err != nil {
		return nil, err
	}
	blockers := []network.DeletionBlocker{}
	const live = "p.is_active = ? AND p.deleted_at IS NULL AND n.is_enabled = ? AND n.lifecycle_status NOT IN ?"
	closed := []string{"deleting", "archived"}
	for _, group := range groups {
		var direct, forward int64
		if err := tx.Table("node_group_endpoints AS m").Joins("JOIN protocol_endpoints AS p ON p.id = m.protocol_endpoint_id").Joins("JOIN nodes AS n ON n.id = p.node_id").Where("m.node_group_id = ? AND p.id <> ?", group.ID, endpointID).Where(live, true, true, closed).Count(&direct).Error; err != nil {
			return nil, err
		}
		if direct > 0 {
			continue
		}
		if err := tx.Table("node_group_network_entries AS m").Joins("JOIN network_entries AS e ON e.id = m.network_entry_id").Joins("JOIN protocol_endpoints AS p ON p.id = e.endpoint_id").Joins("JOIN nodes AS n ON n.id = p.node_id").Joins("LEFT JOIN nodes AS h ON h.id = e.node_id").Where("m.node_group_id = ? AND p.id <> ? AND e.id <> ? AND e.enabled = ?", group.ID, endpointID, entryID, true).Where(live, true, true, closed).Where("e.node_id IS NULL OR e.node_id = 0 OR (h.is_enabled = ? AND h.lifecycle_status NOT IN ?)", true, closed).Count(&forward).Error; err != nil {
			return nil, err
		}
		if forward > 0 {
			continue
		}
		var plans, subscriptions int64
		if err := tx.Model(&model.Plan{}).Where("node_group_id = ? AND is_active = ?", group.ID, true).Count(&plans).Error; err != nil {
			return nil, err
		}
		if err := tx.Model(&model.Subscription{}).Where("node_group_id = ? AND status = ? AND end_at > ?", group.ID, "active", time.Now().UTC()).Count(&subscriptions).Error; err != nil {
			return nil, err
		}
		if group.IsEnabled || plans > 0 || subscriptions > 0 {
			blockers = append(blockers, network.DeletionBlocker{Code: "group_delivery", Message: fmt.Sprintf("删除后，权限组“%s”将没有可用线路（已发布套餐 %d 个、有效订阅 %d 个）。套餐和订阅保留，可随后补充其他线路。", group.Name, plans, subscriptions), Href: fmt.Sprintf("/admin/node-groups?group=%d", group.ID)})
		}
	}
	return blockers, nil
}
func deletionPublicationWarnings(tx *gorm.DB, nodeIDs []uint, lock bool) ([]network.DeletionBlocker, error) {
	blockers := []network.DeletionBlocker{}
	for _, id := range nodeIDs {
		var deployments, activeJobs, tasks int64
		if err := tx.Model(&model.ProtocolDeployment{}).Where("node_id = ? AND status IN ?", id, []string{"running", "unknown"}).Count(&deployments).Error; err != nil {
			return nil, err
		}
		if err := tx.Model(&jobstore.Record{}).Where("owner = ? AND resource IN ? AND state IN ?", "system", []string{fmt.Sprintf("node:%d", id), fmt.Sprintf("node-publish:%d", id)}, []jobs.State{jobs.Running, jobs.Unknown}).Count(&activeJobs).Error; err != nil {
			return nil, err
		}

		var endpointIDs []uint
		if err := tx.Unscoped().Model(&model.ProtocolEndpoint{}).Where("node_id = ?", id).Pluck("id", &endpointIDs).Error; err != nil {
			return nil, err
		}
		endpointTargets := make([]string, 0, len(endpointIDs))
		for _, endpointID := range endpointIDs {
			endpointTargets = append(endpointTargets, strconv.FormatUint(uint64(endpointID), 10))
		}
		if err := tx.Table("tasks").Joins("JOIN task_items ON task_items.task_id = tasks.id").Where("task_items.status = ?", 1).Where("(task_items.target_type = ? AND task_items.target_id = ?) OR (task_items.target_type = ? AND task_items.target_id IN ?)", "node", strconv.FormatUint(uint64(id), 10), "protocol_endpoint", endpointTargets).Distinct("tasks.id").Count(&tasks).Error; err != nil {
			return nil, err
		}
		var publications []model.NodeConfigPublish
		q := tx.Where("node_id = ?", id)
		if lock {
			q = q.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := q.Find(&publications).Error; err != nil {
			return nil, err
		}
		leased := false
		for _, p := range publications {
			leased = leased || p.LeaseToken != ""
		}
		if deployments > 0 || activeJobs > 0 || tasks > 0 || leased {
			blockers = append(blockers, network.DeletionBlocker{Code: "publication_unconfirmed", Message: fmt.Sprintf("服务器 #%d 有尚未确认完成的发布任务。协议会立即从面板删除，配置撤除排队处理；完成前远端可能仍保留旧配置。", id), Href: "/admin/operation-logs?node_id=" + strconv.FormatUint(uint64(id), 10)})
		}
	}
	return blockers, nil
}
