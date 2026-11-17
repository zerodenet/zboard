package platformstore

import (
	"errors"

	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

// DefaultCurrency is only for new defaults and genuinely empty projections.
// Financial records always retain their own currency.
func DefaultCurrency(db *gorm.DB) (string, error) {
	var row model.SystemConfig
	err := db.Select("value").Where("config_key = ?", platform.DefaultCurrencyKey).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return platform.FallbackCurrency, nil
	}
	if err != nil {
		return "", err
	}
	if err := platform.ValidateDefaultCurrency(row.Value); err != nil {
		return "", err
	}
	return row.Value, nil
}
