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
			if err := base().Select("COALESCE(SUM(raw_bytes), 0) AS raw_bytes, COALESCE(SUM(used_bytes), 0) AS used_bytes, COUNT(DISTINCT user_id) AS user_count, COUNT(DISTINCT NULLIF(subscription_id, 0)) AS subscription_count, COUNT(DISTINCT node_id) AS node_count, COUNT(DISTINCT protocol_endpoint_id) AS protocol_endpoint_count").Scan(&out.Aggregates).Error; err != nil {
				return err
			}
			if err := base().Count(&out.Total).Error; err != nil {
				return err
			}
			query := base()
			if q.Cursor == nil && q.Offset > 0 {
				if err := query.Order("record_at desc, id desc").Offset(q.Offset).Limit(q.Limit).Find(&records).Error; err != nil {
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
				if err := query.Order(order).Limit(q.Limit + 1).Find(&records).Error; err != nil {
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
			if err := base().Order("id desc").Find(&records).Error; err != nil {
				return err
			}
		}
		for _, record := range records {
			out.Records = append(out.Records, metering.TrafficRecord(record))
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return metering.RecordsSnapshot{}, err
	}
	return out, nil
}
