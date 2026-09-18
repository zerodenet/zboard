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

type ProxyPoolSubscriptions struct {
	DB *gorm.DB
}

func (s ProxyPoolSubscriptions) LoadProxyPoolSubscription(ctx context.Context, actor network.ProxyPoolSubscriptionActor, id uint) (out network.ProxyPoolSubscriptionSnapshot, err error) {
	if s.DB == nil {
		return out, network.ErrProxyPoolMutationUnavailable
	}
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if !actor.System {
			if err := proxyPoolAdministrator(tx, actor.AccountID); err != nil {
				return err
			}
		}
		var row model.NodeProxyPool
		if err := tx.First(&row, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return network.ErrProxyPoolNotFound
			}
			return err
		}
		out = proxyPoolSubscriptionSnapshot(row)
		return nil
	})
	return
}

func (s ProxyPoolSubscriptions) CommitProxyPoolSubscription(ctx context.Context, actor network.ProxyPoolSubscriptionActor, snapshot network.ProxyPoolSubscriptionSnapshot, ciphertext string, nodeCount int, facts network.ProxyPoolConfigurationFacts, now time.Time) (out network.ProxyPoolRecord, err error) {
	if s.DB == nil {
		return out, network.ErrProxyPoolMutationUnavailable
	}
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row model.NodeProxyPool
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, snapshot.Pool.ID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return network.ErrProxyPoolNotFound
			}
			return err
		}
		if row.Revision != snapshot.Pool.Revision || row.SubscriptionURL != snapshot.SubscriptionURLCiphertext {
			return network.ErrProxyPoolConflict
		}
		var admin *model.User
		if !actor.System {
			current, err := providerAdmin(tx, actor.AccountID)
			if err != nil {
				if errors.Is(err, network.ErrProviderPermission) {
					return network.ErrProxyPoolMutationPermission
				}
				return err
			}
			admin = &current
		}
		var entries []model.NetworkEntry
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("proxy_pool_id = ?", row.ID).Order("id").Find(&entries).Error; err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.NodeID != row.NodeID {
				return &network.ProxyPoolMutationValidation{Message: fmt.Sprintf("入口 %s: 请选择入口节点 A 自己的共享代理池", entry.Name)}
			}
			if entry.Network != "tcp" && !facts.SupportsDatagram {
				message := strings.TrimSpace(facts.DatagramError)
				if message == "" {
					message = "代理池不支持 UDP 转发"
				}
				return &network.ProxyPoolMutationValidation{Message: fmt.Sprintf("入口 %s: %s", entry.Name, message)}
			}
		}
		row.Config, row.SubscriptionNodeCount, row.LastSyncAt = ciphertext, nodeCount, &now
		row.Revision++
		interval := row.SyncIntervalSeconds
		if interval <= 0 {
			interval = network.ProxyPoolDefaultSyncInterval
		}
		next := now.Add(time.Duration(interval) * time.Second)
		row.NextSyncAt, row.LastSyncError = &next, ""
		if err := tx.Save(&row).Error; err != nil {
			return err
		}
		audit := model.AuditLog{Actor: "system:proxy-pool-subscription", Action: "node_proxy_pool.subscription.sync", Target: fmt.Sprintf("node_proxy_pool:%d", row.ID), Detail: fmt.Sprintf("node=%d revision=%d nodes=%d", row.NodeID, row.Revision, nodeCount)}
		if admin != nil {
			audit.UserID, audit.Actor = &admin.ID, admin.Email
		}
		if err := tx.Create(&audit).Error; err != nil {
			return err
		}
		if err := EnqueuePublication(tx, row.NodeID, 0, actor.AccountID); err != nil {
			return err
		}
		out = proxyPoolRecordView(row)
		return nil
	})
	return
}

func (s ProxyPoolSubscriptions) RecordProxyPoolSubscriptionFailure(ctx context.Context, actor network.ProxyPoolSubscriptionActor, snapshot network.ProxyPoolSubscriptionSnapshot, message string, next time.Time) error {
	if s.DB == nil {
		return network.ErrProxyPoolMutationUnavailable
	}
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if !actor.System {
			if err := proxyPoolAdministrator(tx, actor.AccountID); err != nil {
				return err
			}
		}
		return tx.Model(&model.NodeProxyPool{}).
			Where("id = ? AND revision = ? AND subscription_url = ?", snapshot.Pool.ID, snapshot.Pool.Revision, snapshot.SubscriptionURLCiphertext).
			Updates(map[string]any{"last_sync_error": message, "next_sync_at": next.UTC()}).Error
	})
}

func (s ProxyPoolSubscriptions) ClaimDueProxyPoolSubscriptions(ctx context.Context, now, leaseUntil time.Time, limit int) (claims []network.ProxyPoolSubscriptionDueClaim, err error) {
	if s.DB == nil {
		return nil, network.ErrProxyPoolMutationUnavailable
	}
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []model.NodeProxyPool
		if err := tx.Where("auto_sync = ? AND subscription_url <> '' AND (next_sync_at IS NULL OR next_sync_at <= ?)", true, now).
			Order("next_sync_at, id").Limit(limit).Find(&rows).Error; err != nil {
			return err
		}
		claims = make([]network.ProxyPoolSubscriptionDueClaim, 0, len(rows))
		for _, row := range rows {
			result := tx.Model(&model.NodeProxyPool{}).
				Where("id = ? AND revision = ? AND auto_sync = ? AND subscription_url <> '' AND (next_sync_at IS NULL OR next_sync_at <= ?)", row.ID, row.Revision, true, now).
				Update("next_sync_at", leaseUntil)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 1 {
				claims = append(claims, network.ProxyPoolSubscriptionDueClaim{ID: row.ID, Revision: row.Revision})
			}
		}
		return nil
	})
	return
}

func proxyPoolSubscriptionSnapshot(row model.NodeProxyPool) network.ProxyPoolSubscriptionSnapshot {
	return network.ProxyPoolSubscriptionSnapshot{
		Pool: proxyPoolRecordView(row), SubscriptionURLCiphertext: row.SubscriptionURL,
		SubscriptionFormat: row.SubscriptionFormat, SubscriptionUserAgent: row.SubscriptionUserAgent,
	}
}
