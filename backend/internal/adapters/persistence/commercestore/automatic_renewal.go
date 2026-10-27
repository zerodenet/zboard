package commercestore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type AutomaticRenewals struct {
	DB        *gorm.DB
	Issuer    entitlements.CredentialIssuer
	Now       func() time.Time
	Discounts commerce.DiscountQuoter
}

var errAutomaticHistoryBound = fmt.Errorf("automatic renewal evidence budget exceeded: %w", errors.Join(commerce.ErrSubscriptionValueUnavailable, commerce.ErrSubscriptionEvidenceBudget))

// Existing payment and unsubscribe writers lock orders before their owner.
// Consent changes and automatic debits follow that order too. A newly inserted
// target order requires a fresh attempt, rather than being missed by an old RR
// snapshot or introducing an inverse account/order wait.
func automaticRenewalScope(tx *gorm.DB, scope subscriptionOrderScope) (model.User, error) {
	if err := lockSubscriptionOrderIDs(tx, scope); err != nil {
		return model.User{}, err
	}
	var user model.User
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id,status").First(&user, scope.Owner).Error; err != nil {
		return user, resourceError(err)
	}
	if err := verifySubscriptionOrderScope(tx, scope); err != nil {
		return user, err
	}
	return user, nil
}

func prepareAutomaticRenewalScope(db *gorm.DB, owner, subscription uint) (subscriptionOrderScope, error) {
	scope, err := prepareSubscriptionOrderScope(db, owner, subscription)
	if errors.Is(err, commerce.ErrSubscriptionEvidenceBudget) {
		return scope, errAutomaticHistoryBound
	}
	return scope, err
}

func automaticRenewalView(row model.SubscriptionAutomaticRenewal, subscription uint) commerce.AutomaticRenewalView {
	out := commerce.AutomaticRenewalView{SubscriptionID: subscription, Enabled: row.Enabled, PlanSKUID: row.PlanSKUID, Revision: row.Revision, State: row.State, Message: row.Message, LastOrderID: row.LastOrderID}
	if out.State == "" {
		out.State = "disabled"
		out.Message = "余额自动续费默认关闭；开启后仅使用该账户的可用站内余额。"
	}
	if row.Enabled {
		next := row.NextAttemptAt
		out.NextAttemptAt = &next
	}
	return out
}

