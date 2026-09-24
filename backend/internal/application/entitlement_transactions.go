package application

import (
	"context"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/entitlementstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/networkstore"
	zeroadapter "github.com/zerodenet/zboard/backend/internal/adapters/zero"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"time"
)

var ErrCredentialLockTimeout = entitlementstore.ErrCredentialLockTimeout

// Transaction bridges preserve the existing settlement/expiry atomic boundary
// while the remaining credential issuance and settlement orchestration move.
func RevokeSubscriptionOutsideGroup(tx *gorm.DB, sub model.Subscription, now time.Time) error {
	return entitlementstore.RevokeOutsideGroup(tx, sub, now)
}
func ExpireSubscriptionsInTransaction(tx *gorm.DB, userID uint, now time.Time) error {
	return entitlementstore.ExpireInTransaction(tx, userID, now)
}
func ExpireSubscriptionCredentials(tx *gorm.DB, subscriptionID uint, now time.Time) error {
	return entitlementstore.ExpireSubscriptionCredentials(tx, subscriptionID, now)
}
func RequestNodeAndTopologyPublication(tx *gorm.DB, nodeID, endpointID, actor uint) error {
	return networkstore.EnqueueNodePublication(tx, nodeID, endpointID, actor)
}
func RequestSubscriptionPublication(tx *gorm.DB, subscriptionID, actor uint) error {
	return networkstore.EnqueueSubscriptionPublications(tx, subscriptionID, actor)
}

func RecordEntitlementQuotaEvent(tx *gorm.DB, sub model.Subscription, eventType string, delta, before, after int64, referenceType, referenceID string) error {
	return entitlementstore.RecordQuotaEvent(tx, sub, eventType, delta, before, after, referenceType, referenceID)
}
func ApplyDueTrafficReset(tx *gorm.DB, sub *model.Subscription, now time.Time) (bool, error) {
	return entitlementstore.ApplyDueTrafficReset(tx, sub, now)
}

func EnsureSubscriptionCredentials(tx *gorm.DB, sub model.Subscription, cipher zeroadapter.Cipher, mieru bool) ([]model.ProtocolCredential, error) {
	return entitlementstore.EnsureCredentials(tx, sub, zeroadapter.Issuer{Cipher: cipher, Mieru: mieru})
}

func RunCredentialTransaction(ctx context.Context, tx *gorm.DB, operation func(*gorm.DB) error) error {
	return entitlementstore.RunCredentialTransaction(ctx, tx, operation)
}
func NewSubscriptionSecret(cipher zeroadapter.Cipher, protocol, config string) (string, error) {
	return (zeroadapter.Issuer{Cipher: cipher}).Secret(protocol, config)
}

func (s *Services) SubscriptionAccess(cipher entitlements.AccessCipher) entitlements.Access {
	return entitlements.Access{Repository: entitlementstore.Access{DB: s.Identity.db, Cipher: cipher}}
}
func EnsureSubscriptionAccessToken(tx *gorm.DB, sub model.Subscription, cipher entitlements.AccessCipher) (model.SubscriptionToken, string, error) {
	return entitlementstore.EnsureAccessToken(tx, sub, cipher)
}

func (s *Services) ClientSubscriptionAccess() entitlements.ClientAccess {
	return entitlements.ClientAccess{Repository: entitlementstore.ClientAccess{DB: s.Identity.db}}
}

func (s *Services) ReconcileSubscriptionAccess(ctx context.Context, cipher entitlements.AccessCipher) error {
	return entitlementstore.ReconcileAccessTokens(ctx, s.Identity.db, cipher)
}

func (s *Services) TrafficReset(cipher zeroadapter.Cipher, mieru bool) entitlementstore.TrafficReset {
	return entitlementstore.TrafficReset{
		DB: s.Identity.db, Issuer: zeroadapter.Issuer{Cipher: cipher, Mieru: mieru},
		Publish: networkstore.EnqueueSubscriptionPublications,
	}
}
