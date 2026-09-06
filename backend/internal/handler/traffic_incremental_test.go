package handler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestTrafficIncrementalStatisticsTrackAppendMutationAndRollback(t *testing.T) {
	f := newTrafficReadFixture(t)
	f.seedUsage(t)
	if err := datastore.ReconcileTrafficReadSchema(f.h.db); err != nil {
		t.Fatal(err)
	}
	closeView, err := f.h.ConfigureTrafficReads()
	if err != nil {
		t.Fatal(err)
	}
	defer closeView()
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	window := historyWindow{From: start, To: start.Add(24 * time.Hour)}
	bucket, _ := parseTrafficUsageBucket("hour")
	bucket = bucket.forDB(f.h.db)
	base := applyHistoryWindow(f.h.trafficQueryDB().Model(&model.TrafficRecord{}), "record_at", window)
	key := trafficSnapshotQueryKey(base, bucket.Name)
	cache := f.h.trafficIncrementalStats
	check := func() {
		t.Helper()
		got, used, err := cache.load(base, bucket, key)
		if err != nil || !used {
			t.Fatalf("incremental path unavailable: used=%v error=%v", used, err)
		}
		want, err := loadTrafficUsageStatistics(base, bucket, window)
		if err != nil || got.Total != want.Total || got.Aggregates != want.Aggregates || got.Bucket != want.Bucket || got.AsOf.IsZero() {
			t.Fatalf("incremental=%+v full=%+v error=%v", got, want, err)
		}
	}
	check()
	initialRevision := cache.entries[key].state.version.Revision
	record := model.TrafficRecord{UserID: 1, SubscriptionID: 1, NodeID: 1, ProtocolEndpointID: 1, ReportID: "incremental", Nonce: "incremental", At: start.Add(time.Hour), RawBytes: 24, UsedBytes: 12, ProtocolMultiplierMilli: 1000}
	if err := f.h.db.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	check()
	if cache.entries[key].state.version.Revision != initialRevision {
		t.Fatal("ordinary append rewrote the invalidation revision")
	}
	if err := f.h.db.Model(&model.TrafficRecord{}).Where("id = ?", record.ID).Updates(map[string]any{"used_bytes": 39, "node_id": nil, "user_id": nil, "protocol_multiplier_milli": nil, "subscription_id": nil}).Error; err != nil {
		t.Fatal(err)
	}
	check()
	if cache.entries[key].state.version.Revision == initialRevision {
		t.Fatal("existing-row mutation did not invalidate the prefix")
	}
	beforeRollback := cache.entries[key].state.version
	sentinel := errors.New("rollback")
	if err := f.h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.TrafficRecord{}).Where("id = ?", record.ID).Update("used_bytes", 9999).Error; err != nil {
			return err
		}
		return sentinel
	}); !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	check()
	if cache.entries[key].state.version != beforeRollback {
		t.Fatal("rolled-back mutation changed the committed version")
	}
	if err := f.h.db.Delete(&model.TrafficRecord{}, 1).Error; err != nil {
		t.Fatal(err)
	}
	check()
	beforeInsert := cache.entries[key].state.version.Revision
	record.ID, record.ReportID, record.Nonce = 1, "reinsert", "reinsert"
	if err := f.h.db.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	check()
	if cache.entries[key].state.version.Revision == beforeInsert {
		t.Fatal("insertion below high-water mark did not invalidate the prefix")
	}
	// An independent database handle writes through the same durable triggers.
	var databases []struct{ Name, File string }
	if err := f.h.db.Raw("PRAGMA database_list").Scan(&databases).Error; err != nil {
		t.Fatal(err)
	}
	var path string
	for _, db := range databases {
		if db.Name == "main" {
			path = db.File
		}
	}
	other, err := datastore.OpenWithDriver(datastore.DriverSQLite, path)
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := other.DB()
	defer pool.Close()
	if err := other.Exec("UPDATE traffic_records SET used_bytes = 71 WHERE id = 1").Error; err != nil {
		t.Fatal(err)
	}
	check()
	beforeUpgrade := cache.entries[key].state.version
	if err := datastore.ReconcileTrafficReadSchema(f.h.db); err != nil {
		t.Fatal(err)
	}
	check()
	if cache.entries[key].state.version.Revision != beforeUpgrade.Revision || cache.entries[key].state.version.Instance != beforeUpgrade.Instance {
		t.Fatal("schema reconciliation reset committed invalidation state")
	}
	if err := f.h.db.Exec("PRAGMA recursive_triggers = OFF").Error; err != nil {
		t.Fatal(err)
	}
	// Explicit REPLACE of the highest ID keeps count and high-water unchanged.
	if err := f.h.db.Exec(`INSERT OR REPLACE INTO traffic_records
(id,user_id,subscription_id,node_id,protocol_endpoint_id,report_id,nonce,record_at,protocol_multiplier_milli,raw_bytes,used_bytes)
SELECT id,user_id,subscription_id,node_id,protocol_endpoint_id,report_id,nonce,record_at,protocol_multiplier_milli,raw_bytes,83
FROM traffic_records ORDER BY id DESC LIMIT 1`).Error; err != nil {
		t.Fatal(err)
	}
	check()
	beforeReplace := cache.entries[key].state.version
	// A unique-key REPLACE with an automatic ID deletes an older row without a
	// DELETE trigger. The global count must reject an append-only refresh.
	if err := f.h.db.Exec(`INSERT OR REPLACE INTO traffic_records
(user_id,subscription_id,node_id,protocol_endpoint_id,report_id,nonce,record_at,protocol_multiplier_milli,raw_bytes,used_bytes)
SELECT user_id,subscription_id,node_id,protocol_endpoint_id,report_id,nonce,record_at,protocol_multiplier_milli,raw_bytes,87
FROM traffic_records WHERE id=1`).Error; err != nil {
		t.Fatal(err)
	}
	check()
	if cache.entries[key].state.version.Revision != beforeReplace.Revision || cache.entries[key].state.version.Rows != beforeReplace.Rows || cache.entries[key].state.version.MaxID.Int64 <= beforeReplace.MaxID.Int64 {
		t.Fatal("fixture did not exercise REPLACE with a hidden deletion")
	}
	// Replacing the metadata row cannot reuse a previous revision's identity.
	oldInstance := cache.entries[key].state.version.Instance
	if err := f.h.db.Exec("DELETE FROM traffic_ledger_revision").Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Exec("UPDATE traffic_records SET used_bytes = 91 WHERE id = (SELECT MAX(id) FROM traffic_records)").Error; err != nil {
		t.Fatal(err)
	}
	if _, used, err := cache.load(base, bucket, key); err != nil || used {
		t.Fatalf("missing revision used=%v error=%v", used, err)
	}
	if err := f.h.db.Exec("INSERT INTO traffic_ledger_revision(id,revision) VALUES(1,0)").Error; err != nil {
		t.Fatal(err)
	}
	check()
	if cache.entries[key].state.version.Instance == oldInstance {
		t.Fatal("metadata row identity was reused")
	}
	if err := f.h.db.Exec("DROP TRIGGER traffic_ledger_revision_update").Error; err != nil {
		t.Fatal(err)
	}
	if _, used, err := cache.load(base, bucket, key); err != nil || used {
		t.Fatalf("missing trigger used=%v error=%v", used, err)
	}
}

