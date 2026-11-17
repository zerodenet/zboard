package platformstore

import (
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/observability"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"strconv"
)

func SetupPreferenceDefinitions() []model.SystemConfig {
	defaults := []model.SystemConfig{}
	for _, setting := range observability.RetentionSettings() {
		defaults = append(defaults, model.SystemConfig{ConfigKey: setting.Key, Name: setting.Name, Value: strconv.Itoa(setting.Default), ValueType: "int", Description: setting.Description, Revision: 1})
	}
	return append(defaults, []model.SystemConfig{
		{
			ConfigKey:   "system_timezone",
			Name:        "系统时区",
			Value:       "UTC",
			ValueType:   "string",
			Description: "系统级时间展示使用的 IANA 时区，例如 Asia/Shanghai、UTC、America/Los_Angeles。历史时间戳仍以 UTC 存储，但会按该时区展示。",
			IsPublic:    true,
			IsSecret:    false,
			Revision:    1,
		},
	}...)
}

func upsertSetupSystemPreferences(tx *gorm.DB, preferences platform.SetupPreferences) error {
	values := map[string]string{
		"system_timezone":                  preferences.SystemTimezone,
		"audit_log_retention_days":         strconv.Itoa(preferences.AuditLogRetentionDays),
		"operation_history_retention_days": strconv.Itoa(preferences.OperationRetentionDays),
		"task_history_retention_days":      strconv.Itoa(preferences.TaskHistoryRetentionDays),
	}
	definitions := make(map[string]model.SystemConfig, len(values))
	for _, definition := range SetupPreferenceDefinitions() {
		definitions[definition.ConfigKey] = definition
	}
	for key, value := range values {
		var current model.SystemConfig
		err := tx.Where("config_key = ?", key).First(&current).Error
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			definition, ok := definitions[key]
			if !ok {
				return errors.New("setup system preference definition not found: " + key)
			}
			definition.Value = value
			if err := tx.Create(&definition).Error; err != nil {
				return err
			}
		case err != nil:
			return err
		default:
			if err := tx.Model(&current).Updates(map[string]interface{}{
				"value":    value,
				"revision": gorm.Expr("revision + 1"),
			}).Error; err != nil {
				return err
			}
		}
	}
	return nil
}
