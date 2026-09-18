package application

import (
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/networkstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
)

func (s *Services) ProtocolCompatibility() network.ProtocolCompatibility {
	return network.ProtocolCompatibility{Repository: networkstore.ProtocolCompatibility{DB: s.Identity.db}}
}
