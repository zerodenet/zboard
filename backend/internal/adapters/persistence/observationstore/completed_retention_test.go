package observationstore

import (
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestCompletedReceiptGCProtectsPinnedAndInvalidEvidence(t *testing.T) {
	a, _, node := mailboxFixture(t)
	if e := a.Model(&model.NodeObservationMaterialBudget{}).Where("id = 1").Update("retained_bytes", MaterialCapacityBytes).Error; e != nil {
		t.Fatal(e)
	}
	ids := []string{}
	for _, name := range []string{"unreferenced", "pinned", "damaged"} {
		s := retirementSource(node, name, true)
		if e := a.Transaction(func(tx *gorm.DB) error { id, e := Enqueue(tx, s); ids = append(ids, id); return e }); e != nil {
			t.Fatal(e)
		}
	}
	state := model.NodeObservationState{NodeID: node.ID, AssetTag: node.AssetTag, LatestReference: ids[1], CurrentSource: "unknown", UpdatedAt: clockNow()}
	if e := a.Create(&state).Error; e != nil {
		t.Fatal(e)
	}
	if e := a.Model(&model.NodeObservationRetirement{}).Where("intent_id = ?", ids[2]).Update("header_sha256", "invalid").Error; e != nil {
		t.Fatal(e)
	}
	r := MaterialRetention{DB: a, Now: func() time.Time { return clockNow().Add(25 * time.Hour) }}
	if e := r.pruneCompleted(t.Context()); e != nil {
		t.Fatal(e)
	}
	for i, id := range ids {
		var count int64
		a.Model(&model.NodeObservationIntent{}).Where("id = ?", id).Count(&count)
		want := int64(1)
		if i == 0 {
			want = 0
		}
		if count != want {
			t.Fatalf("header %d count %d", i, count)
		}
	}
	var budget model.NodeObservationMaterialBudget
	a.First(&budget, 1)
	if budget.RetainedBytes != MaterialCapacityBytes {
		t.Fatal("GC changed material authority")
	}
}

func TestCompletedReceiptGCIsAtomicOnHeaderDeleteFailure(t *testing.T) {
	a, _, node := mailboxFixture(t)
	a.Model(&model.NodeObservationMaterialBudget{}).Where("id = 1").Update("retained_bytes", MaterialCapacityBytes)
	var id string
	if e := a.Transaction(func(tx *gorm.DB) error {
		var e error
		id, e = Enqueue(tx, retirementSource(node, "atomic", true))
		return e
	}); e != nil {
		t.Fatal(e)
	}
	if e := a.Exec("CREATE TRIGGER reject_header_gc BEFORE DELETE ON node_observation_intents BEGIN SELECT RAISE(ABORT,'fixture header delete failed'); END").Error; e != nil {
		t.Fatal(e)
	}
	r := MaterialRetention{DB: a, Now: func() time.Time { return clockNow().Add(25 * time.Hour) }}
	if e := r.pruneCompleted(t.Context()); e == nil {
		t.Fatal("deletion failure ignored")
	}
	for _, table := range []string{"node_observation_intents", "node_observation_retirements"} {
		var count int64
		key := "intent_id"
		if table == "node_observation_intents" {
			key = "id"
		}
		a.Table(table).Where(key+" = ?", id).Count(&count)
		if count != 1 {
			t.Fatal("GC partially committed", table, count)
		}
	}
}
