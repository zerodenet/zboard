package identitystore

import (
	"context"

	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type InitialPasswords struct{ DB *gorm.DB }

func (s InitialPasswords) SetInitialPassword(ctx context.Context, p identity.Principal, grant identity.InitialPasswordGrant, hash string) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user model.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND status = ? AND password = ?", p.ID, "active", "!external").Take(&user).Error; err != nil {
			return accountError(err)
		}
		var binding model.ExternalIdentity
		if err := tx.Where("id = ? AND user_id = ? AND plugin_id = ? AND publisher = ?", grant.BindingID, user.ID, grant.Authority, grant.Publisher).Take(&binding).Error; err != nil {
			return accountError(err)
		}
		update := tx.Model(&user).Where("password = ?", "!external").Update("password", hash)
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return identity.ErrConflict
		}
		return tx.Create(&model.AuditLog{UserID: &user.ID, Actor: user.Email, Action: "identity.password.setup", Target: grant.Authority, Detail: "initial password set after recent external authentication"}).Error
	})
}
