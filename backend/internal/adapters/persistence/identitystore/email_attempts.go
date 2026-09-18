package identitystore

import (
	"context"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type EmailAttempts struct{ DB *gorm.DB }

func (s EmailAttempts) ChargeRegistrationAttempt(ctx context.Context, email string, now time.Time, limit int) (bool, error) {
	q := s.DB.WithContext(ctx).Model(&model.RegistrationEmailChallenge{}).Where("email = ? AND purpose = ? AND attempts < ? AND consumed_at IS NULL AND expires_at > ?", email, identity.RegistrationPurpose, limit, now).Update("attempts", gorm.Expr("attempts + 1"))
	return q.RowsAffected == 1, q.Error
}
