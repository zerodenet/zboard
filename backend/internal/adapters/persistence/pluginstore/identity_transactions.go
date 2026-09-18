package pluginstore

import (
	"context"
	"errors"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/identitystore"
	identitycap "github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/plugins"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// IdentityTransactions atomically fences the active plugin host and provider
// installation together with the core identity write.
type IdentityTransactions struct {
	DB         *gorm.DB
	Tokens     identitycap.SessionTokens
	EmailCodes identitycap.EmailCodes
}

func (s IdentityTransactions) WithinIdentity(ctx context.Context, fence plugins.IdentityFence, commit func(plugins.IdentityServices) error) error {
	if s.DB == nil || commit == nil || fence.InstallationID == "" || fence.Publisher == "" || fence.Generation == 0 || fence.Revision == 0 || fence.HostOwner == "" || fence.HostEpoch == 0 {
		return plugins.ErrUnavailable
	}
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var lease model.PluginHostLease
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
			"id = ? AND owner = ? AND epoch = ? AND expires_at > ?",
			1, fence.HostOwner, fence.HostEpoch, time.Now().UTC(),
		).Take(&lease).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return plugins.ErrUnavailable
		}
		if err != nil {
			return err
		}
		var installation model.PluginInstallation
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
			"id = ? AND enabled = ? AND state = ? AND generation = ? AND config_revision = ? AND publisher = ?",
			fence.InstallationID, true, "active", fence.Generation, fence.Revision, fence.Publisher,
		).First(&installation).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return plugins.ErrConflict
		}
		if err != nil {
			return err
		}
		return commit(plugins.IdentityServices{
			External: identitycap.ExternalIdentities{
				Repository: identitystore.ExternalIdentities{DB: tx},
				Tokens:     s.Tokens,
				EmailCodes: s.EmailCodes,
			},
			InitialPasswords: identitycap.InitialPasswords{Repository: identitystore.InitialPasswords{DB: tx}},
		})
	})
}
