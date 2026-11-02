package identitystore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Deletion struct {
	DB      *gorm.DB
	Publish func(*gorm.DB, uint, uint, uint) error
}

func cleanupDays(db *gorm.DB, locked bool) (int, error) {
	var setting model.SystemConfig
	if locked {
		db = db.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	err := db.Where("config_key=?", identity.CleanupDaysKey).Take(&setting).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return identity.ParseCleanupDays(setting.Value)
}
func (s Deletion) CleanupEnabled(ctx context.Context) (bool, error) {
	days, err := cleanupDays(s.DB.WithContext(ctx), false)
	return days > 0, err
}

func deletionActor(tx *gorm.DB, actor uint) error {
	var user model.User
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id=? AND is_admin=? AND status=?", actor, true, "active").Take(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return identity.ErrPermission
		}
		return err
	}
	return nil
}
func deletionTarget(tx *gorm.DB, id uint) (model.User, error) {
	var user model.User
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = identity.ErrAccountNotFound
	}
	return user, err
}

// These queries use the owner's indexed rows, never historical traffic tables.
// Account deletion must have constant cost relative to telemetry history.
func deletionPreview(tx *gorm.DB, user model.User, actor uint, now time.Time) (identity.DeletionPreview, error) {
	out := identity.DeletionPreview{UserID: user.ID, Blockers: []string{}, HistoryRetained: true}
	if user.ID == actor {
		out.Blockers = append(out.Blockers, "不能删除当前登录账号。")
	}
	if user.IsAdmin {
		out.Blockers = append(out.Blockers, "管理员账号受保护，请先撤销其管理员权限。")
	}
	checks := []struct {
		query   *gorm.DB
		message string
	}{
		{tx.Model(&model.Subscription{}).Where("user_id=? AND ended_at IS NULL AND end_at>?", user.ID, now), "仍有未结束的订阅，请先在订阅管理中结束并结算。"},
		{tx.Model(&model.SubscriptionMember{}).Where("user_id=? AND removed_at IS NULL AND status='active' AND subscription_id IN (SELECT id FROM subscriptions WHERE ended_at IS NULL AND end_at>?)", user.ID, now), "仍属于共享订阅，请先移除共享成员关系。"},
	}
	for _, guard := range accountFinancialGuards() {
		checks = append(checks, struct {
			query   *gorm.DB
			message string
		}{tx.Table(guard.table).Where(fmt.Sprintf(guard.condition, "?"), user.ID), guard.reason + "，请先处理后再删除。"})
	}
	for _, check := range checks {
		// Lock the exact evidence selected after acquiring the owner lock. This
		// is a current read under MySQL REPEATABLE READ, not an older snapshot.
		var one []map[string]any
		if err := check.query.Clauses(clause.Locking{Strength: "SHARE"}).Select("1 AS id").Limit(1).Find(&one).Error; err != nil {
			return out, err
		}
		if len(one) > 0 {
			out.Blockers = append(out.Blockers, check.message)
		}
	}
	out.Allowed = len(out.Blockers) == 0
	return out, nil
}

func (s Deletion) Inspect(ctx context.Context, actor, id uint, now time.Time) (out identity.DeletionPreview, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := deletionActor(tx, actor); err != nil {
			return err
		}
		user, err := deletionTarget(tx, id)
		if err != nil {
			return err
		}
		out, err = deletionPreview(tx, user, actor, now)
		return err
	})
	return
}
func (s Deletion) Remove(ctx context.Context, actor, id uint, now time.Time) error {
	return jobstore.New(s.DB).WithLedgerLock(ctx, func(tx *gorm.DB) error {
		if err := deletionActor(tx, actor); err != nil {
			return err
		}
		user, err := deletionTarget(tx, id)
		if err != nil {
			return err
		}
		preview, err := deletionPreview(tx, user, actor, now)
		if err != nil {
			return err
		}
		if !preview.Allowed {
			return &identity.DeletionBlocked{Preview: preview}
		}
		return s.hardDeleteAccount(tx, user, actor, "manual", now)
	})
}

