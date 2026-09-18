package application

import (
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/networkstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
)

func (s *Services) ResourceBatchExecution(adapter network.BatchResourceAdapter) network.BatchResourceExecution {
	return network.BatchResourceExecution{Repository: networkstore.BatchResourceExecution{DB: s.Identity.db}, Adapter: adapter}
}
