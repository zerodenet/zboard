package networkstore

import (
	"bytes"
	"context"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/observationstore"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestObservationMaterialRetirementBudgetAndConcurrentPools(t *testing.T) {
	t.Setenv("ZBOARD_TEST_MYSQL_DSN", "")
	assertMaterialBudgetAndConcurrency(t)
}
func TestMySQLObservationMaterialRetirementBudgetAndConcurrentPools(t *testing.T) {
	if os.Getenv("ZBOARD_TEST_MYSQL_DSN") == "" {
		t.Skip("actual MySQL DSN required")
	}
	assertMaterialBudgetAndConcurrency(t)
}
func assertMaterialBudgetAndConcurrency(t *testing.T) {
	a, b := administrationFixture(t)
	node := model.Node{Name: "material-budget", Config: "{}", IsEnabled: true}
	if err := a.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	enqueue := func(node model.Node, id string, seq uint64, large bool) string {
		t.Helper()
		s := observationSource(node, id, "budget-runtime", seq, true)
		if large {
			s.Payload = bytes.Repeat([]byte("x"), 1024*1024)
		}
		id = saveObservation(t, a, s)
		drainObservations(t, a)
		return id
	}
	first := enqueue(node, "large-one", 1, true)
	second := enqueue(node, "large-two", 2, true)
	enqueue(node, "latest-small", 3, false)
	now := time.Now().UTC().Add(8 * 24 * time.Hour).Truncate(time.Millisecond)
	clock := func() time.Time { return now }
	retire := observationstore.MaterialRetention{DB: a, Now: clock}
	for range 2 {
		out, err := retire.Retire(t.Context(), 32, observationstore.MaxApplyBytes)
		if err != nil || out.Retired != 1 || out.Bytes <= 1024*1024 || out.Bytes > uint64(observationstore.MaxApplyBytes) {
			t.Fatal("per-round byte budget lost", out, err)
		}
	}
	for _, id := range []string{first, second} {
		var h model.NodeObservationIntent
		if err := a.First(&h, "id = ?", id).Error; err != nil || h.MaterialRetiredAt == nil {
			t.Fatal("budgeted continuation lost material", id, err)
		}
	}
	enqueue(node, "next-large", 4, true)
	enqueue(node, "next-pin", 5, false)
	other := model.Node{Name: "material-concurrency", Config: "{}", IsEnabled: true}
	if err := a.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	enqueue(other, "other-old", 1, false)
	enqueue(other, "other-pin", 2, false)
	var before []model.NodeObservationState
	if err := a.Order("node_id,asset_tag").Find(&before).Error; err != nil {
		t.Fatal(err)
	}
	type result struct {
		out observationstore.MaterialRetentionResult
		err error
	}
	results := make(chan result, 2)
	gate := make(chan struct{})
	var wg sync.WaitGroup
	for _, db := range []*gorm.DB{a, b} {
		wg.Add(1)
		go func(db *gorm.DB) {
			defer wg.Done()
			<-gate
			out, err := (observationstore.MaterialRetention{DB: db, Now: clock}).Retire(t.Context(), 32, observationstore.MaxDrainBytes)
			results <- result{out, err}
		}(db)
	}
	close(gate)
	wg.Wait()
	close(results)
	retired := 0
	for r := range results {
		// SQLite admits a single writer. A genuine busy rollback may be retried;
		// every other error is a failure, and the committed effects are checked.
		if r.err != nil && !(datastore.IsSQLite(a) && (strings.Contains(r.err.Error(), "database is locked") || strings.Contains(r.err.Error(), "database table is locked"))) {
			t.Fatal("competing maintenance failed", r.err)
		}
		retired += r.out.Retired
	}
	now = now.Add(time.Minute + time.Millisecond)
	retry, err := retire.Retire(context.Background(), 32, observationstore.MaxDrainBytes)
	if err != nil {
		t.Fatal("busy rollback could not recover", err)
	}
	retired += retry.Retired
	if retired != 2 {
		t.Fatal("competing maintenance lost or duplicated retirement", retired)
	}
	var after []model.NodeObservationState
	if err := a.Order("node_id,asset_tag").Find(&after).Error; err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("maintenance contention changed current source state", err)
	}
	var inconsistent int64
	if err := a.Table("node_observation_parts AS p").Joins("JOIN node_observation_intents AS h ON h.id = p.intent_id").Where("h.material_retired_at IS NOT NULL").Count(&inconsistent).Error; err != nil || inconsistent != 0 {
		t.Fatal("partial compact receipt survived contention", inconsistent, err)
	}
}
