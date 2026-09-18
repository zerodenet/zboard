package networkstore

import (
	"context"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"gorm.io/gorm"
)

type PublicationRequests struct{ DB *gorm.DB }

func (s PublicationRequests) QueuePublicationRequests(ctx context.Context, requests []network.PublicationRequest) error {
	if s.DB == nil {
		return network.ErrPublicationRequestUnavailable
	}
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, request := range requests {
			if err := EnqueueNodePublication(tx, request.NodeID, request.TriggerEndpointID, request.RequestedBy); err != nil {
				return err
			}
		}
		return nil
	})
}
