package networkstore

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"gorm.io/gorm/clause"
	"time"

	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type PublicationQueue struct{ DB *gorm.DB }

func (s PublicationQueue) Pending(ctx context.Context, now time.Time) (bool, error) {
	var row struct{ Present int }
	err := s.DB.WithContext(ctx).Model(&model.NodeConfigPublish{}).
		Where("suspended = ? AND lease_until <= ? AND next_attempt_at <= ?", false, now, now).
		Select("1 AS present").Limit(1).Scan(&row).Error
	return row.Present == 1, err
}

func (s PublicationQueue) Claim(ctx context.Context, now time.Time) (network.Publication, bool, error) {
	db := s.DB.WithContext(ctx)
	for attempt := 0; attempt < 4; attempt++ {
		var item model.NodeConfigPublish
		read := db.Where("suspended = ? AND next_attempt_at <= ? AND lease_until <= ?", false, now, now).
			Order("next_attempt_at, node_id").Limit(1).Find(&item)
		if read.Error != nil {
			return network.Publication{}, false, read.Error
		}
		if read.RowsAffected == 0 {
			return network.Publication{}, false, nil
		}
		token := uuid.NewString()
		until := now.Add(network.PublicationLease)
		result := db.Model(&model.NodeConfigPublish{}).
			Where("node_id = ? AND generation = ? AND suspended = ? AND lease_until <= ? AND next_attempt_at <= ?", item.NodeID, item.Generation, false, now, now).
			Updates(map[string]interface{}{"lease_token": token, "lease_until": until})
		if result.Error != nil {
			return network.Publication{}, false, result.Error
		}
		if result.RowsAffected == 1 {
			item.LeaseToken, item.LeaseUntil = token, until
			return publicationFromModel(item), true, nil
		}
	}
	return network.Publication{}, false, nil
}

func (s PublicationQueue) Renew(ctx context.Context, item network.Publication, until time.Time) (bool, error) {
	result := s.DB.WithContext(ctx).Model(&model.NodeConfigPublish{}).
		Where("node_id = ? AND lease_token = ?", item.NodeID, item.LeaseToken).
		Update("lease_until", until)
	return result.RowsAffected == 1, result.Error
}

