package networkstore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ProxyPoolMutations struct {
	DB *gorm.DB
}

func (s ProxyPoolMutations) LoadProxyPoolMutation(ctx context.Context, actor, id uint) (out network.ProxyPoolMutationSnapshot, err error) {
	if s.DB == nil {
		return out, network.ErrProxyPoolMutationUnavailable
	}
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := proxyPoolAdministrator(tx, actor); err != nil {
			return err
		}
		var row model.NodeProxyPool
		if err := tx.First(&row, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return network.ErrProxyPoolNotFound
			}
			return err
		}
		out = proxyPoolMutationSnapshot(row)
		return nil
	})
	return
}

func (s ProxyPoolMutations) CommitProxyPoolMutation(ctx context.Context, actor uint, before *network.ProxyPoolMutationSnapshot, change network.ProxyPoolMutationChange) (out network.ProxyPoolRecord, err error) {
	if s.DB == nil {
		return out, network.ErrProxyPoolMutationUnavailable
	}
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var node model.Node
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&node, change.NodeID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return &network.ProxyPoolMutationValidation{Message: "节点不存在"}
			}
			return err
		}
		if node.LifecycleStatus == "deleting" {
			return &network.ProxyPoolMutationValidation{Message: "节点正在删除"}
		}

		var row model.NodeProxyPool
		if before != nil {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, before.Pool.ID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return network.ErrProxyPoolNotFound
				}
				return err
			}
			if row.NodeID != before.Pool.NodeID || row.Revision != before.Pool.Revision {
				return network.ErrProxyPoolConflict
			}
		} else {
			row.NodeID = change.NodeID
			row.SyncIntervalSeconds = network.ProxyPoolDefaultSyncInterval
		}
		admin, err := providerAdmin(tx, actor)
		if err != nil {
			if errors.Is(err, network.ErrProviderPermission) {
				return network.ErrProxyPoolMutationPermission
			}
			return err
		}

		if change.Delete {
			var count int64
			if err := tx.Model(&model.NetworkEntry{}).Where("proxy_pool_id = ?", row.ID).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return &network.ProxyPoolMutationValidation{Message: fmt.Sprintf("仍有 %d 条前置服务引用该池，请先切换转发路径", count)}
			}
			if err := tx.Delete(&row).Error; err != nil {
				return err
			}
		} else {
			row.NodeID, row.Name = change.NodeID, change.Name
			if change.ReplaceConfig {
				row.Config = change.ConfigCiphertext
			}
			if row.Config == "" {
				return &network.ProxyPoolMutationValidation{Message: "请配置代理池成员"}
			}
			if change.ReplaceSubscription {
				applyProxyPoolSubscriptionChange(&row, change)
			}
			row.Revision++
			if before == nil {
				if err := tx.Create(&row).Error; err != nil {
					return proxyPoolConstraintError(err)
				}
			} else if err := tx.Save(&row).Error; err != nil {
				return proxyPoolConstraintError(err)
			}
			var entries []model.NetworkEntry
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("proxy_pool_id = ?", row.ID).Order("id").Find(&entries).Error; err != nil {
				return err
			}
			for _, entry := range entries {
				if entry.NodeID != row.NodeID {
					return &network.ProxyPoolMutationValidation{Message: fmt.Sprintf("入口 %s: 请选择入口节点 A 自己的共享代理池", entry.Name)}
				}
				if entry.Network != "tcp" && !change.ConfigurationSupportsDatagram {
					message := strings.TrimSpace(change.ConfigurationDatagramError)
					if message == "" {
						message = "代理池不支持 UDP 转发"
					}
					return &network.ProxyPoolMutationValidation{Message: fmt.Sprintf("入口 %s: %s", entry.Name, message)}
				}
			}
		}

		if err := tx.Create(&model.AuditLog{
			UserID: &admin.ID, Actor: admin.Email, Action: "node_proxy_pool.update",
			Target: fmt.Sprintf("node_proxy_pool:%d", row.ID), Detail: fmt.Sprintf("node=%d revision=%d", row.NodeID, row.Revision),
		}).Error; err != nil {
			return err
		}
		if err := EnqueuePublication(tx, row.NodeID, 0, actor); err != nil {
			return err
		}
		out = proxyPoolRecordView(row)
		return nil
	})
	return
}

func proxyPoolAdministrator(tx *gorm.DB, actor uint) error {
	if _, err := providerAdmin(tx, actor); err != nil {
		if errors.Is(err, network.ErrProviderPermission) {
			return network.ErrProxyPoolMutationPermission
		}
		return err
	}
	return nil
}

func proxyPoolMutationSnapshot(row model.NodeProxyPool) network.ProxyPoolMutationSnapshot {
	return network.ProxyPoolMutationSnapshot{
		Pool: proxyPoolRecordView(row), ConfigCiphertext: row.Config,
		SubscriptionURLCiphertext: row.SubscriptionURL, SubscriptionUserAgent: row.SubscriptionUserAgent,
	}
}

func proxyPoolRecordView(row model.NodeProxyPool) network.ProxyPoolRecord {
	return network.ProxyPoolRecord{
		ID: row.ID, NodeID: row.NodeID, Name: row.Name, SubscriptionFormat: row.SubscriptionFormat,
		AutoSync: row.AutoSync, SyncIntervalSeconds: row.SyncIntervalSeconds,
		SubscriptionNodeCount: row.SubscriptionNodeCount, LastSyncAt: row.LastSyncAt, NextSyncAt: row.NextSyncAt,
		LastSyncError: row.LastSyncError, Revision: row.Revision, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func applyProxyPoolSubscriptionChange(row *model.NodeProxyPool, change network.ProxyPoolMutationChange) {
	if change.SubscriptionURLCiphertext == "" {
		row.SubscriptionURL, row.SubscriptionFormat, row.SubscriptionUserAgent = "", "", ""
		row.AutoSync, row.SyncIntervalSeconds, row.SubscriptionNodeCount = false, network.ProxyPoolDefaultSyncInterval, 0
		row.LastSyncAt, row.NextSyncAt, row.LastSyncError = nil, nil, ""
		return
	}
	row.SubscriptionURL = change.SubscriptionURLCiphertext
	row.SubscriptionFormat = change.SubscriptionFormat
	row.SubscriptionUserAgent = change.SubscriptionUserAgent
	row.AutoSync = change.AutoSync
	row.SyncIntervalSeconds = change.SyncIntervalSeconds
	if change.SubscriptionSourceChanged {
		row.SubscriptionNodeCount, row.LastSyncAt, row.LastSyncError = 0, nil, ""
	}
	if change.InitialSyncAt != nil {
		at := change.InitialSyncAt.UTC()
		row.LastSyncAt, row.SubscriptionNodeCount, row.LastSyncError = &at, change.InitialNodeCount, ""
		next := at.Add(time.Duration(row.SyncIntervalSeconds) * time.Second)
		row.NextSyncAt = &next
	} else if row.AutoSync {
		next := change.Now
		row.NextSyncAt = &next
	} else {
		row.NextSyncAt = nil
	}
}

func proxyPoolConstraintError(err error) error {
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "duplicate") || strings.Contains(message, "unique constraint") {
		return &network.ProxyPoolMutationValidation{Message: "同一节点的代理池名称不能重复"}
	}
	return err
}
