package meteringstore

import (
	"errors"
	"math"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func readPrincipalStatistics(db *gorm.DB, scope string, id uint, buckets []metering.TrendBucket) ([]metering.PrincipalTrendRow, error) {
	if len(buckets) == 0 || len(buckets) > 366 {
		return nil, errors.New("principal statistics requires1..366 exact buckets")
	}
	var state model.PrincipalStatisticsState
	if err := db.First(&state, "id = ?", 1).Error; err != nil {
		return nil, err
	}
	if state.LegacyAfterID != state.LegacyUpperID || state.LegacyProjectedRows != state.LegacyExpectedRows {
		return nil, metering.ErrUsageProjectionPending
	}
	var pending struct{ ObservationID uint64 }
	r := db.Model(&model.PrincipalStatisticsPending{}).Select("observation_id").Where("scope_type = ? AND scope_id = ? AND observed_at >= ? AND observed_at < ?", scope, id, buckets[0].StartUTC, buckets[len(buckets)-1].EndUTC).Limit(1).Find(&pending)
	if r.Error != nil {
		return nil, r.Error
	}
	if r.RowsAffected > 0 {
		return nil, metering.ErrUsageProjectionPending
	}
	// Detect an unregistered newer source from an unsupported old writer/fixture
	// without rescanning the raw history. New code always commits its pending row.
	var heads struct {
		SourceHead  uint64
		PendingHead uint64
	}
	if err := db.Raw("SELECT COALESCE((SELECT MAX(id) FROM principal_flow_scope_observations),0) AS source_head,COALESCE((SELECT MAX(observation_id) FROM principal_statistics_pending),0) AS pending_head").Scan(&heads).Error; err != nil {
		return nil, err
	}
	known := state.ProjectedHead
	if heads.PendingHead > known {
		known = heads.PendingHead
	}
	if heads.SourceHead > known {
		return nil, metering.ErrUsageProjectionPending
	}
	var parts []string
	var args []interface{}
	for _, b := range buckets {
		source := principalStatisticsBucket(db, scope, id, b.StartUTC, b.EndUTC)
		query := db.Table("(?) AS principal_bucket", source).Select("? AS day,COALESCE(MAX(active_flows),0) AS peak,COALESCE(SUM(sample_count),0) AS sample_count", b.Key).Having("SUM(sample_count) > 0")
		parts = append(parts, "?")
		args = append(args, query)
	}
	var out []metering.PrincipalTrendRow
	err := db.Table("("+strings.Join(parts, " UNION ALL ")+") AS principal_statistics", args...).Select("day,peak,sample_count").Order("day").Scan(&out).Error
	if err != nil {
		return nil, err
	}
	for _, row := range out {
		if row.Peak < 0 || row.SampleCount < 0 || uint64(row.Peak) > math.MaxInt64 || uint64(row.SampleCount) > math.MaxInt64 {
			return nil, metering.ErrUsageProjectionPending
		}
	}
	return out, nil
}

// Decompose EACH caller bucket. A single decomposition of the whole366-day
// range would incorrectly merge the partial hour at every local-day boundary.
// Peak=max(hour peaks,partial exact samples), count=sum(hour counts,partials).
func principalStatisticsBucket(db *gorm.DB, scope string, id uint, from, to time.Time) *gorm.DB {
	points := func(a, b time.Time) *gorm.DB {
		return db.Model(&model.PrincipalStatisticsSample{}).Select("observed_at,active_flows,1 AS sample_count").Where("scope_type = ? AND scope_id = ? AND observed_at >= ? AND observed_at < ?", scope, id, a, b)
	}
	start := from.UTC().Truncate(time.Hour)
	if start.Before(from) {
		start = start.Add(time.Hour)
	}
	end := to.UTC().Truncate(time.Hour)
	if !start.Before(end) {
		return points(from, to)
	}
	args := []interface{}{db.Model(&model.PrincipalStatisticsHourly{}).Select("hour_at AS observed_at,peak AS active_flows,sample_count").Where("scope_type = ? AND scope_id = ? AND hour_at >= ? AND hour_at < ?", scope, id, start, end)}
	parts := []string{"?"}
	if from.Before(start) {
		parts = append(parts, "?")
		args = append(args, points(from, start))
	}
	if end.Before(to) {
		parts = append(parts, "?")
		args = append(args, points(end, to))
	}
	return db.Table("("+strings.Join(parts, " UNION ALL ")+") AS principal_time_source", args...)
}
