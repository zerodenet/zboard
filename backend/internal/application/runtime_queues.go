package application

import (
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/networkstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
)

func (s *Services) RuntimeQueues() jobs.RuntimeQueues {
	return jobs.RuntimeQueues{
		Admin:       jobstore.AdminRuntimeQueue{DB: s.Identity.db},
		Publication: networkstore.PublicationRuntimeQueue{DB: s.Identity.db},
	}
}
