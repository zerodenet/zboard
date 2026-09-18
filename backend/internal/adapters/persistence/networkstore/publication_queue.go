package networkstore

import (
	"context"
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
		Where("lease_until <= ? AND next_attempt_at <= ?", now, now).
		Select("1 AS present").Limit(1).Scan(&row).Error
	return row.Present == 1, err
}

func (s PublicationQueue) Claim(ctx context.Context, now time.Time) (network.Publication, bool, error) {
	db := s.DB.WithContext(ctx)
	for attempt := 0; attempt < 4; attempt++ {
		var item model.NodeConfigPublish
		read := db.Where("next_attempt_at <= ? AND lease_until <= ?", now, now).
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
			Where("node_id = ? AND generation = ? AND lease_until <= ? AND next_attempt_at <= ?", item.NodeID, item.Generation, now, now).
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
	var result *gorm.DB
	if failure == nil {
		result = owned.Delete(&model.NodeConfigPublish{})
	} else {
		attempts := item.Attempts + 1
		if attempts > 32 {
			attempts = 32
		}
		message := []rune(failure.Error())
		if len(message) > 1000 {
			message = message[:1000]
		}
		result = owned.Model(&model.NodeConfigPublish{}).Updates(map[string]interface{}{
			"attempts": attempts, "last_error": string(message), "next_attempt_at": now.Add(network.PublicationRetryDelay(item.Attempts)),
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
		Generation: item.Generation, Attempts: item.Attempts, LastError: item.LastError,
		NextAttemptAt: item.NextAttemptAt, LeaseUntil: item.LeaseUntil, LeaseToken: item.LeaseToken,
	}
}
