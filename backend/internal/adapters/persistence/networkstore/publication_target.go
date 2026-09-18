package networkstore

import (
	"context"
	"errors"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type PublicationTargets struct{ DB *gorm.DB }

func (s PublicationTargets) Resolve(ctx context.Context, item network.Publication) (network.PublicationTarget, bool, error) {
	db := s.DB.WithContext(ctx)
	var node model.Node
	if err := db.Select("id", "lifecycle_status").First(&node, item.NodeID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return network.PublicationTarget{}, false, nil
		}
		return network.PublicationTarget{}, false, err
	}
	if node.LifecycleStatus == "deleting" {
		return network.PublicationTarget{}, false, nil
	}
	var endpoint model.ProtocolEndpoint
	if err := db.Select("id").Where("node_id = ?", item.NodeID).Order("id").Limit(1).Find(&endpoint).Error; err != nil {
		return network.PublicationTarget{}, false, err
	}
	if endpoint.ID == 0 {
		// A fronting node legitimately has no protocol implementation of its own.
		// A previously published node must also remain eligible so removal of its
		// last endpoint or entry can clear the old remote runtime configuration.
		var entryCount int64
		if err := db.Model(&model.NetworkEntry{}).
			Where("node_id = ? AND enabled = ?", item.NodeID, true).
			Count(&entryCount).Error; err != nil {
			return network.PublicationTarget{}, false, err
		}
		var publishedCount int64
		if entryCount == 0 {
			if err := db.Model(&model.ProtocolDeployment{}).
				Where("node_id = ? AND status = ?", item.NodeID, "succeeded").
				Count(&publishedCount).Error; err != nil {
				return network.PublicationTarget{}, false, err
			}
		}
		if entryCount == 0 && publishedCount == 0 {
			return network.PublicationTarget{}, false, nil
		}
	}
	requestedBy := item.RequestedBy
	if requestedBy != 0 {
		var count int64
		if err := db.Model(&model.User{}).Where("id = ?", requestedBy).Count(&count).Error; err != nil {
			return network.PublicationTarget{}, false, err
		}
		if count == 0 {
			requestedBy = 0
		}
	}
	return network.PublicationTarget{NodeID: item.NodeID, EndpointID: endpoint.ID, RequestedBy: requestedBy}, true, nil
}
