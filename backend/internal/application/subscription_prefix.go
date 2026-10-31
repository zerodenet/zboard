package application

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/entitlementstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/platformstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"gorm.io/gorm"
)

func LegacySubscriptionIdentityLookup(db *gorm.DB) func(context.Context, string) (bool, error) {
	return func(ctx context.Context, token string) (bool, error) {
		return entitlementstore.LegacySubscriptionIdentity(db, ctx, token)
	}
}

func SubscriptionPrefixLookup(db *gorm.DB) func(context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		row, err := (platformstore.Settings{DB: db}).Lookup(ctx, platform.SubscriptionPrefixKey)
		if errors.Is(err, platform.ErrSettingNotFound) {
			return "", nil
		}
		return row.Value, err
	}
}
