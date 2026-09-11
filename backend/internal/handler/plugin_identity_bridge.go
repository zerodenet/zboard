package handler

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/plugins"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type pluginIdentityBinding struct {
	ID         string    `json:"id"`
	ProviderID string    `json:"provider_id"`
	Publisher  string    `json:"publisher"`
	Issuer     string    `json:"issuer"`
	Subject    string    `json:"subject"`
	CreatedAt  time.Time `json:"created_at"`
}

func pluginOwnsProvider(pluginID, providerID string) bool {
	return providerID == pluginID || strings.HasPrefix(providerID, pluginID+"~")
}

func (h *handlers) pluginIdentityProviderViews(pluginID string) ([]plugins.IdentityProviderView, error) {
	if h.identityProviders == nil {
		return nil, plugins.ErrUnavailable
	}
	providers, err := h.identityProviders.IdentityProviders()
	if err != nil {
		return nil, err
	}
	out := make([]plugins.IdentityProviderView, 0, len(providers))
	for _, provider := range providers {
		if pluginOwnsProvider(pluginID, provider.ID) {
			out = append(out, provider)
		}
	}
	return out, nil
}

func (h *handlers) pluginIdentityBindings(userID uint, pluginID string) ([]pluginIdentityBinding, error) {
	rows := []model.ExternalIdentity{}
	if err := h.db.Where("user_id = ?", userID).Order("created_at desc").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := []pluginIdentityBinding{}
	for _, row := range rows {
		if pluginOwnsProvider(pluginID, row.PluginID) {
			out = append(out, pluginIdentityBinding{ID: row.ID, ProviderID: row.PluginID, Publisher: row.Publisher, Issuer: row.Issuer, Subject: row.Subject, CreatedAt: row.CreatedAt})
		}
	}
	return out, nil
}

func (h *handlers) unlinkPluginIdentity(session plugins.Session, claims authClaims, identityID, password string) error {
	if session.Surface != "account" && session.Surface != "admin" {
		return plugins.ErrPermission
	}
	if identityID == "" || len(identityID) > 64 {
		return errors.New("invalid identity binding")
	}
	return h.db.Transaction(func(tx *gorm.DB) error {
		var target model.User
		query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", session.TargetUserID)
		if session.Surface == "account" {
			query = query.Where("status = ?", userStatusActive)
		}
		if err := query.First(&target).Error; err != nil {
			return err
		}
		if session.Surface == "account" && bcrypt.CompareHashAndPassword([]byte(target.Password), []byte(password)) != nil {
			return errors.New("confirm your current account password before unlinking")
		}
		var binding model.ExternalIdentity
		if err := tx.Where("id = ? AND user_id = ?", identityID, target.ID).First(&binding).Error; err != nil {
			return err
		}
		if !pluginOwnsProvider(session.PluginID, binding.PluginID) {
			return plugins.ErrPermission
		}
		if err := tx.Delete(&binding).Error; err != nil {
			return err
		}
		return createAuditLog(tx, claims, "plugin.identity.unlink", fmt.Sprintf("user:%d", target.ID), fmt.Sprintf("plugin=%s provider=%s identity=%s", session.PluginID, binding.PluginID, binding.ID))
	})
}
