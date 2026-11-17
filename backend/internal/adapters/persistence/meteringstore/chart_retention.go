package meteringstore

import (
	"gorm.io/gorm"
	"strings"
	"time"
)

// Delete only indexed, expired chart keys, with a strict row/transaction bound.
// Financial results, pending contributions and report identities are independent.
func retireChartHistory(tx *gorm.DB, now time.Time, limit int) error {
	hourKey := "strftime('%Y-%m-%d %H:%M:%S',record_at) AS record_at"
	dayKey := "strftime('%Y-%m-%d',usage_date) AS usage_date"
	if tx.Dialector.Name() == "mysql" {
		hourKey = "DATE_FORMAT(record_at,'%Y-%m-%d %H:%i:%s') AS record_at"
		dayKey = "DATE_FORMAT(usage_date,'%Y-%m-%d') AS usage_date"
	}
	var hours []struct {
		RecordAt                                           string
		UserID, SubscriptionID, NodeID, ProtocolEndpointID uint
		ProtocolMultiplierMilli                            int64
	}
	if err := tx.Table("traffic_usage_hourly").Select(hourKey+",user_id,subscription_id,node_id,protocol_endpoint_id,protocol_multiplier_milli").Where("record_at < ?", now.Add(-LedgerHourHorizon).Truncate(24*time.Hour).Format("2006-01-02 15:04:05")).Order("record_at").Limit(limit).Find(&hours).Error; err != nil {
		return err
	}
	for _, h := range hours {
		key := h.RecordAt
		if len(key) >= 19 {
			key = strings.ReplaceAll(key[:19], "T", " ")
		}
		if _, err := time.Parse("2006-01-02 15:04:05", key); err != nil {
			return err
		}
		if err := tx.Table("traffic_usage_hourly").Where("record_at=? AND user_id=? AND subscription_id=? AND node_id=? AND protocol_endpoint_id=? AND protocol_multiplier_milli=?", key, h.UserID, h.SubscriptionID, h.NodeID, h.ProtocolEndpointID, h.ProtocolMultiplierMilli).Delete(nil).Error; err != nil {
			return err
		}
	}
	var days []struct {
		ProtocolEndpointID uint
		UsageDate          string
	}
	if err := tx.Table("protocol_endpoint_usage_daily").Select("protocol_endpoint_id,"+dayKey).Where("usage_date < ?", now.Add(-LedgerDayHorizon).Format("2006-01-02")).Limit(limit).Find(&days).Error; err != nil {
		return err
	}
	for _, d := range days {
		key := d.UsageDate
		if len(key) >= 10 {
			key = key[:10]
		}
		if _, err := time.Parse(time.DateOnly, key); err != nil {
			return err
		}
		if err := tx.Table("protocol_endpoint_usage_daily").Where("protocol_endpoint_id=? AND usage_date=?", d.ProtocolEndpointID, key).Delete(nil).Error; err != nil {
			return err
		}
	}
	return nil
}
