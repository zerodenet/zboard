package application

import (
	"context"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/networkstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"gorm.io/gorm"
)

func (s *Services) NodePublication(executor network.PublicationExecutor) network.PublicationQueue {
	return network.PublicationQueue{
		Store:    networkstore.PublicationQueue{DB: s.Identity.db},
		Executor: executor,
	}
}

func (s *Services) NodePublicationTargets() network.PublicationTargets {
	return network.PublicationTargets{Repository: networkstore.PublicationTargets{DB: s.Identity.db}}
}

// ClaimNodePublication and CompleteNodePublication keep legacy in-package
// callers on the application composition boundary while they migrate to Services.
func ClaimNodePublication(tx *gorm.DB, now time.Time) (network.Publication, bool, error) {
	return (networkstore.PublicationQueue{DB: tx}).Claim(tx.Statement.Context, now)
}

func CompleteNodePublication(tx *gorm.DB, item network.Publication, now time.Time, failure error) error {
	return (networkstore.PublicationQueue{DB: tx}).Complete(tx.Statement.Context, item, now, failure)
}

func (s *Services) ConnectionRevocation(cipher metering.CredentialDecryptor) network.ConnectionRevocationAuthorization {
	return networkstore.ConnectionRevocation{DB: s.Identity.db, Cipher: cipher}
}
func (s *Services) MarkPublicationConfigurationApplied(ctx context.Context, item network.Publication, sha string) (bool, error) {
	return (networkstore.PublicationQueue{DB: s.Identity.db}).MarkConfigurationApplied(ctx, item, sha)
}
