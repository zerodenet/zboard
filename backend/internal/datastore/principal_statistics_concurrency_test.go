package datastore_test

import (
	"context"
	"database/sql"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func statisticsFoundRowsDB(t *testing.T, source *gorm.DB) *gorm.DB {
	t.Helper()
	dialect, ok := source.Dialector.(*gormmysql.Dialector)
	if !ok {
		t.Fatal("actual MySQL required")
	}
	cfg, err := mysqldriver.ParseDSN(dialect.Config.DSN)
	if err != nil {
		t.Fatal("isolated DSN parse failed")
	}
	cfg.ClientFoundRows = true
	db, err := datastore.Open(cfg.FormatDSN())
	if err != nil {
		t.Fatal("open isolated found-rows connection")
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	return db
}
func statisticsFacts(t *testing.T, db *gorm.DB) []model.PrincipalStatisticsHourly {
	t.Helper()
	var out []model.PrincipalStatisticsHourly
	if err := db.Order("scope_type,scope_id,hour_at").Find(&out).Error; err != nil {
		t.Fatal(err)
	}
	return out
}
func TestMySQLPrincipalMinuteConcurrentSnapshotsAndFoundRowsReplay(t *testing.T) {
	db := diagnosticSchemaDB(t, true)
	found := statisticsFoundRowsDB(t, db)
	at := time.Now().UTC().Truncate(time.Millisecond)
	credential := model.ProtocolCredential{UserID: 7, SubscriptionID: 9, NodeID: 3, ProtocolEndpointID: 11, CredentialID: "bucket", PrincipalKey: "principal", Secret: "opaque", Status: "active", ExpiresAt: at.Add(time.Hour)}
	if err := db.Create(&credential).Error; err != nil {
		t.Fatal(err)
	}
	event := metering.PrincipalEvent{CoreInstanceID: "core", EventID: "snapshot", Sequence: 1, ObservedAt: at}
	if err := (meteringstore.PrincipalCollection{DB: db}).Boundary(t.Context(), 3, event, false); err != nil {
		t.Fatal(err)
	}
	point := metering.PrincipalObservation{PrincipalKey: "principal", ActiveFlows: 5, SessionRegistryRevision: 1, ObservedAt: at}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, conn := range []*gorm.DB{db, found} {
		wg.Add(1)
		go func(conn *gorm.DB) {
			defer wg.Done()
			errs <- (meteringstore.PrincipalCollection{DB: conn}).Observe(t.Context(), 3, event, point)
		}(conn)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	frozen := statisticsFacts(t, db)
	if len(frozen) != 2 || frozen[0].Peak != 5 || frozen[0].SampleCount != 1 {
		t.Fatal(frozen)
	}
	old := found.Begin(&sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	defer old.Rollback()
	var n int64
	if err := old.Table("principal_flow_scope_observations").Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if err := (meteringstore.PrincipalCollection{DB: old}).Observe(t.Context(), 3, event, point); err != nil {
		t.Fatal(err)
	}
	if err := old.Commit().Error; err != nil {
		t.Fatal(err)
	}
	if after := statisticsFacts(t, db); !reflect.DeepEqual(frozen, after) {
		t.Fatal("duplicate snapshot changed minute/hour counters", after, frozen)
	}
	for _, table := range []string{"principal_flow_observations", "principal_flow_scope_observations", "principal_statistics_pending", "principal_statistics_samples"} {
		if err := db.Table(table).Count(&n).Error; err != nil || n != 0 {
			t.Fatal(table, n, err)
		}
	}
}

func TestMySQLPrincipalBucketMaintenanceAndIngressDoNotDoubleCount(t *testing.T) {
	db := diagnosticSchemaDB(t, true)
	writer := statisticsFoundRowsDB(t, db)
	worker := statisticsFoundRowsDB(t, db)
	at := time.Now().UTC().Truncate(time.Millisecond)
	c := model.ProtocolCredential{UserID: 7, SubscriptionID: 9, NodeID: 3, ProtocolEndpointID: 11, CredentialID: "bucket", PrincipalKey: "principal", Secret: "opaque", Status: "active", ExpiresAt: at.Add(time.Hour)}
	if err := db.Create(&c).Error; err != nil {
		t.Fatal(err)
	}
	legacy := meteringstore.PrincipalFlowScopeObservation{ScopeType: "user", ScopeID: 7, NodeID: 3, EventID: "legacy", CoreInstanceID: "old", Source: "native", ActiveFlows: 5, ObservedAt: at, CreatedAt: at}
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&legacy).Error; err != nil {
			return err
		}
		return meteringstore.EnqueuePrincipalStatistics(tx, legacy)
	}); err != nil {
		t.Fatal(err)
	}
	if n, err := (meteringstore.PrincipalStatistics{DB: db}).Drain(t.Context(), 200); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	errs := make(chan error, 2)
	go func() { _, err := (meteringstore.PrincipalBucketMaintenance{DB: worker}).Drain(ctx, 200); errs <- err }()
	go func() {
		errs <- (meteringstore.PrincipalCollection{DB: writer}).Observe(ctx, 3, metering.PrincipalEvent{CoreInstanceID: "core", EventID: "new", Sequence: 1, ObservedAt: at}, metering.PrincipalObservation{PrincipalKey: "principal", ActiveFlows: 9, SessionRegistryRevision: 1, ObservedAt: at})
	}()
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	var minute model.PrincipalStatisticsMinute
	if err := db.Where("scope_type = ? AND scope_id = ?", "user", 7).First(&minute).Error; err != nil || minute.Peak != 9 || minute.SampleCount != 2 {
		t.Fatal(minute, err)
	}
	facts := statisticsFacts(t, db)
	var user model.PrincipalStatisticsHourly
	for _, row := range facts {
		if row.ScopeType == "user" {
			user = row
		}
	}
	if user.Peak != 9 || user.SampleCount != 2 {
		t.Fatal(facts)
	}
}

func TestMySQLPrincipalStatisticsRejectsFinerSQLiteTimeWithoutPartialCopy(t *testing.T) {
	source := diagnosticSchemaDB(t, false)
	target := diagnosticSchemaDB(t, true)
	at := time.Date(2026, 9, 1, 0, 0, 0, 123456789, time.UTC)
	row := meteringstore.PrincipalFlowScopeObservation{ScopeType: "user", ScopeID: 7, NodeID: 3, EventID: "fine-time", CoreInstanceID: "unchanged-source", Source: "native", ActiveFlows: 7, ObservedAt: at, CreatedAt: at}
	if err := source.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		return meteringstore.EnqueuePrincipalStatistics(tx, row)
	}); err != nil {
		t.Fatal(err)
	}
	if n, err := (meteringstore.PrincipalStatistics{DB: source}).Drain(t.Context(), 200); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	var original model.PrincipalStatisticsState
	if err := target.First(&original, 1).Error; err != nil {
		t.Fatal(err)
	}
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
	if err == nil || !strings.Contains(err.Error(), "millisecond precision") {
		t.Fatal("copy did not refuse actual precision loss", err)
	}
	var preserved model.PrincipalStatisticsState
	if err := target.First(&preserved, 1).Error; err != nil || !reflect.DeepEqual(original, preserved) {
		t.Fatal("refused copy changed original target initialization", err)
	}
	for _, table := range []string{"principal_flow_scope_observations", "principal_statistics_samples", "principal_statistics_pending", "principal_statistics_hourly"} {
		var n int64
		if err := target.Table(table).Count(&n).Error; err != nil || n != 0 {
			t.Fatal("refused copy left partial target authority", table, n, err)
		}
	}
	var retained meteringstore.PrincipalFlowScopeObservation
	if err := source.First(&retained, row.ID).Error; err != nil || !retained.ObservedAt.Equal(at) {
		t.Fatal("source precision was altered", retained.ObservedAt, err)
	}
}
