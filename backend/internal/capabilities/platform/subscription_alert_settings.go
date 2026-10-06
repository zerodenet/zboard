package platform

func subscriptionAlertDefaults() []SystemConfigDefault {
	return []SystemConfigDefault{
		{Key: "subscription_alert_low_enabled", Name: "余量不足提醒", Value: "false", ValueType: "bool", Description: "每个流量周期提醒一次；余量恢复后取消尚未发送的提醒"},
		{Key: "subscription_alert_exhausted_enabled", Name: "流量耗尽提醒", Value: "false", ValueType: "bool", Description: "每个流量周期提醒一次，不停止设备或修改套餐"},
		{Key: "subscription_alert_expiring_enabled", Name: "即将到期提醒", Value: "false", ValueType: "bool", Description: "同一个到期日期提醒一次；续费后按新日期判断"},
		{Key: "subscription_alert_expired_enabled", Name: "已到期提醒", Value: "false", ValueType: "bool", Description: "到期后 7 天内提醒一次，不追发更早的历史到期邮件"},
		{Key: "subscription_alert_remaining_percent", Name: "剩余流量阈值（%）", Value: "20", ValueType: "int", Description: "余量不超过此百分比时触发，范围 1–90"},
		{Key: "subscription_alert_expiring_days", Name: "到期提前天数", Value: "3", ValueType: "int", Description: "进入到期前的此天数范围时触发，范围 1–30"},
		{Key: "subscription_alert_interval_hours", Name: "用户提醒间隔（小时）", Value: "24", ValueType: "int", Description: "跨告警和订阅限制入队及发送尝试频率，范围 6–168"},
	}
}

func alertSettingBounds(key string) (int64, int64, bool) {
	switch key {
	case "subscription_alert_remaining_percent":
		return 1, 90, true
	case "subscription_alert_expiring_days":
		return 1, 30, true
	case "subscription_alert_interval_hours":
		return 6, 168, true
	}
	return 0, 0, false
}
