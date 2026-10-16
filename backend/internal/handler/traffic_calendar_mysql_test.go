package handler

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestMySQLTrafficCalendarProjectionPreservesLocalDayBoundaries(t *testing.T) {
	h, credential := accountingFixtureFromOrder(t, newMySQLOrderFixture(t))
	for _, timezone := range []string{"UTC", "Asia/Shanghai", "Asia/Kathmandu", "Australia/Lord_Howe", "America/New_York"} {
		t.Run(timezone, func(t *testing.T) {
			location, err := time.LoadLocation(timezone)
			if err != nil {
				t.Fatal(err)
			}
			from := time.Date(2026, 10, 31, 0, 0, 0, 0, location)
			q := metering.TrafficTrendQuery{Administrative: true, UserID: credential.UserID, ProtocolEndpointID: credential.ProtocolEndpointID, From: from, Days: 3, Timezone: timezone}
			var records []model.TrafficRecord
			for i := 0; i < q.Days; i++ {
				day := from.AddDate(0, 0, i)
				bucket := metering.TrendBucket{Key: day.Format("2006-01-02"), StartUTC: day.UTC(), EndUTC: day.AddDate(0, 0, 1).UTC()}
				q.Buckets = append(q.Buckets, bucket)
				for j, at := range []time.Time{bucket.StartUTC.Add(-time.Millisecond), bucket.StartUTC, bucket.StartUTC.Add(12 * time.Hour), bucket.EndUTC.Add(-time.Millisecond), bucket.EndUTC} {
					id := fmt.Sprintf("%s-%d-%d", timezone, i, j)
					records = append(records, model.TrafficRecord{UserID: credential.UserID, SubscriptionID: credential.SubscriptionID, NodeID: credential.NodeID, ProtocolEndpointID: credential.ProtocolEndpointID, ReportID: id, Nonce: id, At: at, UploadBytes: 3, DownloadBytes: 7, UsedBytes: 17})
				}
			}
			if err := h.db.Transaction(func(tx *gorm.DB) error {
				if err := tx.Create(&records).Error; err != nil {
					return err
				}
				return meteringstore.RecordUsageProjections(tx, records)
			}); err != nil {
				t.Fatal(err)
			}
			out, err := h.services.TrafficTrends(nil).Read(context.Background(), credential.UserID, q)
			if err != nil || len(out.Snapshot.Points) != q.Days {
				t.Fatalf("calendar: %+v: %v", out, err)
			}
			for i, bucket := range q.Buckets {
				var expected metering.TrafficTrendAggregate
				if err := h.db.Model(&model.TrafficRecord{}).Where("user_id = ? AND protocol_endpoint_id = ? AND record_at >= ? AND record_at < ?", credential.UserID, credential.ProtocolEndpointID, bucket.StartUTC, bucket.EndUTC).
					Select("SUM(used_bytes) AS used_bytes, SUM(upload_bytes) AS upload_bytes, SUM(download_bytes) AS download_bytes, COUNT(*) AS record_count").Scan(&expected).Error; err != nil {
					t.Fatal(err)
				}
				actual := out.Snapshot.Points[i]
				if actual.UsedBytes != expected.UsedBytes || actual.UploadBytes != expected.UploadBytes || actual.DownloadBytes != expected.DownloadBytes || actual.RecordCount != expected.RecordCount {
					t.Fatalf("day %s: %+v != %+v", bucket.Key, actual, expected)
				}
			}
		})
	}
}
