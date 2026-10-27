package networkstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s ObservationIntentOperations) RetryObservationIntent(ctx context.Context, actor uint, id string, in network.ObservationIntentRetryRequest) (out network.ObservationIntentRetryReceipt, err error) {
	in, attempts, err := in.Validate(id)
	if err != nil {
		return out, err
	}
	if s.DB == nil {
		return out, network.ErrNodeAdministrationUnavailable
	}
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		admin, err := nodeAdministrator(tx, actor)
		if err != nil {
			return err
		}
		var h model.NodeObservationIntent
		if err := tx.First(&h, "id = ?", id).Error; err != nil {
			return nodeAdministrationError(err)
		}
		// The inspected attempt can be admitted once. A response loss or concurrent
		// submission returns a conflict thereafter, never a second requeue/audit.
		if tx.Dialector.Name() == "sqlite" && h.Attempts >= math.MaxInt64 {
			return network.ErrObservationIntentRetryConflict
		}
		if h.Status != "held" || h.Reason != "node_inactive" || h.Attempts != attempts || h.Token != "" || h.AssetTag == "" {
			return network.ErrObservationIntentRetryConflict
		}
		var node model.Node
		err = tx.Clauses(clause.Locking{Strength: "SHARE"}).First(&node, h.NodeID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return network.ErrObservationIntentRetryConflict
		}
		if err != nil {
			return err
		}
		if node.AssetTag != h.AssetTag || !node.IsEnabled || node.NodeCredentialRevokedAt != nil || node.LifecycleStatus == "deleting" {
			return network.ErrObservationIntentRetryConflict
		}
		var state model.NodeObservationState
		err = tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("node_id = ? AND asset_tag = ?", h.NodeID, h.AssetTag).First(&state).Error
		if err != nil {
			return err
		}
		if state.Deleted {
			return network.ErrObservationIntentRetryConflict
		}
		// Retention now also handles held material. Follow node -> state ->
		// header, then revalidate every retry predicate using a current read.
		var current model.NodeObservationIntent
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "id = ?", id).Error; err != nil {
			return err
		}
		if current.NodeID != h.NodeID || current.AssetTag != h.AssetTag || current.Status != "held" || current.Reason != "node_inactive" || current.Attempts != attempts || current.Token != "" || current.MaterialRetiredAt != nil || !current.LeaseUntil.Equal(time.Unix(0, 0).UTC()) {
			return network.ErrObservationIntentRetryConflict
		}
		h = current
		now := time.Now().UTC().Truncate(time.Millisecond)
		result := tx.Model(&model.NodeObservationIntent{}).Where("id = ? AND status = ? AND reason = ? AND attempts = ? AND token = ? AND material_retired_at IS NULL", id, "held", "node_inactive", attempts, "").Updates(map[string]any{"status": "pending", "reason": "administrator_retry", "available_at": now, "lease_until": time.Unix(0, 0).UTC(), "updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return network.ErrObservationIntentRetryConflict
		}
		detail, err := json.Marshal(struct {
			Version          int    `json:"version"`
			IntentID         string `json:"intent_id"`
			ExpectedAttempts string `json:"expected_attempts"`
			Reason           string `json:"reason"`
		}{1, id, in.ExpectedAttempts, in.Reason})
		if err != nil {
			return err
		}
		audit := model.AuditLog{UserID: &admin.ID, Actor: admin.Email, Action: "node.observation.retry", Target: fmt.Sprintf("observation:%s:%s", id, in.ExpectedAttempts), Detail: string(detail), CreatedAt: now}
		if err := tx.Create(&audit).Error; err != nil {
			return err
		}
		out = network.ObservationIntentRetryReceipt{ID: id, ExpectedAttempts: in.ExpectedAttempts, AuditID: audit.ID, Status: "accepted"}
		return nil
	})
	if err != nil {
		return network.ObservationIntentRetryReceipt{}, err
	}
	return out, nil
}
