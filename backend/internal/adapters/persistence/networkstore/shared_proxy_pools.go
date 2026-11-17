package networkstore

import (
	"context"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func bindSharedProxyPool(tx *gorm.DB, node, pool uint) error {
	return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.NodeProxyPoolBinding{NodeID: node, ProxyPoolID: pool}).Error
}

// A single source update fans out once per referencing server. Runtime rendering
// still includes the pool only when a forwarding entry actually uses it.
func publishSharedProxyPool(tx *gorm.DB, pool, actor uint) error {
	var nodes []uint
	if e := tx.Raw(`SELECT node_id FROM node_proxy_pool_bindings WHERE proxy_pool_id=?
 UNION SELECT node_id FROM network_entries WHERE proxy_pool_id=? AND enabled=true AND deployment_mode='managed'
 UNION SELECT node_id FROM node_proxy_pools WHERE id=? AND node_id IS NOT NULL AND node_id>0 ORDER BY node_id`, pool, pool, pool).Scan(&nodes).Error; e != nil {
		return e
	}

	if e := lockCurrentResourceNodes(tx, nodes); e != nil {
		return e
	}
	for _, id := range nodes {
		if id > 0 {
			if e := EnqueuePublication(tx, id, 0, actor); e != nil {
				return e
			}
		}
	}
	return nil
}

func (s ProxyPoolMutations) SetProxyPoolNodeReference(ctx context.Context, actor, poolID, nodeID uint, attach bool) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if e := proxyPoolAdministrator(tx, actor); e != nil {
			return e
		}
		var node model.Node
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&node, nodeID).Error; e != nil {
			return e
		}
		if network.NodeManagementClosed(node.LifecycleStatus) {
			return network.ErrProxyPoolConflict
		}
		var pool model.NodeProxyPool
		if e := tx.Clauses(currentResourceReadLock(tx)).First(&pool, poolID).Error; e != nil {
			return currentResourceReadError(e)
		}
		if attach {
			if err := bindSharedProxyPool(tx, nodeID, poolID); err != nil {
				return err
			}
			return auditProxyPoolReference(tx, actor, poolID, nodeID, true)
		}
		var references int64
		if e := tx.Model(&model.NetworkEntry{}).Where("node_id=? AND proxy_pool_id=?", nodeID, poolID).Count(&references).Error; e != nil {
			return e
		}
		if references > 0 {
			return &network.ProxyPoolMutationValidation{Message: fmt.Sprintf("这台服务器仍有 %d 条前置服务引用该池，请先切换转发路径", references)}
		}
		if err := tx.Where("node_id=? AND proxy_pool_id=?", nodeID, poolID).Delete(&model.NodeProxyPoolBinding{}).Error; err != nil {
			return err
		}
		return auditProxyPoolReference(tx, actor, poolID, nodeID, false)
	})
}

func auditProxyPoolReference(tx *gorm.DB, actor, pool, node uint, attach bool) error {
	return tx.Create(&model.AuditLog{UserID: &actor, Actor: fmt.Sprintf("user:%d", actor), Action: "node_proxy_pool.reference", Target: fmt.Sprintf("node_proxy_pool:%d", pool), Detail: fmt.Sprintf("node=%d attached=%t", node, attach)}).Error
}
