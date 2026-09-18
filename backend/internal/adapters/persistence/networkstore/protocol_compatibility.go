package networkstore

import (
	"context"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type ProtocolCompatibility struct{ DB *gorm.DB }

func (s ProtocolCompatibility) LoadProtocolCompatibilityEndpoints(ctx context.Context, endpointIDs []uint) ([]network.ProtocolCompatibilityEndpoint, error) {
	rows := make([]network.ProtocolCompatibilityEndpoint, 0, len(endpointIDs))
	err := s.DB.WithContext(ctx).Model(&model.ProtocolEndpoint{}).
		Select("id, protocol").Where("id IN ?", endpointIDs).Order("id asc").Scan(&rows).Error
	return rows, err
}