// Consent binds a purchased price/period and the actual offered terms. Changes
// to the paid series or material terms require fresh consent. A missing discount
// agreement cannot silently become a higher automatic charge.
func automaticRenewalQuote(tx *gorm.DB, owner uint, sub model.Subscription, sku uint, now time.Time, discounts ...*checkoutDiscount) (model.Order, commerce.AutomaticRenewalOffer, uint, string, error) {
	var discount *checkoutDiscount
	if len(discounts) > 0 {
		discount = discounts[0]
	}
	if hasCheckoutOffer(discount) {
		discount.AcceptedAt = now
	}
	order, preview, err := prepareOrderWithDiscount(tx, owner, commerce.OrderCreateRequest{PlanSKUID: sku, TargetSubscriptionID: sub.ID}, now, discount)
	if err != nil {
		return order, commerce.AutomaticRenewalOffer{}, 0, "", err
	}
	if order.OrderType != "renewal" || (preview.RequiresPriceAcceptance && !hasCheckoutOffer(discount)) || preview.PriceBasis != "purchased_period" || order.BillingUnit == "once" || order.RenewalEffect != "extend_only" {
		return order, commerce.AutomaticRenewalOffer{}, 0, "", quoteError("仅能为已经购买并锁价的续费周期开启自动续费；其他周期请先手动核对购买。")
	}
	root, err := renewalPurchase(tx, sub.ID, owner, sub.PlanID)
	if err != nil {
		return order, commerce.AutomaticRenewalOffer{}, 0, "", err
	}
	roots, err := readOrderEvidence(tx.Model(&model.Order{}).Where("id = ?", root.ID))
	if err != nil {
		return order, commerce.AutomaticRenewalOffer{}, 0, "", err
	}
	if len(roots) != 1 {
		return order, commerce.AutomaticRenewalOffer{}, 0, "", gorm.ErrRecordNotFound
	}
	root = roots[0]
	var evidence commerce.EntitlementValueSnapshot
	if json.Unmarshal([]byte(root.EntitlementValueSnapshot), &evidence) != nil || evidence.Version != 1 || !evidence.PaymentVerified || evidence.LegacyTransferUnproven || evidence.OrderID != root.ID || evidence.SubscriptionID != sub.ID {
		return order, commerce.AutomaticRenewalOffer{}, 0, "", quoteError("原购买缺少可核对的实付与权益快照，请通过工单核实后再开启自动续费。")
	}
	if hasCheckoutOffer(discount) {
		_, digest, err := automaticRecurringApproval(order, *discount)
		quote := discount.Quote
		return order, commerce.AutomaticRenewalOffer{AgreementVersion: 2, PlanSKUID: sku, Currency: order.Currency, BasePriceMinor: order.AmountCents, DiscountAmountMinor: order.DiscountAmount, PayableAmountMinor: order.PayableAmount, BillingUnit: order.BillingUnit, BillingValue: order.BillingValue, QuoteFingerprint: preview.QuoteFingerprint, Discount: &quote}, root.ID, digest, err
	}
	// A verified first-purchase reduction explicitly ends after that purchase.
	// Unknown or recurring reductions continue to pause automatic charging.
	discounted, discountEvidenceErr := readOrderEvidence(tx.Model(&model.Order{}).Where("subscription_id = ? AND user_id = ? AND id >= ? AND status = ? AND order_type IN ? AND (discount_amount <> 0 OR payable_amount <> amount_cents)", sub.ID, owner, root.ID, "paid", []string{"new", "renewal", "upgrade"}))
	if discountEvidenceErr != nil {
		return order, commerce.AutomaticRenewalOffer{}, 0, "", discountEvidenceErr
	}
	for _, historical := range discounted {
		if !knownInitialPurchaseDiscount(historical) {
			return order, commerce.AutomaticRenewalOffer{}, 0, "", quoteError("历史优惠的首次或长期约定尚未接入可核验报价，自动续费暂停，请手动核对；不会恢复原价扣款。")
		}
	}
	var policy commerce.PurchaseSnapshot
	if json.Unmarshal([]byte(order.PurchaseSnapshot), &policy) != nil {
		return order, commerce.AutomaticRenewalOffer{}, 0, "", invalidPurchaseSnapshot()
	}
	policy.RenewalTarget = nil // The agreement covers terms, not a fixed expiry.
	encoded, err := json.Marshal(struct {
		Root, SKU          uint
		Policy             commerce.PurchaseSnapshot
		Traffic            int64
		Speed, DeviceLimit int
	}{root.ID, sku, policy, order.TrafficBytes, order.SpeedLimitMbps, order.DeviceLimit})
	if err != nil {
		return order, commerce.AutomaticRenewalOffer{}, 0, "", err
	}
	digest := sha256.Sum256(encoded)
	offer := commerce.AutomaticRenewalOffer{AgreementVersion: 1, PayableAmountMinor: order.PayableAmount, PlanSKUID: sku, Currency: order.Currency, BasePriceMinor: order.AmountCents, BillingUnit: order.BillingUnit, BillingValue: order.BillingValue, QuoteFingerprint: preview.QuoteFingerprint}
	return order, offer, root.ID, hex.EncodeToString(digest[:]), nil
}

