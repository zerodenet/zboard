package networkstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/observationstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestMySQLObservationBatchPreparesDuplicateHeadersBeforeBudgetLock(t *testing.T) {
	if os.Getenv("ZBOARD_TEST_MYSQL_DSN") == "" {
		t.Skip("actual MySQL DSN required")
	}
	a, b := administrationFixture(t)
	node := model.Node{Name: "budget-lock-order", Config: "{}", IsEnabled: true}
	if e := a.Create(&node).Error; e != nil {
		t.Fatal(e)
	}
	old := observationSource(node, "old-retained-header", "runtime", 1, false)
	oldID := saveObservation(t, a, old)
	// Put a fresh source before the old duplicate in canonical preparation order.
	// The previous one-pass producer would hold budget before reaching duplicate.
	fresh := observationSource(node, "fresh-retained-header", "runtime", 2, false)
	var freshID string
	for i := 0; i < 10000; i++ {
		fresh.Observation.Latest.ID = fmt.Sprintf("fresh-retained-header-%d", i)
		sum := sha256.New()
		if e := network.WriteObservationSource(sum, fresh); e != nil {
			t.Fatal(e)
		}
		freshID = hex.EncodeToString(sum.Sum(nil))
		if freshID < oldID {
			break
		}
	}
	if freshID >= oldID {
		t.Fatal("fixture did not find a stable canonical order")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	oldLocked := a.WithContext(ctx).Begin()
	if oldLocked.Error != nil {
		t.Fatal(oldLocked.Error)
	}
	defer oldLocked.Rollback()
	var locked model.NodeObservationIntent
	if e := oldLocked.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, "id = ?", oldID).Error; e != nil {
		t.Fatal(e)
	}
	enteringOld := make(chan struct{})
	var once sync.Once
	if e := b.Callback().Create().Before("gorm:create").Register("test:duplicate_before_budget", func(tx *gorm.DB) {
		if tx.Statement.Table == "node_observation_intents" && tx.Statement.ReflectValue.Kind().String() == "struct" && tx.Statement.ReflectValue.FieldByName("ID").String() == oldID {
			once.Do(func() { close(enteringOld) })
		}
	}); e != nil {
		t.Fatal(e)
	}
	defer b.Callback().Create().Remove("test:duplicate_before_budget")
	done := make(chan error, 1)
	go func() {
		done <- b.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			_, e := observationstore.EnqueueBatch(tx, []network.ObservationSource{fresh, old})
			return e
		})
	}()
	select {
	case <-enteringOld:
	case <-ctx.Done():
		t.Fatal("producer did not reach duplicate header", ctx.Err())
	}
	// A real old-header owner must still obtain the budget; otherwise this is
	// budget -> old-header versus old-header -> budget ABBA across the two pools.
	var budget model.NodeObservationMaterialBudget
	if e := oldLocked.Clauses(clause.Locking{Strength: "UPDATE"}).First(&budget, "id = ?", 1).Error; e != nil {
		t.Fatal("producer held budget before all old headers", e)
	}
	if e := oldLocked.Commit().Error; e != nil {
		t.Fatal(e)
	}
	select {
	case e := <-done:
		if e != nil {
			t.Fatal("batch did not recover after old-header release", e)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	var actual struct{ Bytes uint64 }
	if e := a.Model(&model.NodeObservationPart{}).Select("COALESCE(SUM(length(data)),0) AS bytes").Scan(&actual).Error; e != nil {
		t.Fatal(e)
	}
	if e := a.First(&budget, "id = ?", 1).Error; e != nil || actual.Bytes != budget.RetainedBytes {
		t.Fatal("duplicate consumed material twice", actual, budget, e)
	}
}
