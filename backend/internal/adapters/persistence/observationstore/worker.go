package observationstore

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrOwnership = errors.New("observation claim no longer owned")
var errBudget = errors.New("observation cycle byte budget exhausted")
var errBusy = errors.New("observation node is busy")

const MaxDrainBytes int64 = 8 * 1024 * 1024
const claimLease = 30 * time.Second

// Scheduling/lease timestamps share MySQL millisecond storage precision. Source
// timestamps remain lossless in material and CounterWrittenStamp.
func clockNow() time.Time { return time.Now().UTC().Truncate(time.Millisecond) }

type Worker struct{ DB *gorm.DB }

func (w Worker) HasPending(ctx context.Context) (bool, error) {
	var h struct{ ID string }
	now := clockNow()
	result := ready(w.DB.WithContext(ctx), now).Select("id").Limit(1).Scan(&h)
	return result.RowsAffected > 0, result.Error
}
func ready(db *gorm.DB, now time.Time) *gorm.DB {
	return db.Model(&model.NodeObservationIntent{}).Where("(status = ? AND available_at <= ?) OR (status = ? AND lease_until <= ?)", "pending", now, "claimed", now)
}
func transaction(db *gorm.DB, run func(*gorm.DB) error) error {
	if db.Dialector.Name() == "sqlite" {
		return db.Transaction(run)
	}
	return db.Transaction(run, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
}
func (w Worker) claim(ctx context.Context) (h model.NodeObservationIntent, found bool, err error) {
	var b [32]byte
	if _, err = rand.Read(b[:]); err != nil {
		return
	}
	err = transaction(w.DB.WithContext(ctx), func(tx *gorm.DB) error {
		now := clockNow()
		q := ready(tx, now).Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Order("available_at, id").First(&h)
		if errors.Is(q.Error, gorm.ErrRecordNotFound) {
			return nil
		}
		if q.Error != nil {
			return q.Error
		}
		maxAttempts := uint64(math.MaxUint64)
		if tx.Dialector.Name() == "sqlite" {
			maxAttempts = math.MaxInt64
		}
		if h.Attempts >= maxAttempts {
			// Preserve exhausted identities as held. Never wrap a claim generation
			// through zero or let one terminal candidate block the rest of a round.
			h.Status, h.Reason, h.Token, h.LeaseUntil = "held", "attempt_limit", "", epoch
			r := tx.Model(&model.NodeObservationIntent{}).Where("id = ?", h.ID).Updates(map[string]any{"status": h.Status, "reason": h.Reason, "token": "", "lease_until": epoch, "updated_at": now})
			if r.Error != nil {
				return r.Error
			}
			if r.RowsAffected != 1 {
				return ErrOwnership
			}
			found = true
			return nil
		}
		h.Token = hex.EncodeToString(b[:])
		h.Status = "claimed"
		h.LeaseUntil = now.Add(claimLease)
		h.Attempts++
		r := tx.Model(&model.NodeObservationIntent{}).Where("id = ?", h.ID).Updates(map[string]any{"status": h.Status, "token": h.Token, "lease_until": h.LeaseUntil, "attempts": h.Attempts, "updated_at": now})
		found = r.Error == nil
		return r.Error
	})
	return
}

// Drain never owns a queue lock while waiting for a node. Only one source is
// claimed in each short transaction, so a bad/busy node cannot own a whole batch.
// It bounds fetched material bytes, including previous Stats/Latest evidence;
// it does not bound total retained pending/held evidence or database growth.
func (w Worker) Drain(ctx context.Context, limit int, budget int64) (done int, err error) {
	if limit < 1 || limit > 200 || budget < 4*MaxApplyBytes || budget > MaxDrainBytes {
		return 0, errors.New("invalid observation drain budget")
	}
	for i := 0; i < limit && budget > 0; i++ {
		h, found, e := w.claim(ctx)
		if e != nil {
			return done, errors.Join(err, e)
		}
		if !found {
			return done, err
		}
		if h.Status == "held" {
			done++
			continue
		}
		e = w.apply(ctx, h, &budget)
		if e != nil {
			if errors.Is(e, ErrOwnership) {
				continue
			}
			if errors.Is(e, ErrMaterial) {
				r := transaction(w.DB.WithContext(ctx), func(tx *gorm.DB) error { return ownedAck(tx, h, "held", "invalid_reference_material") })
				if r != nil {
					return done, errors.Join(e, r)
				}
				done++
				continue
			}
			reason := "projection_failed"
			if errors.Is(e, errBusy) {
				reason = "node_busy"
			}
			recovery := w.retry(ctx, h, reason)
			if recovery != nil {
				return done, errors.Join(e, recovery)
			}
			if errors.Is(e, errBusy) {
				continue
			}
			if errors.Is(e, errBudget) {
				return done, err
			}
			// Keep the real failure visible, but finish this bounded round so a
			// bad node cannot monopolize the periodic handler's shared retry delay.
			err = errors.Join(err, e)
			if ctx.Err() != nil {
				return done, err
			}
			continue
		}
		done++
	}
	return done, err
}
func (w Worker) retry(ctx context.Context, h model.NodeObservationIntent, reason string) error {
	now := clockNow()
	delay := time.Second * time.Duration(min(h.Attempts, 60))
	r := w.DB.WithContext(ctx).Model(&model.NodeObservationIntent{}).Where("id = ? AND status = ? AND token = ?", h.ID, "claimed", h.Token).Updates(map[string]any{"status": "pending", "token": "", "lease_until": epoch, "available_at": now.Add(delay), "reason": reason, "updated_at": now})
	if r.Error != nil {
		return r.Error
	}
	if r.RowsAffected != 1 {
		return ErrOwnership
	}
	return nil
}
func ownedAck(tx *gorm.DB, h model.NodeObservationIntent, status, reason string) error {
	now := clockNow()
	updates := map[string]any{"status": status, "reason": reason, "token": "", "lease_until": epoch, "updated_at": now}
	if status == "applied" {
		updates["material_review_at"] = now.Add(MaterialMinimumAge)
	}
	r := tx.Model(&model.NodeObservationIntent{}).Where("id = ? AND node_id = ? AND asset_tag = ? AND material_bytes = ? AND part_count = ? AND status = ? AND token = ? AND lease_until > ? AND material_retired_at IS NULL", h.ID, h.NodeID, h.AssetTag, h.MaterialBytes, h.PartCount, "claimed", h.Token, now).Updates(updates)
	if r.Error != nil {
		return r.Error
	}
	if r.RowsAffected != 1 {
		return ErrOwnership
	}
	return nil
}
func (w Worker) apply(ctx context.Context, h model.NodeObservationIntent, budget *int64) error {
	return transaction(w.DB.WithContext(ctx), func(tx *gorm.DB) error {
		// Load only bounded header metadata first. Unknown/oversize sources are
		// acknowledged as HELD, never discarded or represented as applied counters.
		if h.MaterialBytes > uint64(MaxApplyBytes) {
			return ownedAck(tx, h, "held", "apply_byte_budget")
		}
		load := func(header model.NodeObservationIntent) (network.ObservationSource, error) {
			if header.MaterialBytes > uint64(MaxApplyBytes) {
				return network.ObservationSource{}, invalid("reference exceeds per-item budget")
			}
			if header.MaterialBytes > uint64(*budget) {
				return network.ObservationSource{}, errBudget
			}
			*budget -= int64(header.MaterialBytes)
			return readSource(tx, header)
		}
		reference := func(id string) (network.ObservationSource, error) {
			var header model.NodeObservationIntent
			if err := tx.First(&header, "id = ?", id).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return network.ObservationSource{}, invalid("missing source reference")
				}
				return network.ObservationSource{}, err
			}
			return load(header)
		}
		source, err := load(h)
		if err != nil {
			if errors.Is(err, ErrMaterial) {
				return ownedAck(tx, h, "held", "invalid_material")
			}
			return err
		}
		if source.Kind == "stats.sampled" && source.Observation.StatsEvent == nil {
			return ownedAck(tx, h, "held", "invalid_stats_payload")
		}
		if source.LegacyFloor {
			return ownedAck(tx, h, "held", "unattributed_legacy_floor")
		}
		p := source.Observation.Latest
		if h.NodeID == 0 || p.AssetTag == "" || p.AssetTag != h.AssetTag || !usable(p) {
			return ownedAck(tx, h, "held", "unknown_source")
		}
		var node model.Node
		e := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).First(&node, h.NodeID).Error
		if errors.Is(e, gorm.ErrRecordNotFound) {
			var n int64
			if err := tx.Model(&model.Node{}).Where("id = ?", h.NodeID).Count(&n).Error; err != nil {
				return err
			}
			if n != 0 {
				return errBusy
			}
			if err := Tombstone(tx, h.NodeID, h.AssetTag); err != nil {
				return err
			}
			return ownedAck(tx, h, "held", "node_deleted")
		}
		if e != nil {
			return e
		}
		if node.AssetTag == "" || node.AssetTag != h.AssetTag {
			return ownedAck(tx, h, "held", "asset_mismatch")
		}
		if node.LifecycleStatus == "archived" {
			return ownedAck(tx, h, "held", "node_archived")
		}
		state, err := lockState(tx, h.NodeID, h.AssetTag)
		if err != nil {
			return err
		}
		if state.Deleted {
			return ownedAck(tx, h, "held", "asset_deleted")
		}
		if !node.IsEnabled || node.NodeCredentialRevokedAt != nil {
			return ownedAck(tx, h, "held", "node_inactive")
		}
		badReference := func(e error) error {
			if !errors.Is(e, ErrMaterial) {
				return e
			}
			state.CurrentKnown = false
			state.LatestKnown = false
			state.StatsKnown = false
			state.CurrentSource = "invalid_reference"
			state.UpdatedAt = clockNow()
			if err := saveState(tx, state); err != nil {
				return err
			}
			return ownedAck(tx, h, "held", "invalid_reference_material")
		}
		previousLatestKnown, previousStatsKnown := state.LatestKnown, state.StatsKnown
		// The unattributed old floor is immutable. No live flow can advance it and
		// no legacy position is relabeled as having actually applied Stats.
		var floor model.NodeObservationState
		e = tx.Where("node_id = ? AND asset_tag = ?", h.NodeID, "").First(&floor).Error
		if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		if e == nil && !floor.Deleted && floor.FloorReference != "" {
			old, err := reference(floor.FloorReference)
			if err != nil {
				return badReference(err)
			}
			if !old.LegacyFloor || old.Observation.NodeID != h.NodeID || old.Observation.Latest.AssetTag != "" {
				return badReference(invalid("invalid floor reference"))
			}
			if !network.EventNewerThanPosition(p, old.Observation.Latest) {
				return ownedAck(tx, h, "held", "unattributed_floor")
			}
		}
		var latest, stats *network.EventPosition
		var oldStats network.NodeStats
		if state.LatestReference != "" {
			old, err := reference(state.LatestReference)
			if err != nil {
				return badReference(err)
			}
			if old.LegacyFloor || old.Observation.NodeID != h.NodeID || old.Observation.Latest.AssetTag != node.AssetTag {
				return badReference(invalid("invalid latest reference"))
			}
			v := old.Observation.Latest
			state.LatestKnown = true
			latest = &v
		}
		if state.StatsReference != "" {
			old, err := reference(state.StatsReference)
			if err != nil {
				return badReference(err)
			}
			state.StatsKnown = true
			oldStats = old.Observation.Stats
			stats = old.Observation.StatsEvent
			if old.LegacyFloor || old.Observation.NodeID != h.NodeID || stats == nil || stats.AssetTag != node.AssetTag {
				return badReference(invalid("invalid stats reference"))
			}
		}
		latestAdvanced := latest == nil || network.EventNewerThanPosition(p, *latest)
		if latestAdvanced {
			state.LatestReference = h.ID
			state.LatestKnown = true
			occurred, received := p.OccurredAt.UTC().Truncate(time.Millisecond), p.ReceivedAt.UTC().Truncate(time.Millisecond)
			state.LatestOccurredAt = &occurred
			state.LatestReceivedAt = &received
			v := p
			latest = &v
		}
		statsAdvanced := false
		if sp := source.Observation.StatsEvent; sp != nil {
			if sp.AssetTag != node.AssetTag || !usable(*sp) {
				return ownedAck(tx, h, "held", "unknown_stats_source")
			}
			if stats != nil && !network.EventNewerThanPosition(*sp, *stats) && !network.EventNewerThanPosition(*stats, *sp) && source.Observation.Stats != oldStats {
				return ownedAck(tx, h, "held", "stats_position_conflict")
			}
			if stats == nil || network.EventNewerThanPosition(*sp, *stats) {
				state.StatsReference = h.ID
				state.StatsKnown = true
				occurred, received := sp.OccurredAt.UTC().Truncate(time.Millisecond), sp.ReceivedAt.UTC().Truncate(time.Millisecond)
				state.StatsOccurredAt = &occurred
				state.StatsReceivedAt = &received
				stats = sp
				statsAdvanced = true
			}
		}
		// Runtime/source mismatch stays explicitly unknown; retaining the historical
		// Stats reference is not a claim that it belongs to the current Latest.
		consistent := latest != nil && stats != nil && latest.AssetTag == stats.AssetTag && latest.CoreInstanceID == stats.CoreInstanceID
		var override time.Time
		if state.CounterWrittenStamp != "" {
			override, err = time.Parse(time.RFC3339Nano, state.CounterWrittenStamp)
			if err != nil {
				return badReference(invalid("invalid counter receiver time"))
			}
		}
		if statsAdvanced && consistent && (override.IsZero() || !stats.ReceivedAt.Before(override)) {
			r := tx.Model(&model.Node{}).Where("id = ? AND asset_tag = ?", node.ID, node.AssetTag).Updates(map[string]any{"active_flows": source.Observation.Stats.ActiveSessions, "bytes_up": source.Observation.Stats.BytesUp, "bytes_down": source.Observation.Stats.BytesDown})
			if r.Error != nil {
				return r.Error
			}
			if r.RowsAffected > 1 {
				return errors.New("non-unique node observation update")
			}
			state.CurrentKnown = true
			state.CurrentSource = "buffered_stats"
			observed := stats.ReceivedAt
			cached := observed.UTC().Truncate(time.Millisecond)
			state.CounterWrittenAt = &cached
			state.CounterWrittenStamp = observed.UTC().Format(time.RFC3339Nano)
		} else if !consistent {
			state.CurrentKnown = false
			if state.CurrentSource == "buffered_stats" {
				state.CurrentSource = "historical_stats"
			}
		}
		if latestAdvanced || statsAdvanced || state.LatestKnown != previousLatestKnown || state.StatsKnown != previousStatsKnown {
			state.UpdatedAt = clockNow()
			if err := saveState(tx, state); err != nil {
				return err
			}
		}
		return ownedAck(tx, h, "applied", "")
	})
}
func usable(p network.EventPosition) bool {
	return strings.TrimSpace(p.CoreInstanceID) != "" && p.Sequence > 0 && !p.OccurredAt.IsZero() && !p.ReceivedAt.IsZero()
}
func lockState(tx *gorm.DB, node uint, tag string) (state model.NodeObservationState, err error) {
	state = model.NodeObservationState{NodeID: node, AssetTag: tag, CurrentSource: "unknown", UpdatedAt: clockNow()}
	if err = tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&state).Error; err != nil {
		return
	}
	err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("node_id = ? AND asset_tag = ?", node, tag).First(&state).Error
	return
}

