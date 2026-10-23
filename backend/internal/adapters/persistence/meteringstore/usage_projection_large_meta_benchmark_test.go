package meteringstore

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Opt in with ZBOARD_BENCH_USAGE_PROJECTION_LARGE_META=1 and run with
// -benchtime=1x -count=5. Each timed operation is one 200-fact SQLite Drain;
// insertion and verification are excluded. This measures projection source
// loading, not accounting acceptance, scheduling, HTTP or host capacity.
func BenchmarkUsageProjectionLargeMeta200(b *testing.B) {
	if os.Getenv("ZBOARD_BENCH_USAGE_PROJECTION_LARGE_META") != "1" {
		b.Skip("set ZBOARD_BENCH_USAGE_PROJECTION_LARGE_META=1 for bounded projection samples")
	}
	if b.N > 10 {
		b.Skip("use -benchtime=1x -count=5 for bounded projection samples")
	}
	db := usageProjectionDB(b, filepath.Join(b.TempDir(), "projection-large-meta.db"))
	trace := &usageProjectionSQLCount{Interface: logger.Discard}
	db = db.Session(&gorm.Session{Logger: trace})
	worker := UsageProjectionWorker{DB: db}
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		b.StopTimer()
		records := largeMetaProjectionRecords(iteration, 200)
		enqueueLargeMetaProjectionRecords(b, db, records)
		trace.active = true
		b.StartTimer()
		count, err := worker.Drain(context.Background(), 200)
		b.StopTimer()
		trace.active = false
		if err != nil || count != 200 {
			b.Fatal(count, err)
		}
	}
	assertLargeMetaProjectionTotals(b, db, b.N*200, 0, b.N*200)
	meta := largeMetaProjectionRecords(0, 1)[0].Meta
	var retained int64
	if err := db.Table("traffic_records").Where("meta = ? AND length(meta) = ?", meta, usageProjectionLargeMetaBytes).Count(&retained).Error; err != nil || retained != int64(b.N*200) {
		b.Fatal("stored source metadata changed", retained, err)
	}
	b.ReportMetric(float64(trace.count)/float64(b.N), "SQL/batch")
	b.ReportMetric(200, "facts/batch")
	b.ReportMetric(float64(200*usageProjectionLargeMetaBytes), "fixture-meta-B/batch")
}
