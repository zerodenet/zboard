package entitlementstore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/networkstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type CredentialMigration struct {
	DB     *gorm.DB
	Issuer entitlements.CredentialIssuer
}
type CredentialMigrationStatus = entitlements.CredentialMigrationStatus

func (s CredentialMigration) Status(ctx context.Context, actor, subscription uint) (out CredentialMigrationStatus, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if e := subscriptionReader(tx, actor, true); e != nil {
			return e
		}
		var sub model.Subscription
		if e := tx.First(&sub, subscription).Error; e != nil {
			return credentialMigrationReadError(e)
		}
		out.SubscriptionID = subscription
		out.State = "legacy"
		var root model.ProtocolCredential
		if e := tx.Select("id").Where("subscription_id=? AND current_scope=1 AND material_scope=1", subscription).Limit(1).Find(&root).Error; e != nil {
			return e
		}
		if root.ID == 0 {
			return tx.Model(&model.ProtocolCredential{}).Where("subscription_id=? AND entitlement_epoch=? AND material_version=0 AND current_scope=1 AND secret<>''", subscription, sub.EntitlementEpoch).Count(&out.LegacyCredentials).Error
		}
		out.State = "current"
		var rows []model.ProtocolCredential
		if e := tx.Select("id,status,compatibility_started_at,compatibility_until,CASE WHEN secret<>'' THEN 'present' ELSE '' END AS secret").Where("subscription_id=? AND entitlement_epoch=? AND material_version=0", subscription, sub.EntitlementEpoch).Find(&rows).Error; e != nil {
			return e
		}
		out.CanRollback = len(rows) > 0
		for _, r := range rows {
			out.LegacyCredentials++
			if r.Secret == "" {
				out.RetiredCredentials++
				out.CanRollback = false
				continue
			}
			if r.Status == "revoked" || r.CompatibilityUntil != nil && !r.CompatibilityUntil.After(time.Now().UTC()) {
				out.RevokingCredentials++
				out.CanRollback = false
				continue
			}
			if r.CompatibilityStartedAt == nil {
				out.PendingConfirmation++
			} else {
				out.CompatibleCredentials++
			}
			if r.CompatibilityUntil != nil && (out.CompatibilityUntil == nil || r.CompatibilityUntil.Before(*out.CompatibilityUntil)) {
				at := *r.CompatibilityUntil
				out.CompatibilityUntil = &at
			}
		}
		if out.PendingConfirmation > 0 {
			out.State = "awaiting_confirmation"
		} else if out.CompatibleCredentials > 0 {
			out.State = "compatibility"
		} else if out.RevokingCredentials > 0 {
			out.State = "revoking"
		}
		return nil
	})
	return out, err
}