func (s Deletion) hardDeleteAccount(tx *gorm.DB, user model.User, actor uint, origin string, now time.Time) error {
	// Preserve immutable numeric owner attribution, receipts, cursors, orders,
	// subscriptions and ledger entries. No telemetry scan or cascade occurs.
	var subject model.AccountSubject
	if err := tx.First(&subject, user.ID).Error; err != nil {
		return fmt.Errorf("accounting identity unavailable: %w", err)
	}
	var routes []struct{ NodeID, ProtocolEndpointID uint }
	if err := tx.Model(&model.ProtocolCredential{}).Select("DISTINCT protocol_credentials.node_id, protocol_credentials.protocol_endpoint_id").
		Joins("JOIN nodes n ON n.id=protocol_credentials.node_id AND n.archived_at IS NULL").
		Joins("JOIN protocol_endpoints e ON e.id=protocol_credentials.protocol_endpoint_id AND e.deleted_at IS NULL").
		Where("protocol_credentials.user_id=? AND protocol_credentials.revoked_at IS NULL", user.ID).Scan(&routes).Error; err != nil {
		return err
	}
	if len(routes) > 0 && s.Publish == nil {
		return errors.New("credential revocation publication unavailable")
	}
	for _, owned := range []struct {
		row       any
		condition string
	}{
		{&model.UserAPIToken{}, "user_id=?"}, {&model.SubscriptionToken{}, "user_id=?"},
		{&model.ExternalIdentity{}, "user_id=?"}, {&model.SubscriptionMember{}, "user_id=?"}, {&model.AnnouncementRead{}, "user_id=?"},
		{&model.RegistrationEmailChallenge{}, "email=?"},
	} {
		var value any = user.ID
		if owned.condition == "email=?" {
			value = user.Email
		}
		if err := tx.Unscoped().Where(owned.condition, value).Delete(owned.row).Error; err != nil {
			return err
		}
	}
	if err := tx.Model(&model.ProtocolCredential{}).Where("user_id=? AND revoked_at IS NULL", user.ID).Updates(map[string]any{"status": "revoked", "revoked_at": now}).Error; err != nil {
		return err
	}
	for _, route := range routes {
		if err := s.Publish(tx, route.NodeID, route.ProtocolEndpointID, actor); err != nil {
			return err
		}
	}
	if err := tx.Where("account_id=?", user.ID).Delete(&model.AccountRegistrationEvent{}).Error; err != nil {
		return err
	}
	if err := tx.Model(&model.SubscriptionAutomaticRenewal{}).Where("user_id=? AND enabled=?", user.ID, true).Updates(map[string]any{"enabled": false, "state": "disabled", "message": "账号已删除，自动续费已关闭。", "revision": gorm.Expr("revision+1"), "updated_at": now}).Error; err != nil {
		return err
	}
	result := tx.Unscoped().Where("id=?", user.ID).Delete(&model.User{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return identity.ErrAccountNotFound
	}
	var actorID *uint
	if actor != 0 {
		actorID = &actor
	}
	actorLabel := "system"
	if actor != 0 {
		actorLabel = fmt.Sprintf("user:%d", actor)
	}
	return tx.Create(&model.AuditLog{UserID: actorID, Actor: actorLabel, Action: "account.delete", Target: fmt.Sprintf("user:%d", user.ID), Detail: "hard_deleted=true;history=retained;origin=" + origin}).Error
}

func cleanupCandidates(db *gorm.DB, now time.Time, days int) *gorm.DB {
	cutoff := now.AddDate(0, 0, -days)
	// The grace period restarts at the end of the last entitlement, not at
	// registration for customers whose paid subscription has just expired.
	query := db.Model(&model.User{}).Where("is_admin=? AND users.created_at<=?", false, cutoff).
		Where("NOT EXISTS (SELECT 1 FROM subscriptions s WHERE s.user_id=users.id AND "+entitlementEnd("s")+">?)", cutoff).
		Where("NOT EXISTS (SELECT 1 FROM subscription_members m JOIN subscriptions s ON s.id=m.subscription_id WHERE m.user_id=users.id AND "+sharedEntitlementEnd()+">?)", cutoff)
	for _, guard := range accountFinancialGuards() {
		query = query.Where("NOT " + guard.exists("users.id"))
	}
	return query

}

func (s Deletion) Cleanup(ctx context.Context, now time.Time, limit int) (int, error) {
	if limit < 1 || limit > 100 {
		limit = 100
	}
	days, err := cleanupDays(s.DB.WithContext(ctx), false)
	if err != nil || days == 0 {
		return 0, err
	}
	var state model.AccountCleanupState
	err = s.DB.WithContext(ctx).First(&state, 1).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, err
	}
	// Scan at most 500 account IDs per tick, including protected accounts.
	// A persistent cursor prevents repeatedly walking every paying customer.
	var window []uint
	if err := s.DB.WithContext(ctx).Model(&model.User{}).Where("id > ?", state.LastUserID).Order("id ASC").Limit(500).Pluck("id", &window).Error; err != nil {
		return 0, err
	}
	cursor := uint(0)
	var ids []uint
	if len(window) > 0 {
		cursor = window[len(window)-1]
		if err := cleanupCandidates(s.DB.WithContext(ctx), now, days).Where("users.id IN ?", window).Order("users.id ASC").Limit(limit).Pluck("users.id", &ids).Error; err != nil {
			return 0, err
		}
		if len(ids) == limit {
			cursor = ids[len(ids)-1]
		}
	}
	deleted := 0
	for _, id := range ids {
		removed := false
		err := jobstore.New(s.DB).WithLedgerLock(ctx, func(tx *gorm.DB) error {
			// Serialize policy changes and recheck candidates under the same
			// owner lock used by financial/subscription writers before deleting.
			days, err := cleanupDays(tx, true)
			if err != nil || days == 0 {
				return err
			}
			user, err := deletionTarget(tx, id)
			if errors.Is(err, identity.ErrAccountNotFound) {
				return nil
			}
			if err != nil {
				return err
			}
			var found []uint
			if err := cleanupCandidates(tx, now, days).Where("users.id=?", id).Clauses(clause.Locking{Strength: "UPDATE"}).Pluck("users.id", &found).Error; err != nil {
				return err
			}
			if len(found) == 0 {
				return nil
			}
			preview, err := deletionPreview(tx, user, 0, now)
			if err != nil {
				return err
			}
			if !preview.Allowed {
				return nil
			}
			if err := s.hardDeleteAccount(tx, user, 0, "automatic", now); err != nil {
				return err
			}
			removed = true
			return nil
		})
		if err != nil {
			return deleted, err
		}
		if removed {
			deleted++
		}
	}
	err = s.DB.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoUpdates: clause.AssignmentColumns([]string{"last_user_id"})}).Create(&model.AccountCleanupState{ID: 1, LastUserID: cursor}).Error
	return deleted, err
}
