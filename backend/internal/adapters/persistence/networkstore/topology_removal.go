package networkstore

import (
	"context"
	"errors"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TopologyRemoval struct{ DB *gorm.DB }

func (s TopologyRemoval) RemoveEntry(ctx context.Context, actor, id uint) error {
	return jobstore.New(s.DB).WithLedgerLock(ctx, func(tx *gorm.DB) error {
		user, err := providerAdmin(tx, actor)
		if err != nil {
			if errors.Is(err, network.ErrProviderPermission) {
				return network.ErrResourcePermission
			}
			return err
		}
		var entry model.NetworkEntry
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&entry, id).Error; err != nil {
			return removalError(err)
		}
		check, err := entryDeletionCheck(tx, entry, true)
		if err != nil {
			return err
		}
		if len(check.Blockers) > 0 {
			return &network.DeletionBlocked{Check: check}
		}
		if _, err := RemoveTopologyEntries(tx, actor, TopologySelection{EntryID: id}); err != nil {
			return err
		}
		return tx.Create(&model.AuditLog{UserID: &user.ID, Actor: user.Email, Action: "network_entry.delete", Target: fmt.Sprintf("network_entry:%d", id), Detail: fmt.Sprintf("node=%d endpoint=%d", entry.NodeID, entry.EndpointID)}).Error
	})
}

func (s TopologyRemoval) CheckEntryDeletion(ctx context.Context, actor, id uint) (out network.DeletionCheck, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := deletionAdministrator(tx, actor); err != nil {
			return err
		}
		var entry model.NetworkEntry
		if err := tx.First(&entry, id).Error; err != nil {
			return removalError(err)
		}
		out, err = entryDeletionCheck(tx, entry, false)
		return err
	})
	return
}
func entryDeletionCheck(tx *gorm.DB, entry model.NetworkEntry, lock bool) (network.DeletionCheck, error) {
	check := network.DeletionCheck{ID: entry.ID, Name: entry.Name, Blockers: []network.DeletionBlocker{}, Effects: []string{"删除此前置入口及权限组关联；父协议和其他线路保留。", "撤回仅由此前置入口授予的访问权限，历史流量账本保留。", "面板托管的转发配置将排队撤除；外部转发需自行停止。"}}
	var endpoint model.ProtocolEndpoint
	if err := tx.Unscoped().First(&endpoint, entry.EndpointID).Error; err != nil {
		return check, removalError(err)
	}
	var nodes []model.Node
	q := tx.Where("id IN ?", uniqueSortedIDs([]uint{entry.NodeID, endpoint.NodeID}))
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := q.Order("id").Find(&nodes).Error; err != nil {
		return check, err
	}
	ids := []uint{}
	for _, node := range nodes {
		ids = append(ids, node.ID)
		if network.NodeManagementClosed(node.LifecycleStatus) {
			check.Blockers = append(check.Blockers, network.DeletionBlocker{Code: "node_deleting", Message: fmt.Sprintf("关联服务器 #%d 正在删除或已删除，请查看服务器删除结果。", node.ID), Href: fmt.Sprintf("/admin/nodes?deletion=%d", node.ID)})
		}
	}
	groups, err := topologyDeletionGroupBlockers(tx, 0, entry.ID, lock)
	if err != nil {
		return check, err
	}
	check.Blockers = append(check.Blockers, groups...)
	publications, err := deletionPublicationBlockers(tx, ids, lock)
	if err != nil {
		return check, err
	}
	check.Blockers = append(check.Blockers, publications...)
	return check, nil
}
