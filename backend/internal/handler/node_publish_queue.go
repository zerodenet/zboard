package handler

import (
	"time"

	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const nodePublishLease = 3 * time.Minute

var nodePublishIdleLease = time.Unix(0, 0).UTC()

func enqueueNodeConfigPublish(tx *gorm.DB, nodeID, endpointID, requestedBy uint) error {
	if nodeID == 0 {
		return nil
	}
	var entryNodes []uint
	if err := tx.Model(&model.NetworkEntry{}).Where("endpoint_id IN (?)", tx.Model(&model.ProtocolEndpoint{}).Select("id").Where("node_id = ?", nodeID)).Distinct().Pluck("node_id", &entryNodes).Error; err != nil {
		return err
	}
	for _, entryNode := range entryNodes {
		if entryNode != nodeID {
			if err := enqueueNodeConfigPublishOnly(tx, entryNode, 0, requestedBy); err != nil {
				return err
			}
		}
	}
	return enqueueNodeConfigPublishOnly(tx, nodeID, endpointID, requestedBy)
}

func enqueueNodeConfigPublishOnly(tx *gorm.DB, nodeID, endpointID, requestedBy uint) error {
	now := time.Now().UTC()
	item := model.NodeConfigPublish{NodeID: nodeID, EndpointID: endpointID, RequestedBy: requestedBy,
		Generation: 1, NextAttemptAt: now, LeaseUntil: nodePublishIdleLease}
	return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "node_id"}}, DoUpdates: clause.Assignments(map[string]interface{}{
		"endpoint_id": endpointID, "requested_by": requestedBy, "generation": gorm.Expr("generation + 1"),
		"attempts": 0, "last_error": "", "next_attempt_at": now, "updated_at": now,
	})}).Create(&item).Error
}

func enqueueSubscriptionConfigPublishes(tx *gorm.DB, subscriptionID, requestedBy uint) error {
	var nodes []struct {
		NodeID     uint
		EndpointID uint
	}
	if err := tx.Table("protocol_endpoints").Select("protocol_endpoints.node_id, MIN(protocol_endpoints.id) AS endpoint_id").
		Joins("JOIN node_group_endpoints ON node_group_endpoints.protocol_endpoint_id = protocol_endpoints.id").
		Joins("JOIN subscriptions ON subscriptions.node_group_id = node_group_endpoints.node_group_id").
		Where("subscriptions.id = ? AND protocol_endpoints.is_active = ?", subscriptionID, true).
		Group("protocol_endpoints.node_id").Order("protocol_endpoints.node_id").Scan(&nodes).Error; err != nil {
		return err
	}
	for _, node := range nodes {
		if err := enqueueNodeConfigPublish(tx, node.NodeID, node.EndpointID, requestedBy); err != nil {
			return err
		}
	}
	return nil
}

func claimNodeConfigPublish(db *gorm.DB, now time.Time) (model.NodeConfigPublish, bool, error) {
	for attempt := 0; attempt < 4; attempt++ {
		var item model.NodeConfigPublish
		read := db.Where("next_attempt_at <= ? AND lease_until <= ?", now, now).
			Order("next_attempt_at, node_id").Limit(1).Find(&item)
		if read.Error != nil {
			return item, false, read.Error
		}
		if read.RowsAffected == 0 {
			return item, false, nil
		}
		token := uuid.NewString()
		until := now.Add(nodePublishLease)
		result := db.Model(&model.NodeConfigPublish{}).Where("node_id = ? AND generation = ? AND lease_until <= ? AND next_attempt_at <= ?", item.NodeID, item.Generation, now, now).
			Updates(map[string]interface{}{"lease_token": token, "lease_until": until})
		if result.Error != nil {
			return item, false, result.Error
		}
		if result.RowsAffected == 1 {
			item.LeaseToken, item.LeaseUntil = token, until
			return item, true, nil
		}
	}
	return model.NodeConfigPublish{}, false, nil
}

func nodePublishRetryDelay(attempts uint) time.Duration {
	if attempts > 6 {
		attempts = 6
	}
	delay := 5 * time.Second * time.Duration(1<<attempts)
	if delay > 5*time.Minute {
		return 5 * time.Minute
	}
	return delay
}

func finishNodeConfigPublish(db *gorm.DB, item model.NodeConfigPublish, now time.Time, failure error) error {
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
			"attempts": attempts, "last_error": string(message), "next_attempt_at": now.Add(nodePublishRetryDelay(item.Attempts)),
			"lease_token": "", "lease_until": nodePublishIdleLease,
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
		Updates(map[string]interface{}{"lease_token": "", "lease_until": nodePublishIdleLease, "next_attempt_at": now}).Error
}
