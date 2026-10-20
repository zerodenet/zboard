package entitlementstore

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Called under the subscription lock in the same transaction as a fresh grant.
// Preserve the subscription URL, but give a fresh service period distinct wire
// credentials. The deployed Zero contract does not expose an authenticated
// per-flow policy revision, and SS can report the password as its principal.
func closeEntitlement(tx *gorm.DB, previous model.Subscription, orderID uint, issuer entitlements.CredentialIssuer, now time.Time) error {
	if previous.EntitlementEpoch == math.MaxUint64 || previous.FlowUsed < 0 || previous.FlowTotal < previous.FlowUsed {
		return errors.New("invalid entitlement epoch or quota")
	}
	closed := model.ClosedEntitlement{SubscriptionID: previous.ID, Epoch: previous.EntitlementEpoch, UserID: previous.UserID, TransitionOrderID: orderID, FlowTotal: previous.FlowTotal, ClosingFlowUsed: previous.FlowUsed, FlowUsed: previous.FlowUsed, TrafficCalcMode: previous.TrafficCalcMode, StartAt: previous.StartAt, EndAt: previous.EndAt, ClosedAt: now, UpdatedAt: now}
	if err := tx.Create(&closed).Error; err != nil {
		return err
	}
	var credentials []model.ProtocolCredential
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("subscription_id = ? AND current_scope = ?", previous.ID, 1).Order("id").Find(&credentials).Error; err != nil {
		return err
	}
	decryptor, available := issuer.(interface{ Decrypt(string) (string, error) })
	for _, row := range credentials {
		if row.EntitlementEpoch != previous.EntitlementEpoch {
			return errors.New("credential current scope has mismatched entitlement epoch")
		}
		hash := row.RetiredPrincipalHash
		if row.Secret != "" {
			if !available {
				return errors.New("fresh entitlement requires historical credential attribution capability")
			}
			plain, err := decryptor.Decrypt(row.Secret)
			if err != nil {
				return fmt.Errorf("close credential %d: %w", row.ID, err)
			}
			hash = entitlements.RetiredPrincipalHash(row.NodeID, plain)
		}
		if err := tx.Model(&row).Updates(map[string]any{"current_scope": nil, "status": "revoked", "revoked_at": now, "secret": "", "retired_principal_hash": hash, "updated_at": now}).Error; err != nil {
			return err
		}
	}
	return nil
}
