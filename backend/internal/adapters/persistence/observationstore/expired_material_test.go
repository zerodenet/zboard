package observationstore

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func retirementSource(node model.Node, id string, unknown bool) network.ObservationSource {
	at := time.Date(2026, 10, 1, 0, 0, 0, 123, time.UTC)
	p := network.EventPosition{AssetTag: node.AssetTag, ID: id, CoreInstanceID: "retirement-fixture-runtime", Sequence: 1, OccurredAt: at, ReceivedAt: at.Add(time.Second), SourceID: "original-sink"}
	if unknown {
		p.AssetTag = ""
	}
	return network.ObservationSource{Observation: network.NodeObservation{NodeID: node.ID, Latest: p}, Kind: "engine.warning", Payload: []byte(`{"reason":"observational-only"}`)}
}
func saveRetirementSource(t *testing.T, db *gorm.DB, source network.ObservationSource) string {
	t.Helper()
	var id string
	if e := db.Transaction(func(tx *gorm.DB) error { var e error; id, e = Enqueue(tx, source); return e }); e != nil {
		t.Fatal(e)
	}
	return id
}
func retirementBudget(t *testing.T, db *gorm.DB) model.NodeObservationMaterialBudget {
	t.Helper()
	var b model.NodeObservationMaterialBudget
	if e := db.First(&b, "id = ?", 1).Error; e != nil {
		t.Fatal(e)
	}
	return b
}
func finishRetirement(t *testing.T, db *gorm.DB, id string, now *time.Time) model.NodeObservationRetirement {
	t.Helper()
	for i := 0; i < 32; i++ {
		*now = now.Add(2 * time.Second)
		if _, e := (MaterialRetention{DB: db, Now: func() time.Time { return *now }}).Retire(t.Context(), 32, MaxDrainBytes); e != nil {
			t.Fatal(e)
		}
		var r model.NodeObservationRetirement
		if e := db.First(&r, "intent_id = ?", id).Error; e != nil {
			t.Fatal(e)
		}
		if r.State == "complete" {
			return r
		}
	}
	t.Fatal("bounded fixture did not finish retirement")
	return model.NodeObservationRetirement{}
}
func TestObservationRetirementExpiresUnknownSourceAndLateReplayKeepsCompactGap(t *testing.T) {
	a, _, node := ownershipFixture(t)
	source := retirementSource(node, "expired-unknown", true)
	id := saveRetirementSource(t, a, source)
	if _, e := (Worker{DB: a}).Drain(t.Context(), 32, MaxDrainBytes); e != nil {
		t.Fatal(e)
	}
	var before model.NodeObservationIntent
	if e := a.First(&before, "id = ?", id).Error; e != nil || before.Status != "held" {
		t.Fatal(before, e)
	}
	originalBudget := retirementBudget(t, a)
	if originalBudget.RetainedBytes != before.MaterialBytes {
		t.Fatal(originalBudget, before)
	}
	now := clockNow().Add(MaterialMinimumAge + time.Hour)
	r := finishRetirement(t, a, id, &now)
	var after model.NodeObservationIntent
	a.First(&after, "id = ?", id)
	if after.Status != "retired" || after.Reason != before.Reason || !after.UpdatedAt.Equal(before.UpdatedAt) || after.MaterialRetiredAt == nil || !r.ContentVerified || r.RemainingBytes != 0 {
		t.Fatal(after, r)
	}
	if !strings.Contains(r.SourceSummary, source.Observation.Latest.ReceivedAt.UTC().Format(time.RFC3339Nano)) {
		t.Fatal("lost original receiver time", r.SourceSummary)
	}
	if got := retirementBudget(t, a); got.RetainedBytes != 0 {
		t.Fatal(got)
	}
	if got := saveRetirementSource(t, a, source); got != id {
		t.Fatal("replay changed identity")
	}
	var count int64
	a.Model(&model.NodeObservationPart{}).Count(&count)
	if count != 0 {
		t.Fatal("late replay recreated expired parts", count)
	}
	if _, e := readSource(a, after); !errors.Is(e, ErrMaterialExpired) {
		t.Fatal("full read did not explain expiration", e)
	}
	if n, e := (Worker{DB: a}).Drain(t.Context(), 32, MaxDrainBytes); e != nil || n != 0 {
		t.Fatal("expired identity reprojected", n, e)
	}
	var actual model.Node
	a.First(&actual, node.ID)
	if actual.ActiveFlows != 0 || actual.BytesUp != 0 || actual.BytesDown != 0 {
		t.Fatal("unknown became numeric observation", actual)
	}
}
func TestObservationRetirementCapacityAndOversizeCreateGapWithoutFinancialAdmissionError(t *testing.T) {
	a, _, node := ownershipFixture(t)
	// Isolated authoritative-budget fixture: capacity admission must consult the
	// row, not infer free capacity from a history scan or an empty read model.
	if e := a.Model(&model.NodeObservationMaterialBudget{}).Where("id = ?", 1).Update("retained_bytes", MaterialCapacityBytes).Error; e != nil {
		t.Fatal(e)
	}
	for _, size := range []int{0, int(MaxApplyBytes) + 1} {
		source := retirementSource(node, "capacity-"+strings.Repeat("x", size%7), false)
		source.Payload = bytes.Repeat([]byte("q"), size)
		id := saveRetirementSource(t, a, source)
		var h model.NodeObservationIntent
		var r model.NodeObservationRetirement
		a.First(&h, "id = ?", id)
		a.First(&r, "intent_id = ?", id)
		if h.Status != "retired" || h.MaterialRetiredAt == nil || r.State != "complete" || r.RemainingBytes != 0 || r.ContentVerified {
			t.Fatal("omitted payload looked applied/verified", h, r)
		}
		expected := "capacity_exceeded"
		if size > int(MaxApplyBytes) {
			expected = "source_size_limit"
		}
		if r.Cause != expected {
			t.Fatal(r)
		}
		if e := validateExpiredReceipt(a, h); e != nil {
			t.Fatal(e)
		}
		if got := saveRetirementSource(t, a, source); got != id {
			t.Fatal(got, id)
		}
	}
	var n int64
	a.Model(&model.NodeObservationPart{}).Count(&n)
	if n != 0 {
		t.Fatal("over-capacity payload reached DB pages", n)
	}
	if b := retirementBudget(t, a); b.RetainedBytes != MaterialCapacityBytes {
		t.Fatal("omission changed existing retained authority", b)
	}
}
func TestObservationRetirementActiveLeaseAndPinsSurviveButExpiredClaimIsFenced(t *testing.T) {
	a, _, node := ownershipFixture(t)
	source := retirementSource(node, "old-claim", false)
	id := saveRetirementSource(t, a, source)
	w := Worker{DB: a}
	claim, found, e := w.claim(t.Context())
	if e != nil || !found {
		t.Fatal(found, e)
	}
	now := clockNow().Add(MaterialMinimumAge + time.Hour)
	if e := a.Model(&model.NodeObservationIntent{}).Where("id = ?", id).Update("lease_until", now.Add(time.Minute)).Error; e != nil {
		t.Fatal(e)
	}
	if _, e := (MaterialRetention{DB: a, Now: func() time.Time { return now }}).Retire(t.Context(), 32, MaxDrainBytes); e != nil {
		t.Fatal(e)
	}
	var h model.NodeObservationIntent
	a.First(&h, "id = ?", id)
	if h.Status != "claimed" || h.MaterialRetiredAt != nil {
		t.Fatal("active owner was stolen", h)
	}
	// The expired owner image is fenced by both status/token and the existing
	// material_retired_at predicate; it cannot commit node counters or state.
	now = now.Add(2 * time.Minute)
	finishRetirement(t, a, id, &now)
	budget := MaxDrainBytes
	if e := w.apply(t.Context(), claim, &budget); !errors.Is(e, ErrOwnership) {
		t.Fatal("old claim did not fail ownership", e)
	}
	var n int64
	a.Model(&model.NodeObservationState{}).Count(&n)
	if n != 0 {
		t.Fatal("expired claim wrote observation state", n)
	}
	// A foreign/current/floor pin is retained even if the referring asset is bad.
	pinned := saveRetirementSource(t, a, retirementSource(node, "pinned-unknown", true))
	if e := a.Create(&model.NodeObservationState{NodeID: node.ID, AssetTag: node.AssetTag, LatestReference: pinned, CurrentSource: "unknown", UpdatedAt: clockNow()}).Error; e != nil {
		t.Fatal(e)
	}
	now = now.Add(MaterialMinimumAge + time.Hour)
	if _, e := (MaterialRetention{DB: a, Now: func() time.Time { return now }}).Retire(t.Context(), 32, MaxDrainBytes); e != nil {
		t.Fatal(e)
	}
	h = model.NodeObservationIntent{}
	a.First(&h, "id = ?", pinned)
	if h.MaterialRetiredAt != nil || h.Status == "retired" {
		t.Fatal("retention cleared a state pin", h)
	}
}

