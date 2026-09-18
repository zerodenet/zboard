package application

import (
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/networkstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
)

func (s *Services) KernelReconciliationState() network.KernelReconciliationState {
	return network.KernelReconciliationState{Repository: networkstore.KernelReconciliationState{DB: s.Identity.db}}
}
