package meteringstore

import (
	"sort"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type endpointUsageKey struct {
	EndpointID uint
	UsageDate  string
}

// AddProtocolEndpointUsage applies ledger deltas to the bounded daily read
// projection. Callers invoke it only after the corresponding TrafficRecord
// insert succeeds and inside the same transaction.
func AddProtocolEndpointUsage(tx *gorm.DB, records []model.TrafficRecord) error {
	if len(records) == 0 {
		return nil
	}
	now := time.Now().UTC()
	rows := make(map[endpointUsageKey]*model.ProtocolEndpointUsageDaily)
	for _, record := range records {
		if record.ProtocolEndpointID == 0 {
			continue
		}
		recordAt := record.At.UTC()
		if recordAt.IsZero() {
			recordAt = now
		}
		key := endpointUsageKey{EndpointID: record.ProtocolEndpointID, UsageDate: recordAt.Format("2006-01-02")}
		row := rows[key]
		if row == nil {
			row = &model.ProtocolEndpointUsageDaily{ProtocolEndpointID: key.EndpointID, UsageDate: key.UsageDate, LastRecordAt: recordAt, UpdatedAt: now}
			rows[key] = row
		}
		row.RawBytes += record.RawBytes
		row.UploadBytes += record.UploadBytes
		row.DownloadBytes += record.DownloadBytes
		row.UsedBytes += record.UsedBytes
		row.RecordCount++
		if recordAt.After(row.LastRecordAt) {
			row.LastRecordAt = recordAt
		}
	}
	keys := make([]endpointUsageKey, 0, len(rows))
	for key := range rows {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].EndpointID != keys[j].EndpointID {
			return keys[i].EndpointID < keys[j].EndpointID
		}
		return keys[i].UsageDate < keys[j].UsageDate
	})
	for _, key := range keys {
		row := rows[key]
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "protocol_endpoint_id"}, {Name: "usage_date"}},
			DoUpdates: clause.Assignments(map[string]any{
				"raw_bytes":      gorm.Expr("raw_bytes + ?", row.RawBytes),
				"upload_bytes":   gorm.Expr("upload_bytes + ?", row.UploadBytes),
				"download_bytes": gorm.Expr("download_bytes + ?", row.DownloadBytes),
				"used_bytes":     gorm.Expr("used_bytes + ?", row.UsedBytes),
				"record_count":   gorm.Expr("record_count + ?", row.RecordCount),
				"last_record_at": gorm.Expr("CASE WHEN last_record_at < ? THEN ? ELSE last_record_at END", row.LastRecordAt, row.LastRecordAt),
				"updated_at":     row.UpdatedAt,
			}),
		}).Create(row).Error; err != nil {
			return err
		}
	}
	return nil
}
