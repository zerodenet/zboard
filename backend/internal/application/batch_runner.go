package application

import (
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
)

func (s *Services) BatchRunner(executor jobs.BatchItemExecutor) jobs.BatchRunner {
	return jobs.BatchRunner{Repository: jobstore.BatchRun{DB: s.Identity.db}, Lifecycle: s.BatchLifecycle(), Executor: executor}
}
