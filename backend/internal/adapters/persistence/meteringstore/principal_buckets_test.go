package meteringstore

import (
	"errors"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"testing"
	"time"
)

func TestPrincipalMinuteIngressDoesNotMultiplyEventHistory(t *testing.T) {
	db := trafficAggregateDB(t)
	at := time.Now().UTC().Truncate(time.Minute).Add(5 * time.Second)
	credential := model.ProtocolCredential{UserID: 7, SubscriptionID: 9, NodeID: 3, ProtocolEndpointID: 11, CredentialID: "bucket", PrincipalKey: "principal", Secret: "opaque", Status: "active", ExpiresAt: at.Add(time.Hour)}
	if err := db.Create(&credential).Error; err != nil {
		t.Fatal(err)
	}
	store := PrincipalCollection{DB: db}
	for i := 1; i <= 1000; i++ {
		e := metering.PrincipalEvent{CoreInstanceID: "core", EventID: fmt.Sprint(i), Sequence: uint64(i), ObservedAt: at}
		p := metering.PrincipalObservation{PrincipalKey: "principal", ActiveFlows: uint64(i % 91), SessionRegistryRevision: uint64(i), ObservedAt: at}
		if err := store.Observe(t.Context(), 3, e, p); err != nil {
			t.Fatal(i, err)
		}
	}
	for _, table := range []string{"principal_flow_observations", "principal_flow_scope_observations", "principal_statistics_pending", "principal_statistics_samples", "traffic_records"} {
		if n := statisticsCount(t, db, table); n != 0 {
			t.Fatal(table, n)
		}
	}
	for _, table := range []string{"principal_statistics_minutes", "principal_statistics_hourly", "principal_flow_scope_currents"} {
		if n := statisticsCount(t, db, table); n != 2 {
			t.Fatal(table, n)
		}
	}
	var minute model.PrincipalStatisticsMinute
	if err := db.Where("scope_type = ? AND scope_id = ?", ScopeUser, 7).First(&minute).Error; err != nil || minute.Peak != 90 || minute.SampleCount != 1000 {
		t.Fatal(minute, err)
	}
	// A stale snapshot cannot inflate peak/counters, even after event diagnostics disappear.
	if err := store.Observe(t.Context(), 3, metering.PrincipalEvent{CoreInstanceID: "core", EventID: "old", ObservedAt: at}, metering.PrincipalObservation{PrincipalKey: "principal", ActiveFlows: 999, SessionRegistryRevision: 1, ObservedAt: at}); err != nil {
		t.Fatal(err)
	}
	got, err := readPrincipalStatistics(db, ScopeUser, 7, []metering.TrendBucket{{Key: at.Format("2006-01-02"), StartUTC: at.Truncate(time.Minute), EndUTC: at.Truncate(time.Minute).Add(time.Minute)}})
	if err != nil || len(got) != 1 || got[0].Peak != 90 || got[0].SampleCount != 1000 || got[0].ResolutionSeconds != 60 {
		t.Fatal(got, err)
	}
	t.Log("1000 snapshots: 2 minute rows, 2 hourly rows, 0 event history rows")
}

func TestPrincipalBucketConversionAtomicAndHourlyUnchanged(t *testing.T) {
	db := trafficAggregateDB(t)
	now := time.Now().UTC()
	at := now.Add(-2 * 24 * time.Hour).Truncate(time.Minute)
	for i := 0; i < 50; i++ {
		statisticsSource(t, db, fmt.Sprint(i), at.Add(time.Duration(i)*time.Second), uint64(i))
	}
	statisticsDrain(t, db, 200)
	var before model.PrincipalStatisticsHourly
	if err := db.First(&before).Error; err != nil {
		t.Fatal(err)
	}
	store := PrincipalBucketMaintenance{DB: db, Now: func() time.Time { return now }}
	failure := errors.New("metric conversion failed")
	db.Callback().Delete().Before("gorm:delete").Register("fail_conversion", func(tx *gorm.DB) {
		if tx.Statement.Table == "principal_statistics_samples" {
			tx.AddError(failure)
		}
	})
	if _, err := store.Drain(t.Context(), 200); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	db.Callback().Delete().Remove("fail_conversion")
	if statisticsCount(t, db, "principal_statistics_samples") != 50 || statisticsCount(t, db, "principal_statistics_minutes") != 0 {
		t.Fatal("conversion acknowledged before commit")
	}
	if n, err := store.Drain(t.Context(), 200); err != nil || n != 99 {
		t.Fatal(n, err)
	} // 50 samples + 49 raw, original allocator anchor retained
	if n, err := store.Drain(t.Context(), 200); err != nil || n != 0 {
		t.Fatal("replay", n, err)
	}
	var minute model.PrincipalStatisticsMinute
	if err := db.First(&minute).Error; err != nil || minute.Peak != 49 || minute.SampleCount != 50 {
		t.Fatal(minute, err)
	}
	var after model.PrincipalStatisticsHourly
	if err := db.First(&after).Error; err != nil || after != before {
		t.Fatal("coarsening changed long-term counts", after, before, err)
	}
	future := now.Add(31 * 24 * time.Hour)
	store.Now = func() time.Time { return future }
	if n, err := store.Drain(t.Context(), 200); err != nil || n != 1 {
		t.Fatal("minute expiry", n, err)
	}
	if statisticsCount(t, db, "principal_statistics_hourly") != 1 {
		t.Fatal("hourly history removed")
	}
}

func TestPrincipalBucketWriteFailureRollsBackCurrentSnapshot(t *testing.T) {
	db := trafficAggregateDB(t)
	at := time.Now().UTC()
	c := model.ProtocolCredential{UserID: 7, SubscriptionID: 9, NodeID: 3, ProtocolEndpointID: 11, CredentialID: "bucket", PrincipalKey: "principal", Secret: "opaque", Status: "active", ExpiresAt: at.Add(time.Hour)}
	if err := db.Create(&c).Error; err != nil {
		t.Fatal(err)
	}
	fail := errors.New("minute persistence failed")
	db.Callback().Create().Before("gorm:create").Register("fail_minute", func(tx *gorm.DB) {
		if tx.Statement.Table == "principal_statistics_minutes" {
			tx.AddError(fail)
		}
	})
	err := (PrincipalCollection{DB: db}).Observe(t.Context(), 3, metering.PrincipalEvent{CoreInstanceID: "core", EventID: "event", ObservedAt: at}, metering.PrincipalObservation{PrincipalKey: "principal", ActiveFlows: 7, SessionRegistryRevision: 1, ObservedAt: at})
	if !errors.Is(err, fail) {
		t.Fatal(err)
	}
	if statisticsCount(t, db, "principal_flow_currents") != 0 || statisticsCount(t, db, "principal_statistics_hourly") != 0 {
		t.Fatal("partial metric/current commit")
	}
}
