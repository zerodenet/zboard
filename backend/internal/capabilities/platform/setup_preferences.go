package platform

import (
	"strings"
	"time"
)

type InstallationInput struct {
	SiteName                 string  `json:"site_name"`
	SiteURL                  string  `json:"site_url"`
	AllowRegistration        bool    `json:"allow_registration"`
	AdminEmail               string  `json:"admin_email"`
	AdminPassword            string  `json:"admin_password"`
	SystemTimezone           *string `json:"system_timezone,omitempty"`
	AuditLogRetentionDays    *int    `json:"audit_log_retention_days,omitempty"`
	OperationRetentionDays   *int    `json:"operation_history_retention_days,omitempty"`
	TaskHistoryRetentionDays *int    `json:"task_history_retention_days,omitempty"`
}

type SetupPreferences struct {
	SystemTimezone           string `json:"system_timezone"`
	AuditLogRetentionDays    int    `json:"audit_log_retention_days"`
	OperationRetentionDays   int    `json:"operation_history_retention_days"`
	TaskHistoryRetentionDays int    `json:"task_history_retention_days"`
}

func DefaultSetupPreferences() SetupPreferences {
	return SetupPreferences{
		SystemTimezone:           "UTC",
		AuditLogRetentionDays:    180,
		OperationRetentionDays:   90,
		TaskHistoryRetentionDays: 90,
	}
}

func NormalizeSetupPreferences(body InstallationInput) (SetupPreferences, error) {
	preferences := DefaultSetupPreferences()
	fields := map[string]string{}

	if body.SystemTimezone != nil {
		preferences.SystemTimezone = strings.TrimSpace(*body.SystemTimezone)
	}
	if preferences.SystemTimezone == "" {
		fields["system_timezone"] = "请输入有效的 IANA 时区，例如 Asia/Shanghai。"
	} else if _, err := time.LoadLocation(preferences.SystemTimezone); err != nil {
		fields["system_timezone"] = "请输入有效的 IANA 时区，例如 Asia/Shanghai。"
	}

	for _, item := range []struct {
		key      string
		provided *int
		target   *int
	}{
		{"audit_log_retention_days", body.AuditLogRetentionDays, &preferences.AuditLogRetentionDays},
		{"operation_history_retention_days", body.OperationRetentionDays, &preferences.OperationRetentionDays},
		{"task_history_retention_days", body.TaskHistoryRetentionDays, &preferences.TaskHistoryRetentionDays},
	} {
		if item.provided != nil {
			*item.target = *item.provided
		}
		if *item.target < 0 || *item.target > 3650 {
			fields[item.key] = "保留天数必须是 0–3650 之间的整数；0 表示永久保留。"
		}
	}

	if len(fields) > 0 {
		return SetupPreferences{}, &InstallationValidation{Message: "系统策略校验失败。", Fields: fields}
	}
	return preferences, nil
}
