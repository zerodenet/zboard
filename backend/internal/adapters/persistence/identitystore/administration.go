package identitystore

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Administration struct{ DB *gorm.DB }
type administrationTx struct {
	db    *gorm.DB
	actor model.User
}

func (s Administration) Administer(ctx context.Context, p identity.Principal, run func(identity.AdministrationTx) error) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// All writers use the same ordered active-administrator lock set before
		// inspecting targets, so concurrent demotions cannot both pass the guard.
		var admins []model.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("is_admin = ?", true).Order("id ASC").Find(&admins).Error; err != nil {
			return err
		}
		var actor model.User
		for _, u := range admins {
			if u.ID == p.ID && u.Status == "active" {
				actor = u
				break
			}
		}
		if actor.ID == 0 {
			return identity.ErrPermission
		}
		return run(administrationTx{db: tx, actor: actor})
	})
}
func (s administrationTx) Account(id uint) (identity.PublicAccount, error) {
	var u model.User
	err := s.db.Clauses(clause.Locking{Strength: "UPDATE"}).First(&u, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = identity.ErrAccountNotFound
	}
	return publicAccount(u), err
}
func (s administrationTx) Create(in identity.PublicAccount, hash string) (identity.PublicAccount, error) {
	u := model.User{Email: in.Email, AccountName: in.Email, Status: in.Status, IsAdmin: in.IsAdmin, Password: hash}
	err := s.db.Create(&u).Error
	if err != nil {
		message := strings.ToLower(err.Error())
		if strings.Contains(message, "duplicate") || strings.Contains(message, "unique constraint") {
			err = identity.ErrEmailConflict
		}
	}
	return publicAccount(u), err
}
func (s administrationTx) Update(id uint, in identity.StoredAccountChange) (identity.PublicAccount, error) {
	values := map[string]any{}
	if in.Status != nil {
		values["status"] = *in.Status
	}
	if in.IsAdmin != nil {
		values["is_admin"] = *in.IsAdmin
	}
	if in.PasswordHash != nil {
		values["password"] = *in.PasswordHash
	}
	if err := s.db.Model(&model.User{}).Where("id = ?", id).Updates(values).Error; err != nil {
		return identity.PublicAccount{}, err
	}
	return s.Account(id)
}
func (s administrationTx) OtherActiveAdministrator(id uint) (bool, error) {
	var count int64
	err := s.db.Model(&model.User{}).Where("id <> ? AND is_admin = ? AND status = ?", id, true, "active").Count(&count).Error
	return count > 0, err
}
func (s administrationTx) Audit(action string, id uint, detail string) error {
	return s.db.Create(&model.AuditLog{UserID: &s.actor.ID, Actor: s.actor.Email, Action: action, Target: fmt.Sprintf("user:%d", id), Detail: detail}).Error
}
