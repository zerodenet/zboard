package entitlementstore

import (
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"time"
)

// TerminateInTransaction joins the caller's financial settlement. Historical
// usage, cumulative metering identities and credentials remain attributable;
// credential and subscription-token revocation is committed with delivery intent.
func TerminateInTransaction(tx *gorm.DB, sub model.Subscription, reference string, now time.Time) error {
	return TerminateWithReferenceInTransaction(tx, sub, "subscription_value_settlement", reference, now)
}

// TerminateWithReferenceInTransaction is the same rights writer for a financial
// settlement or an explicitly approved zero-money entitlement command.
func TerminateWithReferenceInTransaction(tx *gorm.DB, sub model.Subscription, referenceType, reference string, now time.Time) error {
	if err := tx.Model(&model.Subscription{}).Where("id = ?", sub.ID).Updates(map[string]any{"status": "canceled", "ended_at": now, "end_reason": "canceled", "updated_at": now}).Error; err != nil {
		return err
	}
	if err := tx.Model(&model.SubscriptionToken{}).Where("subscription_id = ? AND revoked_at IS NULL", sub.ID).Updates(map[string]any{"revoked_at": now, "updated_at": now}).Error; err != nil {
		return err
	}
	query := tx.Model(&model.ProtocolCredential{}).Where("subscription_id = ? AND status <> ?", sub.ID, "revoked")
	if err := persistCredentialRevocation(tx, query, "revoked", now); err != nil {
		return err
	}
	before := sub.FlowTotal - sub.FlowUsed
	if before < 0 {
		before = 0
	}
	return RecordQuotaEvent(tx, sub, "unsubscribe", -before, before, 0, referenceType, reference)
}
