package entitlementstore

import (
	"context"
	"errors"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

type Access struct {
	DB     *gorm.DB
	Cipher entitlements.AccessCipher
}

func (s Access) Execute(ctx context.Context, actor, id uint, operation string) (entitlements.AccessView, error) {
	var out entitlements.AccessView
	var domainErr error
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if actor == 0 {
			return entitlements.ErrAccessPermission
		}
		if id == 0 {
			return entitlements.ErrAccessNotFound
		}
		if operation != "read" && operation != "rotate" && operation != "revoke" {
			return errors.New("invalid access operation")
		}
		var user model.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "email", "status").First(&user, actor).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return entitlements.ErrAccessPermission
			}
			return err
		}
		if user.Status != "active" {
			return entitlements.ErrAccessPermission
		}
		now := time.Now().UTC()
		if operation != "revoke" {
			if err := ExpireInTransaction(tx, actor, now); err != nil {
				return err
			}
		}
		var sub model.Subscription
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ?", id, actor).First(&sub).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return entitlements.ErrAccessNotFound
			}
			return err
		}
		out = entitlements.AccessView{SubscriptionID: id}
		if operation != "revoke" && !entitlements.AccessAvailable(entitlements.Subscription(sub), now) {
			if operation == "rotate" {
				domainErr = entitlements.ErrAccessInactive
			}
			return nil
		}
		if operation == "revoke" {
			result := tx.Model(&model.SubscriptionToken{}).Where("subscription_id = ? AND user_id = ? AND revoked_at IS NULL", id, actor).Update("revoked_at", now)
			if result.Error != nil {
				return result.Error
			}
			out.Revoked = result.RowsAffected > 0
			if !out.Revoked {
				return nil
			}
			out.RevokedAt = &now
			return tx.Create(&model.AuditLog{UserID: &user.ID, Actor: user.Email, Action: "subscription_token.revoke", Target: fmt.Sprintf("subscription:%d", id), Detail: "credential revoked"}).Error
		}
		if operation == "read" {
			access, raw, err := EnsureAccessToken(tx, sub, s.Cipher)
			if err != nil {
				return err
			}
			if access.RevokedAt != nil || access.TokenCiphertext == "" {
				out = AccessView(access, "", false)
				return nil
			}
			if raw == "" {
				raw, err = entitlements.RecoverAccessToken(s.Cipher, access.TokenCiphertext, access.TokenHash)
				if err != nil {
					return err
				}
			}
			out = AccessView(access, raw, true)
			return nil
		}
		raw, hash, prefix, err := entitlements.NewAccessToken()
		if err != nil {
			return err
		}
		protected, err := s.Cipher.Encrypt(raw)
		if err != nil {
			return err
		}
		var access model.SubscriptionToken
		findErr := tx.Where("subscription_id = ? AND user_id = ?", id, actor).First(&access).Error
		if findErr != nil && !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return findErr
		}
		if errors.Is(findErr, gorm.ErrRecordNotFound) {
			access = model.SubscriptionToken{UserID: actor, SubscriptionID: &id, TokenHash: hash, TokenCiphertext: protected, TokenPrefix: prefix}
			if err := tx.Create(&access).Error; err != nil {
				return err
			}
		} else {
			if err := tx.Model(&access).Updates(map[string]interface{}{"token_hash": hash, "token_ciphertext": protected, "token_prefix": prefix, "last_used_at": nil, "revoked_at": nil}).Error; err != nil {
				return err
			}
			access.TokenHash = hash
			access.TokenCiphertext = protected
			access.TokenPrefix = prefix
			access.LastUsedAt = nil
			access.RevokedAt = nil
		}
		if err := tx.Create(&model.AuditLog{UserID: &user.ID, Actor: user.Email, Action: "subscription_token.rotate", Target: fmt.Sprintf("subscription:%d", id), Detail: prefix}).Error; err != nil {
			return err
		}
		out = AccessView(access, raw, true)
		out.Notice = "previous URL for this subscription is invalid"
		return nil
	})
	if err != nil {
		return entitlements.AccessView{}, err
	}
	if domainErr != nil {
		return entitlements.AccessView{}, domainErr
	}
	return out, nil
}
func AccessView(access model.SubscriptionToken, raw string, configured bool) entitlements.AccessView {
	out := entitlements.AccessView{Configured: configured, TokenPrefix: access.TokenPrefix, LastUsedAt: access.LastUsedAt, RevokedAt: access.RevokedAt}
	if access.SubscriptionID != nil {
		out.SubscriptionID = *access.SubscriptionID
	}
	if !access.CreatedAt.IsZero() {
		value := access.CreatedAt
		out.CreatedAt = &value
	}
	if !access.UpdatedAt.IsZero() {
		value := access.UpdatedAt
		out.UpdatedAt = &value
	}
	if configured {
		out.Token = raw
	}
	return out
}
