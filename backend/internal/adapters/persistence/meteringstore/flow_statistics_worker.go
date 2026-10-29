package meteringstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type FlowStatistics struct{ DB *gorm.DB }

func (s FlowStatistics) HasPending(ctx context.Context) (bool, error) {
	var state model.FlowStartStatisticsState
	if err := s.DB.WithContext(ctx).First(&state, "id=?", 1).Error; err != nil {
		return false, err
	}
	if state.LegacyAfterID < state.LegacyUpperID {
		return true, nil
	}
	var id struct{ SourceID uint64 }
	q := s.DB.WithContext(ctx).Model(&model.FlowStartStatisticsPending{}).Select("source_id").Limit(1).Find(&id)
	if q.Error != nil || q.RowsAffected > 0 {
		return q.RowsAffected > 0, q.Error
	}
	var source struct{ ID uint64 }
	raw := s.DB.WithContext(ctx).Model(&FlowStartRecord{}).Select("id").Where("statistics_registered=?", false).Order("id").Limit(1).Find(&source)
	return raw.RowsAffected > 0, raw.Error
}

type flowMinuteKey struct {
	Subscription, Node uint
	Basis              string
	At                 time.Time
}

const flowPointColumns = "source_id,subscription_id,node_id,occurred_at,received_at,source_created_at,review_at,substr(fact_sha256,1,65) AS fact_sha256"

