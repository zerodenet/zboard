package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/plugins"
	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func externalIdentityID(provider plugins.IdentitySnapshot, identity *pluginv1.VerifiedIdentity) string {
	raw, _ := json.Marshal([]string{provider.Publisher, provider.IdentityKey(), identity.Issuer, identity.Subject})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func (h *handlers) resolveExternalIdentity(db *gorm.DB, flow externalAuthFlow, identity *pluginv1.VerifiedIdentity, result *externalAuthCompletion) error {
	id := externalIdentityID(flow.Provider, identity)
	return db.Transaction(func(tx *gorm.DB) error {
		var row model.ExternalIdentity
		err := tx.Where("id = ?", id).First(&row).Error
		if flow.BindUserID == 0 {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				var installation model.Installation
				if err := tx.First(&installation, 1).Error; err != nil {
					return err
				}
				if !installation.AllowRegistration {
					return errors.New("public registration is disabled")
				}
				result.Identity = identity
				return nil
			}
			if err != nil {
				return err
			}
			var user model.User
			if err := tx.Where("id = ? AND status = ?", row.UserID, userStatusActive).First(&user).Error; err != nil {
				return err
			}
			result.UserID = user.ID
			result.IdentityID = row.ID
			return nil
		}

		var user model.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND status = ? AND password = ?", flow.BindUserID, userStatusActive, flow.PasswordHash).First(&user).Error; err != nil {
			return err
		}
		if err == nil && row.UserID != user.ID {
			return errors.New("identity is linked to another account")
		}
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			row = model.ExternalIdentity{ID: id, UserID: user.ID, PluginID: flow.Provider.IdentityKey(), Publisher: flow.Provider.Publisher, Issuer: identity.Issuer, Subject: identity.Subject}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
			if err := createAuditLog(tx, authClaims{UserID: user.ID, Email: user.Email, IsAdmin: user.IsAdmin}, "plugin.identity.link", flow.Provider.IdentityKey(), "plugin identity linked after core password confirmation"); err != nil {
				return err
			}
		}
		result.UserID = user.ID
		result.IdentityID = row.ID
		result.Linked = true
		return nil
	})
}
func (h *handlers) finishExternalIdentity(db *gorm.DB, result externalAuthCompletion) (any, error) {
	return h.finishExternalIdentityRegistration(db, result, externalRegistrationInput{})
}
func (h *handlers) finishExternalIdentityRegistration(db *gorm.DB, result externalAuthCompletion, input externalRegistrationInput) (any, error) {
	var output any
	err := db.Transaction(func(tx *gorm.DB) error {
		if result.Identity != nil {
			user, binding, err := h.createExternalRegistration(tx, result, input)
			if err != nil {
				return err
			}
			result.UserID = user.ID
			result.IdentityID = binding.ID
		}
		var user model.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND status = ?", result.UserID, userStatusActive).First(&user).Error; err != nil {
			return err
		}
		var identity model.ExternalIdentity
		if err := tx.Where("id = ? AND user_id = ?", result.IdentityID, user.ID).First(&identity).Error; err != nil {
			return err
		}
		if result.Linked {
			output = map[string]bool{"linked": true}
			return nil
		}
		now := time.Now().UTC()
		if err := tx.Model(&user).Update("last_login_at", now).Error; err != nil {
			return err
		}
		token, expires, err := h.issueToken(authClaims{UserID: user.ID, Email: user.Email, IsAdmin: user.IsAdmin})
		if err != nil {
			return err
		}
		if err := createAuditLog(tx, authClaims{UserID: user.ID, Email: user.Email, IsAdmin: user.IsAdmin}, "plugin.identity.login", result.Provider.IdentityKey(), "plugin identity verified; core session issued"); err != nil {
			return err
		}
		output = map[string]any{"user": toPublicUser(user), "auth": tokenResponse{Token: token, ExpiresAt: expires}}
		return nil
	})
	return output, err
}