func TestTrafficIncrementalScopesCapacityAndCancellation(t *testing.T) {
	f := newTrafficReadFixture(t)
	f.seedUsage(t)
	if err := datastore.ReconcileTrafficReadSchema(f.h.db); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	window := historyWindow{From: start.Add(time.Hour + 5*time.Minute), To: start.Add(time.Hour + 55*time.Minute)}
	cache := &trafficIncrementalCache{}
	for _, name := range []string{"minute", "hour", "day"} {
		bucket, _ := parseTrafficUsageBucket(name)
		bucket = bucket.forDB(f.h.db)
		for _, filter := range []string{"1=1", "user_id=1", "protocol_endpoint_id=1", "subscription_id=999", "node_id=1"} {
			base := applyHistoryWindow(f.h.db.Model(&model.TrafficRecord{}).Where(filter), "record_at", window)
			key := trafficSnapshotQueryKey(base, bucket.Name)
			got, used, err := cache.load(base, bucket, key)
			want, fullErr := loadTrafficUsageStatistics(base, bucket, window)
			if err != nil || fullErr != nil || !used || got.Total != want.Total || got.Aggregates != want.Aggregates {
				t.Fatalf("%s %s: got=%+v want=%+v errors=%v %v used=%v", name, filter, got, want, err, fullErr, used)
			}
			if len(cache.entries) > trafficIncrementalEntries {
				t.Fatal("unbounded filter cache")
			}
		}
	}
	bucket, _ := parseTrafficUsageBucket("hour")
	bucket = bucket.forDB(f.h.db)
	base := applyHistoryWindow(f.h.db.Model(&model.TrafficRecord{}), "record_at", window)
	key := trafficSnapshotQueryKey(base, bucket.Name)
	limited := &trafficIncrementalCache{keyLimit: 1}
	for range 2 {
		if _, used, err := limited.load(base, bucket, key); err != nil || used {
			t.Fatalf("capacity fallback used=%v error=%v", used, err)
		}
		if limited.entries[key].state != nil || limited.entries[key].oversize == nil {
			t.Fatal("oversized partial state retained")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := cache.load(base.WithContext(ctx), bucket, key); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation=%v", err)
	}
	if cache.entries[key].state != nil {
		t.Fatal("failed refresh retained mutable state")
	}
	busy := &trafficIncrementalCache{}
	for i := byte(0); i < trafficIncrementalEntries; i++ {
		if busy.acquire([32]byte{i}) == nil {
			t.Fatal("available slot rejected")
		}
	}
	if busy.acquire([32]byte{9}) != nil || len(busy.entries) != trafficIncrementalEntries {
		t.Fatal("busy capacity was exceeded")
	}
}
