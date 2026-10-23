package meteringstore

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type usageProjectionSQLCount struct {
	logger.Interface
	active bool
	count  int
}

func (l *usageProjectionSQLCount) Trace(context.Context, time.Time, func() (string, int64), error) {
	if l.active {
		l.count++
	}
}

// Reproduce with -benchtime=1x -count=5. This measures one hourly/daily recovery
// batch on local SQLite, not event acceptance, billing, HTTP or capacity. Fixture
// insertion is outside the timer; SQL excludes driver BEGIN/COMMIT calls.
func BenchmarkUsageProjectionRecovery200(b *testing.B) {
	if b.N > 10 {
		b.Skip("use -benchtime=1x -count=5 for bounded recovery samples")
	}
	db := usageProjectionDB(b, filepath.Join(b.TempDir(), "projection-recovery.db"))
	trace := &usageProjectionSQLCount{Interface: logger.Discard}
	db = db.Session(&gorm.Session{Logger: trace})
	worker := UsageProjectionWorker{DB: db}
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		b.StopTimer()
		records := make([]model.TrafficRecord, 200)
		for i := range records {
			identity := fmt.Sprintf("sample-%d-%d", iteration, i)
			records[i] = model.TrafficRecord{NodeID: 1, ProtocolEndpointID: uint(i%8 + 1), ReportID: identity, Nonce: identity, At: time.Date(2026, 10, 20, 1, 0, 0, 0, time.UTC), RawBytes: 7, UploadBytes: 3, DownloadBytes: 4, UsedBytes: 11, ProtocolMultiplierMilli: 1500}
		}
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.CreateInBatches(&records, 100).Error; err != nil {
				return err
			}
			return EnqueueUsageProjections(tx, records)
		}); err != nil {
			b.Fatal(err)
		}
		trace.active = true
		b.StartTimer()
		count, err := worker.Drain(context.Background(), 200)
		b.StopTimer()
		trace.active = false
		if err != nil || count != 200 {
			b.Fatal(count, err)
		}
	}
	var usage struct {
		Used    int64
		Records int64
	}
	if err := db.Model(&model.ProtocolEndpointUsageDaily{}).Select("SUM(used_bytes) AS used, SUM(record_count) AS records").Scan(&usage).Error; err != nil || usage.Used != int64(b.N)*2200 || usage.Records != int64(b.N)*200 {
		b.Fatal(usage, err)
	}
	if err := db.Table("traffic_usage_hourly").Select("SUM(used_bytes) AS used, SUM(record_count) AS records").Scan(&usage).Error; err != nil || usage.Used != int64(b.N)*2200 || usage.Records != int64(b.N)*200 {
		b.Fatal(usage, err)
	}
	b.ReportMetric(float64(trace.count)/float64(b.N), "SQL/batch")
	b.ReportMetric(float64(b.N*200)/b.Elapsed().Seconds(), "facts/s")
}
