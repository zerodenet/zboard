package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/plugins"
	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
	"github.com/zeromicro/go-zero/rest/pathvar"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func externalIdentityID(provider plugins.IdentitySnapshot, identity *pluginv1.VerifiedIdentity) string {
	raw, _ := json.Marshal([]string{provider.Publisher, provider.ID, identity.Issuer, identity.Subject})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func (h *handlers) resolveExternalIdentity(db *gorm.DB, flow externalAuthFlow, identity *pluginv1.VerifiedIdentity, result *externalAuthCompletion) error {
	id := externalIdentityID(flow.Provider, identity)
	return db.Transaction(func(tx *gorm.DB) error {
		var row model.ExternalIdentity
		err := tx.Where("id = ?", id).First(&row).Error
		if flow.BindUserID == 0 {
			if err != nil {
				return errors.New("identity is not linked")
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
			row = model.ExternalIdentity{ID: id, UserID: user.ID, PluginID: flow.Provider.ID, Publisher: flow.Provider.Publisher, Issuer: identity.Issuer, Subject: identity.Subject}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
			if err := createAuditLog(tx, authClaims{UserID: user.ID, Email: user.Email, IsAdmin: user.IsAdmin}, "identity.link", flow.Provider.ID, "external identity linked after password confirmation"); err != nil {
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
	var output any
	err := db.Transaction(func(tx *gorm.DB) error {
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
		if err := createAuditLog(tx, authClaims{UserID: user.ID, Email: user.Email, IsAdmin: user.IsAdmin}, "identity.login", result.Provider.ID, "external identity login"); err != nil {
			return err
		}
		output = map[string]any{"user": toPublicUser(user), "auth": tokenResponse{Token: token, ExpiresAt: expires}}
		return nil
	})
	return output, err
}
func (h *handlers) ExternalIdentitiesHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.authFromRequest(r)
	if err != nil {
		Unauthorized(w, "authentication required")
		return
	}
	authNoStore(w)
	rows := []model.ExternalIdentity{}
	if err := h.db.Where("user_id = ?", claims.UserID).Order("created_at desc").Find(&rows).Error; err != nil {
		ServerError(w, err)
		return
	}
	OK(w, rows)
}
func (h *handlers) ExternalIdentityUnlinkHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.authFromRequest(r)
	if err != nil {
		Unauthorized(w, "authentication required")
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if !pluginBody(w, r, &body) {
		return
	}
	var user model.User
	if h.db.Where("id = ? AND status = ?", claims.UserID, userStatusActive).First(&user).Error != nil || bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(body.Password)) != nil {
		Unauthorized(w, "confirm your current account password before unlinking")
		return
	}
	err = h.db.Transaction(func(tx *gorm.DB) error {
		var current model.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND status = ? AND password = ?", user.ID, userStatusActive, user.Password).First(&current).Error; err != nil {
			return err
		}
		row := tx.Where("id = ? AND user_id = ?", pathvar.Vars(r)["id"], user.ID).Delete(&model.ExternalIdentity{})
		if row.Error != nil {
			return row.Error
		}
		if row.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		return createAuditLog(tx, claims, "identity.unlink", pathvar.Vars(r)["id"], "external identity removed after password confirmation")
	})
	if err != nil {
		BadRequest(w, "identity could not be removed")
		return
	}
	authNoStore(w)
	OK(w, map[string]bool{"unlinked": true})
}
