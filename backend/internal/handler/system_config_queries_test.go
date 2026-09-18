package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/platformstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestPlatformSettingsVisibilityAndCurrentAuthority(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql"} {
		t.Run(engine, func(t *testing.T) {
			var h *handlers
			if engine == "mysql" {
				h, _ = newMySQLPublishHandlers(t)
			} else {
				h, _ = newAnnouncementTestHandlers(t)
			}
			actor := model.User{Email: "settings-query@example.test", Password: "unused", Status: "active", IsAdmin: true}
			if err := h.db.Create(&actor).Error; err != nil {
				t.Fatal(err)
			}
			for _, row := range []model.SystemConfig{
				{ConfigKey: "fixture_public_bool", Name: "flag", Value: "true", ValueType: "bool", IsPublic: true, Revision: 1},
				{ConfigKey: "fixture_public_json", Name: "document", Value: `{"count":1}`, ValueType: "json", IsPublic: true, Revision: 1},
				{ConfigKey: "fixture_private_int", Name: "internal", Value: "7", ValueType: "int", Revision: 1},
				{ConfigKey: "fixture_public_secret", Name: "secret", Value: "ciphertext-not-json", ValueType: "json", IsPublic: true, IsSecret: true, Revision: 1},
				{ConfigKey: "fixture_secret_empty", Name: "empty secret", ValueType: "string", IsSecret: true, Revision: 1},
			} {
				if err := h.db.Create(&row).Error; err != nil {
					t.Fatal(err)
				}
			}
			ctx := context.Background()
			public, err := h.services.Settings.Public(ctx)
			if err != nil {
				t.Fatal(err)
			}
			publicKeys := map[string]platform.SettingView{}
			for _, view := range public {
				publicKeys[view.ConfigKey] = view
				if !view.IsPublic || view.IsSecret {
					t.Fatal("private metadata in public projection")
				}
			}
			if publicKeys["fixture_public_bool"].Value != true {
				t.Fatal("public bool lost type")
			}
			if _, ok := publicKeys["fixture_private_int"]; ok {
				t.Fatal("private setting exposed")
			}
			if _, ok := publicKeys["fixture_public_secret"]; ok {
				t.Fatal("public flag bypassed secrecy")
			}
			admin, err := h.services.Settings.Administrative(ctx, actor.ID)
			if err != nil {
				t.Fatal(err)
			}
			byKey := map[string]platform.SettingView{}
			for i, view := range admin {
				byKey[view.ConfigKey] = view
				if i > 0 && admin[i-1].ID >= view.ID {
					t.Fatal("list order changed")
				}
			}
			if byKey["fixture_private_int"].Value != int64(7) {
				t.Fatal("integer type lost")
			}
			secret := byKey["fixture_public_secret"]
			if secret.Value != nil || !secret.Configured || !secret.IsSecret {
				t.Fatal("secret projection invalid")
			}
			if byKey["fixture_secret_empty"].Configured {
				t.Fatal("empty secret reported configured")
			}
			raw, err := (platformstore.Settings{DB: h.db}).Administrative(ctx, actor.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range raw {
				if row.IsSecret && row.Value != "" {
					t.Fatal("ciphertext crossed repository boundary")
				}
			}
			w := httptest.NewRecorder()
			h.PublicSystemConfigsHandler(w, httptest.NewRequest(http.MethodGet, "/api/v1/system/configs", nil))
			if w.Code != 200 || strings.Contains(w.Body.String(), "fixture_private_int") || strings.Contains(w.Body.String(), "fixture_public_secret") {
				t.Fatal("public HTTP visibility", w.Code)
			}
			token, _, err := h.issueToken(authClaims{UserID: actor.ID, Email: actor.Email, IsAdmin: true})
			if err != nil {
				t.Fatal(err)
			}
			w = httptest.NewRecorder()
			h.AdminSystemConfigsListHandler(w, announcementRequest(http.MethodGet, "/api/v1/admin/system-configs", token, ""))
			if w.Code != 200 || strings.Contains(w.Body.String(), "ciphertext-not-json") {
				t.Fatal("admin HTTP leaked secret or failed", w.Code)
			}
			var stored model.SystemConfig
			if err := h.db.Where("config_key = ?", "fixture_public_secret").First(&stored).Error; err != nil || stored.Value != "ciphertext-not-json" {
				t.Fatal("read changed stored ciphertext", err)
			}
			if _, err := h.services.Settings.Administrative(ctx, 0); !errors.Is(err, platform.ErrSettingsPermission) {
				t.Fatal("anonymous capability read", err)
			}
			if err := h.db.Model(&actor).Update("is_admin", false).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := h.services.Settings.Administrative(ctx, actor.ID); !errors.Is(err, platform.ErrSettingsPermission) {
				t.Fatal("revoked administrator read", err)
			}
			canceled, cancel := context.WithCancel(ctx)
			cancel()
			if _, err := h.services.Settings.Public(canceled); err == nil {
				t.Fatal("canceled public read returned data")
			}
		})
	}
}
