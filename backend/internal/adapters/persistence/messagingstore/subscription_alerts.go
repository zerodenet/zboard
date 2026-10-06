package messagingstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/platformstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"github.com/zerodenet/zboard/backend/internal/capabilities/messaging"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type SubscriptionAlerts struct {
	DB     *gorm.DB
	Cipher platform.SettingsCipher
}

// One bounded page per invocation. The durable cursor wraps at the end, so
// inactive or already-notified low IDs cannot starve newer subscriptions.
func (s SubscriptionAlerts) Process(ctx context.Context, now time.Time, limit int) (int, error) {
	if limit < 1 || limit > 200 {
		return 0, messaging.ErrInvalidMessage
	}
	db := s.DB.WithContext(ctx)
	p, email, err := loadAlertPolicy(db)
	if err != nil || !email || !p.Enabled() {
		return 0, err
	}
	if _, err := platformstore.LoadSMTPSettings(db, s.Cipher, true); err != nil {
		return 0, err
	}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.SubscriptionAlertScan{ID: 1}).Error; err != nil {
		return 0, err
	}
	var cursor model.SubscriptionAlertScan
	if err := db.First(&cursor, 1).Error; err != nil {
		return 0, err
	}
	var ids []uint
	if err := db.Model(&model.Subscription{}).Where("id > ? AND status IN ? AND end_at > ?", cursor.LastSubscriptionID, []string{"active", "expired"}, now.Add(-entitlements.RenewalGracePeriod)).Order("id ASC").Limit(limit).Pluck("id", &ids).Error; err != nil {
		return 0, err
	}
	created := 0
	for _, id := range ids {
		ok, err := s.enqueue(ctx, id, now)
		if err != nil {
			return created, err
		}
		if ok {
			created++
		}
	}
	var next uint
	if len(ids) == limit {
		next = ids[len(ids)-1]
	}
	err = db.Model(&model.SubscriptionAlertScan{}).Where("id = 1 AND last_subscription_id = ?", cursor.LastSubscriptionID).Update("last_subscription_id", next).Error
	return created, err
}

func (s SubscriptionAlerts) enqueue(ctx context.Context, id uint, now time.Time) (bool, error) {
	created := false
	// Resolve the owner outside the transaction. The first transactional read
	// must acquire its lock before establishing a repeatable-read snapshot.
	var initial model.Subscription
	if err := s.DB.WithContext(ctx).First(&initial, id).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Serialize deduplication and frequency across the account, not just a
		// subscription. Another process uses exactly the same lock and ledger.
		var user model.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND status = ?", initial.UserID, "active").First(&user).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		} else if err != nil {
			return err
		}
		if !identity.ValidEmail(user.Email) {
			return nil
		}
		var sub model.Subscription
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND user_id = ?", id, user.ID).First(&sub).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		} else if err != nil {
			return err
		}
		p, email, err := loadAlertPolicy(tx)
		if err != nil || !email {
			return err
		}
		candidates := messaging.AlertCandidates(entitlements.Subscription(sub), p, now)
		if len(candidates) == 0 {
			return nil
		}
		var recent int64
		if err := tx.Model(&model.SubscriptionAlert{}).Where("user_id = ? AND created_at > ?", user.ID, now.Add(-time.Duration(p.IntervalHours)*time.Hour)).Count(&recent).Error; err != nil {
			return err
		}
		if recent > 0 {
			return nil
		}
		for _, candidate := range candidates {
			sum := sha256.Sum256([]byte(fmt.Sprintf("%d/%s/%s", id, candidate.Kind, candidate.Episode)))
			key := hex.EncodeToString(sum[:])
			var existing int64
			if err := tx.Model(&model.SubscriptionAlert{}).Where("alert_key = ?", key).Count(&existing).Error; err != nil {
				return err
			}
			if existing > 0 {
				continue
			}
			content := messaging.AlertContent(entitlements.Subscription(sub), candidate)
			var site model.Installation
			if err := tx.First(&site, 1).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			content.SiteName, content.SiteURL = site.SiteName, site.SiteURL
			payload, err := json.Marshal(content)
			if err != nil {
				return err
			}
			task := model.Task{Type: "email", Scope: fmt.Sprintf(`{"user_ids":[%d]}`, user.ID), Content: string(payload), Total: 1, IdempotencyKey: "subscription-alert:" + key, MaxAttempts: 3, ScheduledAt: &now}
			if err := tx.Create(&task).Error; err != nil {
				return err
			}
			if err := tx.Create(&model.TaskItem{TaskID: task.ID, TargetType: "user", TargetID: strconv.FormatUint(uint64(user.ID), 10), Payload: "{}"}).Error; err != nil {
				return err
			}
			if err := tx.Create(&model.SubscriptionAlert{AlertKey: key, UserID: user.ID, SubscriptionID: id, Kind: candidate.Kind, Episode: candidate.Episode, TaskID: task.ID, CreatedAt: now}).Error; err != nil {
				return err
			}
			if err := tx.Create(&model.AuditLog{Actor: "system", Action: "notification.enqueue", Target: fmt.Sprintf("task:%d", task.ID), Detail: fmt.Sprintf("trigger=subscription.%s subscription_id=%d", candidate.Kind, id)}).Error; err != nil {
				return err
			}
			created = true
			return nil
		}
		return nil
	})
	return created && err == nil, err
}

func checkSubscriptionAlert(tx *gorm.DB, userID, taskID uint, guard *messaging.SubscriptionAlertGuard) error {
	// Email preparation already holds the active recipient's row lock.
	p, email, err := loadAlertPolicy(tx)
	if err != nil {
		return err
	}
	var sub model.Subscription
	err = tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND user_id = ?", guard.SubscriptionID, userID).First(&sub).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if email && err == nil {
		for _, current := range messaging.AlertCandidates(entitlements.Subscription(sub), p, time.Now().UTC()) {
			if current == *guard {
				var alert model.SubscriptionAlert
				if err := tx.Where("task_id = ? AND user_id = ? AND subscription_id = ? AND kind = ? AND episode = ?", taskID, userID, guard.SubscriptionID, guard.Kind, guard.Episode).First(&alert).Error; err != nil {
					return err
				}
				now := time.Now().UTC()
				var recent int64
				if err := tx.Model(&model.SubscriptionAlert{}).Where("user_id = ? AND task_id <> ? AND attempted_at > ?", userID, taskID, now.Add(-time.Duration(p.IntervalHours)*time.Hour)).Count(&recent).Error; err != nil {
					return err
				}
				if recent > 0 {
					return fmt.Errorf("%w: 用户近期已安排其他订阅提醒，本次提醒不再发送", messaging.ErrAlertSuppressed)
				}
				return tx.Model(&alert).Update("attempted_at", now).Error
			}
		}
	}
	return fmt.Errorf("%w: 订阅状态已改变或告警已关闭，本次提醒不再发送", messaging.ErrAlertSuppressed)
}
