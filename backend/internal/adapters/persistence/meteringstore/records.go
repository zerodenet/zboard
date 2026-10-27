package meteringstore

import (
	"context"
	"database/sql"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type Records struct{ DB, ReadDB *gorm.DB }

func (s Records) Read(ctx context.Context, actor uint, q metering.RecordsQuery) (metering.RecordsSnapshot, error) {
	return s.read(ctx, actor, q, nil)
}

func (s Records) ReadSummaryPage(ctx context.Context, actor uint, q metering.RecordsQuery) (metering.RecordsSummaryPage, error) {
	if !q.Paged {
		return metering.RecordsSummaryPage{}, &metering.PolicyValidation{Fields: map[string]string{"query": "summary requires pagination"}}
	}
	full, err := s.read(ctx, actor, q, []string{"id", "user_id", "subscription_id", "node_id", "protocol_endpoint_id", "raw_bytes", "upload_bytes", "download_bytes", "protocol_multiplier_milli", "used_bytes", "record_at"})
	if err != nil {
		return metering.RecordsSummaryPage{}, err
	}
	out := metering.RecordsSummaryPage{Records: make([]metering.TrafficRecordSummary, 0, len(full.Records)), Total: full.Total, Aggregates: full.Aggregates, HasMore: full.HasMore}
	for _, record := range full.Records {
		out.Records = append(out.Records, metering.TrafficRecordSummary{
			ID: record.ID, UserID: record.UserID, SubscriptionID: record.SubscriptionID,
			NodeID: record.NodeID, ProtocolEndpointID: record.ProtocolEndpointID,
			RawBytes: record.RawBytes, UploadBytes: record.UploadBytes, DownloadBytes: record.DownloadBytes,
			ProtocolMultiplierMilli: record.ProtocolMultiplierMilli, UsedBytes: record.UsedBytes, At: record.At,
		})
	}
	return out, nil
}

// Full and explicitly summarized pages share authorization, scope, statistics
// and one read snapshot. Only the bounded page's selected columns differ.
func (s Records) read(ctx context.Context, actor uint, q metering.RecordsQuery, columns []string) (metering.RecordsSnapshot, error) {
	if err := authorizeReportingRead(ctx, s.DB, actor, q.Administrative); err != nil {
		return metering.RecordsSnapshot{}, err
	}
	if !q.Administrative {
		q.UserID = actor
	}
	out := metering.RecordsSnapshot{Records: []metering.TrafficRecord{}}
	read := s.ReadDB
	if read == nil {
		read = s.DB
	}
	err := read.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		proofBudget := fullProofReadMaterialBytes
		checkFull := func(query *gorm.DB) error {
			if len(columns) > 0 {
				return nil
			}
			var err error
			proofBudget, err = preflightFullProofRead(tx, query)
			return err
		}
		base := func() *gorm.DB {
			query := tx.Model(&model.TrafficRecord{})
			if q.UserID > 0 {
				query = query.Where("user_id = ?", q.UserID)
			}
			if q.SubscriptionID > 0 {
				query = query.Where("subscription_id = ?", q.SubscriptionID)
			}
			if q.NodeID > 0 {
				query = query.Where("node_id = ?", q.NodeID)
			}
			if q.ProtocolEndpointID > 0 {
				query = query.Where("protocol_endpoint_id = ?", q.ProtocolEndpointID)
			}
			if q.Paged {
				query = query.Where("record_at >= ? AND record_at < ?", q.From, q.To)
			}
			return query
		}
		var records []model.TrafficRecord
		if q.Paged {
			// Count and sums share one scan and snapshot. Detail pagination still
			// reads authoritative rows; cursor movement never changes the totals.
			var statistics struct {
				metering.RecordAggregates `gorm:"embedded"`
				Total                     int64
			}
			if err := base().Select("COUNT(*) AS total, COALESCE(SUM(raw_bytes), 0) AS raw_bytes, COALESCE(SUM(used_bytes), 0) AS used_bytes, COUNT(DISTINCT user_id) AS user_count, COUNT(DISTINCT NULLIF(subscription_id, 0)) AS subscription_count, COUNT(DISTINCT node_id) AS node_count, COUNT(DISTINCT protocol_endpoint_id) AS protocol_endpoint_count").Scan(&statistics).Error; err != nil {
				return err
			}
			out.Aggregates, out.Total = statistics.RecordAggregates, statistics.Total
			query := base()
			if len(columns) > 0 {
				query = query.Select(columns)
			}
			if q.Cursor == nil && q.Offset > 0 {
				query = query.Order("record_at desc, id desc").Offset(q.Offset).Limit(q.Limit)
				if err := checkFull(query); err != nil {
					return err
				}
				if err := query.Find(&records).Error; err != nil {
					return err
				}
			} else {
				order := "record_at desc, id desc"
				if cursor := q.Cursor; cursor != nil {
					if cursor.Direction == "older" {
						query = query.Where("(record_at < ?) OR (record_at = ? AND id < ?)", cursor.At, cursor.At, cursor.ID)
					} else {
						query = query.Where("(record_at > ?) OR (record_at = ? AND id > ?)", cursor.At, cursor.At, cursor.ID)
						order = "record_at asc, id asc"
					}
				}
				query = query.Order(order).Limit(q.Limit + 1)
				if err := checkFull(query); err != nil {
					return err
				}
				if err := query.Find(&records).Error; err != nil {
					return err
				}
				out.HasMore = len(records) > q.Limit
				if out.HasMore {
					records = records[:q.Limit]
				}
				if q.Cursor != nil && q.Cursor.Direction == "newer" {
					for i, j := 0, len(records)-1; i < j; i, j = i+1, j-1 {
						records[i], records[j] = records[j], records[i]
					}
				}
			}
		} else {
			query := base().Order("id desc").Limit(fullProofReadRows + 1)
			if err := checkFull(query); err != nil {
				return err
			}
			if err := query.Find(&records).Error; err != nil {
				return err
			}
		}
		for _, record := range records {
			if len(columns) == 0 {
				var err error
				record, err = hydrateProof(tx, record, &proofBudget)
				if err != nil {
					return err
				}
			}
			out.Records = append(out.Records, metering.TrafficRecord(record))
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return metering.RecordsSnapshot{}, err
	}
	return out, nil
}
