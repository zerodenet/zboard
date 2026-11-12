package observationstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const projectionBytes = 4096

func compactString(s string) string {
	if len(s) <= 191 && utf8.ValidString(s) {
		encoded, _ := json.Marshal(s)
		if len(encoded) <= 193 {
			return s
		}
	}
	h := sha256.Sum256([]byte(s))
	return "sha256:" + hex.EncodeToString(h[:])
}
func compactPosition(p network.EventPosition) network.EventPosition {
	p.ID, p.SourceID, p.CoreInstanceID = compactString(p.ID), compactString(p.SourceID), compactString(strings.TrimSpace(p.CoreInstanceID))
	return p
}
func encodeProjection(o network.NodeObservation) (string, error) {
	o.Latest = compactPosition(o.Latest)
	if o.StatsEvent != nil {
		p := compactPosition(*o.StatsEvent)
		o.StatsEvent = &p
	}
	b, e := json.Marshal(o)
	if e == nil && len(b) > projectionBytes {
		e = invalid("projection exceeds fixed byte bound")
	}
	return string(b), e
}
func decodeProjection(s string) (o network.NodeObservation, e error) {
	if len(s) > projectionBytes {
		return o, invalid("projection exceeds fixed byte bound")
	}
	e = json.Unmarshal([]byte(s), &o)
	return
}
func inlineOrReference(inline, id string, load func(string) (network.ObservationSource, error)) (network.ObservationSource, error) {
	if inline == "" {
		s, err := load(id)
		if err == nil {
			s.Observation.Latest = compactPosition(s.Observation.Latest)
			if s.Observation.StatsEvent != nil {
				p := compactPosition(*s.Observation.StatsEvent)
				s.Observation.StatsEvent = &p
			}
		}
		return s, err
	}
	o, e := decodeProjection(inline)
	return network.ObservationSource{Observation: o}, e
}
func mergeObservation(old *network.NodeObservation, next network.NodeObservation) (bool, error) {
	changed := false
	if usable(next.Latest) && (old.NodeID == 0 || network.EventNewerThanPosition(next.Latest, old.Latest)) {
		old.NodeID = next.NodeID
		old.Latest = next.Latest
		changed = true
	}
	if p := next.StatsEvent; p != nil && usable(*p) {
		if old.StatsEvent == nil || network.EventNewerThanPosition(*p, *old.StatsEvent) {
			v := *p
			old.StatsEvent = &v
			old.Stats = next.Stats
			changed = true
		} else if !network.EventNewerThanPosition(*old.StatsEvent, *p) && old.Stats != next.Stats {
			return false, invalid("same statistics position has conflicting counters")
		}
	}
	return changed, nil
}

// EnqueueLatest shares the accounting transaction, but persists only the newest
// positions and counters. Payload, flow/principal identifiers and per-event
// retirement receipts never enter this projection. Exact financial identities
// continue to be committed by the metering authority before this call.
func EnqueueLatest(tx *gorm.DB, sources []network.ObservationSource) error {
	groups := map[uint][]network.NodeObservation{}
	for _, s := range sources {
		o := s.Observation
		if o.NodeID == 0 || s.LegacyFloor || !usable(o.Latest) || len(o.Latest.AssetTag) != 64 {
			continue
		}
		if _, e := hex.DecodeString(o.Latest.AssetTag); e != nil {
			continue
		}
		o.Latest = compactPosition(o.Latest)
		if o.StatsEvent != nil {
			p := compactPosition(*o.StatsEvent)
			o.StatsEvent = &p
		}
		groups[o.NodeID] = append(groups[o.NodeID], o)
	}
	ids := make([]uint, 0, len(groups))
	for id := range groups {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		var node model.Node
		if e := tx.Clauses(clause.Locking{Strength: "SHARE", Options: "NOWAIT"}).Select("id,asset_tag,lifecycle_status").First(&node, id).Error; e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				continue
			}
			return e
		}
		if network.NodeManagementClosed(node.LifecycleStatus) {
			continue
		}
		row := model.NodeObservationMailbox{NodeID: id, AssetTag: node.AssetTag, Observation: "{}", UpdatedAt: clockNow()}
		if e := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; e != nil {
			return e
		}
		if e := tx.Select("node_id,asset_tag,substr(observation,1,4097) AS observation,pending,pending_since,reason,updated_at").Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, id).Error; e != nil {
			return e
		}
		var merged network.NodeObservation
		if row.AssetTag == node.AssetTag {
			var e error
			merged, e = decodeProjection(row.Observation)
			if e != nil || (merged.NodeID != 0 && (merged.NodeID != id || merged.Latest.AssetTag != node.AssetTag)) {
				// Current observations are replaceable, never accounting authority.
				// The worker still compares the replacement against applied state.
				merged = network.NodeObservation{}
				row.Reason = "invalid_mailbox"
			}
		}
		changed := false
		reason := row.Reason
		for _, o := range groups[id] {
			if o.Latest.AssetTag != node.AssetTag {
				continue
			}
			if o.StatsEvent != nil && o.StatsEvent.AssetTag != node.AssetTag {
				o.StatsEvent = nil
			}
			if o.StatsEvent != nil && (merged.StatsEvent == nil || network.EventNewerThanPosition(*o.StatsEvent, *merged.StatsEvent)) {
				reason = ""
			}
			c, e := mergeObservation(&merged, o)
			if e != nil {
				reason = "stats_position_conflict"
				c = true
			}
			changed = changed || c
		}
		if !changed {
			continue
		}
		body, e := encodeProjection(merged)
		if e != nil {
			return e
		}
		since := row.PendingSince
		if !row.Pending {
			since = clockNow()
		}
		if e = tx.Model(&row).Updates(map[string]any{"asset_tag": node.AssetTag, "observation": body, "pending": true, "pending_since": since, "reason": reason, "updated_at": clockNow()}).Error; e != nil {
			return e
		}
	}
	return nil
}

