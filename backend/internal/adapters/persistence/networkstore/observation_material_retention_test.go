package networkstore

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/observationstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestObservationMaterialRetirementPreservesPinsAndReplay(t *testing.T) {
	t.Setenv("ZBOARD_TEST_MYSQL_DSN", "")
	assertObservationMaterialRetirement(t)
}
func TestMySQLObservationMaterialRetirementPreservesPinsAndReplay(t *testing.T) {
	if os.Getenv("ZBOARD_TEST_MYSQL_DSN") == "" {
		t.Skip("actual MySQL DSN required")
	}
	assertObservationMaterialRetirement(t)
}
func assertObservationMaterialRetirement(t *testing.T) {
	a, _ := administrationFixture(t)
	seedNodeAdministrationAdmin(t, a)
	node := model.Node{Name: "retirement", Config: "{}", IsEnabled: true}
	if err := a.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	sources := []network.ObservationSource{observationSource(node, "stats-one", "runtime", 1, true), observationSource(node, "flow-two", "runtime", 2, false), observationSource(node, "stats-three", "runtime", 3, true), observationSource(node, "flow-four", "runtime", 4, false)}
	ids := []string{}
	for _, s := range sources {
		ids = append(ids, saveObservation(t, a, s))
		drainObservations(t, a)
	}
	pending := saveObservation(t, a, observationSource(node, "pending-five", "runtime", 5, false))
	heldSource := observationSource(node, "unknown-six", "runtime", 6, false)
	heldSource.Observation.Latest.AssetTag = strings.Repeat("d", 64)
	held := saveObservation(t, a, heldSource)
	// A different asset never borrows the current node; restore pending after
	// one ordinary worker round so only the explicitly held row was processed.
	if err := a.Model(&model.NodeObservationIntent{}).Where("id = ?", pending).Update("available_at", time.Now().UTC().Add(24*time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	drainObservations(t, a)
	claimed := saveObservation(t, a, observationSource(node, "claimed-seven", "runtime", 7, false))
	// A durable in-flight ownership image is excluded even if its lease is old;
	// actual claim/reclaim races are covered by the store ownership contracts.
	if err := a.Model(&model.NodeObservationIntent{}).Where("id = ?", claimed).Updates(map[string]any{"status": "claimed", "token": strings.Repeat("b", 64), "lease_until": time.Now().UTC().Add(9 * 24 * time.Hour)}).Error; err != nil {
		t.Fatal(err)
	}
	var before model.NodeObservationState
	if err := a.First(&before, "node_id = ? AND asset_tag = ?", node.ID, node.AssetTag).Error; err != nil {
		t.Fatal(err)
	}
	var counters model.Node
	if err := a.First(&counters, node.ID).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Add(8 * 24 * time.Hour).Truncate(time.Millisecond)
	retainer := observationstore.MaterialRetention{DB: a, Now: func() time.Time { return now }}
	out, err := retainer.Retire(t.Context(), 32, observationstore.MaxDrainBytes)
	if err != nil || out.Retired != 4 {
		t.Fatal("unreferenced retirement", out, err)
	}
	// Verification fences expired pending/held first; physical deletion is a
	// separately bounded durable step, while a genuinely active claim is retained.
	for rounds := 0; rounds < 4; rounds++ {
		now = now.Add(2 * time.Second)
		more, e := retainer.Retire(t.Context(), 32, observationstore.MaxDrainBytes)
		if e != nil {
			t.Fatal(e)
		}
		out.Bytes += more.Bytes
		out.Retired += more.Retired
		out.Inspected += more.Inspected
		if more.Inspected == 0 {
			break
		}
	}
	var expectedBytes uint64
	for i, id := range ids {
		var h model.NodeObservationIntent
		if err := a.First(&h, "id = ?", id).Error; err != nil {
			t.Fatal(err)
		}
		var count int64
		if err := a.Model(&model.NodeObservationPart{}).Where("intent_id = ?", id).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if i < 2 {
			expectedBytes += h.MaterialBytes
			if h.MaterialRetiredAt == nil || count != 0 || h.Status != "applied" || h.PartCount == 0 {
				t.Fatal("lost immutable compact receipt", h, count)
			}
		} else if h.MaterialRetiredAt != nil || count == 0 || !h.MaterialReviewAt.After(now) {
			t.Fatal("pinned evidence retired or starves later scans", h, count)
		}
	}
	for _, id := range []string{pending, held} {
		var h model.NodeObservationIntent
		if err := a.First(&h, "id = ?", id).Error; err != nil || h.MaterialRetiredAt == nil || h.Status != "retired" {
			t.Fatal("expired auxiliary material lacks explicit gap", h, err)
		}
		expectedBytes += h.MaterialBytes
		var n int64
		a.Model(&model.NodeObservationPart{}).Where("intent_id = ?", id).Count(&n)
		if n != 0 {
			t.Fatal("expired source still holds parts", id, n)
		}
	}
	var active model.NodeObservationIntent
	if err := a.First(&active, "id = ?", claimed).Error; err != nil || active.Status != "claimed" || active.MaterialRetiredAt != nil {
		t.Fatal("active claimed evidence retired", active, err)
	}
	if out.Bytes != expectedBytes {
		t.Fatal("total retired bytes mismatch", out.Bytes, expectedBytes)
	}

	for _, source := range sources[:2] {
		if got := saveObservation(t, a, source); got != ids[0] && got != ids[1] {
			t.Fatal("replay identity changed", got)
		}
	}
	var after model.NodeObservationState
	if err := a.First(&after, "node_id = ? AND asset_tag = ?", node.ID, node.AssetTag).Error; err != nil || !reflect.DeepEqual(after, before) {
		t.Fatal("retirement/replay changed source state", after, before, err)
	}
	var current model.Node
	if err := a.First(&current, node.ID).Error; err != nil || current.ActiveFlows != counters.ActiveFlows || current.BytesUp != counters.BytesUp || current.BytesDown != counters.BytesDown {
		t.Fatal("retirement/replay changed runtime counters", current, err)
	}
	if ready, err := retainer.HasPending(t.Context()); err != nil || ready {
		t.Fatal("pinned prefix repeatedly ready", ready, err)
	}
	if again, err := retainer.Retire(t.Context(), 32, observationstore.MaxDrainBytes); err != nil || again.Retired != 0 || again.Inspected != 0 {
		t.Fatal("repeat retired or rescanned deferred evidence", again, err)
	}
	service := network.ObservationIntentOperations{Repository: ObservationIntentOperations{DB: a}}
	page, err := service.List(t.Context(), 1, network.ObservationIntentQuery{Status: "applied", Limit: 100})
	if err != nil || len(page.Items) != 4 {
		t.Fatal("compact lifecycle metadata", page, err)
	}
	compacted := 0
	for _, item := range page.Items {
		if item.MaterialVerified {
			t.Fatal("metadata endpoint pretended to revalidate material")
		}
		if item.ID == ids[0] || item.ID == ids[1] {
			compacted++
			if item.MaterialRetiredAt == nil || item.RetainedBytes != "0" || item.MaterialBytes == "0" {
				t.Fatal("original size confused with retained bytes", item)
			}
		} else if item.MaterialRetiredAt != nil || item.RetainedBytes != item.MaterialBytes {
			t.Fatal("pinned logical material count lost", item)
		}
	}
	if compacted != 2 {
		t.Fatal("compacted lifecycle metadata missing", compacted)
	}
	// Exact canonical incoming content is required; a changed original header
	// cannot turn missing material into an accepted replay.
	var receipt model.NodeObservationIntent
	if err := a.First(&receipt, "id = ?", ids[0]).Error; err != nil {
		t.Fatal(err)
	}
	if err := a.Model(&receipt).UpdateColumn("material_bytes", receipt.MaterialBytes+1).Error; err != nil {
		t.Fatal(err)
	}
	if err := a.Transaction(func(tx *gorm.DB) error { _, e := observationstore.Enqueue(tx, sources[0]); return e }); err == nil {
		t.Fatal("corrupt compact receipt acknowledged")
	}
}

func TestObservationMaterialRetirementRollbackAndCorruption(t *testing.T) {
	t.Setenv("ZBOARD_TEST_MYSQL_DSN", "")
	assertMaterialRetirementFaults(t)
}
func TestMySQLObservationMaterialRetirementRollbackAndCorruption(t *testing.T) {
	if os.Getenv("ZBOARD_TEST_MYSQL_DSN") == "" {
		t.Skip("actual MySQL DSN required")
	}
	assertMaterialRetirementFaults(t)
}
func assertMaterialRetirementFaults(t *testing.T) {
	a, _ := administrationFixture(t)
	node := model.Node{Name: "retirement-fault", Config: "{}", IsEnabled: true}
	if err := a.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	old := observationSource(node, "old", "runtime", 1, true)
	id := saveObservation(t, a, old)
	drainObservations(t, a)
	saveObservation(t, a, observationSource(node, "new", "runtime", 2, true))
	drainObservations(t, a)
	now := time.Now().UTC().Add(8 * 24 * time.Hour).Truncate(time.Millisecond)
	retainer := observationstore.MaterialRetention{DB: a, Now: func() time.Time { return now }}
	trigger := `CREATE TRIGGER reject_material_marker BEFORE UPDATE ON node_observation_intents WHEN NEW.material_retired_at IS NOT NULL BEGIN SELECT RAISE(ABORT, 'marker unavailable'); END`
	if !datastore.IsSQLite(a) {
		trigger = `CREATE TRIGGER reject_material_marker BEFORE UPDATE ON node_observation_intents FOR EACH ROW BEGIN IF NEW.material_retired_at IS NOT NULL THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'marker unavailable'; END IF; END`
	}
	if err := a.Exec(trigger).Error; err != nil {
		t.Fatal(err)
	}
	if out, err := retainer.Retire(t.Context(), 32, observationstore.MaxDrainBytes); err == nil || out.Retired != 0 {
		t.Fatal("partial retirement committed", out, err)
	}
	var h model.NodeObservationIntent
	if err := a.First(&h, "id = ?", id).Error; err != nil || h.MaterialRetiredAt != nil {
		t.Fatal("marker survived rollback", h, err)
	}
	if err := a.Transaction(func(tx *gorm.DB) error { _, e := observationstore.Enqueue(tx, old); return e }); err != nil {
		t.Fatal("rollback lost exact parts", err)
	}
	if err := a.Exec("DROP TRIGGER reject_material_marker").Error; err != nil {
		t.Fatal(err)
	}
	// Source corruption remains intact and visible. Maintenance must not erase
	// the independent evidence that caused a replay to fail.
	var part model.NodeObservationPart
	if err := a.First(&part, "intent_id = ? AND ordinal = ?", id, 0).Error; err != nil {
		t.Fatal(err)
	}
	part.Data[0] ^= 1
	if err := a.Model(&model.NodeObservationPart{}).Where("intent_id = ? AND ordinal = ?", id, 0).UpdateColumn("data", part.Data).Error; err != nil {
		t.Fatal(err)
	}
	out, err := retainer.Retire(context.Background(), 32, observationstore.MaxDrainBytes)
	if !errors.Is(err, observationstore.ErrMaterial) || out.Retired != 0 {
		t.Fatal("corruption discarded or hidden", out, err)
	}
	if err := a.First(&h, "id = ?", id).Error; err != nil || h.MaterialRetiredAt != nil || !h.MaterialReviewAt.After(now) {
		t.Fatal("corrupt candidate not retained/deferred", h, err)
	}
	var retained model.NodeObservationPart
	if err := a.First(&retained, "intent_id = ? AND ordinal = ?", id, 0).Error; err != nil || retained.Data[0] != part.Data[0] {
		t.Fatal("corrupt evidence removed", err)
	}
}
