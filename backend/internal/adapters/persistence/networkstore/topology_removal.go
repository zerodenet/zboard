package networkstore

import (
	"context"
	"errors"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TopologyRemoval struct{ DB *gorm.DB }

func (s TopologyRemoval) RemoveEntry(ctx context.Context, actor, id uint) error {
	return jobstore.New(s.DB).WithLedgerLock(ctx, func(tx *gorm.DB) error {
		user, err := providerAdmin(tx, actor)
		if err != nil {
			if errors.Is(err, network.ErrProviderPermission) {
				return network.ErrResourcePermission
			}
			return err
		}
		var entry model.NetworkEntry
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&entry, id).Error; err != nil {
			return removalError(err)
		}
		if _, err := RemoveTopologyEntries(tx, actor, TopologySelection{EntryID: id}); err != nil {
			return err
		}
		return tx.Create(&model.AuditLog{UserID: &user.ID, Actor: user.Email, Action: "network_entry.delete", Target: fmt.Sprintf("network_entry:%d", id), Detail: fmt.Sprintf("node=%d endpoint=%d", entry.NodeID, entry.EndpointID)}).Error
	})
}