func (s FlowStatistics) Drain(ctx context.Context, limit int) (int, error) {
	if s.DB == nil || ctx == nil || limit < 1 || limit > flowStatisticsLimit {
		return 0, errors.New("flow statistics batch must be1..200")
	}
	processed := 0
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var state model.FlowStartStatisticsState
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&state, "id=?", 1).Error; err != nil {
			return err
		}
		if state.LegacyAfterID > state.LegacyUpperID || state.LegacyProjectedRows > state.LegacyExpectedRows {
			return errors.New("flow legacy coverage is inconsistent")
		}
		legacy := state.LegacyAfterID < state.LegacyUpperID
		var ids []uint64
		if legacy {
			if err := tx.Model(&FlowStartRecord{}).Where("id>? AND id<=?", state.LegacyAfterID, state.LegacyUpperID).Order("id").Limit(limit).Pluck("id", &ids).Error; err != nil {
				return err
			}
		} else {
			if err := tx.Model(&model.FlowStartStatisticsPending{}).Order("source_id").Limit(limit).Pluck("source_id", &ids).Error; err != nil {
				return err
			}
		}

		if !legacy && len(ids) < limit {
			var repair []uint64
			query := tx.Model(&FlowStartRecord{}).Where("statistics_registered=?", false)
			if len(ids) > 0 {
				query = query.Where("id NOT IN ?", ids)
			}
			if err := query.Order("id").Limit(limit-len(ids)).Pluck("id", &repair).Error; err != nil {
				return err
			}
			ids = append(ids, repair...)
			sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		}
		if len(ids) == 0 {
			if legacy {
				if state.LegacyProjectedRows != state.LegacyExpectedRows {
					return errors.New("captured flow legacy source count no longer reconciles")
				}
				return tx.Model(&state).Updates(map[string]any{"legacy_after_id": state.LegacyUpperID, "updated_at": time.Now().UTC()}).Error
			}
			return nil
		}
		// Fixed-width source reads only. Acquire every raw lock before pending and
		// points, matching one-source writers; no Core/event body is transferred.
		var sources []FlowStartRecord
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select(flowStatisticsSourceColumns).Where("id IN ?", ids).Order("id").Find(&sources).Error; err != nil {
			return err
		}
		if len(sources) != len(ids) {
			return errors.New("flow pending/backfill source missing")
		}
		var pending []model.FlowStartStatisticsPending
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("source_id IN ?", ids).Order("source_id").Find(&pending).Error; err != nil {
			return err
		}
		byPending := map[uint64]model.FlowStartStatisticsPending{}
		for _, p := range pending {
			byPending[p.SourceID] = p
		}
		var points []model.FlowStartStatisticsPoint
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select(flowPointColumns).Where("source_id IN ?", ids).Order("source_id").Find(&points).Error; err != nil {
			return err
		}
		known := map[uint64]model.FlowStartStatisticsPoint{}
		for _, p := range points {
			known[p.SourceID] = p
		}
		var insert []model.FlowStartStatisticsPoint
		delta := map[flowMinuteKey]uint64{}
		var clear []uint64
		for i, r := range sources {
			if r.ID != ids[i] || r.ID == 0 || r.OccurredAt.IsZero() || r.ReceivedAt.IsZero() || r.CreatedAt.IsZero() {
				return errors.New("flow source metadata invalid")
			}
			if p, ok := byPending[r.ID]; ok {
				if p.NodeID != r.NodeID || p.SubscriptionID != r.SubscriptionID || !p.OccurredAt.Equal(r.OccurredAt) || !p.ReceivedAt.Equal(r.ReceivedAt) {
					return errors.New("flow pending source changed")
				}
				clear = append(clear, r.ID)
			} else if !legacy && r.StatisticsRegistered {
				if p, ok := known[r.ID]; !ok || !flowStatisticsPointMatches(p, r) {
					return errors.New("flow pending vanished without verified point")
				}
			}
			if p, ok := known[r.ID]; ok {
				if !flowStatisticsPointMatches(p, r) {
					return errors.New("flow projected source changed")
				}
			} else {
				insert = append(insert, model.FlowStartStatisticsPoint{SourceID: r.ID, NodeID: r.NodeID, SubscriptionID: r.SubscriptionID, OccurredAt: r.OccurredAt, ReceivedAt: r.ReceivedAt, SourceCreatedAt: r.CreatedAt, ReviewAt: r.ReceivedAt.Add(metering.ObservationRetentionHorizon), FactSHA256: flowStatisticsDigest(r)})
				for basis, at := range map[string]time.Time{"occurred": r.OccurredAt, "received": r.ReceivedAt} {
					delta[flowMinuteKey{r.SubscriptionID, r.NodeID, basis, at.UTC().Truncate(time.Minute)}]++
				}
			}
			if legacy {
				state.LegacyAfterID = r.ID
				state.LegacyProjectedRows++
			}
			if r.ID > state.ProjectedHead {
				state.ProjectedHead = r.ID
			}
			processed++
		}
		if len(insert) > 0 {
			if err := tx.CreateInBatches(&insert, 100).Error; err != nil {
				return err
			}
		}
		keys := make([]flowMinuteKey, 0, len(delta))
		for key := range delta {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool {
			a, b := keys[i], keys[j]
			if a.Subscription != b.Subscription {
				return a.Subscription < b.Subscription
			}
			if a.Node != b.Node {
				return a.Node < b.Node
			}
			if a.Basis != b.Basis {
				return a.Basis < b.Basis
			}
			return a.At.Before(b.At)
		})
		for _, key := range keys {
			if err := flowMinuteDelta(tx, key, delta[key], false); err != nil {
				return err
			}
		}

		if err := tx.Model(&FlowStartRecord{}).Where("id IN ? AND statistics_registered=?", ids, false).UpdateColumn("statistics_registered", true).Error; err != nil {
			return err
		}
		if len(clear) > 0 {
			q := tx.Where("source_id IN ?", clear).Delete(&model.FlowStartStatisticsPending{})
			if q.Error != nil {
				return q.Error
			}
			if q.RowsAffected != int64(len(clear)) {
				return errors.New("flow checkpoint ownership changed")
			}
		}
		if legacy && len(ids) < limit {
			if state.LegacyProjectedRows != state.LegacyExpectedRows {
				return errors.New("flow legacy source count mismatch")
			}
			state.LegacyAfterID = state.LegacyUpperID
		}
		if state.LegacyAfterID == state.LegacyUpperID && state.LegacyProjectedRows != state.LegacyExpectedRows {
			return errors.New("flow coverage count mismatch")
		}
		if state.Revision >= math.MaxInt64 {
			return errors.New("flow statistics revision exhausted")
		}
		return tx.Model(&model.FlowStartStatisticsState{}).Where("id=?", 1).Updates(map[string]any{"legacy_after_id": state.LegacyAfterID, "legacy_projected_rows": state.LegacyProjectedRows, "projected_head": state.ProjectedHead, "revision": state.Revision + 1, "updated_at": time.Now().UTC()}).Error
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return 0, err
	}
	return processed, nil
}
func flowMinuteDelta(tx *gorm.DB, key flowMinuteKey, n uint64, subtract bool) error {
	var row model.FlowStartStatisticsMinute
	q := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("subscription_id=? AND node_id=? AND basis=? AND minute_at=?", key.Subscription, key.Node, key.Basis, key.At).Limit(1).Find(&row)
	if q.Error != nil {
		return q.Error
	}
	if q.RowsAffected == 0 {
		if subtract {
			return errors.New("flow minute receipt missing before retention")
		}
		return tx.Create(&model.FlowStartStatisticsMinute{SubscriptionID: key.Subscription, NodeID: key.Node, Basis: key.Basis, MinuteAt: key.At, EventCount: n}).Error
	}
	if n == 0 || row.EventCount == 0 || row.EventCount > math.MaxInt64 {
		return errors.New("flow minute count corrupt")
	}
	query := tx.Model(&model.FlowStartStatisticsMinute{}).Where("subscription_id=? AND node_id=? AND basis=? AND minute_at=?", key.Subscription, key.Node, key.Basis, key.At)
	if subtract {
		if row.EventCount < n {
			return errors.New("flow minute count underflow")
		}
		if row.EventCount == n {
			return query.Delete(&model.FlowStartStatisticsMinute{}).Error
		}
		return query.Update("event_count", row.EventCount-n).Error
	}
	if n > math.MaxInt64-row.EventCount {
		return fmt.Errorf("flow minute count overflow")
	}
	return query.Update("event_count", row.EventCount+n).Error
}
