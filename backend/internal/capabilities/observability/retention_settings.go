package observability

import (
	"context"
	"time"
)

const (
	DefaultAuditRetentionDays     = 30
	DefaultOperationRetentionDays = 14
	DefaultTaskRetentionDays      = 7
	CleanupIntervalKey            = "history_cleanup_interval_minutes"
	ObservationMaterialDaysKey    = "observation_material_retention_days"
	ObservationReceiptDaysKey     = "observation_receipt_retention_days"
)

// Defaults, validation and settings metadata share this small policy list.
// Existing administrator values are never overwritten when defaults change.
type RetentionSetting struct {
	Key, Name, Description string
	Default, Maximum       int
}

func RetentionSettings() []RetentionSetting {
	return []RetentionSetting{
		{CleanupIntervalKey, "日志清理间隔（分钟）", "默认每5分钟检查；0关闭日志和任务历史自动清理。", 5, 60},
		{AuditLogRetentionKey, "审计日志保留天数", "过期自动删除；0永久保留。", DefaultAuditRetentionDays, RetentionMaxDays},
		{OperationRetentionKey, "操作历史保留天数", "清理已结束的节点、发布、证书与DNS操作；0永久保留。", DefaultOperationRetentionDays, RetentionMaxDays},
		{TaskRetentionKey, "运营任务保留天数", "清理已结束任务及明细，保留进行中和结果不明的任务；0永久保留。", DefaultTaskRetentionDays, RetentionMaxDays},
		{PeriodicSuccessRetentionKey, "成功周期任务保留天数", "清理已结束的周期执行记录；0永久保留。", 7, RetentionMaxDays},
		{PeriodicFailureRetentionKey, "失败周期任务保留天数", "清理已确认失败或取消的周期执行记录；0永久保留。", 30, RetentionMaxDays},
		{ArchiveCursorRetentionKey, "已删除节点计费游标保留天数", "仅回收已删除节点的旧游标，不删除结算汇总和补报去重依据；0永久保留。", 7, RetentionMaxDays},
		{ObservationMaterialDaysKey, "节点观察材料保留天数", "仅清理辅助观察材料，当前状态和计费依据保留；0关闭按时间淘汰，容量保护仍生效。", 7, RetentionMaxDays},
		{ObservationReceiptDaysKey, "观察清理记录保留天数", "清理已完成且不再被引用的辅助记录；0永久保留。", 1, RetentionMaxDays},
	}
}
func RetentionSettingFor(key string) (RetentionSetting, bool) {
	for _, s := range RetentionSettings() {
		if s.Key == key {
			return s, true
		}
	}
	return RetentionSetting{}, false
}

// Use the existing minute scheduler and wall-clock slots. No additional timer,
// cleanup queue or persistent scheduling table is needed; edits apply live.
func (s HistoryRetention) CleanupDue(ctx context.Context, now time.Time) (bool, error) {
	minutes, err := s.Repository.RetentionDays(ctx, CleanupIntervalKey, 5, 60)
	return err == nil && minutes > 0 && (now.UTC().Unix()/60)%int64(max(1, minutes)) == 0, err
}
