package datastore_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

const principalStatisticsMigration = "0070_principal_statistics.up.sql"

var principalStatisticsTables = []string{"principal_statistics_pending", "principal_statistics_samples", "principal_statistics_hourly", "principal_statistics_state"}

func TestSQLitePrincipalStatisticsLegacyResumeCopyAndPreparedGuards(t *testing.T) {
	principalStatisticsLegacy(t, false)
}
func TestMySQLPrincipalStatisticsLegacyResumeCopyAndPreparedGuards(t *testing.T) {
	principalStatisticsLegacy(t, true)
}
func principalStatisticsLegacy(t *testing.T, mysql bool) {
	db := diagnosticSchemaDB(t, mysql)
	at := time.Date(2026, 9, 1, 1, 15, 0, 0, time.UTC)
	var rows []meteringstore.PrincipalFlowScopeObservation
	for i := 0; i < 3; i++ {
		row := meteringstore.PrincipalFlowScopeObservation{ScopeType: "user", ScopeID: 7, NodeID: 3, EventID: fmt.Sprint("legacy-", i), CoreInstanceID: "unchanged", Source: "legacy", ActiveFlows: uint64(i + 1), ObservedAt: at.Add(time.Duration(i) * time.Minute), CreatedAt: at}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		rows = append(rows, row)
	}
	// This test owns an isolated source and explicitly models a real pre70 schema.
	if err := db.Exec("DELETE FROM schema_migrations WHERE version=?", principalStatisticsMigration).Error; err != nil {
		t.Fatal(err)
	}
	for _, table := range principalStatisticsTables {
		if err := db.Migrator().DropTable(table); err != nil {
			t.Fatal(err)
		}
	}
	target := diagnosticSchemaDB(t, mysql)
	principalRuntimeCopy(t, db, target)
	var captured model.PrincipalStatisticsState
	if err := target.First(&captured, 1).Error; err != nil || captured.LegacyUpperID != uint64(rows[2].ID) || captured.LegacyExpectedRows != 3 || captured.LegacyProjectedRows != 0 {
		t.Fatal("legacy copy lost captured coverage", captured, err)
	}
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	worker := meteringstore.PrincipalStatistics{DB: db}
	if n, err := worker.Drain(context.Background(), 1); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	var checkpoint model.PrincipalStatisticsState
	if err := db.First(&checkpoint, 1).Error; err != nil || checkpoint.LegacyProjectedRows != 1 {
		t.Fatal(checkpoint, err)
	}
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal("repeat migration reset checkpoint", err)
	}
	if n, err := worker.Drain(context.Background(), 1); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if n, err := worker.Drain(context.Background(), 1); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if ready, err := worker.HasPending(context.Background()); err != nil || ready {
		t.Fatal(ready, err)
	}
	var hour model.PrincipalStatisticsHourly
	if err := db.First(&hour).Error; err != nil || hour.SampleCount != 3 || hour.Peak != 3 {
		t.Fatal(hour, err)
	}
	principalRuntimeCopy(t, db, target)
	var copied model.PrincipalStatisticsHourly
	if err := target.First(&copied).Error; err != nil || copied.SampleCount != 3 || copied.Peak != 3 {
		t.Fatal("copy lost exact projected facts", copied, err)
	}
	if err := datastore.ValidatePreparedPrincipalStatisticsSchema(context.Background(), target); err != nil {
		t.Fatal(err)
	}
	if err := target.Migrator().DropTable("principal_statistics_pending"); err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(target); err == nil {
		t.Fatal("recorded missing pending authority recreated")
	}
	if target.Migrator().HasTable("principal_statistics_pending") {
		t.Fatal("failed startup repaired lost authority")
	}
	// Missing receipt cannot launder a lost allocator anchor into a fresh upgrade.
	if err := db.Exec("DELETE FROM schema_migrations WHERE version=?", principalStatisticsMigration).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&meteringstore.PrincipalFlowScopeObservation{}, rows[2].ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(db); err == nil {
		t.Fatal("lost original anchor accepted after receipt removal")
	}
	var receipts int64
	if err := db.Table("schema_migrations").Where("version=?", principalStatisticsMigration).Count(&receipts).Error; err != nil || receipts != 0 {
		t.Fatal("failed recovery wrote receipt", receipts, err)
	}
}
func TestSQLitePrincipalStatisticsSourcePendingCommitTogether(t *testing.T) {
	principalStatisticsPendingAtomic(t, false)
}
func TestMySQLPrincipalStatisticsSourcePendingCommitTogether(t *testing.T) {
	principalStatisticsPendingAtomic(t, true)
}
func principalStatisticsPendingAtomic(t *testing.T, mysql bool) {
	db := diagnosticSchemaDB(t, mysql)
	if err := db.Callback().Create().Before("gorm:create").Register("reject_statistics_pending", func(tx *gorm.DB) {
		if tx.Statement.Table == "principal_statistics_pending" {
			tx.AddError(fmt.Errorf("injected durable wake failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		return meteringstore.RefreshScope(tx, "user", 7, 3, "core", 1, "source-wake", time.Now().UTC(), "native")
	})
	if err == nil {
		t.Fatal("scope committed without durable wake")
	}
	if err := db.Callback().Create().Remove("reject_statistics_pending"); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"principal_flow_scope_observations", "principal_flow_scope_currents", "principal_statistics_pending"} {
		var count int64
		if err := db.Table(table).Count(&count).Error; err != nil || count != 0 {
			t.Fatal(table, count, err)
		}
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return meteringstore.RefreshScope(tx, "user", 7, 3, "core", 1, "source-wake", time.Now().UTC(), "native")
	}); err != nil {
		t.Fatal(err)
	}
	if n, err := (meteringstore.PrincipalStatistics{DB: db}).Drain(context.Background(), 200); err != nil || n != 1 {
		t.Fatal(n, err)
	}
}
func TestMySQLPrincipalStatisticsInvisibleIndexRefusesPreparedWorker(t *testing.T) {
	db := diagnosticSchemaDB(t, true)
	if err := db.Exec("ALTER TABLE principal_statistics_samples ALTER INDEX idx_principal_stats_sample_retention INVISIBLE").Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.ValidatePreparedPrincipalStatisticsSchema(context.Background(), db); err == nil {
		t.Fatal("invisible retention index accepted")
	}
}
