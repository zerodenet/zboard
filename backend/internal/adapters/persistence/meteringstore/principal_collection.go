package meteringstore

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strings"
	"time"
)

type PrincipalCollection struct{ DB *gorm.DB }

func (s PrincipalCollection) Boundary(ctx context.Context, nodeID uint, event metering.PrincipalEvent, stopped bool) error {
	instanceID := strings.TrimSpace(event.CoreInstanceID)
	if nodeID == 0 || instanceID == "" {
		return nil
	}
	observedAt := event.ObservedAt.UTC()
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var generation PrincipalFlowNodeGeneration
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("node_id = ?", nodeID).First(&generation).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if stopped {
			if errors.Is(err, gorm.ErrRecordNotFound) || generation.CoreInstanceID != instanceID || generation.ClosedAt != nil {
				return nil
			}
			if err := ResetNodeProjection(tx, nodeID, instanceID, event.EventID, observedAt, "engine_stopped"); err != nil {
				return err
			}
			return tx.Model(&PrincipalFlowNodeGeneration{}).Where("node_id = ?", nodeID).Updates(map[string]interface{}{
				"closed_at":  observedAt,
				"updated_at": time.Now().UTC(),
			}).Error
		}

		if err == nil {
			if !metering.ShouldReplaceGeneration(generation.CoreInstanceID, generation.StartedAt, instanceID, observedAt) {
				return nil
			}
			if err := ResetNodeProjection(tx, nodeID, instanceID, event.EventID, observedAt, "generation_reset"); err != nil {
				return err
			}
		}
		next := PrincipalFlowNodeGeneration{
			NodeID:         nodeID,
			CoreInstanceID: instanceID,
			StartedAt:      observedAt,
			UpdatedAt:      time.Now().UTC(),
		}
		return tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "node_id"}},
			DoUpdates: clause.Assignments(map[string]interface{}{
				"core_instance_id": instanceID,
				"started_at":       observedAt,
				"closed_at":        nil,
				"updated_at":       next.UpdatedAt,
			}),
		}).Create(&next).Error
	})
}

func (s PrincipalCollection) Observe(ctx context.Context, nodeID uint, event metering.PrincipalEvent, observation metering.PrincipalObservation) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		currentGeneration, err := ensureGeneration(tx, nodeID, event, observation.ObservedAt)
		if err != nil {
			return err
		}

		credential, credentialFound, err := ResolveCredential(tx, nodeID, observation.PrincipalKey)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		raw := PrincipalFlowObservation{
			NodeID:                  nodeID,
			CoreInstanceID:          strings.TrimSpace(event.CoreInstanceID),
			SessionRegistryRevision: observation.SessionRegistryRevision,
			EventID:                 strings.TrimSpace(event.EventID),
			Sequence:                event.Sequence,
			PrincipalKey:            observation.PrincipalKey,
			ActiveFlows:             observation.ActiveFlows,
			ObservedAt:              observation.ObservedAt,
			CreatedAt:               now,
		}
		if credentialFound {
			raw.UserID = credential.UserID
			raw.SubscriptionID = credential.SubscriptionID
			raw.ProtocolCredentialID = credential.ID
			raw.ProtocolEndpointID = credential.ProtocolEndpointID
		}
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&raw)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 || !currentGeneration {
			return nil
		}

		var current PrincipalFlowCurrent
		currentErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("node_id = ? AND principal_key = ?", nodeID, observation.PrincipalKey).
			First(&current).Error
		if currentErr != nil && !errors.Is(currentErr, gorm.ErrRecordNotFound) {
			return currentErr
		}
		if currentErr == nil && !metering.SupersedesPrincipalSnapshot(current.CoreInstanceID, current.SessionRegistryRevision, raw.CoreInstanceID, raw.SessionRegistryRevision) {
			return nil
		}
		next := PrincipalFlowCurrent{
			NodeID:                  nodeID,
			PrincipalKey:            observation.PrincipalKey,
			CoreInstanceID:          raw.CoreInstanceID,
			SessionRegistryRevision: raw.SessionRegistryRevision,
			UserID:                  raw.UserID,
			SubscriptionID:          raw.SubscriptionID,
			ProtocolCredentialID:    raw.ProtocolCredentialID,
			ProtocolEndpointID:      raw.ProtocolEndpointID,
			ActiveFlows:             raw.ActiveFlows,
			ObservedAt:              raw.ObservedAt,
			UpdatedAt:               now,
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "node_id"}, {Name: "principal_key"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"core_instance_id", "session_registry_revision", "user_id", "subscription_id",
				"protocol_credential_id", "protocol_endpoint_id", "active_flows", "observed_at", "updated_at",
			}),
		}).Create(&next).Error; err != nil {
			return err
		}
		if !credentialFound {
			return nil
		}
		if raw.UserID > 0 {
			if err := RefreshScope(tx, ScopeUser, raw.UserID, nodeID, raw.CoreInstanceID, raw.SessionRegistryRevision, raw.EventID, raw.ObservedAt, "lifecycle"); err != nil {
				return err
			}
		}
		if raw.SubscriptionID > 0 {
			if err := RefreshScope(tx, ScopeSubscription, raw.SubscriptionID, nodeID, raw.CoreInstanceID, raw.SessionRegistryRevision, raw.EventID, raw.ObservedAt, "lifecycle"); err != nil {
				return err
			}
		}
		return nil
	})
}

func ensureGeneration(tx *gorm.DB, nodeID uint, event metering.PrincipalEvent, observedAt time.Time) (bool, error) {
	instanceID := strings.TrimSpace(event.CoreInstanceID)
	if instanceID == "" {
		return false, nil
	}
	var generation PrincipalFlowNodeGeneration
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("node_id = ?", nodeID).First(&generation).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		next := PrincipalFlowNodeGeneration{NodeID: nodeID, CoreInstanceID: instanceID, StartedAt: observedAt, UpdatedAt: time.Now().UTC()}
		return true, tx.Create(&next).Error
	}
	if err != nil {
		return false, err
	}
	if !metering.GenerationAcceptsObservation(generation.CoreInstanceID, generation.StartedAt, generation.ClosedAt, instanceID, observedAt) {
		return false, nil
	}
	if generation.CoreInstanceID == instanceID {
		return true, nil
	}
	if err := ResetNodeProjection(tx, nodeID, instanceID, event.EventID, observedAt, "generation_reset"); err != nil {
		return false, err
	}
	if err := tx.Model(&PrincipalFlowNodeGeneration{}).Where("node_id = ?", nodeID).Updates(map[string]interface{}{
		"core_instance_id": instanceID,
		"started_at":       observedAt,
		"closed_at":        nil,
		"updated_at":       time.Now().UTC(),
	}).Error; err != nil {
		return false, err
	}
	return true, nil
}
