package platformstore

import (
	"context"
	"errors"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type Settings struct{ DB *gorm.DB }

func (s Settings) Public(ctx context.Context) (out []platform.Setting, err error) {
	err = settingsQuery(s.DB.WithContext(ctx)).Where("is_public = ? AND is_secret = ?", true, false).Scan(&out).Error
	return out, err
}
func (s Settings) Administrative(ctx context.Context, actor uint) (out []platform.Setting, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := RequireMigrationAdministrator(tx, actor); err != nil {
			if errors.Is(err, jobs.ErrPermission) {
				return platform.ErrSettingsPermission
			}
			return err
		}
		return settingsQuery(tx).Scan(&out).Error
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
func (s Settings) Lookup(ctx context.Context, key string) (out platform.Setting, err error) {
	err = settingsQuery(s.DB.WithContext(ctx)).Where("config_key = ?", key).Take(&out).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return platform.Setting{}, platform.ErrSettingNotFound
	}
	return out, err
}
func settingsQuery(db *gorm.DB) *gorm.DB {
	return db.Model(&model.SystemConfig{}).Select("id, config_key, name, value_type, description, is_public, is_secret, revision, updated_at, CASE WHEN is_secret THEN '' ELSE value END AS value, CASE WHEN value <> '' THEN 1 ELSE 0 END AS configured").Order("id ASC")
}