// Seed exactly the previous durable stream layout to exercise migration-era
// >2MiB sources. The new admission intentionally never persists these bodies.
func seedOldLargeSource(t *testing.T, db *gorm.DB, source network.ObservationSource) string {
	t.Helper()
	hash := sha256.New()
	counter := materialCounter{Writer: hash}
	if e := network.WriteObservationSource(&counter, source); e != nil {
		t.Fatal(e)
	}
	id := hex.EncodeToString(hash.Sum(nil))
	now := clockNow()
	h := model.NodeObservationIntent{ID: id, NodeID: source.Observation.NodeID, AssetTag: source.Observation.Latest.AssetTag, Status: "held", Reason: "apply_byte_budget", LeaseUntil: epoch, AvailableAt: now, MaterialBytes: counter.size, PartCount: (counter.size + PartBytes - 1) / PartBytes, CreatedAt: now, UpdatedAt: now}
	if e := db.Transaction(func(tx *gorm.DB) error {
		if e := tx.Omit("MaterialRetiredAt").Create(&h).Error; e != nil {
			return e
		}
		if _, e := reserveMaterialBudget(tx, h.MaterialBytes, true); e != nil {
			return e
		}
		writer := partWriter{tx: tx, id: id, buf: make([]byte, 0, PartBytes)}
		if e := network.WriteObservationSource(&writer, source); e != nil {
			return e
		}
		return writer.flush()
	}); e != nil {
		t.Fatal(e)
	}
	return id
}
func TestObservationRetirementLargeHistoricalPartsUseBoundedDurableVerificationAndDeletion(t *testing.T) {
	a, b, node := ownershipFixture(t)
	source := retirementSource(node, "large-old", false)
	source.Payload = bytes.Repeat([]byte("L"), 3<<20)
	id := seedOldLargeSource(t, a, source)
	now := clockNow().Add(MaterialMinimumAge + time.Hour)
	// A first32-part slice cannot retire the complete ~52-part source.
	out, e := (MaterialRetention{DB: a, Now: func() time.Time { return now }}).Retire(t.Context(), 1, MaxDrainBytes)
	if e != nil || out.Retired != 0 {
		t.Fatal(out, e)
	}
	var r model.NodeObservationRetirement
	a.First(&r, "intent_id = ?", id)
	if r.State != "verifying" || r.NextPart != retirementPartBatch || r.VerifiedBytes > retirementPartBatch*PartBytes || r.ContentVerified {
		t.Fatal("unbounded or invented verification", r)
	}
	before := retirementBudget(t, a)
	r = finishRetirement(t, b, id, &now)
	if !r.ContentVerified || r.RemainingBytes != 0 || r.DeletePart != r.NextPart || r.VerifiedBytes != before.RetainedBytes || !strings.Contains(r.SourceSummary, "not_captured_before_0067") {
		t.Fatal(r)
	}
	if b := retirementBudget(t, a); b.RetainedBytes != 0 {
		t.Fatal(b)
	}
}
func TestObservationRetirementBadHashPreservesBodyAndDoesNotStarveHealthyHeld(t *testing.T) {
	a, _, node := ownershipFixture(t)
	bad := saveRetirementSource(t, a, retirementSource(node, "bad-source", true))
	good := saveRetirementSource(t, a, retirementSource(node, "good-source", true))
	var p model.NodeObservationPart
	if e := a.First(&p, "intent_id = ? AND ordinal = ?", bad, 0).Error; e != nil {
		t.Fatal(e)
	}
	p.Data[0] ^= 1
	if e := a.Model(&p).Where("intent_id = ? AND ordinal = ?", bad, 0).Update("data", p.Data).Error; e != nil {
		t.Fatal(e)
	}
	now := clockNow().Add(MaterialMinimumAge + time.Hour)
	_, e := (MaterialRetention{DB: a, Now: func() time.Time { return now }}).Retire(t.Context(), 32, MaxDrainBytes)
	if !errors.Is(e, ErrMaterial) {
		t.Fatal("hash corruption was hidden", e)
	}
	var h model.NodeObservationIntent
	var r model.NodeObservationRetirement
	a.First(&h, "id = ?", bad)
	a.First(&r, "intent_id = ?", bad)
	if h.MaterialRetiredAt != nil || r.State != "quarantined" {
		t.Fatal("bad source was silently discarded", h, r)
	}
	r = finishRetirement(t, a, good, &now)
	if r.State != "complete" {
		t.Fatal(r)
	}
	var n int64
	a.Model(&model.NodeObservationPart{}).Where("intent_id = ?", bad).Count(&n)
	if n != 1 {
		t.Fatal("bad evidence was removed", n)
	}
}
func TestObservationRetirementDeletionFailureRollsBackBudgetAndPrefix(t *testing.T) {
	a, _, node := ownershipFixture(t)
	id := saveRetirementSource(t, a, retirementSource(node, "rollback-delete", true))
	now := clockNow().Add(MaterialMinimumAge + time.Hour)
	if _, e := (MaterialRetention{DB: a, Now: func() time.Time { return now }}).Retire(t.Context(), 1, MaxDrainBytes); e != nil {
		t.Fatal(e)
	}
	before := retirementBudget(t, a)
	if e := a.Exec("CREATE TRIGGER reject_observation_part_delete BEFORE DELETE ON node_observation_parts BEGIN SELECT RAISE(ABORT,'fixture delete failure'); END").Error; e != nil {
		t.Fatal(e)
	}
	now = now.Add(2 * time.Second)
	if _, e := (MaterialRetention{DB: a, Now: func() time.Time { return now }}).Retire(t.Context(), 1, MaxDrainBytes); e == nil {
		t.Fatal("delete failure hidden")
	}
	var r model.NodeObservationRetirement
	a.First(&r, "intent_id = ?", id)
	if r.State != "deleting" || r.DeletePart != 0 || r.RemainingBytes != before.RetainedBytes || retirementBudget(t, a) != before {
		t.Fatal("partial deletion/budget commit", r, before)
	}
	if e := a.Exec("DROP TRIGGER reject_observation_part_delete").Error; e != nil {
		t.Fatal(e)
	}
	finishRetirement(t, a, id, &now)
}
func TestObservationMaterialBudgetMissingAuthorityFailsClosedAndRollbackPreservesReservation(t *testing.T) {
	a, _, node := ownershipFixture(t)
	sentinel := errors.New("rollback material admission")
	if e := a.Transaction(func(tx *gorm.DB) error {
		if _, e := Enqueue(tx, retirementSource(node, "rollback-source", false)); e != nil {
			return e
		}
		return sentinel
	}); !errors.Is(e, sentinel) {
		t.Fatal(e)
	}
	if b := retirementBudget(t, a); b.RetainedBytes != 0 || b.Revision != 1 {
		t.Fatal("budget escaped authority rollback", b)
	}
	if e := a.Where("id = ?", 1).Delete(&model.NodeObservationMaterialBudget{}).Error; e != nil {
		t.Fatal(e)
	}
	if e := a.Transaction(func(tx *gorm.DB) error {
		_, e := Enqueue(tx, retirementSource(node, "missing-budget", false))
		return e
	}); !errors.Is(e, ErrMaterialBudget) {
		t.Fatal("missing authority recreated as zero", e)
	}
	var n int64
	a.Model(&model.NodeObservationIntent{}).Count(&n)
	if n != 0 {
		t.Fatal("missing budget accepted new source", n)
	}
}

