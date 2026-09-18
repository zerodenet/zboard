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
