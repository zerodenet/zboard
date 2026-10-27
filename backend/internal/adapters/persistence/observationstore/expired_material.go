package observationstore

import (
	"context"
	"crypto/sha256"
	"encoding"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const retirementPartBatch = 32

func (s MaterialRetention) retentionNow() time.Time {
	if s.Now != nil {
		return s.Now().UTC().Truncate(time.Millisecond)
	}
	return clockNow()
}
func (s MaterialRetention) HasPending(ctx context.Context) (bool, error) {
	if s.DB == nil {
		return false, errors.New("observation retention unavailable")
	}
	now := s.retentionNow()
	var id struct{ IntentID string }
	q := s.DB.WithContext(ctx).Model(&model.NodeObservationRetirement{}).Select("intent_id").Where("state IN ? AND review_at <= ?", []string{"verifying", "deleting"}, now).Limit(1).Scan(&id)
	if q.Error != nil || q.RowsAffected > 0 {
		return q.RowsAffected > 0, q.Error
	}
	var h struct{ ID string }
	q = s.DB.WithContext(ctx).Model(&model.NodeObservationIntent{}).Select("id").Where("status IN ? AND material_retired_at IS NULL AND material_review_at <= ?", []string{"pending", "held", "claimed"}, now).Limit(1).Scan(&h)
	if q.Error != nil || q.RowsAffected > 0 {
		return q.RowsAffected > 0, q.Error
	}
	return s.hasAppliedPending(ctx)
}
func (s MaterialRetention) Retire(ctx context.Context, limit int, budget int64) (out MaterialRetentionResult, err error) {
	if s.DB == nil || limit < 1 || limit > MaterialRetentionLimit || budget < MaxApplyBytes || budget > MaxDrainBytes {
		return out, errors.New("invalid observation retention budget")
	}
	now := s.retentionNow()
	for out.Inspected < limit && budget > 0 {
		changed, freed, found, e := s.expireStep(ctx, now, &budget)
		if e != nil {
			err = errors.Join(err, e)
		}
		if !found {
			break
		}
		out.Inspected++
		if changed {
			out.Retired++
		}
		out.Bytes += freed
		if ctx.Err() != nil {
			return out, errors.Join(err, ctx.Err())
		}
	}
	if out.Inspected < limit && budget >= MaxApplyBytes {
		more, e := s.retireApplied(ctx, limit-out.Inspected, budget)
		out.Inspected += more.Inspected
		out.Retired += more.Retired
		out.Bytes += more.Bytes
		err = errors.Join(err, e)
	}
	return out, err
}

// One step locks node -> exact state -> header -> retirement -> budget.
// It reads at most32 parts and the caller's remaining raw-byte budget, even for
// a huge historical source. Progress is durable and never a financial cursor.
func (s MaterialRetention) expireStep(ctx context.Context, now time.Time, budget *int64) (retired bool, freed uint64, found bool, failure error) {
	var candidateID string
	var progress model.NodeObservationRetirement
	q := s.DB.WithContext(ctx).Select("intent_id").Where("state IN ? AND review_at <= ?", []string{"verifying", "deleting"}, now).Order("state,review_at,intent_id").First(&progress)
	if q.Error == nil {
		candidateID = progress.IntentID
	} else if !errors.Is(q.Error, gorm.ErrRecordNotFound) {
		return false, 0, false, q.Error
	}
	if candidateID == "" {
		// Three exact leading-index ranges, eachLIMIT1. Avoid a global filesort
		// over pending/held/claimed histories merely to discover one due source.
		var chosen model.NodeObservationIntent
		for _, status := range []string{"pending", "held", "claimed"} {
			var h model.NodeObservationIntent
			q := s.DB.WithContext(ctx).Where("status = ? AND material_retired_at IS NULL AND material_review_at <= ?", status, now).Order("material_review_at,id").First(&h)
			if errors.Is(q.Error, gorm.ErrRecordNotFound) {
				continue
			}
			if q.Error != nil {
				return false, 0, false, q.Error
			}
			if chosen.ID == "" || h.MaterialReviewAt.Before(chosen.MaterialReviewAt) || h.MaterialReviewAt.Equal(chosen.MaterialReviewAt) && h.ID < chosen.ID {
				chosen = h
			}
		}
		if chosen.ID == "" {
			return
		}
		candidateID = chosen.ID
	}

	found = true
	var materialErr error
	failure = transaction(s.DB.WithContext(ctx), func(tx *gorm.DB) error {
		var identity model.NodeObservationIntent
		if err := tx.First(&identity, "id = ?", candidateID).Error; err != nil {
			return err
		}
		var node model.Node
		e := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Select("id").First(&node, identity.NodeID).Error
		if errors.Is(e, gorm.ErrRecordNotFound) {
			var n int64
			if err := tx.Model(&model.Node{}).Where("id = ?", identity.NodeID).Count(&n).Error; err != nil {
				return err
			}
			if n != 0 {
				return s.deferExpired(tx, identity, now.Add(time.Minute))
			}
		} else if e != nil {
			return e
		}
		var state model.NodeObservationState
		e = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("node_id = ? AND asset_tag = ?", identity.NodeID, identity.AssetTag).First(&state).Error
		if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		var h model.NodeObservationIntent
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&h, "id = ?", candidateID).Error; e != nil {
			return e
		}
		r, e := readRetirement(tx, h.ID, true)
		exists := e == nil
		if errors.Is(e, ErrMaterial) {
			materialErr = e
			if err := s.deferExpired(tx, h, now.Add(MaterialReviewDelay)); err != nil {
				return err
			}
			return tx.Model(&model.NodeObservationRetirement{}).Where("intent_id = ?", h.ID).UpdateColumns(map[string]any{"state": "quarantined", "review_at": now.Add(MaterialReviewDelay), "updated_at": now}).Error
		}
		if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}

		quarantine := func(reason string) error {
			materialErr = invalid(reason)
			if exists {
				if e := s.deferExpired(tx, h, now.Add(MaterialReviewDelay)); e != nil {
					return e
				}
				return tx.Model(&r).UpdateColumns(map[string]any{"state": "quarantined", "review_at": now.Add(MaterialReviewDelay), "updated_at": now}).Error
			}
			return s.deferExpired(tx, h, now.Add(MaterialReviewDelay))
		}
		if exists && r.HeaderSHA256 != materialHeaderHash(h) {
			return quarantine("retirement immutable header changed")
		}
		if h.Status == "retired" {
			if !exists || r.State != "deleting" {
				return quarantine("retired observation lacks deletion authority")
			}
		} else {
			if h.Status != "pending" && h.Status != "held" && h.Status != "claimed" {
				return s.deferExpired(tx, h, now.Add(MaterialReviewDelay))
			}
			if h.MaterialRetiredAt != nil || h.CreatedAt.IsZero() || h.UpdatedAt.Before(h.CreatedAt) || h.CreatedAt.After(now) || h.UpdatedAt.After(now) {
				return quarantine("invalid observation lifecycle")
			}
			if h.CreatedAt.Add(MaterialMinimumAge).After(now) {
				return s.deferExpired(tx, h, h.CreatedAt.Add(MaterialMinimumAge))
			}
			if h.Reason == "unattributed_legacy_floor" {
				return s.deferExpired(tx, h, now.Add(MaterialReviewDelay))
			}
			if h.Status == "claimed" && h.LeaseUntil.After(now) {
				return s.deferExpired(tx, h, h.LeaseUntil.Add(time.Second))
			}
			if h.Status != "claimed" && (h.Token != "" || !h.LeaseUntil.Equal(epoch)) {
				return quarantine("invalid unclaimed observation owner")
			}
		}
		// Global reference indexes prevent retiring any current/floor or even
		// malformed foreign-asset pin. Never clear state references to make room.
		for _, column := range []string{"latest_reference", "stats_reference", "floor_reference"} {
			var pin struct{ NodeID uint }
			q := tx.Model(&model.NodeObservationState{}).Select("node_id").Where(column+" = ?", h.ID).Limit(1).Scan(&pin)
			if q.Error != nil {
				return q.Error
			}
			if q.RowsAffected > 0 {
				return s.deferExpired(tx, h, now.Add(MaterialReviewDelay))
			}
		}
		if !exists {
			hasher := sha256.New()
			encoded, _ := hasher.(encoding.BinaryMarshaler).MarshalBinary()
			r = model.NodeObservationRetirement{IntentID: h.ID, HeaderSHA256: materialHeaderHash(h), State: "verifying", Cause: "retention_expired", SourceSummary: `{"version":1,"positions":"not_captured_before_0067","replay":"expired_optional_source"}`, HashState: encoded, RemainingBytes: h.MaterialBytes, ReviewAt: now, CreatedAt: now, UpdatedAt: now}
			if e := tx.Create(&r).Error; e != nil {
				return e
			}
			exists = true
		} else if r.State == "retained" {
			hasher := sha256.New()
			encoded, _ := hasher.(encoding.BinaryMarshaler).MarshalBinary()
			r.State, r.Cause, r.HashState = "verifying", "retention_expired", encoded
			if e := tx.Model(&r).UpdateColumns(map[string]any{"state": r.State, "cause": r.Cause, "hash_state": encoded, "updated_at": now}).Error; e != nil {
				return e
			}
		} else if r.State == "quarantined" || r.State == "complete" {
			return s.deferExpired(tx, h, now.Add(MaterialReviewDelay))
		}
		if r.State == "deleting" {
			if e := validateExpiredReceipt(tx, h); e != nil {
				return quarantine("invalid compact deletion receipt")
			}
			e := deleteExpiredPartBatch(tx, h, &r, now, budget, &freed)
			if errors.Is(e, ErrMaterial) {
				return quarantine(e.Error())
			}
			return e
		}
		if r.State != "verifying" || h.MaterialBytes == 0 || h.PartCount == 0 || h.PartCount != (h.MaterialBytes+PartBytes-1)/PartBytes || r.NextPart > h.PartCount || r.VerifiedBytes > h.MaterialBytes || r.RemainingBytes != h.MaterialBytes || len(r.HashState) > 256 {
			return quarantine("invalid material verification checkpoint")
		}
		hash := sha256.New()
		if e := hash.(encoding.BinaryUnmarshaler).UnmarshalBinary(r.HashState); e != nil {
			return quarantine("invalid SHA checkpoint")
		}
		for n := 0; n < retirementPartBatch && r.NextPart < h.PartCount; n++ {
			var length int64
			q := tx.Model(&model.NodeObservationPart{}).Select("length(data)").Where("intent_id = ? AND ordinal = ?", h.ID, r.NextPart).Scan(&length)
			if q.Error != nil {
				return q.Error
			}
			expected := uint64(PartBytes)
			if r.NextPart+1 == h.PartCount {
				expected = h.MaterialBytes - r.VerifiedBytes
			}
			if q.RowsAffected != 1 || length <= 0 || length > PartBytes || uint64(length) != expected || uint64(length) > h.MaterialBytes-r.VerifiedBytes {
				return quarantine("part metadata differs from immutable source")
			}
			if length > *budget {
				break
			}
			*budget -= length
			var p model.NodeObservationPart
			if e := tx.Select("ordinal,substr(data,1,?) AS data", PartBytes+1).First(&p, "intent_id = ? AND ordinal = ?", h.ID, r.NextPart).Error; e != nil {
				return e
			}
			if int64(len(p.Data)) != length {
				return quarantine("part changed during verification")
			}

			digest := sha256.Sum256(p.Data)
			if e := tx.Create(&model.NodeObservationRetirementPart{IntentID: h.ID, Ordinal: r.NextPart, SHA256: hex.EncodeToString(digest[:])}).Error; e != nil {
				return e
			}
			hash.Write(p.Data)
			r.NextPart++
			r.VerifiedBytes += uint64(length)
		}
		r.HashState, _ = hash.(encoding.BinaryMarshaler).MarshalBinary()
		if r.NextPart == h.PartCount {
			var extra struct{ Ordinal uint64 }
			q := tx.Model(&model.NodeObservationPart{}).Select("ordinal").Where("intent_id = ? AND ordinal >= ?", h.ID, h.PartCount).Limit(1).Scan(&extra)
			if q.Error != nil {
				return q.Error
			}
			if q.RowsAffected != 0 || r.VerifiedBytes != h.MaterialBytes || hex.EncodeToString(hash.Sum(nil)) != h.ID {
				return quarantine("original observation SHA256 differs; material retained")
			}

			// For pre0067 sources that fit the original apply budget, preserve both
			// original positions after validating the complete source again. Large old
			// sources retain identity/queue times with explicit unavailable positions;
			// never allocate an enormous field merely to fabricate a summary.
			if strings.Contains(r.SourceSummary, "not_captured_before_0067") && h.MaterialBytes <= uint64(MaxApplyBytes) {
				if h.MaterialBytes > uint64(*budget) {
					return tx.Model(&r).UpdateColumns(map[string]any{"next_part": r.NextPart, "verified_bytes": r.VerifiedBytes, "hash_state": r.HashState, "review_at": now.Add(time.Second), "updated_at": now}).Error
				}
				*budget -= int64(h.MaterialBytes)
				source, e := readSource(tx, h)
				if e != nil {
					return quarantine("verified old source cannot be decoded")
				}
				if source.LegacyFloor {
					return s.deferExpired(tx, h, now.Add(MaterialReviewDelay))
				}
				r.SourceSummary = sourceSummary(source)
			}
			// All bytes verified. Fence old claims before bounded physical deletion;
			// remaining_bytes reports actual registered retained material during cleanup.
			r.State = "deleting"
			r.ContentVerified = true
			q = tx.Model(&h).Where("status = ? AND token = ? AND material_retired_at IS NULL", h.Status, h.Token).UpdateColumns(map[string]any{"status": "retired", "token": "", "lease_until": epoch, "material_retired_at": now})
			if q.Error != nil {
				return q.Error
			}
			if q.RowsAffected != 1 {
				return ErrOwnership
			}
			retired = true
		}
		return tx.Model(&r).UpdateColumns(map[string]any{"state": r.State, "next_part": r.NextPart, "verified_bytes": r.VerifiedBytes, "hash_state": r.HashState, "content_verified": r.ContentVerified, "source_summary": r.SourceSummary, "review_at": now.Add(time.Second), "updated_at": now}).Error
	})
	return retired, freed, found, errors.Join(failure, materialErr)
}
func (s MaterialRetention) deferExpired(tx *gorm.DB, h model.NodeObservationIntent, next time.Time) error {
	if err := tx.Model(&h).UpdateColumns(map[string]any{"material_review_at": next}).Error; err != nil {
		return err
	}
	return tx.Model(&model.NodeObservationRetirement{}).Where("intent_id = ? AND state IN ?", h.ID, []string{"verifying", "deleting"}).UpdateColumns(map[string]any{"review_at": next}).Error
}
func deleteExpiredPartBatch(tx *gorm.DB, h model.NodeObservationIntent, r *model.NodeObservationRetirement, now time.Time, budget *int64, freed *uint64) error {
	ordinals := []uint64{}
	var bytes uint64
	for n := 0; n < retirementPartBatch && r.DeletePart < h.PartCount; n++ {
		var length int64
		q := tx.Model(&model.NodeObservationPart{}).Select("length(data)").Where("intent_id = ? AND ordinal = ?", h.ID, r.DeletePart).Scan(&length)
		if q.Error != nil {
			return q.Error
		}
		if q.RowsAffected != 1 || length <= 0 || length > PartBytes || bytes > r.RemainingBytes || uint64(length) > r.RemainingBytes-bytes {
			return invalid("verified material changed before deletion")
		}
		if length > *budget {
			break
		}
		*budget -= length

		var proof model.NodeObservationRetirementPart
		if e := tx.First(&proof, "intent_id = ? AND ordinal = ?", h.ID, r.DeletePart).Error; e != nil {
			return invalid("verified part checksum authority missing")
		}
		var part model.NodeObservationPart
		if e := tx.Select("ordinal,substr(data,1,?) AS data", PartBytes+1).First(&part, "intent_id = ? AND ordinal = ?", h.ID, r.DeletePart).Error; e != nil {
			return e
		}
		if len(part.Data) != int(length) {
			return invalid("verified material changed length before deletion")
		}
		sum := sha256.Sum256(part.Data)
		if proof.SHA256 != hex.EncodeToString(sum[:]) {
			return invalid("verified material hash changed before deletion; retained")
		}
		bytes += uint64(length)
		ordinals = append(ordinals, r.DeletePart)
		r.DeletePart++
	}
	if len(ordinals) == 0 {
		return tx.Model(r).UpdateColumns(map[string]any{"review_at": now.Add(time.Second)}).Error
	}
	if r.DeletePart == h.PartCount && r.RemainingBytes != bytes {
		return invalid("material prefix length differs from complete receipt")
	}
	// The same SQL transaction removes only this verified prefix, reduces actual
	// retained-byte authority, and advances the resumable deletion checkpoint.
	d := tx.Where("intent_id = ? AND ordinal IN ?", h.ID, ordinals).Delete(&model.NodeObservationPart{})
	if d.Error != nil {
		return d.Error
	}
	if d.RowsAffected != int64(len(ordinals)) {
		return ErrOwnership
	}

	checks := tx.Where("intent_id = ? AND ordinal IN ?", h.ID, ordinals).Delete(&model.NodeObservationRetirementPart{})
	if checks.Error != nil {
		return checks.Error
	}
	if checks.RowsAffected != int64(len(ordinals)) {
		return ErrOwnership
	}
	if e := releaseMaterialBudget(tx, bytes); e != nil {
		return e
	}
	r.RemainingBytes -= bytes
	if r.DeletePart == h.PartCount {
		if r.RemainingBytes != 0 {
			return fmt.Errorf("%w: deleted source bytes differ", ErrMaterialBudget)
		}
		r.State = "complete"
	}
	if e := tx.Model(r).UpdateColumns(map[string]any{"state": r.State, "delete_part": r.DeletePart, "remaining_bytes": r.RemainingBytes, "review_at": now.Add(time.Second), "updated_at": now}).Error; e != nil {
		return e
	}
	*freed = bytes
	return nil
}
