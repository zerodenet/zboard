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

type ExternalIdentities struct{ DB *gorm.DB }
type externalTx struct{ db *gorm.DB }

func (s ExternalIdentities) WithinExternal(ctx context.Context, run func(identity.ExternalTx) error) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return run(externalTx{tx}) })
}
func (s externalTx) FindBinding(id string) (identity.ExternalBinding, bool, error) {
	var row model.ExternalIdentity
	q := s.db.Where("id = ?", id).Limit(1).Find(&row)
	return identity.ExternalBinding{ID: row.ID, UserID: row.UserID, Authority: row.PluginID, Publisher: row.Publisher, Issuer: row.Issuer, Subject: row.Subject}, q.RowsAffected == 1, q.Error
}
func (s externalTx) Account(id uint, password *string) (identity.LoginAccount, error) {
	var user model.User
	q := s.db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND status = ?", id, "active")
	if password != nil {
		q = q.Where("password = ?", *password)
	}
	err := q.Take(&user).Error
	return identity.LoginAccount{PublicAccount: publicAccount(user), PasswordHash: user.Password}, accountError(err)
}
func (s externalTx) RegistrationAllowed() (bool, error) {
	var installation model.Installation
	err := s.db.Clauses(clause.Locking{Strength: "UPDATE"}).First(&installation, 1).Error
	return installation.AllowRegistration, err
}
func (s externalTx) CreateBinding(in identity.ExternalBinding) error {
	return s.db.Create(&model.ExternalIdentity{ID: in.ID, UserID: in.UserID, PluginID: in.Authority, Publisher: in.Publisher, Issuer: in.Issuer, Subject: in.Subject}).Error
}
func (s externalTx) CreateExternalAccount(email string, now time.Time) (identity.PublicAccount, error) {
	row := model.User{Email: email, AccountName: email, Password: "!external", Status: "active", IsAdmin: false, EmailVerifiedAt: &now}
	err := s.db.Create(&row).Error
	if err == nil {
		err = s.db.Create(&model.AccountRegistrationEvent{AccountID: row.ID, OccurredAt: row.CreatedAt}).Error
	}
	return publicAccount(row), err
}
func (s externalTx) EmailInUse(email string) (bool, error) {
	var n int64
	err := s.db.Unscoped().Model(&model.User{}).Where("email = ?", email).Count(&n).Error
	return n > 0, err
}
func (s externalTx) EmailChallenge(email string) (identity.EmailChallenge, error) {
	var row model.RegistrationEmailChallenge
	err := s.db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("email = ? AND purpose = ?", email, identity.RegistrationPurpose).First(&row).Error
	return identity.EmailChallenge{Hash: row.CodeHash, ExpiresAt: row.ExpiresAt, Attempts: row.Attempts, Consumed: row.ConsumedAt != nil}, err
}
func (s externalTx) ConsumeEmailChallenge(email string, now time.Time) error {
	q := s.db.Model(&model.RegistrationEmailChallenge{}).Where("email = ? AND purpose = ? AND consumed_at IS NULL", email, identity.RegistrationPurpose).Update("consumed_at", now)
	if q.Error != nil {
		return q.Error
	}
	if q.RowsAffected != 1 {
		return identity.ErrVerifiedEmail
	}
	return nil
}
func (s externalTx) TouchLogin(id uint, now time.Time) error {
	return s.db.Model(&model.User{}).Where("id = ?", id).Update("last_login_at", now).Error
}
func (s externalTx) Audit(user identity.PublicAccount, action, target, detail string) error {
	if user.ID == 0 {
		return errors.New("audit requires account")
	}
	return s.db.Create(&model.AuditLog{UserID: &user.ID, Actor: user.Email, Action: action, Target: target, Detail: detail}).Error
}
