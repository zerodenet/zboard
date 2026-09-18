package identitystore

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Registration struct{ DB *gorm.DB }
type registrationTx struct{ externalTx }

func (s Registration) WithinRegistration(ctx context.Context, fn func(identity.RegistrationTx) error) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return fn(registrationTx{externalTx{tx}}) })
}
func (s registrationTx) VerificationRequired() (bool, error) {
	var row model.SystemConfig
	err := s.db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("config_key = ?", "register_email_verification").First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return strconv.ParseBool(strings.TrimSpace(row.Value))
}
func (s registrationTx) EmailChallenge(email string) (identity.EmailChallenge, error) {
	challenge, err := s.externalTx.EmailChallenge(email)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = &identity.AccountValidation{Fields: map[string]string{"verification_code": "请先获取邮箱验证码。"}}
	}
	return challenge, err
}
func (s registrationTx) RejectEmailChallenge(email string, attempts int, expires *time.Time) error {
	updates := map[string]any{"attempts": attempts}
	if expires != nil {
		updates["expires_at"] = *expires
	}
	return s.db.Model(&model.RegistrationEmailChallenge{}).Where("email = ? AND purpose = ?", email, identity.RegistrationPurpose).
		Updates(updates).Error
}
func (s registrationTx) CreateLocalAccount(email, hash string, verified *time.Time) (identity.PublicAccount, error) {
	row := model.User{Email: email, AccountName: email, Password: hash, Status: "active", IsAdmin: false, EmailVerifiedAt: verified}
	err := s.db.Create(&row).Error
	if err == nil {
		err = s.db.Create(&model.AccountRegistrationEvent{AccountID: row.ID, OccurredAt: row.CreatedAt}).Error
	}
	if err != nil {
		message := strings.ToLower(err.Error())
		if strings.Contains(message, "duplicate") || strings.Contains(message, "unique constraint") {
			err = identity.ErrEmailConflict
		}
	}
	return publicAccount(row), err
}
