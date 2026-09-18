package networkstore

import (
	"context"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type ManagedPublicationInventory struct{ DB *gorm.DB }

func (s ManagedPublicationInventory) ListManagedPublicationTargets(ctx context.Context, protocols []string) ([]network.PublicationRequest, error) {
	var rows []model.ProtocolEndpoint
	if err := s.DB.WithContext(ctx).Select("id", "node_id").Where("LOWER(protocol) IN ?", protocols).Order("id asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]network.PublicationRequest, 0, len(rows))
	for _, row := range rows {
		result = append(result, network.PublicationRequest{NodeID: row.NodeID, TriggerEndpointID: row.ID})
	}
	return result, nil
}

var _ network.ManagedPublicationInventoryRepository = ManagedPublicationInventory{}
