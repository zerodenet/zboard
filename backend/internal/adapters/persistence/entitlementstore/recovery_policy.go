package entitlementstore

import (
	"errors"
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const RecoveryGraceDaysKey = entitlements.RecoveryGraceDaysKey

// Missing configuration retains the previous seven-day policy. A corrupt
// stored policy is not an invitation to extend eligibility or destroy keys.
// In authority transactions, a current shared read serializes configuration
// changes with the grant/cleanup that consumes it, without an early RR snapshot.
func RecoveryGracePeriod(db *gorm.DB) (time.Duration, error) {
	var row model.SystemConfig
	query := db.Select("value", "value_type").Where("config_key = ?", RecoveryGraceDaysKey)
	if _, transactional := db.Statement.ConnPool.(gorm.TxCommitter); transactional {
		query = query.Clauses(clause.Locking{Strength: "SHARE"})
	}
	err := query.First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if db.Migrator().HasTable("schema_migrations") {
			var initialized int64
			if err := db.Table("schema_migrations").Where("version = ?", model.SubscriptionRecoveryPolicySchemaVersion).Count(&initialized).Error; err != nil {
				return 0, err
			}
			if initialized > 0 {
				return 0, fmt.Errorf("initialized subscription recovery policy missing; restore configuration")
			}
		}
		return entitlements.RenewalGracePeriod, nil
	}
	if err != nil {
		return 0, err
	}
	if row.ValueType != "int" {
		return 0, fmt.Errorf("subscription recovery policy metadata invalid; restore configuration")
	}
	grace, err := entitlements.ParseRecoveryGraceDays(row.Value)
	if err != nil {
		return 0, fmt.Errorf("subscription recovery policy invalid; restore configuration: %w", err)
	}
	return grace, nil
}
