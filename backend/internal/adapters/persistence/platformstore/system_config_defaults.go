package platformstore

import (
	"context"
	"fmt"

	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type SystemConfigDefaults struct{ DB *gorm.DB }

func (s SystemConfigDefaults) ReconcileSystemConfigDefaults(ctx context.Context, defaults []platform.SystemConfigDefault) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, definition := range defaults {
			item := model.SystemConfig{
				ConfigKey: definition.Key, Name: definition.Name, Value: definition.Value,
				ValueType: definition.ValueType, Description: definition.Description,
				IsPublic: definition.Public, IsSecret: definition.Secret,
			}
			if err := tx.Where("config_key = ?", item.ConfigKey).FirstOrCreate(&item).Error; err != nil {
				return fmt.Errorf("reconcile system config %s: %w", item.ConfigKey, err)
			}
		}
		return nil
	})
}
