package application

import (
	"context"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/platformstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"time"
)

func (s *Services) MigrationExecution(cipher platform.MigrationDecryptor) platform.MigrationExecution {
	return platform.MigrationExecution{Repository: platformstore.MigrationExecution{DB: s.Identity.db}, Cipher: cipher, Copier: platformstore.MigrationCopier{DB: s.Identity.db, PrepareSchema: PrepareDatabaseSchema}}
}
func (s *Services) RegisterDatabaseMigrationJobs(cipher platform.MigrationDecryptor) error {
	return s.Jobs.RegisterMaintenance(jobs.Definition{ID: "database_migration", Handler: "database_migration", Owner: "system", Resource: jobs.MaintenanceResource, Name: "数据库迁移", Revision: "1", Timeout: time.Hour}, s.MigrationExecution(cipher).Execute)
}
func (s *Services) RecoverLegacyDatabaseMigrations(ctx context.Context) error {
	return (platformstore.MigrationExecution{DB: s.Identity.db}).RecoverLegacy(ctx)
}
