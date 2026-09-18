package application

import (
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/networkstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
)

func (s *Services) KernelDetection(executor network.KernelProbeExecutor, artifactAvailable bool) network.KernelDetection {
	return network.KernelDetection{
		Repository: networkstore.KernelDetection{DB: s.Identity.db}, Executor: executor, ArtifactAvailable: artifactAvailable,
	}
}
