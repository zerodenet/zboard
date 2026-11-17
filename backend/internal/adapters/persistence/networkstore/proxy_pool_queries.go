package networkstore

import (
	"context"
	"errors"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type ProxyPoolQueries struct {
	DB *gorm.DB
}

func proxyPoolQueryRecord(pool model.NodeProxyPool) network.ProxyPoolRecord {
	return network.ProxyPoolRecord{ID: pool.ID, NodeID: pool.NodeID, Name: pool.Name, SubscriptionFormat: pool.SubscriptionFormat,
		AutoSync: pool.AutoSync, SyncIntervalSeconds: pool.SyncIntervalSeconds, SubscriptionNodeCount: pool.SubscriptionNodeCount,
		LastSyncAt: pool.LastSyncAt, NextSyncAt: pool.NextSyncAt, LastSyncError: pool.LastSyncError, Revision: pool.Revision,
		CreatedAt: pool.CreatedAt, UpdatedAt: pool.UpdatedAt, ConfigCiphertext: pool.Config,
		SubscriptionURLCiphertext: pool.SubscriptionURL, SubscriptionUserAgent: pool.SubscriptionUserAgent}
}

func (q ProxyPoolQueries) GetProxyPool(ctx context.Context, actor, id uint) (out network.ProxyPoolRecord, err error) {
	err = q.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := providerAdmin(tx, actor); err != nil {
			if errors.Is(err, network.ErrProviderPermission) {
				return network.ErrProxyPoolQueryPermission
			}
			return err
		}
		var pool model.NodeProxyPool
		if err := tx.First(&pool, id).Error; err != nil {
			return err
		}
		out = proxyPoolQueryRecord(pool)
		var e error
		out.Nodes, e = proxyPoolReferenceNodes(tx, pool.ID)
		return e
	})
	return
}

func (q ProxyPoolQueries) ListProxyPools(ctx context.Context, actor, nodeID uint) (result []network.ProxyPool, err error) {
	if q.DB == nil {
		return nil, network.ErrProxyPoolQueryUnavailable
	}
	err = q.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := providerAdmin(tx, actor); err != nil {
			if errors.Is(err, network.ErrProviderPermission) {
				return network.ErrProxyPoolQueryPermission
			}
			return err
		}
		type row struct {
			model.NodeProxyPool
			EntryCount int64 `gorm:"column:entry_count"`
		}
		var rows []row
		query := tx.Model(&model.NodeProxyPool{}).
			Select("node_proxy_pools.*, COUNT(network_entries.id) AS entry_count").
			Joins("LEFT JOIN network_entries ON network_entries.proxy_pool_id = node_proxy_pools.id").
			Group("node_proxy_pools.id").Order("node_proxy_pools.id")
		if nodeID > 0 {
			query = query.Where("node_proxy_pools.node_id=? OR EXISTS (SELECT 1 FROM node_proxy_pool_bindings b WHERE b.proxy_pool_id=node_proxy_pools.id AND b.node_id=?) OR EXISTS (SELECT 1 FROM network_entries e WHERE e.proxy_pool_id=node_proxy_pools.id AND e.node_id=?)", nodeID, nodeID, nodeID)
		}
		if err := query.Scan(&rows).Error; err != nil {
			return err
		}
		ids := make([]uint, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, row.ID)
		}
		var bindings []struct {
			PoolID uint
			ID     uint
			Name   string
		}
		if len(ids) > 0 {
			if e := tx.Table("node_proxy_pool_bindings b").Select("b.proxy_pool_id AS pool_id,n.id,n.name").Joins("JOIN nodes n ON n.id=b.node_id").Where("b.proxy_pool_id IN ?", ids).Order("n.id").Scan(&bindings).Error; e != nil {
				return e
			}
		}
		nodes := map[uint][]network.ProxyPoolNode{}
		for _, r := range bindings {
			nodes[r.PoolID] = append(nodes[r.PoolID], network.ProxyPoolNode{ID: r.ID, Name: r.Name})
		}
		result = make([]network.ProxyPool, 0, len(rows))
		for _, item := range rows {
			pool := item.NodeProxyPool
			record := proxyPoolQueryRecord(pool)
			record.Nodes = nodes[pool.ID]
			if record.Nodes == nil {
				record.Nodes = []network.ProxyPoolNode{}
			}
			result = append(result, network.ProxyPool{ProxyPoolRecord: record, EntryCount: item.EntryCount, SubscriptionConfigured: pool.SubscriptionURL != ""})
		}
		return nil
	})
	return
}

func proxyPoolReferenceNodes(tx *gorm.DB, id uint) ([]network.ProxyPoolNode, error) {
	rows := []network.ProxyPoolNode{}
	e := tx.Table("node_proxy_pool_bindings b").Select("n.id,n.name").Joins("JOIN nodes n ON n.id=b.node_id").Where("b.proxy_pool_id=?", id).Order("n.id").Scan(&rows).Error
	return rows, e
}
