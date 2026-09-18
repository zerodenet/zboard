package handler

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestPlatformSettingUpdateFencesAuthorityAndSecrets(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	ctx := context.Background()
	actor := model.User{Email: "setting-writer@example.test", Password: "unused", IsAdmin: true, Status: "active"}
	if err := h.db.Create(&actor).Error; err != nil {
		t.Fatal(err)
	}
	row := model.SystemConfig{ConfigKey: "fixture_secret", ValueType: "string", IsSecret: true, Revision: 1}
	if err := h.db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	service := h.services.SettingUpdate(h.credentialCipher)
	revision := uint64(1)
	in := platform.SettingUpdateInput{Key: row.ConfigKey, Value: json.RawMessage(`"secret-plaintext"`), ExpectedRevision: &revision}
	out, err := service.Update(ctx, actor.ID, in)
	if err != nil || out.Value != nil || !out.Configured || out.Revision != 2 {
		t.Fatal(out, err)
	}
	if err := h.db.First(&row, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if row.Value == "secret-plaintext" || row.Value == "" {
		t.Fatal("secret stored unprotected")
	}
	plain, err := h.credentialCipher.Decrypt(row.Value)
	if err != nil || plain != "secret-plaintext" {
		t.Fatal("secret cannot be recovered", err)
	}
	if _, err := service.Update(ctx, actor.ID, in); !errors.Is(err, platform.ErrSettingRevision) {
		t.Fatal("stale write accepted", err)
	}
	if err := h.db.Model(&actor).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	in.ExpectedRevision = nil
	if _, err := service.Update(ctx, actor.ID, in); !errors.Is(err, platform.ErrSettingsPermission) {
		t.Fatal("revoked administrator accepted", err)
	}
	if err := h.db.Model(&actor).Update("is_admin", true).Error; err != nil {
		t.Fatal(err)
	}
	in.Key = "maintenance_task_id"
	if _, err := service.Update(ctx, actor.ID, in); !errors.Is(err, platform.ErrSettingsPermission) {
		t.Fatal("migration ownership bypassed", err)
	}
}

func TestPlatformSettingUpdateRollsBackIncompleteMailConfiguration(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	actor := model.User{Email: "mail-setting-writer@example.test", Password: "unused", IsAdmin: true, Status: "active"}
	if err := h.db.Create(&actor).Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range []model.SystemConfig{
		{ConfigKey: "register_email_verification", Value: "false", ValueType: "bool", Revision: 1},
		{ConfigKey: "task_email_enabled", Value: "false", ValueType: "bool", Revision: 1},
		{ConfigKey: "smtp_port", Value: "587", ValueType: "int", Revision: 1},
	} {
		if err := h.db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	_, err := h.services.SettingUpdate(h.credentialCipher).Update(context.Background(), actor.ID, platform.SettingUpdateInput{Key: "register_email_verification", Value: json.RawMessage(`true`)})
	var invalid *platform.SettingValidation
	if !errors.As(err, &invalid) {
		t.Fatal("incomplete SMTP accepted", err)
	}
	var row model.SystemConfig
	if err := h.db.Where("config_key = ?", "register_email_verification").First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.Value != "false" || row.Revision != 1 {
		t.Fatal("failed validation committed", row)
	}
	var count int64
	if err := h.db.Model(&model.AuditLog{}).Where("action = ?", "system.config.update").Count(&count).Error; err != nil || count != 0 {
		t.Fatal(count, err)
	}
	for _, config := range []model.SystemConfig{
		{ConfigKey: "smtp_host", Value: "smtp.example.test", ValueType: "string", Revision: 1},
		{ConfigKey: "smtp_from", Value: "sender@example.test", ValueType: "string", Revision: 1},
		{ConfigKey: "smtp_tls_mode", Value: "starttls", ValueType: "string", Revision: 1},
	} {
		if err := h.db.Create(&config).Error; err != nil {
			t.Fatal(err)
		}
	}
	service := h.services.SettingUpdate(h.credentialCipher)
	if _, err := service.Update(context.Background(), actor.ID, platform.SettingUpdateInput{Key: "register_email_verification", Value: json.RawMessage(`true`)}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Update(context.Background(), actor.ID, platform.SettingUpdateInput{Key: "smtp_host", Value: json.RawMessage(`""`)}); !errors.As(err, &invalid) {
		t.Fatal("enabled verification lost SMTP configuration", err)
	}
	var host model.SystemConfig
	if err := h.db.Where("config_key = ?", "smtp_host").First(&host).Error; err != nil {
		t.Fatal(err)
	}
	if host.Value != "smtp.example.test" || host.Revision != 1 {
		t.Fatal("invalid mail edit committed", host)
	}
}
