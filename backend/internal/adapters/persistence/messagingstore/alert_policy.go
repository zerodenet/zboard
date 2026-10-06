package messagingstore

import (
	"strconv"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/capabilities/messaging"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func loadAlertPolicy(tx *gorm.DB) (messaging.AlertPolicy, bool, error) {
	p := messaging.AlertPolicy{RemainingPercent: 20, ExpiringDays: 3, IntervalHours: 24}
	var rows []model.SystemConfig
	if err := tx.Where("config_key LIKE ? OR config_key = ?", "subscription_alert_%", "task_email_enabled").Find(&rows).Error; err != nil {
		return p, false, err
	}
	email := false
	for _, row := range rows {
		if strings.HasPrefix(row.ConfigKey, "subscription_alert_") {
			if err := platform.ValidateSettingValue(row.ConfigKey, row.Value); err != nil {
				return p, false, err
			}
		}
		value, _ := strconv.ParseBool(row.Value)
		switch row.ConfigKey {
		case "task_email_enabled":
			email = value
		case "subscription_alert_low_enabled":
			p.Low = value
		case "subscription_alert_exhausted_enabled":
			p.Exhausted = value
		case "subscription_alert_expiring_enabled":
			p.Expiring = value
		case "subscription_alert_expired_enabled":
			p.Expired = value
		case "subscription_alert_remaining_percent":
			p.RemainingPercent, _ = strconv.Atoi(row.Value)
		case "subscription_alert_expiring_days":
			p.ExpiringDays, _ = strconv.Atoi(row.Value)
		case "subscription_alert_interval_hours":
			p.IntervalHours, _ = strconv.Atoi(row.Value)
		}
	}
	return p, email, nil
}