// Empty asset labels are valid keys for unattributed floors. GORM Save treats
// that composite key as incomplete and may INSERT a duplicate. The caller owns
// the existing row lock; update that exact row without any insert fallback.
func saveState(tx *gorm.DB, state model.NodeObservationState) error {
	return tx.Model(&model.NodeObservationState{}).
		Where("node_id = ? AND asset_tag = ?", state.NodeID, state.AssetTag).
		// UpdatedAt is already clockNow(): do not replace its portable millisecond
		// precision with an implicit ORM Now during this explicit state update.
		Select("*").UpdateColumns(&state).Error
}

// Tombstone is called with the node lock (or after current-read absence). It
// retains all intents/material and also retires the unattributed legacy floor.
func Tombstone(tx *gorm.DB, node uint, tag string) error {
	for _, t := range []string{tag, ""} {
		s, err := lockState(tx, node, t)
		if err != nil {
			return err
		}
		s.Deleted = true
		s.CurrentKnown = false
		s.CurrentSource = "deleted"
		s.UpdatedAt = clockNow()
		if err := saveState(tx, s); err != nil {
			return err
		}
	}
	return nil
}

// CounterOverride shares node→state ordering. Heartbeat/legacy counter writes
// have no buffered Stats position; preserve history but mark current unknown.
func CounterOverride(tx *gorm.DB, node uint, tag, source string, at time.Time) error {
	s, err := lockState(tx, node, tag)
	if err != nil {
		return err
	}
	s.CurrentKnown = false
	s.CurrentSource = source
	observed := at.UTC()
	cached := observed.Truncate(time.Millisecond)
	s.CounterWrittenAt = &cached
	s.CounterWrittenStamp = observed.Format(time.RFC3339Nano)
	s.UpdatedAt = clockNow()
	return saveState(tx, s)
}