func (s AutomaticRenewals) Read(ctx context.Context, owner, subscription, sku uint) (out commerce.AutomaticRenewalView, err error) {
	prepared, prepErr := s.evaluateAutomaticDiscount(ctx, owner, subscription, sku)
	if prepErr != nil {
		return s.readAutomaticWithoutOffer(ctx, owner, subscription, prepErr)
	}
	if prepared != nil {
		sku = prepared.SKU
	}
	scope, scopeErr := prepareAutomaticRenewalScope(s.DB.WithContext(ctx), owner, subscription)
	if errors.Is(scopeErr, errAutomaticHistoryBound) {
		return s.readAutomaticConsentWithoutHistory(ctx, owner, subscription)
	}
	if scopeErr != nil {
		return out, scopeErr
	}
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		user, err := automaticRenewalScope(tx, scope)
		if err != nil {
			return err
		}
		if user.Status != "active" {
			return commerce.ErrOrderPermission
		}
		var sub model.Subscription
		if err := tx.Where("id = ? AND user_id = ?", subscription, owner).First(&sub).Error; err != nil {
			return resourceError(err)
		}
		var row model.SubscriptionAutomaticRenewal
		if err := tx.Where("subscription_id = ? AND user_id = ?", subscription, owner).First(&row).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		out = automaticRenewalView(row, subscription)
		if sku == 0 {
			sku = row.PlanSKUID
			if sku == 0 {
				sku = sub.PlanSKUID
			}
		}
		_, offer, _, _, err := automaticRenewalQuote(tx, owner, sub, sku, commerceNow(s.Now), automaticAccepted(prepared))
		var invalid *commerce.ValidationError
		if errors.As(err, &invalid) {
			out.Message = invalid.Message
			return nil
		}
		if err != nil {
			return err
		}
		out.Offer = &offer
		return nil
	})
	if errors.Is(err, errAutomaticHistoryBound) || errors.Is(err, commerce.ErrSubscriptionEvidenceBudget) {
		// The failed transaction has released its bounded order locks. A
		// consent/status read needs no financial history or new debit quote.
		return s.readAutomaticConsentWithoutHistory(ctx, owner, subscription)
	}
	return
}

func (s AutomaticRenewals) Set(ctx context.Context, owner, subscription uint, in commerce.AutomaticRenewalRequest) (out commerce.AutomaticRenewalView, err error) {
	if !in.Enabled {
		return s.disableAutomaticConsent(ctx, owner, subscription, in.ExpectedRevision)
	}
	prepared, err := s.evaluateAutomaticDiscount(ctx, owner, subscription, in.PlanSKUID)
	if err != nil {
		return out, err
	}
	scope, scopeErr := prepareAutomaticRenewalScope(s.DB.WithContext(ctx), owner, subscription)
	if scopeErr != nil {
		return out, scopeErr
	}
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		user, err := automaticRenewalScope(tx, scope)
		if err != nil {
			return err
		}
		if user.Status != "active" {
			return commerce.ErrOrderPermission
		}
		var sub model.Subscription
		if err := tx.Where("id = ? AND user_id = ?", subscription, owner).First(&sub).Error; err != nil {
			return resourceError(err)
		}
		now := commerceNow(s.Now)
		order, offer, root, digest, err := automaticRenewalQuote(tx, owner, sub, in.PlanSKUID, now, automaticAccepted(prepared))
		if err != nil {
			return err
		}
		if offer.QuoteFingerprint != in.QuoteFingerprint {
			return commerce.ErrSubscriptionQuoteChanged
		}
		// Cleanup also uses subscription -> consent. Read the current revision
		// only after the subscription lock when granting debit consent.
		var row model.SubscriptionAutomaticRenewal
		lookup := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("subscription_id = ? AND user_id = ?", subscription, owner).First(&row).Error
		if lookup != nil && !errors.Is(lookup, gorm.ErrRecordNotFound) {
			return lookup
		}
		if row.Revision != in.ExpectedRevision {
			return commerce.ErrSubscriptionQuoteChanged
		}
		if accepted := automaticAccepted(prepared); hasCheckoutOffer(accepted) {
			if err := validateCheckoutOffer(accepted.Context, accepted.Quote, accepted.Quote.Provider.PluginID, commerceNow(s.Now)); err != nil {
				return err
			}
		}
		row.PlanSKUID, row.RootOrderID, row.AgreementDigest = in.PlanSKUID, root, digest
		row.AgreementVersion, row.ApprovalSnapshot = 1, ""
		if hasCheckoutOffer(automaticAccepted(prepared)) {
			row.ApprovalSnapshot, _, err = automaticRecurringApproval(order, *prepared.Discount)
			if err != nil {
				return err
			}
			row.AgreementVersion = 2
		}
		row.State, row.Message = "waiting", "到期后使用站内可用余额续费；余额不足时保留提醒并在恢复窗口内重试。"
		row.NextAttemptAt = maxTime(sub.EndAt, now)
		row.SubscriptionID, row.UserID, row.Enabled = subscription, owner, true
		row.Revision++
		row.UpdatedAt = now
		if errors.Is(lookup, gorm.ErrRecordNotFound) {
			row.CreatedAt = now
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		} else if err := tx.Save(&row).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.AuditLog{UserID: &owner, Actor: fmt.Sprintf("user:%d", owner), Action: "commerce.auto_renew.consent", Target: fmt.Sprintf("subscription:%d", subscription), Detail: fmt.Sprintf("enabled=%t sku=%d revision=%d agreement_version=%d approval_digest=%s", row.Enabled, row.PlanSKUID, row.Revision, row.AgreementVersion, row.AgreementDigest)}).Error; err != nil {
			return err
		}
		out = automaticRenewalView(row, subscription)
		return nil
	})
	return
}

