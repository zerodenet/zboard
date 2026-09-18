package meteringstore

import (
	"context"
	"database/sql"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"sort"
	"strings"
	"time"
)

type NodeSeries struct{ DB, ReadDB *gorm.DB }

func nodeSeriesBucket(db *gorm.DB, bucket string) (string, error) {
	spec, err := ParseUsageBucket(bucket)
	if err != nil {
		return "", err
	}
	return spec.ForDB(db).Expression, nil
}
func (s NodeSeries) Read(ctx context.Context, actor uint, q metering.NodeSeriesQuery) (metering.NodeSeriesSnapshot, error) {
	if err := authorizeReportingRead(ctx, s.DB, actor, q.Administrative); err != nil {
		return metering.NodeSeriesSnapshot{}, err
	}
	if !q.Administrative {
		q.UserID = actor
	}
	result := metering.NodeSeriesSnapshot{Points: []metering.NodeSeriesPoint{}, Nodes: []metering.SeriesNode{}, AsOf: time.Now().UTC()}
	read := s.ReadDB
	if read == nil {
		read = s.DB
	}
	err := read.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		expression, err := nodeSeriesBucket(tx, q.Bucket)
		if err != nil {
			return err
		}
		query := tx.Model(&model.TrafficRecord{}).Where("record_at >= ? AND record_at < ?", q.From, q.To)
		if q.UserID > 0 {
			query = query.Where("user_id = ?", q.UserID)
		}
		if q.SubscriptionID > 0 {
			query = query.Where("subscription_id = ?", q.SubscriptionID)
		}
		if q.NodeID > 0 {
			query = query.Where("node_id = ?", q.NodeID)
		} else {
			var totals []struct {
				NodeID    uint
				UsedBytes int64
			}
			if err := query.Session(&gorm.Session{}).Select("node_id, COALESCE(SUM(used_bytes), 0) AS used_bytes").Where("node_id > 0").Group("node_id").Order("COALESCE(SUM(used_bytes), 0) DESC, node_id ASC").Limit(metering.NodeSeriesNodeLimit + 1).Scan(&totals).Error; err != nil {
				return err
			}
			if len(totals) > metering.NodeSeriesNodeLimit {
				result.Truncated = true
				totals = totals[:metering.NodeSeriesNodeLimit]
			}
			if len(totals) == 0 {
				return nil
			}
			ids := make([]uint, 0, len(totals))
			for _, total := range totals {
				ids = append(ids, total.NodeID)
			}
			query = query.Where("node_id IN ?", ids)
		}
		var rows []struct {
			RecordAt                                                     BucketTime
			NodeID                                                       uint
			RawBytes, UploadBytes, DownloadBytes, UsedBytes, RecordCount int64
		}
		if err := query.Session(&gorm.Session{}).Select(expression + " AS record_at, node_id, COALESCE(SUM(raw_bytes), 0) AS raw_bytes, COALESCE(SUM(upload_bytes), 0) AS upload_bytes, COALESCE(SUM(download_bytes), 0) AS download_bytes, COALESCE(SUM(used_bytes), 0) AS used_bytes, COUNT(*) AS record_count").Group(expression + ", node_id").Order("record_at asc, node_id asc").Scan(&rows).Error; err != nil {
			return err
		}
		ids := make([]uint, 0)
		seen := map[uint]bool{}
		for _, row := range rows {
			result.Points = append(result.Points, metering.NodeSeriesPoint{RecordAt: row.RecordAt.Time, NodeID: row.NodeID, RawBytes: row.RawBytes, UploadBytes: row.UploadBytes, DownloadBytes: row.DownloadBytes, UsedBytes: row.UsedBytes, RecordCount: row.RecordCount})
			if row.NodeID > 0 && !seen[row.NodeID] {
				seen[row.NodeID] = true
				ids = append(ids, row.NodeID)
			}
		}
		if len(ids) == 0 {
			return nil
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		var nodes []model.Node
		if err := tx.Select("id, name, region, lifecycle_status").Where("id IN ?", ids).Find(&nodes).Error; err != nil {
			return err
		}
		refs := map[uint]metering.SeriesNode{}
		for _, node := range nodes {
			refs[node.ID] = metering.SeriesNode{ID: node.ID, Name: strings.TrimSpace(node.Name), Region: strings.TrimSpace(node.Region), Status: node.LifecycleStatus}
		}
		for _, id := range ids {
			node, ok := refs[id]
			if !ok {
				node = metering.SeriesNode{ID: id, Missing: true}
			}
			result.Nodes = append(result.Nodes, node)
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return metering.NodeSeriesSnapshot{}, err
	}
	return result, nil
}
