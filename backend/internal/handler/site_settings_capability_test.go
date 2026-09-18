package handler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestPlatformSiteSettingsAuthorityAndAtomicProjection(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	ctx := context.Background()
	actor := model.User{Email: "site-settings@example.test", Password: "unused", IsAdmin: true, Status: "active"}
	if err := h.db.Create(&actor).Error; err != nil {
		t.Fatal(err)
	}
	installation := model.Installation{ID: 1, SiteName: "old", SiteURL: "https://old.example", InstalledAt: time.Now()}
	if err := h.db.Save(&installation).Error; err != nil {
		t.Fatal(err)
	}
	for _, config := range []model.SystemConfig{
		{ConfigKey: "site_name", Value: "old", ValueType: "string", Revision: 1},
		{ConfigKey: "site_url", Value: "https://old.example", ValueType: "string", Revision: 1},
		{ConfigKey: "register_switch", Value: "false", ValueType: "bool", Revision: 1},
	} {
		if err := h.db.Create(&config).Error; err != nil {
			t.Fatal(err)
		}
	}
	in := platform.SiteSettingsInput{SiteName: " new ", SiteURL: "https://new.example/", AllowRegistration: true}
	out, err := h.services.SiteSettings.Update(ctx, actor.ID, in)
	if err != nil || out.SiteName != "new" || out.SiteURL != "https://new.example" {
		t.Fatal(out, err)
	}
	var config model.SystemConfig
	if err := h.db.Where("config_key = ?", "site_name").First(&config).Error; err != nil {
		t.Fatal(err)
	}
	if config.Value != "new" {
		t.Fatal("public projection not updated", config.Value)
	}
	var auditCount int64
	if err := h.db.Model(&model.AuditLog{}).Where("action = ? AND user_id = ?", "system.settings.update", actor.ID).Count(&auditCount).Error; err != nil || auditCount != 1 {
		t.Fatal(auditCount, err)
	}
	// Audit failure must roll back both authoritative and public settings.
	const callback = "site-setting-audit-failure"
	if err := h.db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "audit_logs" {
			tx.AddError(errors.New("fixture audit failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	in.SiteName = "must-rollback"
	if _, err := h.services.SiteSettings.Update(ctx, actor.ID, in); err == nil {
		t.Fatal("audit failure ignored")
	}
	h.db.Callback().Create().Remove(callback)
	if err := h.db.First(&installation, 1).Error; err != nil {
		t.Fatal(err)
	}
	if installation.SiteName != "new" {
		t.Fatal("installation escaped rollback")
	}
	if err := h.db.First(&config, config.ID).Error; err != nil {
		t.Fatal(err)
	}
	if config.Value != "new" {
		t.Fatal("public projection escaped rollback")
	}
	if err := h.db.Model(&actor).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := h.services.SiteSettings.Update(ctx, actor.ID, in); !errors.Is(err, platform.ErrSettingsPermission) {
		t.Fatal("revoked administrator accepted", err)
	}
	if err := h.db.Model(&actor).Update("is_admin", true).Error; err != nil {
		t.Fatal(err)
	}
	task := model.Task{Type: "database_migration", Status: 0}
	if err := h.db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := h.services.SiteSettings.Update(ctx, actor.ID, in); !errors.Is(err, platform.ErrMaintenanceBusy) {
		t.Fatal("migration lock bypassed", err)
	}
}