func (s PublicationQueue) Complete(ctx context.Context, item network.Publication, now time.Time, failure error) error {
	db := s.DB.WithContext(ctx)
	owned := db.Where("node_id = ? AND lease_token = ? AND generation = ?", item.NodeID, item.LeaseToken, item.Generation)
	message := ""
	if failure != nil {
		runes := []rune(failure.Error())
		if len(runes) > 1000 {
			runes = runes[:1000]
		}
		message = string(runes)
	}
	var result *gorm.DB
	if failure == nil {
		completionErr := db.Transaction(func(tx *gorm.DB) error {
			var held model.NodeConfigPublish
			find := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("node_id = ? AND lease_token = ? AND generation = ?", item.NodeID, item.LeaseToken, item.Generation).Limit(1).Find(&held)
			if find.Error != nil {
				return find.Error
			}
			if find.RowsAffected == 0 {
				return nil
			}
			if held.Phase == "connections" {
				detail, _ := json.Marshal(map[string]any{"generation": held.Generation, "applied_config_sha256": held.AppliedConfigSHA256, "scope": "native_managed_principals", "confirmation": "same_connection_active_queries"})
				if err := tx.Create(&model.AuditLog{Actor: "node_publish_worker", Action: "node.connection_revocation_verified", Target: fmt.Sprint(item.NodeID), Detail: string(detail), CreatedAt: now}).Error; err != nil {
					return err
				}
			}
			return tx.Where("node_id = ? AND lease_token = ? AND generation = ?", item.NodeID, item.LeaseToken, item.Generation).Delete(&model.NodeConfigPublish{}).Error
		})
		if completionErr != nil {
			return completionErr
		}
		return db.Model(&model.NodeConfigPublish{}).Where("node_id = ? AND lease_token = ?", item.NodeID, item.LeaseToken).Updates(map[string]interface{}{"lease_token": "", "lease_until": publicationIdleLease, "next_attempt_at": now}).Error

	} else if errors.Is(failure, network.ErrConnectionRevocationPending) {
		result = owned.Model(&model.NodeConfigPublish{}).Updates(map[string]interface{}{
			"last_error": message, "next_attempt_at": now.Add(network.PublicationRecheckInterval), "lease_token": "", "lease_until": publicationIdleLease,
		})
	} else if errors.Is(failure, network.ErrConfigurationOutcomeUnknown) {
		result = owned.Model(&model.NodeConfigPublish{}).Updates(map[string]interface{}{
			"reconciling": true, "suspended": false, "last_error": message,
			"next_attempt_at": now.Add(network.PublicationRecheckInterval), "lease_token": "", "lease_until": publicationIdleLease,
		})
	} else if errors.Is(failure, network.ErrKernelOperationRunning) {
		result = owned.Model(&model.NodeConfigPublish{}).Updates(map[string]interface{}{
			"next_attempt_at": now.Add(network.PublicationRecheckInterval), "lease_token": "", "lease_until": publicationIdleLease,
		})
	} else if network.IsConfigurationCapabilityDeferral(failure) {
		result = owned.Model(&model.NodeConfigPublish{}).Updates(map[string]interface{}{
			"deferred": true, "reconciling": false, "suspended": false, "last_error": message,
			"next_attempt_at": now.Add(network.PublicationCapabilityRecheck), "lease_token": "", "lease_until": publicationIdleLease,
		})
	} else {
		attempts := item.Attempts + 1
		if attempts > network.PublicationFailureLimit {
			attempts = network.PublicationFailureLimit
		}
		result = owned.Model(&model.NodeConfigPublish{}).Updates(map[string]interface{}{
			"deferred": false, "reconciling": false, "suspended": attempts >= network.PublicationFailureLimit,
			"attempts": attempts, "last_error": message, "next_attempt_at": now.Add(network.PublicationRetryDelay(item.Attempts)),
			"lease_token": "", "lease_until": publicationIdleLease,
		})
	}
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 0 {
		return nil
	}
	// A newer generation arrived during execution. Release only our own lease;
	// an old acknowledgement must never erase a newer worker's claim.
	return db.Model(&model.NodeConfigPublish{}).Where("node_id = ? AND lease_token = ?", item.NodeID, item.LeaseToken).
		Updates(map[string]interface{}{"lease_token": "", "lease_until": publicationIdleLease, "next_attempt_at": now}).Error
}

var publicationIdleLease = time.Unix(0, 0).UTC()

func publicationFromModel(item model.NodeConfigPublish) network.Publication {
	return network.Publication{
		NodeID: item.NodeID, EndpointID: item.EndpointID, RequestedBy: item.RequestedBy,
		Phase: item.Phase, AppliedConfigSHA256: item.AppliedConfigSHA256,
		Generation: item.Generation, Attempts: item.Attempts, LastError: item.LastError,
		Deferred: item.Deferred, Reconciling: item.Reconciling, Suspended: item.Suspended,
		NextAttemptAt: item.NextAttemptAt, LeaseUntil: item.LeaseUntil, LeaseToken: item.LeaseToken,
	}
}

// MarkConfigurationApplied advances only this lease and generation. A newer
// business intent must retain its configuration phase and cannot be erased.
func (s PublicationQueue) MarkConfigurationApplied(ctx context.Context, item network.Publication, sha string) (bool, error) {
	decoded, decodeErr := hex.DecodeString(sha)
	if decodeErr != nil || len(decoded) != 32 {
		return false, network.ErrConfigurationPublicationCommit
	}
	result := s.DB.WithContext(ctx).Model(&model.NodeConfigPublish{}).Where("node_id = ? AND lease_token = ? AND generation = ?", item.NodeID, item.LeaseToken, item.Generation).Updates(map[string]any{"phase": "connections", "applied_config_sha256": sha, "reconciling": false, "deferred": false, "attempts": 0, "last_error": ""})
	return result.RowsAffected == 1, result.Error
}
