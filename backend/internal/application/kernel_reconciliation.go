package application

import "github.com/zerodenet/zboard/backend/internal/capabilities/network"

func (s *Services) KernelReconciliation(preparer network.KernelReconciliationPreparer) network.KernelReconciliation {
	return network.KernelReconciliation{State: s.KernelReconciliationState(), Preparer: preparer}
}
