package datastore

import (
	"fmt"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

// AutoMigrate adds current indexes but does not remove the former user-level
// unique index. Reconcile both boundaries, including already-created databases.
func reconcileSQLiteSubscriptionAccess(db *gorm.DB) error {
	if !db.Migrator().HasTable(&model.SubscriptionToken{}) {
		return nil
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("DELETE FROM subscription_tokens WHERE subscription_id IS NULL").Error; err != nil {
			return err
		}
		if err := tx.Exec("CREATE INDEX IF NOT EXISTS idx_subscription_tokens_user ON subscription_tokens(user_id)").Error; err != nil {
			return err
		}
		var indexes []struct {
			Name   string
			Unique int
		}
		if err := tx.Raw("PRAGMA index_list('subscription_tokens')").Scan(&indexes).Error; err != nil {
			return err
		}
		for _, index := range indexes {
			if index.Unique == 0 {
				continue
			}
			var columns []struct{ Name string }
			if err := tx.Raw(`PRAGMA index_info("` + strings.ReplaceAll(index.Name, `"`, `""`) + `")`).Scan(&columns).Error; err != nil {
				return err
			}
			if len(columns) == 1 && columns[0].Name == "user_id" {
				if err := tx.Migrator().DropIndex(&model.SubscriptionToken{}, index.Name); err != nil {
					return fmt.Errorf("remove legacy SQLite token user uniqueness: %w", err)
				}
			}
		}
		if err := tx.Exec("CREATE UNIQUE INDEX IF NOT EXISTS uq_subscription_token_subscription ON subscription_tokens(subscription_id)").Error; err != nil {
			return fmt.Errorf("enforce SQLite subscription token ownership: %w", err)
		}
		return nil
	})
}
