package meteringstore

import (
	"sort"
	"time"

	"github.com/zerodenet/zboard/backend/internal/datastore"
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
	if len(keys) == 0 {
		return nil
	}
	batch := make([]model.ProtocolEndpointUsageDaily, 0, len(keys))
	for _, key := range keys {
		batch = append(batch, *rows[key])
	}
	// Keep one atomic delta per endpoint/day, but send bounded batches instead
	// of one round trip per key. Sorted keys preserve the writers' lock order.
	incoming := func(column string) string {
		if datastore.IsSQLite(tx) {
			return "excluded." + column
		}
		return "VALUES(" + column + ")"
	}
	updates := make(map[string]any)
	for _, column := range []string{"raw_bytes", "upload_bytes", "download_bytes", "used_bytes", "record_count"} {
		updates[column] = gorm.Expr(column + " + " + incoming(column))
	}
	last := incoming("last_record_at")
	updates["last_record_at"] = gorm.Expr("CASE WHEN last_record_at < " + last + " THEN " + last + " ELSE last_record_at END")
	updates["updated_at"] = gorm.Expr(incoming("updated_at"))
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "protocol_endpoint_id"}, {Name: "usage_date"}},
		DoUpdates: clause.Assignments(updates),
	}).CreateInBatches(&batch, 100).Error
}
