package datastore_test

import (
	"context"
	"errors"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"testing"
	"time"
)

const flowStatisticsMigration = "0071_flow_start_statistics.up.sql"

var flowStatisticsTables = []string{"flow_start_statistics_pending", "flow_start_statistics_points", "flow_start_statistics_minutes", "flow_start_statistics_state"}

func TestSQLiteFlowStatisticsLegacyCopyAndPreparedAuthority(t *testing.T) {
	flowStatisticsLegacy(t, false)
}
func TestMySQLFlowStatisticsLegacyCopyAndPreparedAuthority(t *testing.T) {
	flowStatisticsLegacy(t, true)
}
func flowStatisticsLegacy(t *testing.T, mysql bool) {
	db := diagnosticSchemaDB(t, mysql)
	at := time.Date(2026, 10, 1, 1, 15, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		row := meteringstore.FlowStartRecord{NodeID: 3, SubscriptionID: 7, EventID: fmt.Sprint("legacy-flow-", i), CoreInstanceID: "same-original", OccurredAt: at.Add(time.Duration(i) * time.Minute), ReceivedAt: at, CreatedAt: at}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version=?", flowStatisticsMigration).Error; err != nil {
		t.Fatal(err)
	}
	for _, table := range flowStatisticsTables {
		if err := db.Migrator().DropTable(table); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Migrator().DropIndex("subscription_flow_start_events", "idx_flow_start_statistics_registered"); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropColumn("subscription_flow_start_events", "statistics_registered"); err != nil {
		t.Fatal(err)
	}
	target := diagnosticSchemaDB(t, mysql)
	principalRuntimeCopy(t, db, target)
	var copied model.FlowStartStatisticsState
	if err := target.First(&copied, 1).Error; err != nil || copied.LegacyExpectedRows != 3 || copied.LegacyProjectedRows != 0 || copied.LegacyUpperID == 0 {
		t.Fatal("legacy import lost original coverage", copied, err)
	}
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	worker := meteringstore.FlowStatistics{DB: db}
	for i := 0; i < 3; i++ {
		if n, err := worker.Drain(t.Context(), 1); err != nil || n != 1 {
			t.Fatal(n, err)
		}
		if err := datastore.RunMigrations(db); err != nil {
			t.Fatal("repeat preparation reset checkpoint", err)
		}
	}
	if ready, err := worker.HasPending(t.Context()); err != nil || ready {
		t.Fatal(ready, err)
	}
	principalRuntimeCopy(t, db, target)
	if err := datastore.ValidatePreparedProjectionSchema(t.Context(), target); err != nil {
		t.Fatal(err)
	}
	// Recorded missing singleton must never be silently seeded as an empty table.
	if err := target.Where("id=?", 1).Delete(&model.FlowStartStatisticsState{}).Error; err != nil {
		t.Fatal(err)
	}
	before := preparedRetirementSnapshot(t, target)
	if err := datastore.ValidatePreparedProjectionSchema(t.Context(), target); !errors.Is(err, datastore.ErrProjectionNotPrepared) {
		t.Fatal("missing recorded checkpoint admitted", err)
	}
	if err := datastore.RunMigrations(target); err == nil {
		t.Fatal("recorded lost checkpoint recreated")
	}
	if before != preparedRetirementSnapshot(t, target) {
		t.Fatal("failed recovery mutated authority")
	}
}
func TestSQLiteFlowStatisticsPreparedDependenciesRemainReadOnly(t *testing.T) {
	flowStatisticsReadOnly(t, false)
}
func TestMySQLFlowStatisticsPreparedDependenciesRemainReadOnly(t *testing.T) {
	flowStatisticsReadOnly(t, true)
}
func flowStatisticsReadOnly(t *testing.T, mysql bool) {
	db := diagnosticSchemaDB(t, mysql)
	check := func(want error) {
		t.Helper()
		before := preparedRetirementSnapshot(t, db)
		release := guardPreparedRetirementReadOnly(t, db)
		err := datastore.ValidatePreparedProjectionSchema(context.Background(), db)
		release()
		if want == nil && err != nil || want != nil && !errors.Is(err, want) {
			t.Fatal(err, want)
		}
		if before != preparedRetirementSnapshot(t, db) {
			t.Fatal("read admission changed authority")
		}
	}
	check(nil)
	for _, table := range flowStatisticsTables {
		t.Run("missing_"+table, func(t *testing.T) {
			if err := db.Exec("ALTER TABLE " + table + " RENAME TO flow_statistics_saved").Error; err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := db.Exec("ALTER TABLE flow_statistics_saved RENAME TO " + table).Error; err != nil {
					t.Fatal(err)
				}
			}()
			check(datastore.ErrProjectionNotPrepared)
		})
	}
	if err := db.Migrator().DropIndex("subscription_flow_start_events", "idx_flow_start_statistics_registered"); err != nil {
		t.Fatal(err)
	}
	check(datastore.ErrProjectionNotPrepared)
	if err := db.Exec("CREATE INDEX idx_flow_start_statistics_registered ON subscription_flow_start_events(statistics_registered,id)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ALTER TABLE subscription_flow_start_events RENAME COLUMN statistics_registered TO lost_registration").Error; err != nil {
		t.Fatal(err)
	}
	check(datastore.ErrProjectionNotPrepared)
	if err := datastore.RunMigrations(db); err == nil {
		t.Fatal("recorded missing registration repaired")
	}
}
func TestMySQLFlowStatisticsFalseDefaultAndVisibleIndexRequired(t *testing.T) {
	db := diagnosticSchemaDB(t, true)
	for _, bad := range []string{"ALTER TABLE subscription_flow_start_events ALTER COLUMN statistics_registered SET DEFAULT true", "ALTER TABLE subscription_flow_start_events ALTER INDEX idx_flow_start_statistics_registered INVISIBLE"} {
		if err := db.Exec(bad).Error; err != nil {
			t.Fatal(err)
		}
		if err := datastore.ValidatePreparedProjectionSchema(t.Context(), db); !errors.Is(err, datastore.ErrProjectionNotPrepared) {
			t.Fatal("unsafe source registration accepted", bad, err)
		}
		if err := db.Exec("ALTER TABLE subscription_flow_start_events ALTER COLUMN statistics_registered SET DEFAULT false").Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("ALTER TABLE subscription_flow_start_events ALTER INDEX idx_flow_start_statistics_registered VISIBLE").Error; err != nil {
			t.Fatal(err)
		}
	}
}

