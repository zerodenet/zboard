package datastore_test

import (
	"testing"

	"github.com/zerodenet/zboard/backend/internal/application"
)

// BenchmarkMySQLSchemaPreparation measures one full production schema boundary
// on a fresh random schema, under the fixture's schema-only account. Run with
// ZBOARD_TEST_MYSQL_DSN set and -run '^$' -bench '^BenchmarkMySQLSchemaPreparation$'
// -benchtime=1x -count=1. Fixture provisioning and cleanup are outside the timer.
func BenchmarkMySQLSchemaPreparation(b *testing.B) {
	b.StopTimer()
	if b.N != 1 {
		b.Fatal("schema preparation requires exactly one fresh-schema operation; use -benchtime=1x")
	}
	db, _ := hourlyMySQLFixture(b)
	pool, err := db.DB()
	if err != nil {
		b.Fatal(err)
	}
	// Do not hold a checked-out connection around PrepareDatabaseSchema: its
	// hourly migration legitimately acquires a dedicated session of its own.
	// One retained pool connection serves both those sessions and raw SQL calls.
	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(1)
	pool.SetConnMaxLifetime(0)
	pool.SetConnMaxIdleTime(0)
	counters := func() (uint64, uint64) {
		var connectionID, alterations uint64
		var variable string
		if err := pool.QueryRow("SELECT CONNECTION_ID()").Scan(&connectionID); err != nil {
			b.Fatal(err)
		}
		if err := pool.QueryRow("SHOW SESSION STATUS LIKE 'Com_alter_table'").Scan(&variable, &alterations); err != nil {
			b.Fatal(err)
		}
		return connectionID, alterations
	}
	beforeID, before := counters()
	b.ResetTimer()
	b.StartTimer()
	err = application.PrepareDatabaseSchema(db)
	b.StopTimer()
	if err != nil {
		b.Fatal(err)
	}
	elapsed := b.Elapsed()
	afterID, after := counters()
	if beforeID != afterID || after < before {
		b.Fatalf("schema preparation changed MySQL session: id %d→%d, ALTER count %d→%d", beforeID, afterID, before, after)
	}
	b.ReportMetric(float64(after-before), "ALTER/op")
	b.ReportMetric(elapsed.Seconds(), "s/prepare")
	b.Logf("one PrepareDatabaseSchema call, same MySQL session %d, Com_alter_table %d→%d", beforeID, before, after)
}
