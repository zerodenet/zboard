package meteringstore

import (
	"context"
	"database/sql"
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

		rows, distinct, err := flowStatisticsSeries(tx, subscription, spec, now)
		if err != nil {
			return err
		}
		out, err = metering.ProjectObservationSeries(subscription, spec, now, coverage, rows, distinct)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return metering.ObservationSeries{}, err
	}
	return out, nil
}
