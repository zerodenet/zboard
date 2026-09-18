package platformstore

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type SettingUpdate struct {
	DB     *gorm.DB
	Cipher platform.SettingsCipher
}

func (s SettingUpdate) Update(ctx context.Context, actor uint, in platform.SettingUpdateInput) (platform.Setting, error) {
	var out platform.Setting
	err := jobstore.New(s.DB).WithLedgerLock(ctx, func(tx *gorm.DB) error {
		if err := RequireMigrationAdministrator(tx, actor); err != nil {
			if errors.Is(err, jobs.ErrPermission) {
				return platform.ErrSettingsPermission
			}
			return err
		}
		if strings.HasPrefix(in.Key, "maintenance_") {
			return platform.ErrSettingsPermission
		}
		current, err := maintenanceData(tx, true)
		if err != nil {
			return err
		}
		state := current.State()
		if state.MigrationInProgress || state.MigrationCutoverPending {
			return platform.ErrMaintenanceBusy
		}
		var row model.SystemConfig
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("config_key = ?", in.Key).First(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return platform.ErrSettingNotFound
			}
			return err
		}
		if in.ExpectedRevision != nil && row.Revision != *in.ExpectedRevision {
			return platform.ErrSettingRevision
		}
		value, err := platform.NormalizeSettingValue(platform.Setting{ValueType: row.ValueType}, in.Value)
		if err != nil {
			return &platform.SettingValidation{Cause: err}
		}
		if err := platform.ValidateSettingValue(row.ConfigKey, value); err != nil {
			return &platform.SettingValidation{Cause: err}
		}
		stored := value
		if row.IsSecret && value != "" {
			stored, err = s.Cipher.Encrypt(value)
			if err != nil {
				return err
			}
		}
		row.Value, row.Revision = stored, row.Revision+1
		if err := tx.Save(&row).Error; err != nil {
			return err
		}
		if err := syncInstallationConfig(tx, row.ConfigKey, value); err != nil {
			return err
		}
		if row.ConfigKey == "task_email_enabled" || row.ConfigKey == "register_email_verification" || strings.HasPrefix(row.ConfigKey, "smtp_") {
			settings, err := LoadSMTPSettings(tx, s.Cipher, false)
			if err != nil {
				return err
			}
			var verificationEnabled int64
			if err := tx.Model(&model.SystemConfig{}).Where("config_key = ? AND value = ?", "register_email_verification", "true").Count(&verificationEnabled).Error; err != nil {
				return err
			}
			if verificationEnabled > 0 {
				if err := platform.ValidateSMTPDeliverySettings(settings, false); err != nil {
					return &platform.SettingValidation{Cause: fmt.Errorf("启用注册邮箱验证码前需要完整配置 SMTP：%w", err)}
				}
			}
		}
		var user model.User
		if err := tx.Select("id", "email").First(&user, actor).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.AuditLog{UserID: &user.ID, Actor: user.Email, Action: "system.config.update", Target: "system_config:" + row.ConfigKey, Detail: fmt.Sprintf("revision=%d", row.Revision)}).Error; err != nil {
			return err
		}
		return settingsQuery(tx).Where("id = ?", row.ID).Scan(&out).Error
	})
	if err != nil {
		return platform.Setting{}, err
	}
	return out, nil
}
