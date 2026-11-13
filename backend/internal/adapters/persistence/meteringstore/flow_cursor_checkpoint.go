package meteringstore

import (
	"context"
	"errors"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const FlowCursorHotHorizon = 24 * time.Hour

func compactFlowCursor(row model.FlowUsage) model.FlowCursorCheckpoint {
	return model.FlowCursorCheckpoint{NodeID: row.NodeID, FlowID: row.FlowID, ProtocolCredentialID: row.ProtocolCredentialID,
		Revision: row.Revision, RawBytes: row.RawBytes, UploadBytes: row.UploadBytes, DownloadBytes: row.DownloadBytes, UsedBytes: row.UsedBytes, IsCompleted: row.Status == "completed"}
}

func checkpointFlowUsage(row model.FlowCursorCheckpoint) model.FlowUsage {
	status := "active"
	if row.IsCompleted {
		status = "completed"
	}
	return model.FlowUsage{NodeID: row.NodeID, FlowID: row.FlowID, ProtocolCredentialID: row.ProtocolCredentialID,
		Revision: row.Revision, RawBytes: row.RawBytes, UploadBytes: row.UploadBytes, DownloadBytes: row.DownloadBytes, UsedBytes: row.UsedBytes, Status: status}
}

// Both tables use the original database identity collation. Lookup and transfer
// happen under the caller's accounting transaction, after attribution/quota locks.
func loadFlowUsageCandidates(tx *gorm.DB, node uint, keys []string) ([]model.FlowUsage, error) {
	if err := lockIdentityReader(tx, node); err != nil {
		return nil, err
	}
	var hot []model.FlowUsage
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("node_id = ? AND flow_id IN ?", node, keys).Order("flow_id").Find(&hot).Error; err != nil {
		return nil, err
	}
	var cold []model.FlowCursorCheckpoint
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("node_id = ? AND flow_id IN ?", node, keys).Order("flow_id").Find(&cold).Error; err != nil {
		return nil, err
	}
	sealed, err := coldFloorCandidates(tx, node, keys)
	if err != nil {
		return nil, err
	}
	if len(sealed) > 0 && len(cold) > 0 {
		for _, r := range sealed {
			var n int64
			if err := tx.Model(&model.FlowCursorCheckpoint{}).Where("node_id=? AND flow_id=?", node, r.FlowID).Count(&n).Error; err != nil {
				return nil, err
			}
			if n > 0 {
				return nil, errors.New("duplicate compact/sealed floor authority")
			}
		}
	}
	cold = append(cold, sealed...)
	if len(hot) > 0 && len(cold) > 0 {
		// Compare through the database column, including case/accent/padding
		// equivalence. Go string equality cannot certify identity disjointness.
		coldKeys := make([]string, 0, len(cold))
		for _, row := range cold {
			coldKeys = append(coldKeys, row.FlowID)
		}
		var duplicate struct{ ID uint }
		r := tx.Model(&model.FlowUsage{}).Select("id").Where("node_id = ? AND flow_id IN ?", node, coldKeys).Limit(1).Find(&duplicate)
		if r.Error != nil {
			return nil, r.Error
		}
		if r.RowsAffected > 0 {
			return nil, errors.New("duplicate hot/checkpoint flow accounting authority")
		}
	}
	for _, row := range cold {
		hot = append(hot, checkpointFlowUsage(row))
	}
	return hot, nil
}

// A completion can legitimately increase an arbitrarily old cumulative floor.
// Rehydrate only on a write; ordinary duplicate/stale reports cause no hot insert.
func saveFlowUsage(tx *gorm.DB, row *model.FlowUsage, found bool, previousKey string) error {
	if found && row.ID == 0 {
		removed := tx.Where("node_id = ? AND flow_id = ?", row.NodeID, previousKey).Delete(&model.FlowCursorCheckpoint{})
		if removed.Error != nil {
			return removed.Error
		}
		if removed.RowsAffected != 1 {
			found, err := takeColdFloor(tx, row.NodeID, previousKey)
			if err != nil {
				return err
			}
			if !found {
				return errors.New("flow checkpoint accounting floor disappeared during update")
			}
		}
	}
	if found && row.ID != 0 && previousKey == row.FlowID {
		return tx.Save(row).Error
	}
	// An alias can compare equal in MySQL while PickFlowUsage intentionally
	// does not accept it as the exact original Go identity. The old hot unique
	// key rejected that insert. Preserve that rejection across the two tables,
	// including a legacy-to-runtime rename, before a new hot row can commit.
	var conflict model.FlowCursorCheckpoint
	r := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("node_id,flow_id").Where("node_id = ? AND flow_id = ?", row.NodeID, row.FlowID).Limit(1).Find(&conflict)
	if r.Error != nil {
		return r.Error
	}
	if r.RowsAffected > 0 {
		return errors.New("flow identity already has a compact accounting floor")
	}
	if _, ok, err := lookupColdIdentity(tx, row.NodeID, coldFloor, row.FlowID, true); err != nil {
		return err
	} else if ok {
		return errors.New("flow identity already has sealed accounting authority")
	}
	if found && row.ID != 0 {
		return tx.Save(row).Error
	}
	return tx.Create(row).Error
}

type FlowCursorCompaction struct {
	DB  *gorm.DB
	Now func() time.Time
}

func (s FlowCursorCompaction) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s FlowCursorCompaction) HasPending(ctx context.Context) (bool, error) {
	var state model.FlowCursorCompactionState
	err := s.DB.WithContext(ctx).First(&state, 1).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return true, nil
	}
	return err == nil && !s.now().Before(state.NextPassAt), err
}

