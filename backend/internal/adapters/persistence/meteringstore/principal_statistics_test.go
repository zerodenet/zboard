package meteringstore

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func statisticsSource(t *testing.T, db *gorm.DB, event string, at time.Time, active uint64) PrincipalFlowScopeObservation {
	t.Helper()
	row := PrincipalFlowScopeObservation{ScopeType: ScopeUser, ScopeID: 7, NodeID: 3, CoreInstanceID: "original-core", SessionRegistryRevision: 1, EventID: event, Source: "principal_observation", ActiveFlows: active, ObservedAt: at, CreatedAt: at}
	if err := db.Transaction(func(tx *gorm.DB) error { known := false; return persistPrincipalScopeObservation(tx, &row, &known) }); err != nil {
		t.Fatal(err)
	}
	return row
}
func statisticsDrain(t *testing.T, db *gorm.DB, limit int) int {
	t.Helper()
	n, err := (PrincipalStatistics{DB: db}).Drain(context.Background(), limit)
	if err != nil {
		t.Fatal(n, err)
	}
	return n
}
func statisticsCount(t *testing.T, db *gorm.DB, table string) int64 {
	t.Helper()
	var n int64
	if err := db.Table(table).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}
func TestPrincipalStatisticsSourceAndProjectionRollbackThenResume(t *testing.T) {
	db := trafficAggregateDB(t)
	ctx := context.Background()
	at := time.Date(2026, 9, 1, 1, 15, 0, 0, time.UTC)
	rollback := errors.New("source transaction aborted")
	row := PrincipalFlowScopeObservation{ScopeType: ScopeUser, ScopeID: 7, NodeID: 3, EventID: "aborted", CoreInstanceID: "core", ObservedAt: at, CreatedAt: at, ActiveFlows: 7}
	if err := db.Transaction(func(tx *gorm.DB) error {
		known := false
		if err := persistPrincipalScopeObservation(tx, &row, &known); err != nil {
			return err
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	for _, table := range []string{"principal_flow_scope_observations", "principal_statistics_pending", "principal_statistics_samples"} {
		if n := statisticsCount(t, db, table); n != 0 {
			t.Fatal(table, n)
		}
	}
	for i := 0; i < 3; i++ {
		statisticsSource(t, db, fmt.Sprint(i), at.Add(time.Duration(i)*time.Minute), uint64(i+1))
	}
	buckets := []metering.TrendBucket{{Key: "2026-09-01", StartUTC: at.Truncate(time.Hour), EndUTC: at.Truncate(time.Hour).Add(time.Hour)}}
	if _, err := readPrincipalStatistics(db, ScopeUser, 7, buckets); !errors.Is(err, metering.ErrUsageProjectionPending) {
		t.Fatal("pending was presented as zero", err)
	}
	failure := errors.New("sample persist failed")
	if err := db.Callback().Create().Before("gorm:create").Register("statistics_fail", func(tx *gorm.DB) {
		if tx.Statement.Table == "principal_statistics_samples" {
			tx.AddError(failure)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := (PrincipalStatistics{DB: db}).Drain(ctx, 2); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if err := db.Callback().Create().Remove("statistics_fail"); err != nil {
		t.Fatal(err)
	}
	if statisticsCount(t, db, "principal_statistics_pending") != 3 || statisticsCount(t, db, "principal_statistics_samples") != 0 || statisticsCount(t, db, "principal_statistics_hourly") != 0 {
		t.Fatal("failed transaction acknowledged source")
	}
	if n := statisticsDrain(t, db, 2); n != 2 {
		t.Fatal(n)
	}
	if n := statisticsDrain(t, db, 2); n != 1 {
		t.Fatal(n)
	}
	got, err := readPrincipalStatistics(db, ScopeUser, 7, buckets)
	if err != nil || len(got) != 1 || got[0].Peak != 3 || got[0].SampleCount != 3 {
		t.Fatal(got, err)
	}
	if n := statisticsDrain(t, db, 2); n != 0 {
		t.Fatal("idle replay", n)
	}
}
func TestPrincipalStatisticsCalendarResolutionAndNonZeroMax(t *testing.T) {
	db := trafficAggregateDB(t)
	locations := []string{"Asia/Kathmandu", "America/New_York"}
	for _, name := range locations {
		loc, err := time.LoadLocation(name)
		if err != nil {
			t.Fatal(err)
		}
		dates := [][3]int{{2026, 3, 8}, {2026, 11, 1}}
		if name == "Asia/Kathmandu" {
			dates = [][3]int{{2026, 9, 1}}
		}
		for _, d := range dates {
			start := time.Date(d[0], time.Month(d[1]), d[2], 0, 0, 0, 0, loc)
			for i, point := range []struct {
				at     time.Time
				active uint64
			}{{start.Add(-5 * time.Minute), 99}, {start.Add(5 * time.Minute), 7}, {start.AddDate(0, 0, 1).Add(-5 * time.Minute), 11}, {start.AddDate(0, 0, 1).Add(5 * time.Minute), 88}} {
				statisticsSource(t, db, fmt.Sprintf("%s-%v-%d", name, d, i), point.at, point.active)
			}
		}
	}
	for {
		pending, err := (PrincipalStatistics{DB: db}).HasPending(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if !pending {
			break
		}
		statisticsDrain(t, db, 200)
	}
	for _, name := range locations {
		loc, _ := time.LoadLocation(name)
		dates := [][3]int{{2026, 3, 8}, {2026, 11, 1}}
		if name == "Asia/Kathmandu" {
			dates = [][3]int{{2026, 9, 1}}
		}
		for _, d := range dates {
			start := time.Date(d[0], time.Month(d[1]), d[2], 0, 0, 0, 0, loc)
			buckets := []metering.TrendBucket{{Key: start.Format("2006-01-02"), StartUTC: start.UTC(), EndUTC: start.AddDate(0, 0, 1).UTC()}, {Key: start.AddDate(0, 0, 1).Format("2006-01-02"), StartUTC: start.AddDate(0, 0, 1).UTC(), EndUTC: start.AddDate(0, 0, 2).UTC()}}
			got, err := readPrincipalStatistics(db, ScopeUser, 7, buckets)
			wantPeak, wantSecondCount := int64(11), int64(1)
			if name == "Asia/Kathmandu" {
				wantPeak, wantSecondCount = 99, 2
			} // expired minute edges use adjacent UTC hours
			if err != nil || len(got) != 2 || got[0].Peak != wantPeak || got[0].SampleCount != 2 || got[1].Peak != 88 || got[1].SampleCount != wantSecondCount {
				t.Fatal(name, d, got, err)
			}
		}
	}
	// Sub-minute display boundaries are deliberately quantized to minutes.
	start := time.Date(2026, 12, 1, 0, 0, 17, 123456789, time.UTC)
	statisticsSource(t, db, "nanosecond-out", start.Add(-time.Nanosecond), 99)
	statisticsSource(t, db, "nanosecond-in", start, 5)
	statisticsSource(t, db, "nanosecond-end", start.Add(time.Hour), 88)
	statisticsDrain(t, db, 200)
	got, err := readPrincipalStatistics(db, ScopeUser, 7, []metering.TrendBucket{{Key: "2026-12-01", StartUTC: start, EndUTC: start.Add(time.Hour)}})
	if err != nil || len(got) != 1 || got[0].Peak != 99 || got[0].SampleCount != 2 || got[0].ResolutionSeconds != 60 {
		t.Fatal("fractional boundary", got, err)
	}
}
func TestPrincipalStatisticsVerifiedRetentionReplayAndLateNewSource(t *testing.T) {
	db := trafficAggregateDB(t)
	at := time.Date(2026, 8, 1, 1, 15, 0, 0, time.UTC)
	a := statisticsSource(t, db, "a", at, 7)
	statisticsSource(t, db, "b", at.Add(time.Minute), 11)
	anchor := statisticsSource(t, db, "anchor", at.Add(2*time.Minute), 3)
	statisticsDrain(t, db, 200)
	var before []model.PrincipalStatisticsHourly
	if err := db.Find(&before).Error; err != nil {
		t.Fatal(err)
	}
	store := PrincipalScopeRetention{DB: db, Now: func() time.Time { return at.Add(16 * 24 * time.Hour) }}
	if n, err := store.Retire(t.Context(), 32, 8*1024*1024); err != nil || n != 2 {
		t.Fatal(n, err)
	}
	if statisticsCount(t, db, "principal_flow_scope_observations") != 1 {
		t.Fatal("raw allocator anchor not retained")
	}
	var actual PrincipalFlowScopeObservation
	if err := db.First(&actual, anchor.ID).Error; err != nil {
		t.Fatal(err)
	}
	retry := a
	retry.ID = 0
	retry.CreatedAt = at.Add(16 * 24 * time.Hour)
	known := false
	if err := db.Transaction(func(tx *gorm.DB) error { return persistPrincipalScopeObservation(tx, &retry, &known) }); err != nil || !known {
		t.Fatal("retired replay", known, err)
	}
	if statisticsCount(t, db, "principal_flow_scope_observations") != 1 || statisticsCount(t, db, "principal_statistics_pending") != 0 {
		t.Fatal("replay rebuilt retained body/pending")
	}
	var after []model.PrincipalStatisticsHourly
	if err := db.Find(&after).Error; err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal(before, after, err)
	}
	statisticsSource(t, db, "late-new", at.Add(30*time.Second), 13)
	statisticsDrain(t, db, 200)
	got, err := readPrincipalStatistics(db, ScopeUser, 7, []metering.TrendBucket{{Key: "2026-08-01", StartUTC: at.Truncate(time.Hour), EndUTC: at.Truncate(time.Hour).Add(time.Hour)}})
	if err != nil || len(got) != 1 || got[0].Peak != 13 || got[0].SampleCount != 4 {
		t.Fatal("late distinct fact", got, err)
	}
}
func TestPrincipalStatisticsCorruptReceiptHeldAndOtherMaterialContinues(t *testing.T) {
	db := trafficAggregateDB(t)
	at := time.Date(2026, 8, 1, 1, 0, 0, 0, time.UTC)
	bad := statisticsSource(t, db, "bad", at, 7)
	good := statisticsSource(t, db, "good", at.Add(time.Minute), 11)
	statisticsSource(t, db, "anchor", at.Add(2*time.Minute), 3)
	statisticsDrain(t, db, 200)
	if err := db.Model(&model.PrincipalStatisticsSample{}).Where("observation_id = ?", bad.ID).Update("source_digest", "corrupt").Error; err != nil {
		t.Fatal(err)
	}
	n, err := (PrincipalScopeRetention{DB: db, Now: func() time.Time { return at.Add(16 * 24 * time.Hour) }}).Retire(t.Context(), 32, 8*1024*1024)
	if err == nil || n != 1 {
		t.Fatal("corruption hidden or poisoned later work", n, err)
	}
	if err := db.First(&PrincipalFlowScopeObservation{}, bad.ID).Error; err != nil {
		t.Fatal("bad original deleted", err)
	}
	if err := db.First(&PrincipalFlowScopeObservation{}, good.ID).Error; !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal("valid source not retired", err)
	}
}
