package application

import (
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
)

func (s *Services) BatchQueries() jobs.BatchQueries {
	return jobs.BatchQueries{Repository: jobstore.BatchQueries{DB: s.Identity.db}}
}
