package observationstore

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestObservationRetirementCheckpointRejectsOversizedBytesBeforeCompleteFetch(t *testing.T) {
	for _, variant := range []string{"hash-giant", "summary-giant", "summary-multibyte"} {
		t.Run(variant, func(t *testing.T) {
			a, _, node := ownershipFixture(t)
			id := saveRetirementSource(t, a, retirementSource(node, variant, true))
			column := "hash_state"
			value := any(bytes.Repeat([]byte("x"), 4<<20))
			if variant == "summary-giant" {
				column = "source_summary"
				value = strings.Repeat("s", 4<<20)
			}
			if variant == "summary-multibyte" {
				column = "source_summary"
				value = strings.Repeat("界", 3000)
			}
			if e := a.Model(&model.NodeObservationRetirement{}).Where("intent_id = ?", id).Update(column, value).Error; e != nil {
				t.Fatal(e)
			}
			fullFetches := 0
			if e := a.Callback().Query().Before("gorm:query").Register("test:complete_retirement_fetch", func(tx *gorm.DB) {
				if tx.Statement.Table == "node_observation_retirements" && (len(tx.Statement.Selects) == 0 || !strings.Contains(strings.Join(tx.Statement.Selects, ","), " AS hash_bytes")) {
					fullFetches++
				}
			}); e != nil {
				t.Fatal(e)
			}
			defer a.Callback().Query().Remove("test:complete_retirement_fetch")
			before := retirementBudget(t, a)
			var h model.NodeObservationIntent
			if e := a.First(&h, "id = ?", id).Error; e != nil {
				t.Fatal(e)
			}
			for _, lock := range []bool{false, true} {
				if e := a.Transaction(func(tx *gorm.DB) error { _, e := readRetirement(tx, id, lock); return e }); !errors.Is(e, ErrMaterial) {
					t.Fatal("oversized checkpoint was fetched", variant, lock, e)
				}
			}
			if e := a.Transaction(func(tx *gorm.DB) error { return releaseAppliedMaterialBudget(tx, h, clockNow()) }); !errors.Is(e, ErrMaterial) {
				t.Fatal("applied retirement bypassed bounded checkpoint read", e)
			}
			expiry := clockNow()
			h.Status = "retired"
			h.MaterialRetiredAt = &expiry
			if e := validateExpiredReceipt(a, h); !errors.Is(e, ErrMaterial) {
				t.Fatal("expired replay bypassed bounded checkpoint read", e)
			}
			if fullFetches != 0 {
				t.Fatal("oversized recovery authority reached full model SELECT", fullFetches)
			}
			if b := retirementBudget(t, a); b != before {
				t.Fatal("invalid recovery state changed budget", before, b)
			}
		})
	}
}
func TestObservationRetirementOversizedCheckpointDoesNotStarveHealthyNeighbor(t *testing.T) {
	for _, column := range []string{"hash_state", "source_summary"} {
		t.Run(column, func(t *testing.T) {
			a, _, node := ownershipFixture(t)
			bad := saveRetirementSource(t, a, retirementSource(node, "bad-"+column, true))
			good := saveRetirementSource(t, a, retirementSource(node, "good-"+column, true))
			var badParts []model.NodeObservationPart
			if e := a.Where("intent_id = ?", bad).Order("ordinal").Find(&badParts).Error; e != nil {
				t.Fatal(e)
			}
			var goodHeader model.NodeObservationIntent
			if e := a.First(&goodHeader, "id = ?", good).Error; e != nil {
				t.Fatal(e)
			}
			value := any(bytes.Repeat([]byte("x"), 4<<20))
			if column == "source_summary" {
				value = strings.Repeat("s", 4<<20)
			}
			if e := a.Model(&model.NodeObservationRetirement{}).Where("intent_id = ?", bad).Update(column, value).Error; e != nil {
				t.Fatal(e)
			}
			before := retirementBudget(t, a)
			now := clockNow().Add(MaterialMinimumAge + time.Hour)
			if _, e := (MaterialRetention{DB: a, Now: func() time.Time { return now }}).Retire(t.Context(), 32, MaxDrainBytes); !errors.Is(e, ErrMaterial) {
				t.Fatal("oversized recovery authority was hidden", e)
			}
			finishRetirement(t, a, good, &now)
			var state struct{ State string }
			if e := a.Model(&model.NodeObservationRetirement{}).Select("state").Where("intent_id = ?", bad).Take(&state).Error; e != nil || state.State != "quarantined" {
				t.Fatal(state, e)
			}
			var current []model.NodeObservationPart
			if e := a.Where("intent_id = ?", bad).Order("ordinal").Find(&current).Error; e != nil || !reflect.DeepEqual(current, badParts) {
				t.Fatal("malformed checkpoint deleted original material", e)
			}
			if b := retirementBudget(t, a); b.RetainedBytes != before.RetainedBytes-goodHeader.MaterialBytes {
				t.Fatal("bad recovery state changed retained accounting", before, b)
			}
		})
	}
}
func TestObservationRetirementAppliedOversizedCheckpointRollsBackAndDefersOnlyBadSource(t *testing.T) {
	a, _, node := ownershipFixture(t)
	ids := []string{}
	for i := 0; i < 3; i++ {
		source := retirementSource(node, "applied-neighbor-"+strings.Repeat("x", i+1), false)
		source.Observation.Latest.Sequence = uint64(i + 1)
		ids = append(ids, saveRetirementSource(t, a, source))
		if _, e := (Worker{DB: a}).Drain(t.Context(), 32, MaxDrainBytes); e != nil {
			t.Fatal(e)
		}
	}
	bad, good, pin := ids[0], ids[1], ids[2]
	if e := a.Model(&model.NodeObservationRetirement{}).Where("intent_id = ?", bad).Update("hash_state", bytes.Repeat([]byte("x"), 4<<20)).Error; e != nil {
		t.Fatal(e)
	}
	var before []model.NodeObservationPart
	if e := a.Where("intent_id IN ?", []string{bad, pin}).Order("intent_id,ordinal").Find(&before).Error; e != nil {
		t.Fatal(e)
	}
	now := clockNow().Add(MaterialMinimumAge + time.Hour)
	if _, e := (MaterialRetention{DB: a, Now: func() time.Time { return now }}).Retire(t.Context(), 32, MaxDrainBytes); !errors.Is(e, ErrMaterial) {
		t.Fatal("bad applied checkpoint was hidden", e)
	}
	var healthy model.NodeObservationIntent
	if e := a.First(&healthy, "id = ?", good).Error; e != nil || healthy.MaterialRetiredAt == nil {
		t.Fatal("healthy applied neighbor was starved", healthy, e)
	}
	var after []model.NodeObservationPart
	if e := a.Where("intent_id IN ?", []string{bad, pin}).Order("intent_id,ordinal").Find(&after).Error; e != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("bad checkpoint/current pin material changed", e)
	}
	var actual struct{ Bytes uint64 }
	if e := a.Model(&model.NodeObservationPart{}).Select("SUM(length(CAST(data AS BLOB))) AS bytes").Scan(&actual).Error; e != nil {
		t.Fatal(e)
	}
	if b := retirementBudget(t, a); b.RetainedBytes != actual.Bytes {
		t.Fatal("rollback lost byte-budget consistency", b, actual)
	}
}
func TestObservationRetirementCompleteFetchRepeatsBytePredicatesAcrossUnlockedResize(t *testing.T) {
	a, b, node := ownershipFixture(t)
	id := saveRetirementSource(t, a, retirementSource(node, "checkpoint-resize", true))
	var once sync.Once
	if e := a.Callback().Query().After("gorm:query").Register("test:resize_after_length_probe", func(tx *gorm.DB) {
		if tx.Statement.Table == "node_observation_retirements" && strings.Contains(strings.Join(tx.Statement.Selects, ","), " AS hash_bytes") {
			once.Do(func() {
				if e := b.Model(&model.NodeObservationRetirement{}).Where("intent_id = ?", id).Update("source_summary", strings.Repeat("x", 4<<20)).Error; e != nil {
					t.Error(e)
				}
			})
		}
	}); e != nil {
		t.Fatal(e)
	}
	defer a.Callback().Query().Remove("test:resize_after_length_probe")
	out, e := readRetirement(a, id, false)
	if !errors.Is(e, ErrMaterial) || len(out.SourceSummary) != 0 || len(out.HashState) != 0 {
		t.Fatal("full fetch ignored changed length metadata", len(out.SourceSummary), len(out.HashState), e)
	}
}
