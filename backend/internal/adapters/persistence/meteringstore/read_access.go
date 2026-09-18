package meteringstore

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

// Reporting reads authorize on the primary before acquiring a read snapshot.
// They must not retain the primary connection while waiting on the read pool.
func authorizeReportingRead(ctx context.Context, db *gorm.DB, actor uint, administrative bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if actor == 0 {
		return metering.ErrTrendPermission
	}
	var user model.User
	err := db.WithContext(ctx).Select("id", "is_admin", "status").First(&user, actor).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return metering.ErrTrendPermission
	}
	if err != nil {
		return err
	}
	if user.Status != "active" || (administrative && !user.IsAdmin) {
		return metering.ErrTrendPermission
	}
	return nil
}
