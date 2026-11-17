package networkstore

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"

	mysqlconfig "github.com/go-sql-driver/mysql"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/observationstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func observationFoundRowsFixture(t *testing.T) (*gorm.DB, *gorm.DB, model.Node) {
	t.Helper()
	raw := os.Getenv("ZBOARD_TEST_MYSQL_DSN")
	if raw == "" {
		t.Skip("actual MySQL DSN required")
	}
	config, e := mysqlconfig.ParseDSN(raw)
	if e != nil {
		t.Fatal(e)
	}
	config.ClientFoundRows = true
	t.Setenv("ZBOARD_TEST_MYSQL_DSN", config.FormatDSN())
	a, b := administrationFixture(t)
	node := model.Node{Name: "found-rows-observation", Config: "{}", IsEnabled: true}
	if e := a.Create(&node).Error; e != nil {
		t.Fatal(e)
	}
	return a, b, node
}
func observationBudgetPartsEqual(t *testing.T, db *gorm.DB) model.NodeObservationMaterialBudget {
	t.Helper()
	var b model.NodeObservationMaterialBudget
	if e := db.First(&b, "id = ?", 1).Error; e != nil {
		t.Fatal(e)
	}
	var sum struct{ Bytes uint64 }
	if e := db.Model(&model.NodeObservationPart{}).Select("COALESCE(SUM(OCTET_LENGTH(data)),0) AS bytes").Scan(&sum).Error; e != nil || sum.Bytes != b.RetainedBytes {
		t.Fatal("retained budget differs from actual parts", sum, b, e)
	}
	return b
}
func TestMySQLObservationFoundRowsDuplicateAndConcurrentReplayDoNotReserveAgain(t *testing.T) {
	a, b, node := observationFoundRowsFixture(t)
	source := observationSource(node, "found-rows-repeat", "runtime", 1, false)
	id := saveObservation(t, a, source)
	before := observationBudgetPartsEqual(t, a)
	var receipt model.NodeObservationRetirement
	if e := a.First(&receipt, "intent_id = ?", id).Error; e != nil {
		t.Fatal(e)
	}
	if got := saveObservation(t, b, source); got != id {
		t.Fatal(got, id)
	}
	var wg sync.WaitGroup
	failures := make(chan error, 4)
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			failures <- b.Transaction(func(tx *gorm.DB) error {
				got, e := observationstore.Enqueue(tx, source)
				if e == nil && got != id {
					return errors.New("replay identity changed")
				}
				return e
			})
		}()
	}
	wg.Wait()
	close(failures)
	for e := range failures {
		if e != nil {
			t.Fatal("concurrent exact duplicate failed", e)
		}
	}
	if after := observationBudgetPartsEqual(t, b); after != before {
		t.Fatal("exact replay re-reserved budget", before, after)
	}
	var after model.NodeObservationRetirement
	if e := b.First(&after, "intent_id = ?", id).Error; e != nil || !reflect.DeepEqual(receipt, after) {
		t.Fatal("restart/separate connection changed receipt", e)
	}
}
func beginObservationRR(t *testing.T, db *gorm.DB, ctx context.Context) *gorm.DB {
	t.Helper()
	tx := db.WithContext(ctx).Begin(&sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	t.Cleanup(func() { tx.Rollback() })
	var n int64
	if e := tx.Model(&model.NodeObservationIntent{}).Count(&n).Error; e != nil {
		t.Fatal(e)
	} // establish the old consistent snapshot
	return tx
}
func startObservationDuplicate(t *testing.T, db, tx *gorm.DB, source network.ObservationSource) (<-chan struct{}, <-chan error) {
	t.Helper()
	entered := make(chan struct{})
	done := make(chan error, 1)
	var once sync.Once
	if e := db.Callback().Create().Before("gorm:create").Register("test:observation_duplicate_wait", func(query *gorm.DB) {
		if query.Statement.Table == "node_observation_intents" {
			once.Do(func() { close(entered) })
		}
	}); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Callback().Create().Remove("test:observation_duplicate_wait") })
	go func() {
		_, e := observationstore.Enqueue(tx, source)
		if e == nil {
			e = tx.Commit().Error
		} else {
			tx.Rollback()
		}
		done <- e
	}()
	return entered, done
}
func TestMySQLObservationRRBeforeFirstCommitUsesCurrentPartsAndFoundRowsSafeInsert(t *testing.T) {
	a, b, node := observationFoundRowsFixture(t)
	source := observationSource(node, "new-source-after-rr-snapshot", "runtime", 1, false)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	writer := a.WithContext(ctx).Begin()
	if writer.Error != nil {
		t.Fatal(writer.Error)
	}
	defer writer.Rollback()
	id, e := observationstore.Enqueue(writer, source)
	if e != nil {
		t.Fatal(e)
	}
	reader := beginObservationRR(t, b, ctx)
	entered, done := startObservationDuplicate(t, b, reader, source)
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if e := writer.Commit().Error; e != nil {
		t.Fatal(e)
	}
	select {
	case e := <-done:
		if e != nil {
			t.Fatal("old RR snapshot rejected newly committed exact source", e)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	before := observationBudgetPartsEqual(t, a)
	if got := saveObservation(t, b, source); got != id {
		t.Fatal(got, id)
	}
	if after := observationBudgetPartsEqual(t, b); after != before {
		t.Fatal("fresh transaction replay changed budget", after, before)
	}
}
func TestMySQLObservationRRBeforeRetirementUsesCurrentCompleteReceiptAndEmptyParts(t *testing.T) {
	a, b, node := observationFoundRowsFixture(t)
	source := observationSource(node, "retired-after-rr-snapshot", "runtime", 1, false)
	id := saveObservation(t, a, source)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	reader := beginObservationRR(t, b, ctx)
	retiredEntered, releaseRetirement := make(chan struct{}), make(chan struct{})
	var pause, release sync.Once
	t.Cleanup(func() { release.Do(func() { close(releaseRetirement) }) })
	if e := a.Callback().Update().After("gorm:update").Register("test:hold_complete_retirement_commit", func(tx *gorm.DB) {
		values, ok := tx.Statement.Dest.(map[string]any)
		if tx.Statement.Table == "node_observation_retirements" && ok && values["state"] == "complete" {
			pause.Do(func() {
				close(retiredEntered)
				select {
				case <-releaseRetirement:
				case <-ctx.Done():
				}
			})
		}
	}); e != nil {
		t.Fatal(e)
	}
	defer a.Callback().Update().Remove("test:hold_complete_retirement_commit")
	retirementDone := make(chan error, 1)
	go func() {
		now := time.Now().UTC().Add(observationstore.MaterialMinimumAge + time.Hour)
		retire := observationstore.MaterialRetention{DB: a, Now: func() time.Time { return now }}
		_, e := retire.Retire(ctx, 1, observationstore.MaxDrainBytes)
		if e == nil {
			now = now.Add(2 * time.Second)
			_, e = retire.Retire(ctx, 1, observationstore.MaxDrainBytes)
		}
		retirementDone <- e
	}()
	select {
	case <-retiredEntered:
	case e := <-retirementDone:
		t.Fatal("retirement did not reach held real commit", e)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	entered, done := startObservationDuplicate(t, b, reader, source)
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	release.Do(func() { close(releaseRetirement) })
	select {
	case e := <-retirementDone:
		if e != nil {
			t.Fatal(e)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case e := <-done:
		if e != nil {
			t.Fatal("old RR snapshot rejected current complete receipt", e)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if got := observationBudgetPartsEqual(t, a); got.RetainedBytes != 0 {
		t.Fatal(got)
	}
	var h model.NodeObservationIntent
	if e := a.First(&h, "id = ?", id).Error; e != nil || h.Status != "retired" || h.MaterialRetiredAt == nil {
		t.Fatal(h, e)
	}
	var r model.NodeObservationRetirement
	if e := a.First(&r, "intent_id = ?", id).Error; e != nil || r.State != "complete" || r.RemainingBytes != 0 {
		t.Fatal(r, e)
	}
}
func TestMySQLObservationHeaderNonDuplicateSQLFailureIsNotIgnored(t *testing.T) {
	a, _, node := observationFoundRowsFixture(t)
	if e := a.Exec("CREATE TRIGGER reject_observation_header BEFORE INSERT ON node_observation_intents FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'fixture header rejected'").Error; e != nil {
		t.Fatal(e)
	}
	before := observationBudgetPartsEqual(t, a)
	e := a.Transaction(func(tx *gorm.DB) error {
		_, e := observationstore.Enqueue(tx, observationSource(node, "rejected-header", "runtime", 1, false))
		return e
	})
	var failure *mysqlconfig.MySQLError
	if !errors.As(e, &failure) || failure.Number != 1644 {
		t.Fatal("non-duplicate SQL error was degraded or swallowed", e)
	}
	if after := observationBudgetPartsEqual(t, a); after != before {
		t.Fatal("failed insert changed authority", before, after)
	}
	var n int64
	if e := a.Model(&model.NodeObservationIntent{}).Count(&n).Error; e != nil || n != 0 {
		t.Fatal(n, e)
	}
}