func TestObservationRetirementSameLengthDamageAfterVerificationIsNotDeleted(t *testing.T) {
	a, _, node := ownershipFixture(t)
	id := saveRetirementSource(t, a, retirementSource(node, "changed-after-verification", true))
	now := clockNow().Add(MaterialMinimumAge + time.Hour)
	if out, e := (MaterialRetention{DB: a, Now: func() time.Time { return now }}).Retire(t.Context(), 1, MaxDrainBytes); e != nil || out.Retired != 1 {
		t.Fatal(out, e)
	}
	before := retirementBudget(t, a)
	var part model.NodeObservationPart
	if e := a.First(&part, "intent_id = ? AND ordinal = ?", id, 0).Error; e != nil {
		t.Fatal(e)
	}
	part.Data[len(part.Data)-1] ^= 1
	if e := a.Model(&part).Where("intent_id = ? AND ordinal = ?", id, 0).Update("data", part.Data).Error; e != nil {
		t.Fatal(e)
	}
	now = now.Add(2 * time.Second)
	if _, e := (MaterialRetention{DB: a, Now: func() time.Time { return now }}).Retire(t.Context(), 1, MaxDrainBytes); !errors.Is(e, ErrMaterial) {
		t.Fatal("same-length damage was not detected", e)
	}
	var row model.NodeObservationRetirement
	a.First(&row, "intent_id = ?", id)
	var n int64
	a.Model(&model.NodeObservationPart{}).Where("intent_id = ?", id).Count(&n)
	if row.State != "quarantined" || row.DeletePart != 0 || n != 1 || retirementBudget(t, a) != before {
		t.Fatal("corrupt material or its authority was removed", row, n, before)
	}
}