func (s CredentialMigration) Prepare(ctx context.Context, actor, subscription uint) error {
	return RunCredentialTransaction(ctx, s.DB, func(tx *gorm.DB) error {
		if e := subscriptionReader(tx, actor, true); e != nil {
			return e
		}
		var sub model.Subscription
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&sub, subscription).Error; e != nil {
			return credentialMigrationReadError(e)
		}
		if sub.Status != "active" || !sub.EndAt.After(time.Now().UTC()) || sub.FlowUsed >= sub.FlowTotal {
			return entitlements.ErrSubscriptionMutationInvalid
		}
		endpoints, e := networkstore.CurrentCredentialEndpoints(tx, sub.NodeGroupID)
		if e != nil {
			return e
		}
		supported := 0
		for _, ep := range endpoints {
			if s.Issuer.Supports(ep.Protocol) {
				supported++
			}
		}
		if supported == 1 && (sub.DeviceLimit > 0 || sub.SpeedLimitMbps > 0) {
			return fmt.Errorf("%w: 单入口硬限速或设备数限制无法在当前内核中由双凭证共同承担，未开始迁移", entitlements.ErrCredentialMigrationBlocked)
		}
		for _, ep := range endpoints {
			if !s.Issuer.Supports(ep.Protocol) {
				continue
			}
			if !ep.ManagedPrincipalReady || (strings.EqualFold(ep.Protocol, "mieru") && !ep.MieruPrincipalReady) {
				return fmt.Errorf("%w: 接入点 %s 尚未确认支持稳定的用户归属标识，请先向该节点发布配置后重试", entitlements.ErrCredentialMigrationBlocked, ep.Name)
			}
			// A one-user SS2022 ChaCha listener cannot safely host old and new
			// authentication simultaneously. Do not silently break old clients.
			if strings.EqualFold(ep.Protocol, "shadowsocks") {
				cipher, ok := s.Issuer.(subscriptionMaterialIssuer)
				if !ok {
					return entitlements.ErrCredentialMaterial
				}
				raw, e := cipher.Decrypt(ep.ServerConfig)
				if e != nil {
					return e
				}
				if strings.Contains(strings.ToLower(raw), "2022-blake3-chacha20-poly1305") {
					return fmt.Errorf("%w: SS2022 ChaCha 接入点不支持双凭证兼容，请先调整该接入点后再迁移", entitlements.ErrCredentialMigrationBlocked)
				}
			}
		}
		if _, e := prepareSubscriptionMaterial(tx, sub, s.Issuer); e != nil {
			return e
		}
		if e := networkstore.EnqueueSubscriptionPublications(tx, sub.ID, actor); e != nil {
			return e
		}
		return tx.Create(&model.AuditLog{UserID: &actor, Actor: "admin", Action: "subscription.credential_migration.prepare", Target: fmt.Sprintf("subscription:%d", sub.ID), Detail: "legacy credentials retained; compatibility begins after exact node confirmation"}).Error
	})
}

func (s CredentialMigration) Rollback(ctx context.Context, actor, subscription uint) error {
	return RunCredentialTransaction(ctx, s.DB, func(tx *gorm.DB) error {
		if e := subscriptionReader(tx, actor, true); e != nil {
			return e
		}
		var sub model.Subscription
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&sub, subscription).Error; e != nil {
			return credentialMigrationReadError(e)
		}
		var old []model.ProtocolCredential
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("subscription_id=? AND entitlement_epoch=? AND material_version=0", subscription, sub.EntitlementEpoch).Find(&old).Error; e != nil {
			return e
		}
		if len(old) == 0 {
			return fmt.Errorf("%w: 该订阅没有可恢复的旧凭证", entitlements.ErrCredentialMigrationBlocked)
		}
		now := time.Now().UTC()
		for _, r := range old {
			if r.Secret == "" || r.Status == "revoked" || r.CompatibilityUntil != nil && !r.CompatibilityUntil.After(now) {
				return fmt.Errorf("%w: 旧凭证已进入清退阶段，不能无损回退", entitlements.ErrCredentialMigrationBlocked)
			}
		}
		if e := tx.Model(&model.Subscription{}).Where("id=?", sub.ID).UpdateColumn("credential_material_mode", 2).Error; e != nil {
			return e
		}
		if e := tx.Model(&model.ProtocolCredential{}).Where("subscription_id=? AND current_scope=1 AND material_scope=1", sub.ID).Updates(map[string]any{"status": "revoked", "revoked_at": now, "current_scope": nil, "material_scope": nil, "secret": ""}).Error; e != nil {
			return e
		}
		if e := tx.Model(&model.ProtocolCredential{}).Where("subscription_id=? AND entitlement_epoch=? AND material_version=0", sub.ID, sub.EntitlementEpoch).Updates(map[string]any{"compatibility_started_at": nil, "compatibility_until": nil}).Error; e != nil {
			return e
		}
		if e := networkstore.EnqueueSubscriptionPublications(tx, sub.ID, actor); e != nil {
			return e
		}
		return tx.Create(&model.AuditLog{UserID: &actor, Actor: "admin", Action: "subscription.credential_migration.rollback", Target: fmt.Sprintf("subscription:%d", sub.ID), Detail: "legacy output restored; retired material verifier kept for historical accounting"}).Error
	})
}

func credentialMigrationReadError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return entitlements.ErrAccessNotFound
	}
	return err
}
