package model

import (
	"errors"
	"fmt"
	"strings"
	"time"
	_ "time/tzdata"

	"gorm.io/gorm"
)

const systemTimezoneConfigKey = "system_timezone"

func validateSystemTimezone(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("system timezone is required")
	}
	if _, err := time.LoadLocation(value); err != nil {
		return fmt.Errorf("invalid IANA system timezone %q", value)
	}
	return nil
}

func systemConfigUpdateValue(tx *gorm.DB, fallback string) string {
	switch value := tx.Statement.Dest.(type) {
	case map[string]interface{}:
		if candidate, ok := value["value"]; ok {
			if text, ok := candidate.(string); ok {
				return text
			}
		}
	case map[string]string:
		if candidate, ok := value["value"]; ok {
			return candidate
		}
	case SystemConfig:
		return value.Value
	case *SystemConfig:
		if value != nil {
			return value.Value
		}
	}
	return fallback
}

func (config *SystemConfig) BeforeCreate(_ *gorm.DB) error {
	if strings.TrimSpace(config.ConfigKey) != systemTimezoneConfigKey {
		return nil
	}
	return validateSystemTimezone(config.Value)
}

func (config *SystemConfig) BeforeUpdate(tx *gorm.DB) error {
	if strings.TrimSpace(config.ConfigKey) != systemTimezoneConfigKey {
		return nil
	}
	return validateSystemTimezone(systemConfigUpdateValue(tx, config.Value))
}
