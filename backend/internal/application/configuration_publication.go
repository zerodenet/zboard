package application

import (
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/networkstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
)

func (s *Services) ConfigurationPublicationState() network.ConfigurationPublicationState {
	return network.ConfigurationPublicationState{Repository: networkstore.ConfigurationPublicationState{DB: s.Identity.db}}
}
