package meteringstore

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type trafficRevisionDuringRead struct {
	logger.Interface
	change func()
}

func (l *trafficRevisionDuringRead) Trace(_ context.Context, _ time.Time, sql func() (string, int64), _ error) {
	query, _ := sql()
	if l.change != nil && strings.HasPrefix(query, "SELECT revision, instance FROM traffic_ledger_revision") {
		change := l.change
		l.change = nil
		change()
	}
}

func TestTrafficIncrementalRevisionAndRowsShareOneSnapshot(t *testing.T) {
	f := newIncrementalFixture(t)
	f.seedUsage(t)
	if err := datastore.ReconcileTrafficReadSchema(f.db); err != nil {
		t.Fatal(err)
	}
	closeView, err := f.configureReads()
	if err != nil {
		t.Fatal(err)
	}
	defer closeView()
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	window := UsageWindow{From: start, To: start.Add(24 * time.Hour)}
	bucket, _ := ParseUsageBucket("hour")
	bucket = bucket.ForDB(f.db)
	base := applyUsageWindow(f.readDB.Model(&model.TrafficRecord{}), "record_at", window)
	key := UsageSnapshotQueryKey(base, bucket.Name)
	before, used, err := f.cache.Load(base, bucket, key)
	if err != nil || !used {
		t.Fatalf("initial: used=%v err=%v", used, err)
	}
	committed := false
	trace := &trafficRevisionDuringRead{Interface: logger.Discard, change: func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := f.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Model(&model.TrafficRecord{}).Where("id=1").Update("used_bytes", 99).Error; err != nil {
				return err
			}
			return tx.Create(&model.TrafficRecord{UserID: 1, SubscriptionID: 1, NodeID: 1, ReportID: "snapshot-append", Nonce: "snapshot-append", At: start.Add(4 * time.Hour), UsedBytes: 73, RawBytes: 73, ProtocolMultiplierMilli: 1000}).Error
		}); err != nil {
			t.Fatalf("writer blocked by incremental read snapshot: %v", err)
		}
		committed = true
	}}
	during, used, err := f.cache.Load(base.Session(&gorm.Session{Logger: trace}), bucket, key)
	if err != nil || !used || !committed || during.Total != before.Total || during.Aggregates != before.Aggregates {
		t.Fatalf("snapshot mixed commits: before=%+v during=%+v used=%v committed=%v err=%v", before, during, used, committed, err)
	}
	after, used, err := f.cache.Load(base, bucket, key)
	want, fullErr := LoadUsageStatistics(base, bucket, window)
	if err != nil || fullErr != nil || !used || after.Total != want.Total || after.Aggregates != want.Aggregates || after.Aggregates == before.Aggregates {
		t.Fatalf("next snapshot missed committed mutation: after=%+v want=%+v errors=%v %v", after, want, err, fullErr)
	}
}
