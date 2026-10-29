package networkstore

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/observationstore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestObservationMaterialRetirementRetainsInconsistentTimes(t *testing.T) {
	t.Setenv("ZBOARD_TEST_MYSQL_DSN", "")
	assertMaterialRetirementTimes(t)
}
func TestMySQLObservationMaterialRetirementRetainsInconsistentTimes(t *testing.T) {
	if os.Getenv("ZBOARD_TEST_MYSQL_DSN") == "" {
		t.Skip("actual MySQL DSN required")
	}
	assertMaterialRetirementTimes(t)
}
func assertMaterialRetirementTimes(t *testing.T) {
	a, _ := administrationFixture(t)
	node := model.Node{Name: "material-time", Config: "{}", IsEnabled: true}
	if err := a.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	source := observationSource(node, "old", "runtime", 1, true)
	id := saveObservation(t, a, source)
	drainObservations(t, a)
	saveObservation(t, a, observationSource(node, "new", "runtime", 2, true))
	drainObservations(t, a)
	now := time.Now().UTC().Add(8 * 24 * time.Hour).Truncate(time.Millisecond)
	// A lifecycle timestamp is not covered by the canonical source hash. It
	// cannot prove seven days of retention when the stored ordering is invalid.
	if err := a.Model(&model.NodeObservationIntent{}).Where("id = ?", id).UpdateColumn("created_at", now).Error; err != nil {
		t.Fatal(err)
	}
	out, err := (observationstore.MaterialRetention{DB: a, Now: func() time.Time { return now }}).Retire(t.Context(), 32, observationstore.MaxDrainBytes)
	if !errors.Is(err, observationstore.ErrMaterial) || out.Retired != 0 {
		t.Fatal("inconsistent lifecycle erased source", out, err)
	}
	var h model.NodeObservationIntent
	if err := a.First(&h, "id = ?", id).Error; err != nil || h.MaterialRetiredAt != nil || !h.MaterialReviewAt.After(now) {
		t.Fatal("invalid time source not retained and deferred", h, err)
	}
	if err := a.Transaction(func(tx *gorm.DB) error { _, e := observationstore.Enqueue(tx, source); return e }); err != nil {
		t.Fatal("maintenance damaged reliable retained replay", err)
	}
}
