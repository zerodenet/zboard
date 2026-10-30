package platformstore

import (
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"strconv"
)

func SetupPreferenceDefinitions() []model.SystemConfig {
	return []model.SystemConfig{
		{ConfigKey: "periodic_success_retention_days", Name: "成功周期任务保留天数", Value: "7", ValueType: "int", Description: "已结束的周期任务执行记录；不影响运营任务、显式提交幂等键或当前任务。0 表示永久保留。", Revision: 1},
		{ConfigKey: "periodic_failure_retention_days", Name: "失败周期任务保留天数", Value: "30", ValueType: "int", Description: "已确认失败或取消的周期任务；结果不明和中断的执行证据保留。0 表示永久保留。", Revision: 1},
		{ConfigKey: "archived_flow_cursor_retention_days", Name: "归档节点计费游标保留天数", Value: "7", ValueType: "int", Description: "仅回收永久归档且凭证已撤销节点的累计游标；账本、报文回执和历史身份永久保留。0 表示永久保留。", Revision: 1},
		{
			ConfigKey:   "audit_log_retention_days",
			Name:        "审计日志保留天数",
			Value:       strconv.Itoa(180),
			ValueType:   "int",
			Description: "超过该天数的审计日志会自动清理；0 表示永久保留。",
			IsPublic:    false,
			IsSecret:    false,
			Revision:    1,
		},
		{
			ConfigKey:   "operation_history_retention_days",
			Name:        "运行历史保留天数",
			Value:       strconv.Itoa(90),
			ValueType:   "int",
			Description: "超过该天数且已结束的节点操作、协议发布、证书与供应商操作会自动清理；0 表示永久保留。",
			IsPublic:    false,
			IsSecret:    false,
			Revision:    1,
		},
		{
			ConfigKey:   "task_history_retention_days",
			Name:        "运营任务保留天数",
			Value:       strconv.Itoa(90),
			ValueType:   "int",
			Description: "超过该天数且已完成的运营任务及任务项会自动清理；0 表示永久保留。",
			IsPublic:    false,
			IsSecret:    false,
			Revision:    1,
		},
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
	}
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
