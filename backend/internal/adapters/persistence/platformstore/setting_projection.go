package platformstore

import (
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"strconv"
)

func syncInstallationConfig(tx *gorm.DB, key, value string) error {
	updates := map[string]interface{}{}
	switch key {
	case "site_name":
		updates["site_name"] = value
	case "site_url":
		updates["site_url"] = value
	case "register_switch":
		enabled, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		updates["allow_registration"] = enabled
	default:
		return nil
	}
	return tx.Model(&model.Installation{}).Where("id = ?", 1).Updates(updates).Error
}
