package handler

import (
	"os"
	"reflect"
	"testing"
	"time"

	mysqlconfig "github.com/go-sql-driver/mysql"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"gorm.io/gorm"
)

// BenchmarkMySQLBusinessPreparation measures the unchanged real seven-step
// initialization against an immutable complete-schema install. Each sample
// owns a fresh random database and verifies its full schema/seeds outside the
// timed preparation. Open/capture/drop and the one-time template bootstrap are
// separately reported; ns/op is a pair, not a production throughput measure.
// Normal go test does not run benchmarks or skip any functional coverage.
func BenchmarkMySQLBusinessPreparation(b *testing.B) {
	b.StopTimer()
	dsn := os.Getenv("ZBOARD_TEST_MYSQL_DSN")
	if dsn == "" {
		b.Skip("set ZBOARD_TEST_MYSQL_DSN for an isolated real MySQL benchmark")
	}
	cfg, err := mysqlconfig.ParseDSN(dsn)
	if err != nil {
		b.Fatal("invalid isolated MySQL configuration")
	}
	cfg.DBName, cfg.ParseTime, cfg.Loc = "", true, time.UTC
	cfg.Timeout, cfg.ReadTimeout, cfg.WriteTimeout = 5*time.Second, 10*time.Second, 10*time.Second
	bootstrapStart := time.Now()
	template, err := buildMySQLBusinessTemplate(cfg)
	if err != nil {
		b.Fatal(err)
	}
	bootstrap := time.Since(bootstrapStart)
	var fullSeconds, businessSeconds, totalSeconds float64
	var redoCapacity uint64
	run := func(mode string, index int) float64 {
		b.StopTimer()
		target, err := openMySQLBusinessDatabase(cfg)
		if err != nil {
			b.Fatal(err)
		}
		defer func() {
			b.StopTimer()
			if err := target.close(); err != nil {
				b.Error("close own benchmark database", err)
			}
		}()
		if err := target.DB.Raw("SELECT @@GLOBAL.innodb_redo_log_capacity").Scan(&redoCapacity).Error; err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		started := time.Now()
		if mode == "full" {
			for _, prepare := range []func(*gorm.DB) error{datastore.RunMigrations, datastore.ReconcileCommerceSchema, datastore.ReconcileSubscriptionAccessSchema, datastore.ReconcileZeroEventSchema, datastore.ReconcileTrafficReadSchema, datastore.ReconcileOperationsSchema, datastore.ReconcileFairUseTelemetrySchema} {
				if err := prepare(target.DB); err != nil {
					b.StopTimer()
					b.Fatal(err)
				}
			}
		} else if err := template.install(target.DB); err != nil {
			b.StopTimer()
			b.Fatal(err)
		}
		elapsed := time.Since(started).Seconds()
		b.StopTimer()
		actual, err := captureMySQLBusinessSnapshot(target.DB)
		if err != nil {
			b.Fatal(err)
		}
		if mode == "full" {
			if !reflect.DeepEqual(businessWithoutSeedClocks(businessComparableSnapshot(actual)), businessWithoutSeedClocks(businessComparableSnapshot(template))) {
				b.Fatal("full real preparation differs from template except generated seed clocks")
			}
		} else if !businessSnapshotsEqual(actual, template) {
			b.Fatal("business install differs in complete schema/seeds/settings", businessSnapshotDifference(actual, template))
		}
		b.Logf("bench_n=%d pair=%d mode=%s preparation_seconds=%.9f full_equivalence=true redo_capacity_bytes=%d", b.N, index, mode, elapsed, redoCapacity)
		return elapsed
	}
	b.ResetTimer()
	b.StopTimer()
	for i := 0; i < b.N; i++ {
		pairStarted := time.Now()
		if i%2 == 0 {
			fullSeconds += run("full", i)
			businessSeconds += run("business", i)
		} else {
			businessSeconds += run("business", i)
			fullSeconds += run("full", i)
		}
		totalSeconds += time.Since(pairStarted).Seconds()
	}
	b.ReportMetric(fullSeconds/float64(b.N), "full_s/op")
	b.ReportMetric(businessSeconds/float64(b.N), "business_s/op")
	b.ReportMetric(totalSeconds/float64(b.N), "pair_wall_s/op")
	b.ReportMetric(bootstrap.Seconds(), "template_bootstrap_s")
	b.ReportMetric(float64(redoCapacity), "redo_capacity_bytes")
}
