package meteringstore

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

const usageProjectionLargeMetaBytes = 32 * 1024

func largeMetaProjectionRecords(iteration, count int) []model.TrafficRecord {
	meta := `{"diagnostic":"` + strings.Repeat("m", usageProjectionLargeMetaBytes-17) + `"}`
	records := make([]model.TrafficRecord, count)
	for i := range records {
		identity := fmt.Sprintf("large-meta-%d-%d", iteration, i)
		records[i] = model.TrafficRecord{
			UserID: 7, SubscriptionID: 11, NodeID: 1, ProtocolEndpointID: uint(i%8 + 1),
			ReportID: identity, Nonce: identity, Meta: meta,
			At:       time.Date(2026, 10, 20, 1+i%2, 0, 0, 0, time.UTC),
			RawBytes: 7, UploadBytes: 3, DownloadBytes: 4, UsedBytes: 11, ProtocolMultiplierMilli: 1500,
		}
	}
	return records
}

func enqueueLargeMetaProjectionRecords(t testing.TB, db *gorm.DB, records []model.TrafficRecord) {
	t.Helper()
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.CreateInBatches(&records, 100).Error; err != nil {
			return err
		}
		return EnqueueUsageProjections(tx, records)
	}); err != nil {
		t.Fatal(err)
	}
}

func assertLargeMetaProjectionTotals(t testing.TB, db *gorm.DB, facts, pending, projected int) {
	t.Helper()
	for _, check := range []struct {
		table string
		count int
	}{{"traffic_records", facts}, {"metering_usage_projection_pending", pending}} {
		var count int64
		if err := db.Table(check.table).Count(&count).Error; err != nil || count != int64(check.count) {
			t.Fatalf("%s count=%d want=%d err=%v", check.table, count, check.count, err)
		}
	}
	for _, table := range []string{"traffic_usage_hourly", "protocol_endpoint_usage_daily"} {
		var totals struct{ Raw, Upload, Download, Used, Records int64 }
		err := db.Table(table).Select("COALESCE(SUM(raw_bytes),0) AS raw, COALESCE(SUM(upload_bytes),0) AS upload, COALESCE(SUM(download_bytes),0) AS download, COALESCE(SUM(used_bytes),0) AS used, COALESCE(SUM(record_count),0) AS records").Scan(&totals).Error
		want := int64(projected)
		if err != nil || totals.Raw != want*7 || totals.Upload != want*3 || totals.Download != want*4 || totals.Used != want*11 || totals.Records != want {
			t.Fatalf("%s settled deltas changed: %+v err=%v", table, totals, err)
		}
	}
}

func TestUsageProjectionReadsOnlyRequiredColumnsAndPreservesMetadata(t *testing.T) {
	db := usageProjectionDB(t, filepath.Join(t.TempDir(), "projection-columns.db"))
	records := largeMetaProjectionRecords(0, 3)
	enqueueLargeMetaProjectionRecords(t, db, records)
	var columns []string
	var sourceReads int
	metaRead := false
	active := true
	if err := db.Callback().Query().After("gorm:query").Register("test:projection_source_columns", func(tx *gorm.DB) {
		if !active || tx.Statement.Table != "traffic_records" {
			return
		}
		sourceReads++
		columns = append([]string(nil), tx.Statement.Selects...)
		if loaded, ok := tx.Statement.Dest.(*[]model.TrafficRecord); ok {
			for _, record := range *loaded {
				metaRead = metaRead || record.Meta != ""
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	count, err := (UsageProjectionWorker{DB: db}).Drain(t.Context(), UsageProjectionBatchLimit)
	active = false
	if err != nil || count != len(records) {
		t.Fatal(count, err)
	}
	wantColumns := []string{"id", "user_id", "subscription_id", "node_id", "protocol_endpoint_id", "record_at", "raw_bytes", "upload_bytes", "download_bytes", "used_bytes", "protocol_multiplier_milli", "is_aggregate", "record_count"}
	if sourceReads != 1 || metaRead || !reflect.DeepEqual(columns, wantColumns) {
		t.Fatalf("projection loaded unrelated source data: reads=%d meta=%t columns=%v", sourceReads, metaRead, columns)
	}
	assertLargeMetaProjectionTotals(t, db, 3, 0, 3)
	var stored []model.TrafficRecord
	if err := db.Order("id").Find(&stored).Error; err != nil || len(stored) != len(records) {
		t.Fatal("source facts disappeared", err)
	}
	for i, original := range records {
		got := stored[i]
		if got.ID != original.ID || got.UserID != original.UserID || got.SubscriptionID != original.SubscriptionID || got.NodeID != original.NodeID || got.ProtocolEndpointID != original.ProtocolEndpointID ||
			got.ReportID != original.ReportID || got.Nonce != original.Nonce || !got.At.Equal(original.At) || got.Meta != original.Meta || len(got.Meta) != usageProjectionLargeMetaBytes ||
			got.RawBytes != original.RawBytes || got.UploadBytes != original.UploadBytes || got.DownloadBytes != original.DownloadBytes || got.UsedBytes != original.UsedBytes || got.ProtocolMultiplierMilli != original.ProtocolMultiplierMilli {
			t.Fatalf("projection mutated source fact %d", original.ID)
		}
	}
}

func TestUsageProjectionNarrowColumnsStillRejectChangedSourceEvidence(t *testing.T) {
	for _, test := range []struct {
		name, column string
		changed      any
		original     any
	}{
		{name: "endpoint_identity", column: "protocol_endpoint_id", changed: uint(99), original: uint(1)},
		{name: "user_scope", column: "user_id", changed: uint(99), original: uint(7)},
		{name: "subscription_scope", column: "subscription_id", changed: uint(99), original: uint(11)},
		{name: "node_scope", column: "node_id", changed: uint(99), original: uint(1)},
		{name: "hour", column: "record_at", changed: time.Date(2026, 10, 20, 2, 0, 0, 0, time.UTC), original: time.Date(2026, 10, 20, 1, 0, 0, 0, time.UTC)},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := usageProjectionDB(t, filepath.Join(t.TempDir(), "projection-scope.db"))
			records := largeMetaProjectionRecords(0, 1)
			enqueueLargeMetaProjectionRecords(t, db, records)
			update := func(value any) {
				if err := db.Model(&model.MeteringUsageProjectionPending{}).Where("traffic_record_id = ?", records[0].ID).Update(test.column, value).Error; err != nil {
					t.Fatal(err)
				}
			}
			update(test.changed)
			worker := UsageProjectionWorker{DB: db}
			if count, err := worker.Drain(context.Background(), 200); err == nil || count != 0 {
				t.Fatalf("changed evidence was acknowledged: count=%d err=%v", count, err)
			}
			assertLargeMetaProjectionTotals(t, db, 1, 1, 0)
			update(test.original)
			if count, err := worker.Drain(context.Background(), 200); err != nil || count != 1 {
				t.Fatal("original evidence could not recover", count, err)
			}
			assertLargeMetaProjectionTotals(t, db, 1, 0, 1)
			var stored model.TrafficRecord
			if err := db.First(&stored, records[0].ID).Error; err != nil || stored.Meta != records[0].Meta || stored.UsedBytes != 11 {
				t.Fatal("recovery changed source fact", err)
			}
		})
	}
}
