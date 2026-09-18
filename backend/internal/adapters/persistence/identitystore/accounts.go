package identitystore

import (
	"context"
	"errors"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Accounts struct{ DB *gorm.DB }

func publicAccount(v model.User) identity.PublicAccount {
	return identity.PublicAccount{ID: v.ID, Email: v.Email, IsAdmin: v.IsAdmin, Status: v.Status}
}
func accountError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return identity.ErrUnavailable
	}
	return err
}
func (s Accounts) LoginAccount(ctx context.Context, email string) (identity.LoginAccount, error) {
	var user model.User
	err := s.DB.WithContext(ctx).Where("email = ? AND status = ?", email, "active").Take(&user).Error
	return identity.LoginAccount{PublicAccount: publicAccount(user), PasswordHash: user.Password}, accountError(err)
}
func (s Accounts) CurrentAccount(ctx context.Context, id uint) (identity.PublicAccount, error) {
	var user model.User
	err := s.DB.WithContext(ctx).Select("id,email,is_admin,status").Where("id = ? AND status = ?", id, "active").Take(&user).Error
	return publicAccount(user), accountError(err)
}
func (s Accounts) CompleteLogin(ctx context.Context, before identity.LoginAccount, now time.Time) (identity.PublicAccount, error) {
	var current model.User
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND password = ? AND status = ?", before.ID, before.PasswordHash, "active").Take(&current).Error; err != nil {
			return accountError(err)
		}
		return tx.Model(&current).Update("last_login_at", now).Error
	})
	if err != nil {
		return identity.PublicAccount{}, err
	}
	return publicAccount(current), nil
}
