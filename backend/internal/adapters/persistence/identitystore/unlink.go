package identitystore

import (
	"context"
	"fmt"
	"sort"

	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type BindingRemoval struct{ DB *gorm.DB }

func (s BindingRemoval) RemoveBinding(ctx context.Context, actorID, targetID uint, plugin, id string, authorize func(identity.LoginAccount, identity.LoginAccount, identity.ExternalBinding) error) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		ids := []uint{actorID}
		if actorID != targetID {
			ids = append(ids, targetID)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		users := map[uint]model.User{}
		for _, id := range ids {
			var user model.User
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, id).Error; err != nil {
				return accountError(err)
			}
			users[id] = user
		}
		var binding model.ExternalIdentity
		if err := tx.Where("id = ? AND user_id = ?", id, targetID).Take(&binding).Error; err != nil {
			return accountError(err)
		}
		actor, target := users[actorID], users[targetID]
		if err := authorize(identity.LoginAccount{PublicAccount: publicAccount(actor), PasswordHash: actor.Password}, identity.LoginAccount{PublicAccount: publicAccount(target), PasswordHash: target.Password}, identity.ExternalBinding{ID: binding.ID, UserID: binding.UserID, Authority: binding.PluginID, Publisher: binding.Publisher, Issuer: binding.Issuer, Subject: binding.Subject}); err != nil {
			return err
		}
		if err := tx.Delete(&binding).Error; err != nil {
			return err
		}
		return tx.Create(&model.AuditLog{UserID: &actor.ID, Actor: actor.Email, Action: "plugin.identity.unlink", Target: fmt.Sprintf("user:%d", target.ID), Detail: fmt.Sprintf("plugin=%s provider=%s identity=%s", plugin, binding.PluginID, binding.ID)}).Error
	})
}
