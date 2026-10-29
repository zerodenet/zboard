package datastore_test

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
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
func statisticsRefresh(t *testing.T, db *gorm.DB, event string, at time.Time) {
	t.Helper()
	if err := db.Transaction(func(tx *gorm.DB) error {
		return meteringstore.RefreshScope(tx, "user", 7, 3, "same-source-core", 1, event, at, "native")
	}); err != nil {
		t.Fatal(err)
	}
}
func statisticsSeedCurrent(t *testing.T, db *gorm.DB, at time.Time) {
	t.Helper()
	current := meteringstore.PrincipalFlowCurrent{NodeID: 3, PrincipalKey: "statistics-concurrent-principal", UserID: 7, SubscriptionID: 9, CoreInstanceID: "same-source-core", SessionRegistryRevision: 1, ActiveFlows: 5, ObservedAt: at, UpdatedAt: at}
	if err := db.Create(&current).Error; err != nil {
		t.Fatal(err)
	}
}
func statisticsFacts(t *testing.T, db *gorm.DB) []model.PrincipalStatisticsHourly {
	t.Helper()
	var out []model.PrincipalStatisticsHourly
	if err := db.Order("scope_type,scope_id,hour_at").Find(&out).Error; err != nil {
		t.Fatal(err)
	}
	return out
}
func TestMySQLPrincipalStatisticsOldRRSnapshotAndRetiredReplayUseCurrentFacts(t *testing.T) {
	db := diagnosticSchemaDB(t, true)
	found := statisticsFoundRowsDB(t, db)
	at := time.Now().UTC().Truncate(time.Millisecond)
	statisticsSeedCurrent(t, db, at)
	old := found.Begin(&sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if old.Error != nil {
		t.Fatal(old.Error)
	}
	defer old.Rollback()
	var before int64
	if err := old.Model(&meteringstore.PrincipalFlowScopeObservation{}).Count(&before).Error; err != nil || before != 0 {
		t.Fatal(before, err)
	}
	statisticsRefresh(t, db, "after-snapshot", at)
	statisticsRefresh(t, db, "pinned-anchor", at.Add(time.Second))
	if n, err := (meteringstore.PrincipalStatistics{DB: db}).Drain(t.Context(), 200); err != nil || n != 2 {
		t.Fatal(n, err)
	}
	frozen := statisticsFacts(t, db)
	var stale int64
	if err := old.Model(&meteringstore.PrincipalFlowScopeObservation{}).Count(&stale).Error; err != nil || stale != 0 {
		t.Fatal("RR snapshot was not retained", stale, err)
	}
	if err := meteringstore.RefreshScope(old, "user", 7, 3, "same-source-core", 1, "after-snapshot", at, "native"); err != nil {
		t.Fatal("legitimate same-event replay used stale snapshot", err)
	}
	if err := old.Commit().Error; err != nil {
		t.Fatal(err)
	}
	if after := statisticsFacts(t, db); !reflect.DeepEqual(frozen, after) {
		t.Fatal("RR duplicate changed hourly count", frozen, after)
	}
	retention := meteringstore.PrincipalScopeRetention{DB: db, Now: func() time.Time { return at.Add(16 * 24 * time.Hour) }}
	if n, err := retention.Retire(t.Context(), 32, 8*1024*1024); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	statisticsRefresh(t, found, "after-snapshot", at)
	if after := statisticsFacts(t, db); !reflect.DeepEqual(frozen, after) {
		t.Fatal("retired replay changed statistics", after)
	}
	var raw, pending int64
	if err := db.Model(&meteringstore.PrincipalFlowScopeObservation{}).Count(&raw).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.PrincipalStatisticsPending{}).Count(&pending).Error; err != nil || raw != 1 || pending != 0 {
		t.Fatal("retired replay rebuilt committed body/intent", raw, pending, err)
	}
	// Corrupt receipt must fail closed even when the driver counts matched rows.
	var receipt model.PrincipalStatisticsSample
	if err := db.Where("event_id=?", "after-snapshot").First(&receipt).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.PrincipalStatisticsSample{}).Where("observation_id=?", receipt.ObservationID).Update("source_digest", "changed").Error; err != nil {
		t.Fatal(err)
	}
	err := found.Transaction(func(tx *gorm.DB) error {
		return meteringstore.RefreshScope(tx, "user", 7, 3, "same-source-core", 1, "after-snapshot", at, "native")
	})
	if err == nil {
		t.Fatal("found-rows accepted conflicting retained proof")
	}
	if after := statisticsFacts(t, db); !reflect.DeepEqual(frozen, after) {
		t.Fatal("rejected proof changed accounting projection")
	}
}
func TestMySQLPrincipalStatisticsProjectorAndDuplicateFollowOneLockOrder(t *testing.T) {
	db := diagnosticSchemaDB(t, true)
	writer := statisticsFoundRowsDB(t, db)
	projector := statisticsFoundRowsDB(t, db)
	at := time.Now().UTC().Truncate(time.Millisecond)
	statisticsSeedCurrent(t, db, at)
	statisticsRefresh(t, db, "concurrent-original", at)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	rawLocked := make(chan struct{})
	sampleRequested := make(chan struct{})
	release := make(chan struct{})
	var rawOnce, sampleOnce, releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	if err := projector.Callback().Query().After("gorm:query").Register("statistics_after_source", func(tx *gorm.DB) {
		if tx.Statement.Table == "principal_flow_scope_observations" && tx.Error == nil {
			rawOnce.Do(func() {
				close(rawLocked)
				select {
				case <-release:
				case <-ctx.Done():
					tx.AddError(ctx.Err())
				}
			})
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer projector.Callback().Query().Remove("statistics_after_source")
	if err := writer.Callback().Query().Before("gorm:query").Register("statistics_before_receipt", func(tx *gorm.DB) {
		if tx.Statement.Table == "principal_statistics_samples" {
			sampleOnce.Do(func() { close(sampleRequested) })
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer writer.Callback().Query().Remove("statistics_before_receipt")
	projected := make(chan error, 1)
	go func() {
		n, err := (meteringstore.PrincipalStatistics{DB: projector}).Drain(ctx, 200)
		if err == nil && n != 1 {
			err = fmt.Errorf("projected %d, expected1", n)
		}
		projected <- err
	}()
	select {
	case <-rawLocked:
	case <-ctx.Done():
		t.Fatal("projector never acquired actual raw lock", ctx.Err())
	}
	replayed := make(chan error, 1)
	go func() {
		replayed <- writer.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			return meteringstore.RefreshScope(tx, "user", 7, 3, "same-source-core", 1, "concurrent-original", at, "native")
		})
	}()
	select {
	case <-sampleRequested:
	case <-ctx.Done():
		unblock()
		t.Fatal("writer could not reach sample under raw->pending order", ctx.Err())
	}
	unblock()
	for name, result := range map[string]<-chan error{"projector": projected, "duplicate": replayed} {
		select {
		case err := <-result:
			if err != nil {
				t.Fatal(name, err)
			}
		case <-ctx.Done():
			t.Fatal(name, "did not finish", ctx.Err())
		}
	}
	facts := statisticsFacts(t, db)
	if len(facts) != 1 || facts[0].SampleCount != 1 || facts[0].Peak != 5 {
		t.Fatal("concurrent duplicate changed exact statistical fact", facts)
	}
	var pending int64
	if err := db.Model(&model.PrincipalStatisticsPending{}).Count(&pending).Error; err != nil || pending != 0 {
		t.Fatal(pending, err)
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
