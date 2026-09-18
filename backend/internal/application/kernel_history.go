package application

import (
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/networkstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
)

func (s *Services) KernelHistory() network.KernelHistory {
	return network.KernelHistory{Repository: networkstore.KernelHistory{DB: s.Identity.db}}
}
