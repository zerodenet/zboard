package networkstore

import (
	"context"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type ConnectorActivity struct{ DB *gorm.DB }

func (s ConnectorActivity) ReadConnectorLastSeen(ctx context.Context, nodeID uint) (*time.Time, error) {
	var activity struct {
		ConnectorLastSeenAt *time.Time
	}
	if err := s.DB.WithContext(ctx).Model(&model.Node{}).Select("connector_last_seen_at").Where("id = ?", nodeID).Take(&activity).Error; err != nil {
		return nil, err
	}
	return activity.ConnectorLastSeenAt, nil
}
