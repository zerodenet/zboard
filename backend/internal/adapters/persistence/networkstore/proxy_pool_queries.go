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
		return nil
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
		if err := tx.Model(&model.NodeProxyPool{}).
			Select("node_proxy_pools.*, COUNT(network_entries.id) AS entry_count").
			Joins("LEFT JOIN network_entries ON network_entries.proxy_pool_id = node_proxy_pools.id").
			Where("node_proxy_pools.node_id = ?", nodeID).
			Group("node_proxy_pools.id").Order("node_proxy_pools.id").Scan(&rows).Error; err != nil {
			return err
		}
		result = make([]network.ProxyPool, 0, len(rows))
		for _, item := range rows {
			pool := item.NodeProxyPool
			result = append(result, network.ProxyPool{ProxyPoolRecord: proxyPoolQueryRecord(pool), EntryCount: item.EntryCount, SubscriptionConfigured: pool.SubscriptionURL != ""})
		}
		return nil
	})
	return
}
