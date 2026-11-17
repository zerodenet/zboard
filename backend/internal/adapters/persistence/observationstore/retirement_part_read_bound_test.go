package observationstore

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestObservationRetirementOversizedPartDigestNeverFullFetchesOrDeletesMaterial(t *testing.T) {
	a, _, node := ownershipFixture(t)
	id := saveRetirementSource(t, a, retirementSource(node, "giant-per-part-digest", true))
	now := clockNow().Add(MaterialMinimumAge + time.Hour)
	if _, e := (MaterialRetention{DB: a, Now: func() time.Time { return now }}).Retire(t.Context(), 1, MaxDrainBytes); e != nil {
		t.Fatal(e)
	}
	if e := a.Model(&model.NodeObservationRetirementPart{}).Where("intent_id = ? AND ordinal = ?", id, 0).Update("sha256", strings.Repeat("x", 4<<20)).Error; e != nil {
		t.Fatal(e)
	}
	full := 0
	if e := a.Callback().Query().Before("gorm:query").Register("test:part_digest_fetch", func(tx *gorm.DB) {
		if tx.Statement.Table == "node_observation_retirement_parts" && len(tx.Statement.Selects) == 0 {
			full++
		}
	}); e != nil {
		t.Fatal(e)
	}
	defer a.Callback().Query().Remove("test:part_digest_fetch")
	before := retirementBudget(t, a)
	now = now.Add(2 * time.Second)
	if _, e := (MaterialRetention{DB: a, Now: func() time.Time { return now }}).Retire(t.Context(), 1, MaxDrainBytes); !errors.Is(e, ErrMaterial) {
		t.Fatal("oversized part digest was accepted", e)
	}
	if full != 0 {
		t.Fatal("oversized per-part SHA reached full model read", full)
	}
	if after := retirementBudget(t, a); after != before {
		t.Fatal("bad digest altered budget", before, after)
	}
	var parts int64
	if e := a.Model(&model.NodeObservationPart{}).Where("intent_id = ?", id).Count(&parts).Error; e != nil || parts != 1 {
		t.Fatal("bad digest deleted retained original parts", parts, e)
	}
}
