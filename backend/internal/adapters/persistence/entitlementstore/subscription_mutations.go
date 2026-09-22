package entitlementstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type SubscriptionMutations struct {
	DB      *gorm.DB
	Publish func(*gorm.DB, uint, uint) error
}

func subscriptionMutationHash(in entitlements.SubscriptionMutationInput) string {
	data, _ := json.Marshal(struct {
		SubscriptionID uint   `json:"subscription_id"`
		Kind           string `json:"kind"`
		Days           int    `json:"days"`
		Reason         string `json:"reason"`
		Origin         string `json:"origin"`
	}{in.SubscriptionID, in.Kind, in.Days, in.Reason, in.Origin})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func subscriptionMutationReceipt(row model.SubscriptionMutation) entitlements.SubscriptionMutationReceipt {
	return entitlements.SubscriptionMutationReceipt{MutationID: row.ID, SubscriptionID: row.SubscriptionID, Status: row.Status, EndAt: row.EndAt}
}

func (s SubscriptionMutations) Apply(ctx context.Context, actor uint, in entitlements.SubscriptionMutationInput) (out entitlements.SubscriptionMutationReceipt, err error) {
	if s.DB == nil || s.Publish == nil {
		return out, entitlements.ErrSubscriptionMutationUnavailable
	}
	hash := subscriptionMutationHash(in)
	lookup := func(tx *gorm.DB) (bool, error) {
		var old model.SubscriptionMutation
		err := tx.Where("idempotency_key = ?", in.IdempotencyKey).First(&old).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if old.ActorID != actor || old.RequestHash != hash {
			return false, entitlements.ErrSubscriptionMutationConflict
		}
		out = subscriptionMutationReceipt(old)
		return true, nil
	}
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := subscriptionReader(tx, actor, true); err != nil {
			return err
		}
		if found, err := lookup(tx); err != nil || found {
			return err
		}
		var sub model.Subscription
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&sub, in.SubscriptionID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return entitlements.ErrAccessNotFound
			}
			return err
		}
		now := time.Now().UTC()
		if sub.Status != "active" || !sub.EndAt.After(now) {
			return entitlements.ErrSubscriptionMutationInvalid
		}
		switch in.Kind {
		case entitlements.SubscriptionExtend:
			if entitlements.IsPerpetualEnd(sub.EndAt) {
				return entitlements.ErrSubscriptionMutationInvalid
			}
			next := sub.EndAt.AddDate(0, 0, in.Days)
			if !next.After(sub.EndAt) || entitlements.IsPerpetualEnd(next) {
				return entitlements.ErrSubscriptionMutationInvalid
			}
			sub.EndAt = next
			if err := tx.Model(&sub).Updates(map[string]any{"end_at": next, "updated_at": now}).Error; err != nil {
				return err
			}
			if err := tx.Model(&model.ProtocolCredential{}).Where("subscription_id = ? AND status IN ? AND revoked_at IS NULL", sub.ID, []string{"active", "prepared"}).Updates(map[string]any{"expires_at": next, "updated_at": now}).Error; err != nil {
				return err
			}
		case entitlements.SubscriptionCancel:
			sub.Status = "canceled"
			if err := tx.Model(&sub).Updates(map[string]any{"status": sub.Status, "updated_at": now}).Error; err != nil {
				return err
			}
			query := tx.Model(&model.ProtocolCredential{}).Where("subscription_id = ? AND status IN ?", sub.ID, []string{"active", "prepared"})
			if err := persistCredentialRevocation(tx, query, "revoked", now); err != nil {
				return err
			}
		default:
			return entitlements.ErrSubscriptionMutationInvalid
		}
		if err := s.Publish(tx, sub.ID, actor); err != nil {
			return err
		}
		row := model.SubscriptionMutation{SubscriptionID: sub.ID, ActorID: actor, Kind: in.Kind, Days: in.Days, Reason: in.Reason, Origin: in.Origin, IdempotencyKey: in.IdempotencyKey, RequestHash: hash, Status: sub.Status, EndAt: sub.EndAt, CreatedAt: now}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		var admin model.User
		if err := tx.Select("id", "email").First(&admin, actor).Error; err != nil {
			return err
		}
		detail, err := json.Marshal(map[string]any{"mutation_id": row.ID, "reason": in.Reason, "origin": in.Origin})
		if err != nil {
			return err
		}
		if err := tx.Create(&model.AuditLog{UserID: &admin.ID, Actor: admin.Email, Action: "subscription." + in.Kind, Target: fmt.Sprintf("subscription:%d", sub.ID), Detail: string(detail)}).Error; err != nil {
			return err
		}
		out = subscriptionMutationReceipt(row)
		return nil
	})
	if err == nil {
		return out, nil
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) || strings.Contains(strings.ToLower(err.Error()), "unique constraint") || strings.Contains(strings.ToLower(err.Error()), "duplicate entry") {
		if lookupErr := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := subscriptionReader(tx, actor, true); err != nil {
				return err
			}
			found, err := lookup(tx)
			if err != nil {
				return err
			}
			if !found {
				return entitlements.ErrSubscriptionMutationConflict
			}
			return nil
		}); lookupErr != nil {
			return entitlements.SubscriptionMutationReceipt{}, lookupErr
		}
		return out, nil
	}
	return entitlements.SubscriptionMutationReceipt{}, err
}