// Do not hide initializer failure behind a write callback test: this validates
// the owned singleton itself and retains the failed partial DDL for inspection.
func TestSQLiteFlowStatisticsInterruptedPreparationPreservesExistingRegistration(t *testing.T) {
	db := diagnosticSchemaDB(t, false)
	if err := db.Exec("DELETE FROM schema_migrations WHERE version=?", flowStatisticsMigration).Error; err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Millisecond)
	row := meteringstore.FlowStartRecord{NodeID: 3, SubscriptionID: 7, EventID: "retained-registration", OccurredAt: at, ReceivedAt: at, CreatedAt: at}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("subscription_flow_start_events").Where("id=?", row.ID).Update("statistics_registered", true).Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	var registered bool
	if err := db.Table("subscription_flow_start_events").Select("statistics_registered").Where("id=?", row.ID).Scan(&registered).Error; err != nil || !registered {
		t.Fatal("DDL retry reset retained authority", registered, err)
	}
}

func TestMySQLFlowStatisticsCopyRefusesSQLiteSubmillisecondFacts(t *testing.T) {
	source := diagnosticSchemaDB(t, false)
	target := diagnosticSchemaDB(t, true)
	at := time.Date(2026, 10, 1, 1, 15, 0, 123456789, time.UTC)
	row := meteringstore.FlowStartRecord{NodeID: 3, SubscriptionID: 7, EventID: "precision-must-survive", OccurredAt: at, ReceivedAt: at, CreatedAt: at}
	if err := source.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	sourceBefore := preparedRetirementSnapshot(t, source)
	targetBefore := preparedRetirementSnapshot(t, target)
	err := datastore.WithCopyDestination(t.Context(), target, func(tx *gorm.DB) error {
		tables, err := datastore.MigrationTables(tx)
		if err != nil {
			return err
		}
		if err := datastore.ClearCopyDestinationRows(tx, tables); err != nil {
			return err
		}
		return datastore.CopyApplicationData(source, tx)
	})
	if err == nil {
		t.Fatal("precision-losing cross-database copy accepted")
	}
	if sourceBefore != preparedRetirementSnapshot(t, source) || targetBefore != preparedRetirementSnapshot(t, target) {
		t.Fatal("refused copy changed source or committed partial destination")
	}
	var kept meteringstore.FlowStartRecord
	if err := source.First(&kept, row.ID).Error; err != nil || !kept.OccurredAt.Equal(at) || !kept.ReceivedAt.Equal(at) || !kept.CreatedAt.Equal(at) {
		t.Fatal("refused copy rewrote original fact", kept, err)
	}
}

func TestSQLiteFlowStatisticsRemovedReceiptCannotLaunderLostRegisteredAuthority(t *testing.T) {
	db := diagnosticSchemaDB(t, false)
	if err := db.Exec("DELETE FROM schema_migrations WHERE version=?", flowStatisticsMigration).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropTable("flow_start_statistics_points"); err != nil {
		t.Fatal(err)
	}
	before := preparedRetirementSnapshot(t, db)
	if err := datastore.RunMigrations(db); err == nil {
		t.Fatal("surviving registration accepted with lost proof table")
	}
	if before != preparedRetirementSnapshot(t, db) {
		t.Fatal("rejected authority loss modified database")
	}
}
