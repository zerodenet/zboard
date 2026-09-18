package application

import (
	"context"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"gorm.io/gorm"
)

func (s *Services) BatchLifecycle() jobs.BatchLifecycle {
	return jobs.BatchLifecycle{Repository: jobstore.BatchLifecycle{DB: s.Identity.db}}
}
func (s *Services) WithBatchLease(ctx context.Context, id uint, token string, write func(*gorm.DB) error) error {
	return jobstore.WithBatchLease(ctx, s.Identity.db, id, token, write)
}
