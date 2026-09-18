package handler

import (
	"time"

	"github.com/zerodenet/zboard/backend/internal/application"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

const nodePublishLease = network.PublicationLease

func enqueueNodeConfigPublish(tx *gorm.DB, nodeID, endpointID, actor uint) error {
	return application.RequestNodeAndTopologyPublication(tx, nodeID, endpointID, actor)
}
func enqueueNodeConfigPublishOnly(tx *gorm.DB, nodeID, endpointID, actor uint) error {
	return application.RequestNodePublication(tx, nodeID, endpointID, actor)
}
func enqueueSubscriptionConfigPublishes(tx *gorm.DB, subscriptionID, actor uint) error {
	return application.RequestSubscriptionPublication(tx, subscriptionID, actor)
}

func claimNodeConfigPublish(db *gorm.DB, now time.Time) (model.NodeConfigPublish, bool, error) {
	item, ok, err := application.ClaimNodePublication(db, now)
	return publicationModel(item), ok, err
}

func nodePublishRetryDelay(attempts uint) time.Duration {
	return network.PublicationRetryDelay(attempts)
}

func finishNodeConfigPublish(db *gorm.DB, item model.NodeConfigPublish, now time.Time, failure error) error {
	return application.CompleteNodePublication(db, publicationCapability(item), now, failure)
}

func publicationCapability(item model.NodeConfigPublish) network.Publication {
	return network.Publication{
		NodeID: item.NodeID, EndpointID: item.EndpointID, RequestedBy: item.RequestedBy,
		Generation: item.Generation, Attempts: item.Attempts, LastError: item.LastError,
		NextAttemptAt: item.NextAttemptAt, LeaseUntil: item.LeaseUntil, LeaseToken: item.LeaseToken,
	}
}

func publicationModel(item network.Publication) model.NodeConfigPublish {
	return model.NodeConfigPublish{
		NodeID: item.NodeID, EndpointID: item.EndpointID, RequestedBy: item.RequestedBy,
		Generation: item.Generation, Attempts: item.Attempts, LastError: item.LastError,
		NextAttemptAt: item.NextAttemptAt, LeaseUntil: item.LeaseUntil, LeaseToken: item.LeaseToken,
	}
}
