package observationstore

import (
	"crypto/sha256"
	"encoding"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Capacity applies to optional retained parts, not financial proofs, header
// receipts, indexes, or the entire database. Historical/pinned excess survives.
const MaterialCapacityBytes uint64 = 64 << 20

var ErrMaterialBudget = errors.New("observation material budget authority unavailable or inconsistent")
var ErrMaterialExpired = errors.New("optional observation material expired; complete source replay unavailable")

func initialMaterialHashState() []byte {
	b, _ := sha256.New().(encoding.BinaryMarshaler).MarshalBinary()
	return b
}
func materialHeaderHash(h model.NodeObservationIntent) string {
	raw, _ := json.Marshal(struct {
		Version      int
		ID           string
		NodeID       uint
		AssetTag     string
		Bytes, Parts uint64
		CreatedAt    string
	}{1, h.ID, h.NodeID, h.AssetTag, h.MaterialBytes, h.PartCount, h.CreatedAt.UTC().Format(time.RFC3339Nano)})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func sourceSummary(s network.ObservationSource) string {
	type position struct {
		IDHash, CoreHash, SourceHash string
		Sequence, ConfigRevision     uint64
		OccurredAt, ReceivedAt       string
	}
	hash := func(v string) string { x := sha256.Sum256([]byte(v)); return hex.EncodeToString(x[:]) }
	project := func(p network.EventPosition) position {
		return position{hash(p.ID), hash(p.CoreInstanceID), hash(p.SourceID), p.Sequence, p.ConfigRevision, p.OccurredAt.UTC().Format(time.RFC3339Nano), p.ReceivedAt.UTC().Format(time.RFC3339Nano)}
	}
	var stats *position
	if s.Observation.StatsEvent != nil {
		v := project(*s.Observation.StatsEvent)
		stats = &v
	}
	raw, _ := json.Marshal(struct {
		Version     int
		Latest      position
		Stats       *position
		KindHash    string
		LegacyFloor bool
	}{1, project(s.Observation.Latest), stats, hash(s.Kind), s.LegacyFloor})
	return string(raw)
}
func reserveMaterialBudget(tx *gorm.DB, size uint64, legacyFloor bool) (bool, error) {
	// A true legacy floor can be imported before0067 exists only by the owned
	// migration. Live producers never set LegacyFloor. Initial0067 sums its parts.
	if legacyFloor && !tx.Migrator().HasTable(&model.NodeObservationMaterialBudget{}) {
		return true, nil
	}
	var b model.NodeObservationMaterialBudget
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&b, "id = ?", 1).Error; err != nil {
		return false, errors.Join(ErrMaterialBudget, err)
	}
	if b.ID != 1 || b.Revision == 0 || b.Revision >= math.MaxInt64 || b.RetainedBytes > math.MaxInt64 || size > math.MaxInt64-b.RetainedBytes {
		return false, ErrMaterialBudget
	}
	if !legacyFloor && (size > uint64(MaxApplyBytes) || size > MaterialCapacityBytes || b.RetainedBytes > MaterialCapacityBytes-size) {
		return false, nil
	}
	r := tx.Model(&b).Where("id = ? AND revision = ?", 1, b.Revision).UpdateColumns(map[string]any{"retained_bytes": b.RetainedBytes + size, "revision": b.Revision + 1})
	if r.Error != nil {
		return false, r.Error
	}
	if r.RowsAffected != 1 {
		return false, ErrMaterialBudget
	}
	return true, nil
}
func releaseMaterialBudget(tx *gorm.DB, bytes uint64) error {
	var b model.NodeObservationMaterialBudget
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&b, "id = ?", 1).Error; err != nil {
		return errors.Join(ErrMaterialBudget, err)
	}
	if b.ID != 1 || b.Revision == 0 || b.Revision >= math.MaxInt64 || bytes > b.RetainedBytes {
		return ErrMaterialBudget
	}
	r := tx.Model(&b).Where("revision = ?", b.Revision).UpdateColumns(map[string]any{"retained_bytes": b.RetainedBytes - bytes, "revision": b.Revision + 1})
	if r.Error != nil {
		return r.Error
	}
	if r.RowsAffected != 1 {
		return ErrMaterialBudget
	}
	return nil
}
func registerMaterialSource(tx *gorm.DB, h model.NodeObservationIntent, s network.ObservationSource, keep bool, now time.Time) error {
	cause, state, remaining := "", "retained", h.MaterialBytes
	if !keep {
		cause = "capacity_exceeded"
		if h.MaterialBytes > uint64(MaxApplyBytes) {
			cause = "source_size_limit"
		}
		state = "complete"
		remaining = 0
	}
	row := model.NodeObservationRetirement{IntentID: h.ID, HeaderSHA256: materialHeaderHash(h), State: state, Cause: cause, SourceSummary: sourceSummary(s), HashState: initialMaterialHashState(), RemainingBytes: remaining, ReviewAt: now.Add(MaterialMinimumAge), CreatedAt: now, UpdatedAt: now}
	if err := tx.Create(&row).Error; err != nil {
		return err
	}
	if keep {
		return nil
	}
	// No retained body is claimed to have been verified. The immutable complete
	// incoming source hash and original receiver positions remain in the receipt.
	return tx.Model(&model.NodeObservationIntent{}).Where("id = ? AND status = ? AND material_retired_at IS NULL", h.ID, "pending").UpdateColumns(map[string]any{"status": "retired", "material_retired_at": now}).Error
}
func releaseAppliedMaterialBudget(tx *gorm.DB, h model.NodeObservationIntent, now time.Time) error {
	r, err := readRetirement(tx, h.ID, true)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if e := releaseMaterialBudget(tx, h.MaterialBytes); e != nil {
		return e
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		r = model.NodeObservationRetirement{IntentID: h.ID, HeaderSHA256: materialHeaderHash(h), State: "complete", Cause: "applied_unreferenced", SourceSummary: `{"version":1,"positions":"not_captured_before_0067"}`, HashState: initialMaterialHashState(), ContentVerified: true, CreatedAt: now, UpdatedAt: now, ReviewAt: now}
		return tx.Create(&r).Error
	}
	if err != nil {
		return err
	}
	if r.HeaderSHA256 != materialHeaderHash(h) || (r.State != "retained" && r.State != "verifying") || r.RemainingBytes != h.MaterialBytes {
		return ErrMaterialBudget
	}

	if r.State == "verifying" {
		ordinals := make([]uint64, 0, h.PartCount)
		for i := uint64(0); i < h.PartCount; i++ {
			ordinals = append(ordinals, i)
		}
		if e := tx.Where("intent_id = ? AND ordinal IN ?", h.ID, ordinals).Delete(&model.NodeObservationRetirementPart{}).Error; e != nil {
			return e
		}
	}
	return tx.Model(&r).UpdateColumns(map[string]any{"state": "complete", "cause": "applied_unreferenced", "remaining_bytes": 0, "content_verified": true, "updated_at": now}).Error
}
func validateExpiredReceipt(tx *gorm.DB, h model.NodeObservationIntent) error {
	if h.MaterialRetiredAt == nil || h.Status != "retired" || h.Token != "" || !h.LeaseUntil.Equal(epoch) || h.MaterialRetiredAt.Before(h.CreatedAt) {
		return invalid("invalid expired observation receipt")
	}
	r, err := readRetirement(tx, h.ID, false)
	if err != nil {
		return err
	}
	if r.HeaderSHA256 != materialHeaderHash(h) || len(r.SourceSummary) > 8192 || (r.State != "deleting" && r.State != "complete") || (r.Cause != "retention_expired" && r.Cause != "capacity_exceeded" && r.Cause != "source_size_limit") {
		return invalid("expired observation identity mismatch")
	}
	if r.DeletePart > h.PartCount || r.RemainingBytes > h.MaterialBytes {
		return invalid("invalid material cleanup boundary")
	}
	if r.State == "deleting" && (!r.ContentVerified || r.VerifiedBytes != h.MaterialBytes || r.NextPart != h.PartCount) {
		return invalid("unverified observation deletion receipt")
	}
	if r.State == "complete" && r.RemainingBytes != 0 {
		return invalid("expired observation cleanup incomplete")
	}
	if r.Cause == "retention_expired" && (!r.ContentVerified || r.NextPart != h.PartCount || r.VerifiedBytes != h.MaterialBytes || (r.State == "complete" && r.DeletePart != h.PartCount)) {
		return invalid("expired observation lacks verified complete source")
	}
	if r.Cause != "retention_expired" && (r.State != "complete" || r.ContentVerified || r.NextPart != 0 || r.DeletePart != 0 || r.VerifiedBytes != 0 || (r.Cause == "source_size_limit" && h.MaterialBytes <= uint64(MaxApplyBytes)) || (r.Cause == "capacity_exceeded" && h.MaterialBytes > uint64(MaxApplyBytes))) {
		return invalid("invalid original material omission receipt")
	}
	// At most one indexed metadata row; no unbounded COUNT/blob read on replay.
	var extra struct{ Ordinal uint64 }
	q := tx.Model(&model.NodeObservationPart{}).Select("ordinal").Where("intent_id = ? AND ordinal < ?", h.ID, r.DeletePart).Limit(1).Scan(&extra)
	if q.Error != nil {
		return q.Error
	}
	if q.RowsAffected != 0 {
		return invalid("expired observation deleted prefix reappeared")
	}

	if r.State == "complete" {
		var extra model.NodeObservationRetirementPart
		proof := tx.Select("ordinal").Where("intent_id = ?", h.ID).Limit(1).Find(&extra)
		if proof.Error != nil {
			return proof.Error
		}
		if proof.RowsAffected != 0 {
			return invalid("completed observation still has verification parts")
		}
	}
	if r.State == "complete" {
		q = tx.Model(&model.NodeObservationPart{}).Select("ordinal").Where("intent_id = ?", h.ID).Limit(1).Scan(&extra)
		if q.Error != nil {
			return q.Error
		}
		if q.RowsAffected != 0 {
			return invalid("expired observation receipt still has parts")
		}
	}
	return nil
}
