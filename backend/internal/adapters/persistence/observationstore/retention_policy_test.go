package observationstore

import (
	"github.com/zerodenet/zboard/backend/internal/capabilities/observability"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"testing"
	"time"
)

func TestObservationRetentionReadsAdministratorPolicyWithoutRestart(t *testing.T) {
	db, _, node := mailboxFixture(t)
	db.Model(&model.NodeObservationMaterialBudget{}).Where("id=1").Update("retained_bytes", MaterialCapacityBytes)
	var id string
	if e := db.Transaction(func(tx *gorm.DB) error {
		var e error
		id, e = Enqueue(tx, retirementSource(node, "policy", true))
		return e
	}); e != nil {
		t.Fatal(e)
	}
	row := model.SystemConfig{ConfigKey: observability.ObservationReceiptDaysKey, Value: "0", ValueType: "int"}
	if e := db.Create(&row).Error; e != nil {
		t.Fatal(e)
	}
	r := MaterialRetention{DB: db, Now: func() time.Time { return clockNow().Add(25 * time.Hour) }}
	if _, e := r.Retire(t.Context(), 32, MaxDrainBytes); e != nil {
		t.Fatal(e)
	}
	var count int64
	db.Model(&model.NodeObservationIntent{}).Where("id=?", id).Count(&count)
	if count != 1 {
		t.Fatal("disabled receipt retention deleted history")
	}
	if e := db.Model(&row).UpdateColumns(map[string]any{"value": "1", "updated_at": r.retentionNow()}).Error; e != nil {
		t.Fatal(e)
	}
	if _, e := r.Retire(t.Context(), 32, MaxDrainBytes); e != nil {
		t.Fatal(e)
	}
	db.Model(&model.NodeObservationIntent{}).Where("id=?", id).Count(&count)
	if count != 0 {
		t.Fatal("new policy was not applied live")
	}
}
func TestShorterMaterialPolicyRevisitsOldFutureReview(t *testing.T) {
	db, _, node := mailboxFixture(t)
	now := clockNow()
	h := model.NodeObservationIntent{ID: "future-review", NodeID: node.ID, AssetTag: node.AssetTag, Status: "held", CreatedAt: now.Add(-48 * time.Hour), UpdatedAt: now.Add(-48 * time.Hour), MaterialReviewAt: now.Add(5 * 24 * time.Hour), LeaseUntil: epoch}
	if e := db.Create(&h).Error; e != nil {
		t.Fatal(e)
	}
	if e := db.Create(&model.SystemConfig{ConfigKey: observability.ObservationMaterialDaysKey, Value: "1", ValueType: "int"}).Error; e != nil {
		t.Fatal(e)
	}
	r, e := (MaterialRetention{DB: db, Now: func() time.Time { return now }}).withPolicy(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	if e := r.pruneCompleted(t.Context()); e != nil {
		t.Fatal(e)
	}
	if e := db.First(&h, "id=?", h.ID).Error; e != nil || h.MaterialReviewAt.After(now) {
		t.Fatal("shortened lifetime left old review time in place", h.MaterialReviewAt, e)
	}
}