// The batch bound covers examined rows, not just successful transfers. There is
// no startup scan, age-based financial deletion, global admission mutex or disk
// free-space floor that could permanently starve retention on a nearly full disk.
func (s FlowCursorCompaction) Drain(ctx context.Context, limit int) (int, error) {
	if ctx == nil || s.DB == nil || limit < 1 || limit > 1000 {
		return 0, errors.New("flow cursor compaction requires 1..1000 rows")
	}
	// MySQL DATETIME(3) rounds fractional milliseconds on persistence. Keep
	// scheduling comparisons at that precision so the initial due time cannot
	// round into the future and skip the first batch.
	now := s.now().Truncate(time.Millisecond)
	examined := 0
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		initial := model.FlowCursorCompactionState{ID: 1, NextPassAt: now}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&initial).Error; err != nil {
			return err
		}
		var state model.FlowCursorCompactionState
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&state, 1).Error; err != nil {
			return err
		}
		if now.Before(state.NextPassAt) {
			return nil
		}
		var rows []model.FlowUsage
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id > ?", state.AfterID).Order("id").Limit(limit).Find(&rows).Error; err != nil {
			return err
		}
		examined = len(rows)
		for _, row := range rows {
			state.AfterID = row.ID
			cold := row.Status == "active" && !row.UpdatedAt.IsZero() && row.UpdatedAt.Before(now.Add(-FlowCursorHotHorizon))
			if row.Status == "completed" {
				at := row.UpdatedAt
				if row.CompletedAt != nil {
					at = *row.CompletedAt
				}
				cold = !at.IsZero() && at.Before(now.Add(-FlowCursorHotHorizon))
			}
			if !cold {
				continue
			}
			want := compactFlowCursor(row)
			var existing model.FlowCursorCheckpoint
			got := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("node_id = ? AND flow_id = ?", row.NodeID, row.FlowID).Limit(1).Find(&existing)
			if got.Error != nil {
				return got.Error
			}
			if got.RowsAffected > 0 {
				return errors.New("flow checkpoint identity already has accounting authority; compaction held")
			}
			if err := tx.Create(&want).Error; err != nil {
				return err
			}
			if err := tx.Where("id = ?", row.ID).Delete(&model.FlowUsage{}).Error; err != nil {
				return err
			}
		}
		if len(rows) < limit {
			state.AfterID = 0
			state.NextPassAt = now.Add(time.Hour)
		}
		return tx.Save(&state).Error
	})
	if err != nil {
		return 0, err
	}
	return examined, nil
}
