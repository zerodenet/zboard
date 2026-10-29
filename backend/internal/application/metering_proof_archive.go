package application

import (
	"context"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"gorm.io/gorm"
)

// Explicit local maintenance shares this application boundary. It never starts
// bootstrap, HTTP, plugins, the file spool or background execution.
func ArchiveMeteringProof(ctx context.Context, db *gorm.DB, q metering.ProofArchiveRequest) (metering.ProofArchiveResult, error) {
	if ctx == nil || db == nil {
		return metering.ProofArchiveResult{}, metering.ErrProofArchive
	}
	if err := datastore.ValidateTrafficMetaArchiveDatabase(db.WithContext(ctx)); err != nil {
		return metering.ProofArchiveResult{}, err
	}
	return (metering.ProofArchives{Repository: meteringstore.ProofArchives{DB: db}}).Archive(ctx, q)
}

func RestoreMeteringProof(ctx context.Context, db *gorm.DB, id uint) (metering.ProofRestoreResult, error) {
	if ctx == nil || db == nil {
		return metering.ProofRestoreResult{}, metering.ErrProofArchive
	}
	if err := datastore.ValidateTrafficMetaArchiveDatabase(db.WithContext(ctx)); err != nil {
		return metering.ProofRestoreResult{}, err
	}
	return (metering.ProofArchives{Repository: meteringstore.ProofArchives{DB: db}}).Restore(ctx, id)
}
