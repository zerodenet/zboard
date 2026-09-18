package application

import (
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/networkstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
)

func (s *Services) ConnectorActivityObserver() network.ConnectorActivityObserver {
	return network.ConnectorActivityObserver{Repository: networkstore.ConnectorActivity{DB: s.Identity.db}}
}
