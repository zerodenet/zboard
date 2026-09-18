package identitystore

import (
	"context"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type CodeIssuance struct{ DB *gorm.DB }
type codeIssuanceTx struct{ registrationTx }

func (s CodeIssuance) WithinCodeIssuance(ctx context.Context, fn func(identity.CodeIssuanceTx) error) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return fn(codeIssuanceTx{registrationTx{externalTx{tx}}}) })
}
func (s codeIssuanceTx) RecentSourceAddresses(source string, since time.Time) (int64, error) {
	var count int64
	err := s.db.Model(&model.RegistrationEmailChallenge{}).Where("requested_ip_hash = ? AND updated_at > ?", source, since).Count(&count).Error
	return count, err
}
func (s codeIssuanceTx) LastCodeSent(email string) (time.Time, bool, error) {
	var row model.RegistrationEmailChallenge
	q := s.db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("email = ? AND purpose = ?", email, identity.RegistrationPurpose).Limit(1).Find(&row)
	return row.LastSentAt, q.RowsAffected == 1, q.Error
}
func (s codeIssuanceTx) StoreCode(in identity.IssuedCode) error {
	row := model.RegistrationEmailChallenge{Email: in.Email, Purpose: identity.RegistrationPurpose, CodeHash: in.Hash, RequestedIPHash: in.SourceHash, LastSentAt: in.SentAt, ExpiresAt: in.ExpiresAt}
	return s.db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "email"}, {Name: "purpose"}}, DoUpdates: clause.Assignments(map[string]any{
		"code_hash": in.Hash, "requested_ip_hash": in.SourceHash, "last_sent_at": in.SentAt, "expires_at": in.ExpiresAt, "attempts": 0, "consumed_at": nil, "updated_at": in.SentAt,
	})}).Create(&row).Error
}
func (s CodeIssuance) InvalidateCode(ctx context.Context, in identity.IssuedCode, now time.Time) error {
	return s.DB.WithContext(ctx).Model(&model.RegistrationEmailChallenge{}).
		Where("email = ? AND purpose = ? AND code_hash = ? AND last_sent_at = ? AND consumed_at IS NULL", in.Email, identity.RegistrationPurpose, in.Hash, in.SentAt).
		Updates(map[string]any{"code_hash": "", "expires_at": now, "last_sent_at": now.Add(-identity.CodeCooldown)}).Error
}
