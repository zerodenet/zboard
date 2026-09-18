package networkstore

import (
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strings"
	"time"
)

func observationPosition(c ObservationCursor) network.EventPosition {
	return network.EventPosition{CoreInstanceID: c.CoreInstanceID, Sequence: c.Sequence, ConfigRevision: c.ConfigRevision, OccurredAt: c.OccurredAt}
}
func ProjectObservation(tx *gorm.DB, projection network.NodeObservation) error {
	var cursor ObservationCursor
	cursorErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("node_id = ?", projection.NodeID).First(&cursor).Error
	if cursorErr != nil && !errors.Is(cursorErr, gorm.ErrRecordNotFound) {
		return cursorErr
	}
	if cursorErr == nil && !network.EventNewerThanPosition(projection.Latest, observationPosition(cursor)) {
		return nil
	}

	eventOccurredAt := projection.Latest.OccurredAt.UTC()
	if eventOccurredAt.IsZero() {
		eventOccurredAt = time.Now().UTC()
	}
	now := time.Now().UTC()
	if projection.StatsEvent != nil && (cursorErr != nil || network.EventNewerThanPosition(*projection.StatsEvent, observationPosition(cursor))) {
		result := tx.Model(&model.Node{}).
			Where("id = ? AND is_enabled = ? AND node_credential_revoked_at IS NULL", projection.NodeID, true).
			Updates(map[string]interface{}{
				"active_flows": projection.Stats.ActiveSessions,
				"bytes_up":     projection.Stats.BytesUp,
				"bytes_down":   projection.Stats.BytesDown,
			})
		if result.Error != nil {
			return result.Error
		}
	}

	next := ObservationCursor{
		NodeID:         projection.NodeID,
		CoreInstanceID: strings.TrimSpace(projection.Latest.CoreInstanceID),
		Sequence:       projection.Latest.Sequence,
		ConfigRevision: projection.Latest.ConfigRevision,
		OccurredAt:     eventOccurredAt,
		UpdatedAt:      now,
	}
	if errors.Is(cursorErr, gorm.ErrRecordNotFound) {
		return tx.Create(&next).Error
	}
	return tx.Model(&ObservationCursor{}).Where("node_id = ?", projection.NodeID).Updates(map[string]interface{}{
		"core_instance_id": next.CoreInstanceID,
		"sequence":         next.Sequence,
		"config_revision":  next.ConfigRevision,
		"occurred_at":      next.OccurredAt,
		"updated_at":       next.UpdatedAt,
	}).Error
}
