package platformstore

import (
	"context"
	"errors"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type SiteCustomizationDefaults struct{ DB *gorm.DB }

func (s SiteCustomizationDefaults) ReconcileSiteCustomizationDefaults(ctx context.Context, definitions []platform.SiteCustomizationDefault) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, definition := range definitions {
			var existing model.SystemConfig
			err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("config_key = ?", definition.ConfigKey).First(&existing).Error
			switch {
			case errors.Is(err, gorm.ErrRecordNotFound):
				value := definition.Value
				if definition.LegacyKey != "" {
					var legacy model.SystemConfig
					legacyErr := tx.Where("config_key = ?", definition.LegacyKey).First(&legacy).Error
					if legacyErr != nil && !errors.Is(legacyErr, gorm.ErrRecordNotFound) {
						return legacyErr
					}
					if legacyValue := strings.TrimSpace(legacy.Value); legacyErr == nil && legacyValue != "" {
						value = legacyValue
					}
				}
				revision := definition.Revision
				if revision == 0 {
					revision = 1
				}
				row := model.SystemConfig{ConfigKey: definition.ConfigKey, Name: definition.Name, Value: value, ValueType: definition.ValueType, Description: definition.Description, IsPublic: definition.IsPublic, IsSecret: definition.IsSecret, Revision: revision}
				if err := tx.Create(&row).Error; err != nil {
					return err
				}
			case err != nil:
				return err
			default:
				if err := tx.Model(&existing).Updates(map[string]any{"name": definition.Name, "value_type": definition.ValueType, "description": definition.Description, "is_public": definition.IsPublic, "is_secret": definition.IsSecret}).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}
