package meteringstore

import (
	"context"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"gorm.io/gorm"
	"time"
)

type ObservationSeries struct{ DB *gorm.DB }

func (s ObservationSeries) Read(ctx context.Context, actor, subscription uint, spec metering.ObservationRange, now time.Time) (metering.ObservationSeries, error) {
	var out metering.ObservationSeries
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := observationReader(tx, actor, subscription); err != nil {
			return err
		}
		coverage, err := loadCoverage(tx, subscription, int(spec.Duration/time.Second), now)
		if err != nil {
			return err
		}
		since := now.Add(-spec.Duration)
		seconds := int64(spec.BucketDuration / time.Second)
		expression := "FLOOR(TIMESTAMPDIFF(SECOND, '1970-01-01 00:00:00', received_at) / ?)"
		if tx.Dialector.Name() == "sqlite" {
			expression = "CAST(CAST(strftime('%s', received_at) AS INTEGER) / ? AS INTEGER)"
		}
		var rows []metering.ObservationAggregate
		if err := tx.Table("subscription_flow_start_events").Select(expression+" AS bucket_index, COUNT(*) AS connection_starts, COUNT(DISTINCT node_id) AS working_nodes", seconds).Where("subscription_id = ? AND received_at >= ? AND received_at <= ?", subscription, since, now).Group("bucket_index").Order("bucket_index ASC").Scan(&rows).Error; err != nil {
			return err
		}
		var distinct int64
		if err := tx.Model(&FlowStartRecord{}).Where("subscription_id = ? AND received_at >= ? AND received_at <= ?", subscription, since, now).Distinct("node_id").Count(&distinct).Error; err != nil {
			return err
		}
		out, err = metering.ProjectObservationSeries(subscription, spec, now, coverage, rows, distinct)
		return err
	})
	if err != nil {
		return metering.ObservationSeries{}, err
	}
	return out, nil
}
