package entitlementstore

import (
	"context"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"strings"
)

// Only real, non-revoked identities may discover the configured prefix through
// the temporary legacy redirect. Random probes cannot enumerate the new path.
func LegacySubscriptionIdentity(db *gorm.DB, ctx context.Context, token string) (bool, error) {
	token = strings.TrimSpace(token)
	if token == "" || strings.Contains(token, "/") {
		return false, nil
	}
	var rows []uint
	err := db.WithContext(ctx).Model(&model.SubscriptionToken{}).
		Joins("JOIN users ON users.id=subscription_tokens.user_id AND users.deleted_at IS NULL AND users.status='active'").
		Where("subscription_tokens.token_hash=? AND subscription_tokens.revoked_at IS NULL AND subscription_tokens.subscription_id IS NOT NULL", entitlements.HashAccessToken(token)).
		Limit(1).Pluck("subscription_tokens.id", &rows).Error
	return len(rows) == 1, err
}
