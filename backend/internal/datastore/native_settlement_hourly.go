package datastore

import (
	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
	"strings"
)

// Legacy hourly-pending facts contribute zero. A minute row may contain both
// previously projected amounts and new pending deltas: include total-pending,
// then let the ordinary worker apply its still-owned delta exactly once.
func rebuildMinuteAwareHourly(tx *gorm.DB) error {
	if !tx.Migrator().HasTable("traffic_usage_hourly") {
		asset := "0024_traffic_usage_hourly.up.sql"
		if IsSQLite(tx) {
			asset = "sqlite/0024_traffic_usage_hourly.sql"
		}
		raw, err := migrations.Files.ReadFile(asset)
		if err != nil {
			return err
		}
		statements, err := splitMigrationStatements(string(raw))
		if err != nil {
			return err
		}
		for _, stmt := range statements {
			upper := strings.ToUpper(strings.TrimSpace(stmt))
			if strings.HasPrefix(upper, "CREATE TABLE") || strings.HasPrefix(upper, "CREATE INDEX") {
				if err := tx.Exec(stmt).Error; err != nil {
					return err
				}
			}
		}
	}
	columns := []string{"raw_bytes", "upload_bytes", "download_bytes", "used_bytes", "record_count"}
	selected := []string{"r.record_at", "COALESCE(r.user_id,0) AS user_id", "COALESCE(r.subscription_id,0) AS subscription_id", "COALESCE(r.node_id,0) AS node_id", "COALESCE(r.protocol_endpoint_id,0) AS protocol_endpoint_id", "COALESCE(r.protocol_multiplier_milli,0) AS protocol_multiplier_milli"}
	for _, c := range columns {
		selected = append(selected, "CASE WHEN COALESCE(p.hourly_pending,0)=0 THEN r."+c+" WHEN p.delta_record_count>0 THEN r."+c+"-p.delta_"+c+" ELSE 0 END AS "+c)
	}
	source := "SELECT " + strings.Join(selected, ",") + " FROM traffic_records r LEFT JOIN metering_usage_projection_pending p ON p.traffic_record_id=r.id"
	hour := "DATE_FORMAT(record_at,'%Y-%m-%d %H:00:00')"
	if IsSQLite(tx) {
		hour = "strftime('%Y-%m-%d %H:00:00',record_at)"
	}
	dims := []string{"user_id", "subscription_id", "node_id", "protocol_endpoint_id", "protocol_multiplier_milli"}
	values := []string{hour}
	values = append(values, dims...)
	for _, c := range columns {
		values = append(values, "SUM("+c+")")
	}
	target := append([]string{"record_at"}, dims...)
	target = append(target, columns...)
	if err := tx.Exec("DELETE FROM traffic_usage_hourly").Error; err != nil {
		return err
	}
	return tx.Exec("INSERT INTO traffic_usage_hourly(" + strings.Join(target, ",") + ") SELECT " + strings.Join(values, ",") + " FROM (" + source + ") AS projected_source WHERE record_count>0 GROUP BY " + hour + "," + strings.Join(dims, ",")).Error
}
