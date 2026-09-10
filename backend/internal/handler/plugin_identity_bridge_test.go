package handler

import (
	"errors"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/plugins"
	"golang.org/x/crypto/bcrypt"
)

func TestPluginIdentityBindingsAreNamespaceScoped(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	for _, binding := range []model.ExternalIdentity{
		{ID: "own", UserID: 1, PluginID: "test.oauth~github", Publisher: "official", Issuer: "https://github.com", Subject: "1"},
		{ID: "other", UserID: 1, PluginID: "other.oauth~github", Publisher: "other", Issuer: "https://github.com", Subject: "1"},
	} {
		if err := h.db.Create(&binding).Error; err != nil {
			t.Fatal(err)
		}
	}
	bindings, err := h.pluginIdentityBindings(1, "test.oauth")
	if err != nil || len(bindings) != 1 || bindings[0].ID != "own" || bindings[0].Subject != "1" || bindings[0].Publisher != "official" {
		t.Fatal(bindings, err)
	}
}

func TestPluginIdentityUnlinkRequiresScopedSessionAndPassword(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	hash, _ := bcrypt.GenerateFromPassword([]byte("account-password"), bcrypt.MinCost)
	if err := h.db.Model(&model.User{}).Where("id = ?", 1).Updates(map[string]any{"password": string(hash), "status": userStatusActive}).Error; err != nil {
		t.Fatal(err)
	}
	for _, binding := range []model.ExternalIdentity{
		{ID: "own", UserID: 1, PluginID: "test.oauth~github", Publisher: "official", Issuer: "https://github.com", Subject: "1"},
		{ID: "other", UserID: 1, PluginID: "other.oauth~github", Publisher: "other", Issuer: "https://github.com", Subject: "1"},
	} {
		if err := h.db.Create(&binding).Error; err != nil {
			t.Fatal(err)
		}
	}
	session := plugins.Session{PluginID: "test.oauth", Surface: "account", Purpose: "slot", UserID: 1, TargetUserID: 1}
	claims := authClaims{UserID: 1, Email: "admin@example.com", IsAdmin: true}
	if err := h.unlinkPluginIdentity(session, claims, "own", "wrong"); err == nil {
		t.Fatal("wrong password unlinked identity")
	}
	if err := h.unlinkPluginIdentity(session, claims, "other", "account-password"); !errors.Is(err, plugins.ErrPermission) {
		t.Fatal("plugin crossed identity namespace", err)
	}
	if err := h.unlinkPluginIdentity(session, claims, "own", "account-password"); err != nil {
		t.Fatal(err)
	}
	var own, other int64
	h.db.Model(&model.ExternalIdentity{}).Where("id = ?", "own").Count(&own)
	h.db.Model(&model.ExternalIdentity{}).Where("id = ?", "other").Count(&other)
	if own != 0 || other != 1 {
		t.Fatal("unexpected remaining bindings", own, other)
	}
	var audit int64
	h.db.Model(&model.AuditLog{}).Where("action = ?", "plugin.identity.unlink").Count(&audit)
	if audit != 1 {
		t.Fatal("unlink audit missing")
	}
}
