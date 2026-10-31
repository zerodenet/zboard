package meteringstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"gorm.io/gorm"
)

type Breakdown struct {
	DB, ReadDB *gorm.DB
	Cache      metering.BreakdownCache
}

const breakdownSums = "COALESCE(SUM(raw_bytes),0) AS raw_bytes, COALESCE(SUM(upload_bytes),0) AS upload_bytes, COALESCE(SUM(download_bytes),0) AS download_bytes, COALESCE(SUM(used_bytes),0) AS used_bytes, COALESCE(SUM(record_count),0) AS record_count"

func (s Breakdown) Read(ctx context.Context, actor uint, q metering.BreakdownQuery) (metering.BreakdownSnapshot, error) {
	if err := authorizeReportingRead(ctx, s.DB, actor, q.Administrative); err != nil {
		return metering.BreakdownSnapshot{}, err
	}
	if !q.Administrative {
		q.UserID = actor
	}
	column, ok := map[string]string{"user": "user_id", "node": "node_id", "subscription": "subscription_id", "protocol_endpoint": "protocol_endpoint_id"}[q.Dimension]
	if !ok {
		return metering.BreakdownSnapshot{}, fmt.Errorf("invalid statistics dimension")
	}
	read := s.ReadDB
	if read == nil {
		read = s.DB
	}
	scope := TrafficScope{q.UserID, q.SubscriptionID, q.NodeID, q.ProtocolEndpointID}
	// A cached historical snapshot must not disguise pending current facts.
	pending := false
	if s.Cache != nil {
		var err error
		pending, err = inspectHourlyUsageWindows(read.WithContext(ctx), scope, [][2]time.Time{{q.From, q.To}})
		if err != nil {
			return metering.BreakdownSnapshot{}, err
		}
	}
	load := func() (metering.BreakdownSnapshot, error) {
		out := metering.BreakdownSnapshot{Dimension: q.Dimension, From: q.From, To: q.To, AsOf: time.Now().UTC(), Items: []metering.BreakdownRow{}, Offset: q.Offset, Limit: q.Limit}
		err := read.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := EnsureHourlyUsageReadable(tx, scope, q.From, q.To); err != nil {
				return err
			}
			source := func() *gorm.DB {
				return TrafficAggregateSource(tx, q.From, q.To, TrafficScope{q.UserID, q.SubscriptionID, q.NodeID, q.ProtocolEndpointID})
			}
			var statistics struct {
				metering.BreakdownCounters `gorm:"embedded"`
				Total                      int64
			}
			id := "COALESCE(" + column + ",0)"
			if err := source().Select(breakdownSums + ", COUNT(DISTINCT " + id + ") AS total").Scan(&statistics).Error; err != nil {
				return err
			}
			out.Total, out.Totals = statistics.Total, statistics.BreakdownCounters
			if err := source().Select(id + " AS id, " + breakdownSums).Group(id).Order("used_bytes DESC, id ASC").Offset(q.Offset).Limit(q.Limit).Scan(&out.Items).Error; err != nil {
				return err
			}
			return loadBreakdownNames(tx, q, out.Items)
		}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
		return out, err
	}
	if s.Cache == nil {
		return load()
	}
	key, err := json.Marshal(q)
	if err != nil {
		return metering.BreakdownSnapshot{}, err
	}
	if pending {
		invalidateUsageSnapshot(s.Cache, sha256.Sum256(key))
		return load()
	}
	return s.Cache.Get(ctx, sha256.Sum256(key), load)
}

func loadBreakdownNames(tx *gorm.DB, q metering.BreakdownQuery, rows []metering.BreakdownRow) error {
	ids := make([]uint, 0, len(rows))
	for _, row := range rows {
		if row.ID > 0 {
			ids = append(ids, row.ID)
		}
	}
	type reference struct {
		ID              uint
		Name, Secondary string
	}
	var references []reference
	if len(ids) > 0 {
		var query *gorm.DB
		switch q.Dimension {
		case "user":
			query = tx.Table("users").Select("id, email AS name").Where("id IN ?", ids)
		case "node":
			query = tx.Table("nodes").Select("id, name, region AS secondary").Where("id IN ?", ids)
		case "protocol_endpoint":
			query = tx.Table("protocol_endpoints").Select("id, name, protocol AS secondary").Where("id IN ?", ids)
		case "subscription":
			query = tx.Table("subscriptions").Select("subscriptions.id, plans.name AS name, plan_skus.name AS secondary").Joins("LEFT JOIN plans ON plans.id = subscriptions.plan_id").Joins("LEFT JOIN plan_skus ON plan_skus.id = subscriptions.plan_sku_id").Where("subscriptions.id IN ?", ids)
			if !q.Administrative {
				query = query.Where("subscriptions.user_id = ?", q.UserID)
			}
		}
		if err := query.Scan(&references).Error; err != nil {
			return err
		}
	}
	byID := make(map[uint]reference, len(references))
	for _, ref := range references {
		byID[ref.ID] = ref
	}
	for i := range rows {
		ref, found := byID[rows[i].ID]
		rows[i].Name, rows[i].Secondary, rows[i].Missing = ref.Name, ref.Secondary, !found
	}
	return nil
}
