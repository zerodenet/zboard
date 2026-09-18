package application

import (
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/messagingstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
)

func (s *Services) BatchItemRunner(executor jobs.BatchBusinessExecutor) jobs.BatchItemRunner {
	return jobs.BatchItemRunner{Repository: jobstore.BatchItems{DB: s.Identity.db, Hooks: jobstore.BatchItemHooks{Begin: messagingstore.BeginBatchItem, Complete: messagingstore.CompleteBatchItem}}, Executor: executor}
}
