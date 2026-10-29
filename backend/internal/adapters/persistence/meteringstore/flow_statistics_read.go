package meteringstore

import (
	"errors"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func flowStatisticsColumn(basis string) (string, error) {
	switch basis {
	case "received":
		return "received_at", nil
	case "occurred":
		return "occurred_at", nil
	}
	return "", errors.New("unsupported flow statistics time basis")
}
func ensureFlowStatisticsReady(tx *gorm.DB, subscription uint, basis string, from, to time.Time) error {
	column, err := flowStatisticsColumn(basis)
	if err != nil {
		return err
	}
	var state model.FlowStartStatisticsState
	if err := tx.First(&state, "id=?", 1).Error; err != nil {
		return err
	}
	if state.LegacyAfterID != state.LegacyUpperID || state.LegacyProjectedRows != state.LegacyExpectedRows {
		return metering.ErrUsageProjectionPending
	}
	var pending struct{ SourceID uint64 }
	q := tx.Model(&model.FlowStartStatisticsPending{}).Select("source_id").Where("subscription_id=? AND "+column+">=? AND "+column+"<=?", subscription, from, to).Limit(1).Find(&pending)
	if q.Error != nil {
		return q.Error
	}
	if q.RowsAffected > 0 {
		return metering.ErrUsageProjectionPending
	}

	// New compatible sources register in the same pending transaction. False
	// legacy/unsupported writers remain visible even after an allocator resets
	// on a fully expired source copy; no MAX-based absence inference is needed.
	var missing struct{ ID uint64 }
	fresh := tx.Model(&FlowStartRecord{}).Select("id").Where("statistics_registered=?", false).Order("id").Limit(1).Find(&missing)
	if fresh.Error != nil {
		return fresh.Error
	}
	if fresh.RowsAffected > 0 {
		return metering.ErrUsageProjectionPending
	}

	return nil
}

// Exact inclusive original window [from,to]: complete UTC minutes plus two
// boundary point slices. The right slice includes an event exactly at to even
// when to itself is a minute boundary; no event is counted twice.
func flowStatisticsSource(tx *gorm.DB, subscription uint, basis string, from, to time.Time) (*gorm.DB, error) {
	column, err := flowStatisticsColumn(basis)
	if err != nil {
		return nil, err
	}
	if from.After(to) {
		return nil, errors.New("invalid flow statistics window")
	}
	points := func(a, b time.Time, inclusive bool) *gorm.DB {
		query := tx.Model(&model.FlowStartStatisticsPoint{}).Select(column+" AS event_at,node_id,1 AS event_count").Where("subscription_id=? AND "+column+">=?", subscription, a)
		if inclusive {
			return query.Where(column+"<=?", b)
		}
		return query.Where(column+"<?", b)
	}
	start := from.UTC().Truncate(time.Minute)
	if start.Before(from) {
		start = start.Add(time.Minute)
	}
	end := to.UTC().Truncate(time.Minute)
	if !start.Before(end) {
		return points(from, to, true), nil
	}
	args := []interface{}{tx.Model(&model.FlowStartStatisticsMinute{}).Select("minute_at AS event_at,node_id,event_count").Where("subscription_id=? AND basis=? AND minute_at>=? AND minute_at<? AND event_count>0", subscription, basis, start, end)}
	parts := []string{"?"}
	if from.Before(start) {
		parts = append(parts, "?")
		args = append(args, points(from, start, false))
	}
	parts = append(parts, "?")
	args = append(args, points(end, to, true))
	return tx.Table("("+strings.Join(parts, " UNION ALL ")+") AS exact_flow_statistics", args...), nil
}
func flowStatisticsWindow(tx *gorm.DB, subscription uint, basis string, from, to time.Time) (count, nodes int64, err error) {
	if err = ensureFlowStatisticsReady(tx, subscription, basis, from, to); err != nil {
		return
	}
	source, e := flowStatisticsSource(tx, subscription, basis, from, to)
	if e != nil {
		err = e
		return
	}
	var totals struct{ Count, Nodes int64 }
	err = tx.Table("(?) AS flow_window", source).Select("COALESCE(SUM(event_count),0) AS count,COUNT(DISTINCT node_id) AS nodes").Scan(&totals).Error
	return totals.Count, totals.Nodes, err
}
func flowStatisticsSeries(tx *gorm.DB, subscription uint, spec metering.ObservationRange, now time.Time) ([]metering.ObservationAggregate, int64, error) {
	if spec.BucketDuration < time.Minute || spec.BucketDuration%time.Minute != 0 {
		return nil, 0, errors.New("flow statistics bucket must be whole minutes")
	}
	from := now.Add(-spec.Duration)
	if err := ensureFlowStatisticsReady(tx, subscription, "received", from, now); err != nil {
		return nil, 0, err
	}
	source, err := flowStatisticsSource(tx, subscription, "received", from, now)
	if err != nil {
		return nil, 0, err
	}
	seconds := int64(spec.BucketDuration / time.Second)
	expression := "FLOOR(TIMESTAMPDIFF(SECOND,'1970-01-01 00:00:00',event_at)/?)"
	if tx.Dialector.Name() == "sqlite" {
		expression = "CAST(CAST(strftime('%s',event_at) AS INTEGER)/? AS INTEGER)"
	}
	var rows []metering.ObservationAggregate
	if err := tx.Table("(?) AS flow_series", source).Select(expression+" AS bucket_index,COALESCE(SUM(event_count),0) AS connection_starts,COUNT(DISTINCT node_id) AS working_nodes", seconds).Group("bucket_index").Order("bucket_index").Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
	// Whole-window distinct is computed over the same union, never a sum of
	// bucket distinct counts. Database work scales with populated node/minutes.
	var distinct int64
	if err := tx.Table("(?) AS flow_nodes", source).Distinct("node_id").Count(&distinct).Error; err != nil {
		return nil, 0, err
	}
	return rows, distinct, nil
}
