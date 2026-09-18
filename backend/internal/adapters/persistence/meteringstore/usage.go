package meteringstore

import (
	"context"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type usageBucketRow struct {
	ID                      uint       `json:"id" gorm:"column:id"`
	UserID                  uint       `json:"user_id" gorm:"column:user_id"`
	SubscriptionID          uint       `json:"subscription_id,omitempty" gorm:"column:subscription_id"`
	NodeID                  uint       `json:"node_id" gorm:"column:node_id"`
	RawBytes                int64      `json:"raw_bytes" gorm:"column:raw_bytes"`
	UploadBytes             int64      `json:"upload_bytes" gorm:"column:upload_bytes"`
	DownloadBytes           int64      `json:"download_bytes" gorm:"column:download_bytes"`
	ProtocolMultiplierMilli int64      `json:"protocol_multiplier_milli" gorm:"column:protocol_multiplier_milli"`
	UsedBytes               int64      `json:"used_bytes" gorm:"column:used_bytes"`
	RecordAt                BucketTime `json:"record_at" gorm:"column:record_at"`
	RecordCount             int64      `json:"record_count" gorm:"column:record_count"`
}

type Usage struct {
	DB, ReadDB *gorm.DB
	Statistics UsageStatisticsReader
}

func (s Usage) Read(ctx context.Context, actor uint, q metering.UsageQuery) (metering.UsageSnapshot, error) {
	if err := authorizeReportingRead(ctx, s.DB, actor, q.Administrative); err != nil {
		return metering.UsageSnapshot{}, err
	}
	if !q.Administrative {
		q.UserID = actor
	}
	out := metering.UsageSnapshot{Rows: []metering.UsageRow{}, References: []metering.UsageReference{}}
	read := s.ReadDB
	if read == nil {
		read = s.DB
	}
	db := read.WithContext(ctx)
	bucket, err := ParseUsageBucket(q.Bucket)
	if err != nil {
		return out, err
	}
	bucket = bucket.ForDB(db)
	window := UsageWindow{From: q.From, To: q.To}
	cursor, offset, limit := q.Cursor, q.Offset, q.Limit
	base := db.Model(&model.TrafficRecord{})
	if q.UserID > 0 {
		base = base.Where("user_id = ?", q.UserID)
	}
	if q.SubscriptionID > 0 {
		base = base.Where("subscription_id = ?", q.SubscriptionID)
	}
	if q.NodeID > 0 {
		base = base.Where("node_id = ?", q.NodeID)
	}
	if q.ProtocolEndpointID > 0 {
		base = base.Where("protocol_endpoint_id = ?", q.ProtocolEndpointID)
	}
	pageScope := base.Session(&gorm.Session{})
	base = applyUsageWindow(base.Session(&gorm.Session{}), "record_at", window)
	if q.IncludeTotals || q.SummaryOnly {
		statistics, err := s.Statistics.Read(base, bucket, window)
		if err != nil {
			return metering.UsageSnapshot{}, err
		}
		out.Statistics = &statistics
	}
	if q.SummaryOnly {
		return out, nil
	}
	pageSource := bucket.SeekSource(base.Session(&gorm.Session{}), cursor)
	if cursor == nil && offset == 0 {
		pageSource = bucket.FirstPageSource(pageScope, window, limit)
	}
	grouped := pageSource.Session(&gorm.Session{}).
		Select(`
			MIN(id) AS id,
			user_id,
			COALESCE(subscription_id, 0) AS subscription_id,
			node_id,
			protocol_multiplier_milli,
			COALESCE(SUM(raw_bytes), 0) AS raw_bytes,
			COALESCE(SUM(upload_bytes), 0) AS upload_bytes,
			COALESCE(SUM(download_bytes), 0) AS download_bytes,
			COALESCE(SUM(used_bytes), 0) AS used_bytes,
			` + bucket.Expression + ` AS record_at,
			COUNT(*) AS record_count
		`).
		Group(bucket.Group())

	bucketQuery := db.Table("(?) AS traffic_usage_buckets", grouped)
	if cursor == nil && offset == 0 && datastore.IsSQLite(db) {
		bucketQuery = bucket.SelectedFirstPageQuery(base, pageSource, limit)
	}
	if cursor != nil {
		var at any = cursor.At
		if datastore.IsSQLite(db) {
			at = cursor.At.Format("2006-01-02 15:04:05.999999999")
		}
		if cursor.Direction == "older" {
			bucketQuery = bucketQuery.Where("(record_at < ?) OR (record_at = ? AND id < ?)", at, at, cursor.ID)
		} else {
			bucketQuery = bucketQuery.Where("(record_at > ?) OR (record_at = ? AND id > ?)", at, at, cursor.ID)
		}
	}
	order := "record_at desc, id desc"
	if cursor != nil && cursor.Direction == "newer" {
		order = "record_at asc, id asc"
	}

	rows := make([]usageBucketRow, 0, limit+1)
	if cursor == nil && offset > 0 {
		err = bucketQuery.Order("record_at desc, id desc").Offset(offset).Limit(limit).Scan(&rows).Error
	} else {
		err = bucketQuery.Order(order).Limit(limit + 1).Scan(&rows).Error
		if len(rows) > limit {
			out.HasMore = true
			rows = rows[:limit]
		}
		if cursor != nil && cursor.Direction == "newer" {
			for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
				rows[i], rows[j] = rows[j], rows[i]
			}
		}
	}
	if err != nil {
		return metering.UsageSnapshot{}, err
	}
	for _, row := range rows {
		out.Rows = append(out.Rows, metering.UsageRow{ID: row.ID, UserID: row.UserID, SubscriptionID: row.SubscriptionID, NodeID: row.NodeID, RawBytes: row.RawBytes, UploadBytes: row.UploadBytes, DownloadBytes: row.DownloadBytes, ProtocolMultiplierMilli: row.ProtocolMultiplierMilli, UsedBytes: row.UsedBytes, RecordAt: row.RecordAt.Time, RecordCount: row.RecordCount})
	}
	if !q.Administrative {
		out.References, err = usageReferences(db, out.Rows, actor)
		if err != nil {
			return metering.UsageSnapshot{}, err
		}
	}
	return out, nil
}
