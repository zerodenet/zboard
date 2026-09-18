package identitystore

import (
	"context"
	"errors"
	"fmt"

	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type Passwords struct{ DB *gorm.DB }

func (s Passwords) ActiveAccount(ctx context.Context, id uint) (identity.Account, error) {
	var user model.User
	err := s.DB.WithContext(ctx).Select("id,password").Where("id = ? AND status = ?", id, "active").Take(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return identity.Account{}, identity.ErrUnavailable
	}
	return identity.Account{ID: user.ID, PasswordHash: user.Password}, err
}

func (s Passwords) ReplacePasswordAndAudit(ctx context.Context, p identity.Principal, before identity.Account, hash string) error {
	if p.ID == 0 || p.ID != before.ID {
		return identity.ErrUnavailable
	}
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.User{}).Where("id = ? AND password = ? AND status = ?", p.ID, before.PasswordHash, "active").Update("password", hash)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return identity.ErrConflict
		}
		return tx.Create(&model.AuditLog{UserID: &p.ID, Actor: p.Actor, Action: "account.password.change", Target: fmt.Sprintf("user:%d", p.ID), Detail: "local password changed after password verification"}).Error
	})
}
