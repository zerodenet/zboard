package application

import (
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/networkstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
)

func (s *Services) RuntimeConfigurationSource() network.RuntimeConfigurationSource {
	return network.RuntimeConfigurationSource{Repository: networkstore.RuntimeConfiguration{DB: s.Identity.db}}
}