func ownedAutomaticConsent(tx *gorm.DB, owner, subscription uint, lock bool) (model.SubscriptionAutomaticRenewal, error) {
	if err := orderReader(tx, owner, false); err != nil {
		return model.SubscriptionAutomaticRenewal{}, err
	}
	var sub model.Subscription
	if err := tx.Select("id").Where("id = ? AND user_id = ?", subscription, owner).First(&sub).Error; err != nil {
		return model.SubscriptionAutomaticRenewal{}, resourceError(err)
	}
	var row model.SubscriptionAutomaticRenewal
	query := tx.Where("subscription_id = ? AND user_id = ?", subscription, owner)
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	err := query.First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return row, nil
	}
	return row, err
}

func (s AutomaticRenewals) readAutomaticConsentWithoutHistory(ctx context.Context, owner, subscription uint) (out commerce.AutomaticRenewalView, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row, err := ownedAutomaticConsent(tx, owner, subscription, false)
		if err != nil {
			return err
		}
		out = automaticRenewalView(row, subscription)
		if row.Enabled {
			out.State = "history_review_required"
			out.Message = automaticHistoryReviewMessage
		}
		return nil
	})
	return
}

// Revoking future debit permission does not settle value, acquire old orders,
// or change a subscription. User SHARE -> consent UPDATE matches payment's
// owner-before-consent order without an inverse subscription/order wait.
func (s AutomaticRenewals) disableAutomaticConsent(ctx context.Context, owner, subscription uint, revision uint64) (out commerce.AutomaticRenewalView, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if tx.Dialector.Name() == "sqlite" {
			// Acquire its writer reservation before a read snapshot. The no-op
			// changes no consent; all authorization checks remain in this TX.
			if err := tx.Exec("UPDATE subscription_automatic_renewals SET revision = revision WHERE subscription_id = ? AND user_id = ?", subscription, owner).Error; err != nil {
				return err
			}
		}
		row, err := ownedAutomaticConsent(tx, owner, subscription, true)
		if err != nil {
			return err
		}
		if row.Revision != revision {
			return commerce.ErrSubscriptionQuoteChanged
		}
		if row.Revision == 0 {
			out = automaticRenewalView(row, subscription)
			return nil
		}
		now := commerceNow(s.Now)
		if err := tx.Model(&row).Updates(map[string]any{"enabled": false, "state": "disabled", "message": "已关闭余额自动续费。", "revision": gorm.Expr("revision + 1"), "updated_at": now}).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.AuditLog{UserID: &owner, Actor: fmt.Sprintf("user:%d", owner), Action: "commerce.auto_renew.consent", Target: fmt.Sprintf("subscription:%d", subscription), Detail: fmt.Sprintf("enabled=false sku=%d revision=%d", row.PlanSKUID, revision+1)}).Error; err != nil {
			return err
		}
		row.Enabled, row.State, row.Message, row.Revision = false, "disabled", "已关闭余额自动续费。", revision+1
		out = automaticRenewalView(row, subscription)
		return nil
	})
	return
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

// The caller owns the terminal/change transaction and its subscription lock.
func stopAutomaticRenewal(tx *gorm.DB, subscription uint, state, message string, now time.Time) error {
	return tx.Model(&model.SubscriptionAutomaticRenewal{}).Where("subscription_id = ? AND enabled = ?", subscription, true).
		Updates(map[string]any{"enabled": false, "revision": gorm.Expr("revision + 1"), "state": state, "message": message, "updated_at": now}).Error
}
