package platformstore

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// A fixed cyclic primary key bounds this installation's monitor history to
// 1440 rows, even if expiry is interrupted. No per-report or per-boot rows.
type metricMinute struct {
	Slot        uint16    `gorm:"primaryKey;autoIncrement:false"`
	MinuteAt    time.Time `gorm:"index"`
	SampleCount uint64
	CPUSum      float64
	MemorySum   float64
	DiskSum     float64
	RXSum       float64
	TXSum       float64
	CPUCount    int
	MemoryCount int
	DiskCount   int
	RXCount     int
	TXCount     int
}

func (metricMinute) TableName() string { return "runtime_metric_minutes" }

type SystemMetricHistory struct{ DB *gorm.DB }

func (s SystemMetricHistory) Load(ctx context.Context, now time.Time) (out []platform.SystemMinute, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if e := tx.Where("minute_at < ?", now.UTC().Add(-24*time.Hour)).Delete(&metricMinute{}).Error; e != nil {
			return e
		}
		var rows []metricMinute
		if e := tx.Where("minute_at >= ? AND minute_at <= ?", now.UTC().Add(-24*time.Hour), now.UTC()).Order("minute_at ASC").Limit(1440).Find(&rows).Error; e != nil {
			return e
		}
		out = make([]platform.SystemMinute, 0, len(rows))
		for _, row := range rows {
			out = append(out, platform.SystemMinute{At: row.MinuteAt.UTC(), Sums: [5]float64{row.CPUSum, row.MemorySum, row.DiskSum, row.RXSum, row.TXSum}, Counts: [5]int{row.CPUCount, row.MemoryCount, row.DiskCount, row.RXCount, row.TXCount}})
		}
		return nil
	})
	return
}
func (s SystemMetricHistory) Save(ctx context.Context, minutes []platform.SystemMinute, now time.Time) error {
	if len(minutes) > 1440 {
		return errors.New("monitor history batch exceeds retention capacity")
	}
	rows := make([]metricMinute, 0, len(minutes))
	for _, minute := range minutes {
		at := minute.At.UTC()
		if !at.Equal(at.Truncate(time.Minute)) || at.Unix() < 0 || at.After(now) {
			return errors.New("invalid monitor minute timestamp")
		}
		if at.Before(now.Add(-24 * time.Hour)) {
			continue
		}
		var samples uint64
		for i, sum := range minute.Sums {
			samples += uint64(minute.Counts[i])
			if math.IsNaN(sum) || math.IsInf(sum, 0) || sum < 0 || minute.Counts[i] < 0 {
				return errors.New("invalid monitor aggregate")
			}
		}
		rows = append(rows, metricMinute{Slot: uint16((at.Unix() / 60) % 1440), MinuteAt: at, SampleCount: samples, CPUSum: minute.Sums[0], MemorySum: minute.Sums[1], DiskSum: minute.Sums[2], RXSum: minute.Sums[3], TXSum: minute.Sums[4], CPUCount: minute.Counts[0], MemoryCount: minute.Counts[1], DiskCount: minute.Counts[2], RXCount: minute.Counts[3], TXCount: minute.Counts[4]})
	}
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("minute_at < ?", now.UTC().Add(-24*time.Hour)).Delete(&metricMinute{}).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}

		return tx.Clauses(metricHistoryConflict(tx.Dialector.Name())).CreateInBatches(rows, 100).Error
	})
}

func metricHistoryConflict(dialect string) clause.OnConflict {
	incoming := func(column string) string {
		if dialect == "mysql" {
			return "VALUES(" + column + ")"
		}
		return "excluded." + column
	}
	// Update the timestamp last: MySQL evaluates assignments left-to-right.
	// An old retry must never replace a newer occupant of the same ring slot.
	columns := []string{"cpu_sum", "memory_sum", "disk_sum", "rx_sum", "tx_sum", "cpu_count", "memory_count", "disk_count", "rx_count", "tx_count", "sample_count", "minute_at"}
	assignments := make(clause.Set, 0, len(columns))
	for _, column := range columns {
		assignments = append(assignments, clause.Assignment{Column: clause.Column{Name: column}, Value: gorm.Expr("CASE WHEN minute_at < " + incoming("minute_at") + " OR (minute_at = " + incoming("minute_at") + " AND sample_count <= " + incoming("sample_count") + ") THEN " + incoming(column) + " ELSE " + column + " END")})
	}
	return clause.OnConflict{Columns: []clause.Column{{Name: "slot"}}, DoUpdates: assignments}
}
