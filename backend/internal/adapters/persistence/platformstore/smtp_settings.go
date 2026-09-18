package platformstore

import (
	"errors"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"strconv"
)

func LoadSMTPSettings(db *gorm.DB, cipher platform.SettingsCipher, requireEnabled bool) (platform.SMTPSettings, error) {
	keys := []string{"task_email_enabled", "smtp_host", "smtp_port", "smtp_username", "smtp_password", "smtp_from", "smtp_tls_mode"}
	var configs []model.SystemConfig
	if err := db.Where("config_key IN ?", keys).Find(&configs).Error; err != nil {
		return platform.SMTPSettings{}, err
	}
	values := make(map[string]string, len(configs))
	for _, config := range configs {
		value := config.Value
		if config.IsSecret && value != "" {
			decrypted, err := cipher.Decrypt(value)
			if err != nil {
				return platform.SMTPSettings{}, fmt.Errorf("decrypt %s: %w", config.ConfigKey, err)
			}
			value = decrypted
		}
		values[config.ConfigKey] = value
	}
	port, err := strconv.Atoi(values["smtp_port"])
	if err != nil {
		return platform.SMTPSettings{}, &platform.SettingValidation{Cause: errors.New("smtp_port is invalid")}
	}
	enabled, _ := strconv.ParseBool(values["task_email_enabled"])
	settings := platform.SMTPSettings{
		Enabled: enabled, Host: values["smtp_host"], Port: port,
		Username: values["smtp_username"], Password: values["smtp_password"],
		From: values["smtp_from"], TLSMode: values["smtp_tls_mode"],
	}
	if !settings.Enabled && !requireEnabled {
		return settings, nil
	}
	if err := platform.ValidateSMTPDeliverySettings(settings, requireEnabled); err != nil {
		return platform.SMTPSettings{}, &platform.SettingValidation{Cause: err}
	}
	return settings, nil
}
