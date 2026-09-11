package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zeromicro/go-zero/rest/pathvar"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type adminExternalIdentity struct {
	ID         string    `json:"id"`
	PluginID   string    `json:"plugin_id"`
	ProviderID string    `json:"provider_id"`
	Publisher  string    `json:"publisher"`
	Issuer     string    `json:"issuer"`
	Subject    string    `json:"subject"`
	CreatedAt  time.Time `json:"created_at"`
}

func newAdminExternalIdentity(identity model.ExternalIdentity) adminExternalIdentity {
	pluginID, providerID, _ := strings.Cut(identity.PluginID, "~")
	return adminExternalIdentity{
		ID: identity.ID, PluginID: pluginID, ProviderID: providerID,
		Publisher: identity.Publisher, Issuer: identity.Issuer,
		Subject: identity.Subject, CreatedAt: identity.CreatedAt,
	}
}

// AdminExternalIdentityDeleteHandler removes one host-owned identity binding.
// It deliberately does not call the plugin, so an administrator can recover
// an account after a provider or plugin has been disabled or removed.
func (h *handlers) AdminExternalIdentityDeleteHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	vars := pathvar.Vars(r)
	userIDValue, err := strconv.ParseUint(vars["user_id"], 10, strconv.IntSize)
	identityID := strings.TrimSpace(vars["identity_id"])
	if err != nil || userIDValue == 0 || identityID == "" || len(identityID) > 64 {
		BadRequest(w, "invalid user or identity binding")
		return
	}
	userID := uint(userIDValue)
	var removed model.ExternalIdentity
	err = h.db.Transaction(func(tx *gorm.DB) error {
		var user model.User
		if err := tx.Select("id").First(&user, userID).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ?", identityID, userID).First(&removed).Error; err != nil {
			return err
		}
		if err := tx.Delete(&removed).Error; err != nil {
			return err
		}
		return createAuditLog(tx, claims, "identity.unlink.admin", "identity:"+removed.ID, fmt.Sprintf("user=%d plugin=%s publisher=%s", userID, removed.PluginID, removed.Publisher))
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			NotFound(w)
			return
		}
		ServerError(w, err)
		return
	}
	authNoStore(w)
	OK(w, map[string]bool{"unlinked": true})
}
