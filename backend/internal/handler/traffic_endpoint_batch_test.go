package handler

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestEndpointUsageBatchesKeepPerKeyDeltasAndRollback(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	checkEndpointUsageBatches(t, h.db)
}

func TestMySQLEndpointUsageBatchesKeepPerKeyDeltasAndRollback(t *testing.T) {
	h, _ := newMySQLPublishHandlers(t)
	checkEndpointUsageBatches(t, h.db)
}

func checkEndpointUsageBatches(t *testing.T, db *gorm.DB) {
	t.Helper()
	trace := &trafficQueryLog{Interface: logger.Discard}
	db = db.Session(&gorm.Session{Logger: trace})
	at := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	records := make([]model.TrafficRecord, 205)
	for i := range records {
		records[i] = model.TrafficRecord{ProtocolEndpointID: uint(205 - i), At: at, RawBytes: int64(i + 1), UsedBytes: int64(i + 2)}
	}
	apply := func(abort error) error {
		return db.Transaction(func(tx *gorm.DB) error {
			if err := meteringstore.AddProtocolEndpointUsage(tx, records); err != nil {
				return err
			}
			return abort
		})
	}
	if err := apply(nil); err != nil {
		t.Fatal(err)
	}
	inserts := 0
	for _, query := range trace.queries {
		if strings.HasPrefix(query, "INSERT INTO `protocol_endpoint_usage_daily`") {
			inserts++
		}
	}
	if inserts != 3 {
		t.Fatalf("205 keys need three bounded inserts, got %d", inserts)
	}
	for i := range records {
		records[i].At = at.Add(-time.Hour)
		records[i].RawBytes, records[i].UsedBytes = 7, 9
	}
	if err := apply(nil); err != nil {
		t.Fatal(err)
	}
	abort := errors.New("rollback all three batches")
	if err := apply(abort); !errors.Is(err, abort) {
		t.Fatal(err)
	}
	var rows []model.ProtocolEndpointUsageDaily
	if err := db.Order("protocol_endpoint_id DESC").Find(&rows).Error; err != nil || len(rows) != len(records) {
		t.Fatalf("rows=%d: %v", len(rows), err)
	}
	for i, row := range rows {
		if row.RawBytes != int64(i+1)+7 || row.UsedBytes != int64(i+2)+9 || row.RecordCount != 2 || !row.LastRecordAt.Equal(at) {
			t.Fatalf("key %d lost delta/latest timestamp or committed aborted batch: %+v", row.ProtocolEndpointID, row)
		}
	}
	trace.queries = nil
	if err := db.Transaction(func(tx *gorm.DB) error {
		return meteringstore.AddProtocolEndpointUsage(tx, []model.TrafficRecord{{UsedBytes: 100}})
	}); err != nil || len(trace.queries) != 0 {
		t.Fatalf("unattributed endpoint should not write projection: %v %v", trace.queries, err)
	}
}
