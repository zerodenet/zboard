package application

import (
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/networkstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
)

func (s *Services) BatchScopes() network.BatchScopes {
	return network.BatchScopes{Repository: networkstore.BatchScopes{DB: s.Identity.db}}
}