func (w Worker) drainMailboxes(ctx context.Context, limit int, budget *int64) (int, error) {
	done := 0
	var failures error
	var candidates []model.NodeObservationMailbox
	if e := w.DB.WithContext(ctx).Select("node_id").Where("pending = ?", true).Order("pending_since,node_id").Limit(limit).Find(&candidates).Error; e != nil {
		return 0, e
	}
	for _, candidate := range candidates {
		var e error
		processed := false
		e = transaction(w.DB.WithContext(ctx), func(tx *gorm.DB) error {
			// Producer already owns accounting's node SHARE locks before mailbox
			// writes. The consumer owns node first, then mailbox, avoiding inversion.
			var node model.Node
			if e := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).First(&node, candidate.NodeID).Error; e != nil {
				return e
			}
			var row model.NodeObservationMailbox
			if e := tx.Select("node_id,asset_tag,substr(observation,1,4097) AS observation,pending,pending_since,reason,updated_at").Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, candidate.NodeID).Error; e != nil {
				return e
			}
			if !row.Pending {
				return nil
			}
			ack := func(status, reason string) error {
				e := tx.Model(&row).Updates(map[string]any{"pending": false, "reason": reason, "updated_at": clockNow()}).Error
				processed = e == nil
				return e
			}
			if row.AssetTag != node.AssetTag || !node.IsEnabled || node.NodeCredentialRevokedAt != nil || network.NodeManagementClosed(node.LifecycleStatus) {
				return ack("held", "asset_inactive")
			}
			o, e := decodeProjection(row.Observation)
			if e != nil || o.NodeID != node.ID || o.Latest.AssetTag != node.AssetTag {
				return ack("held", "invalid_mailbox")
			}
			sum := sha256.Sum256([]byte(row.Observation))
			h := model.NodeObservationIntent{ID: hex.EncodeToString(sum[:]), NodeID: node.ID, AssetTag: node.AssetTag}
			load := func(id string) (network.ObservationSource, error) {
				var old model.NodeObservationIntent
				if e := tx.First(&old, "id = ?", id).Error; e != nil {
					return network.ObservationSource{}, e
				}
				if old.MaterialBytes > uint64(MaxApplyBytes) {
					return network.ObservationSource{}, invalid("legacy reference too large")
				}
				if old.MaterialBytes > uint64(*budget) {
					return network.ObservationSource{}, errBudget
				}
				*budget -= int64(old.MaterialBytes)
				return readSource(tx, old)
			}
			kind := "compact"
			if o.StatsEvent != nil {
				kind = "stats.sampled"
			}
			conflict := row.Reason == "stats_position_conflict"
			if conflict {
				o.StatsEvent = nil
				kind = "compact"
			}
			if e := applyProjection(tx, h, network.ObservationSource{Observation: o, Kind: kind}, load, true, ack); e != nil {
				return e
			}
			if conflict {
				return tx.Model(&row).Update("reason", "stats_position_conflict").Error
			}
			return nil
		})
		if e != nil {
			if errors.Is(e, errBudget) {
				break
			}
			if errors.Is(e, gorm.ErrRecordNotFound) {
				continue
			}
			failures = errors.Join(failures, e)
			if ctx.Err() != nil {
				return done, failures
			}
			continue
		}
		if processed {
			done++
		}
	}
	return done, failures
}

// A ready mailbox is independent of optional diagnostic retention and old
// material capacity. Busy legacy sources cannot starve current node state.
func (w Worker) hasMailboxes(ctx context.Context) (bool, error) {
	var row struct{ NodeID uint }
	r := w.DB.WithContext(ctx).Model(&model.NodeObservationMailbox{}).Select("node_id").Where("pending = ?", true).Limit(1).Scan(&row)
	return r.RowsAffected > 0, r.Error
}

func projectionReference(kind string, o network.NodeObservation) string {
	body, _ := encodeProjection(o)
	h := sha256.Sum256([]byte(kind + ":" + body))
	return hex.EncodeToString(h[:])
}
