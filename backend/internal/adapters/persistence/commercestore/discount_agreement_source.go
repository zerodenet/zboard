package commercestore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// This scope locks only the selected root/base/latest primary keys. It does
// not lock the subscription's entire history. Preparation is outside the TX.
type discountSourceScope struct {
	Buyer, Target uint
	IDs           []uint
}

func selectedDiscountSourceIDs(db *gorm.DB, buyer, target, plan uint, currency, unit string, period int) ([]uint, error) {
	var root model.Order
	err := db.Select("id").Where("user_id = ? AND subscription_id = ? AND plan_id = ? AND status = ? AND order_type IN ?", buyer, target, plan, "paid", []string{"new", "upgrade"}).Order("id DESC").First(&root).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	ids := []uint{root.ID}
	for _, direction := range []string{"ASC", "DESC"} {
		var row model.Order
		err = db.Select("id").Where("user_id = ? AND subscription_id = ? AND plan_id = ? AND status = ? AND id >= ? AND order_type IN ? AND currency = ? AND billing_unit = ? AND billing_value = ?", buyer, target, plan, "paid", root.ID, []string{"new", "upgrade", "renewal"}, currency, unit, period).Order("id " + direction).First(&row).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		if err == nil {
			ids = append(ids, row.ID)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	unique := ids[:0]
	for _, id := range ids {
		if len(unique) == 0 || unique[len(unique)-1] != id {
			unique = append(unique, id)
		}
	}
	return unique, nil
}

func prepareDiscountSourceScope(db *gorm.DB, buyer uint, request commerce.OrderCreateRequest) (*discountSourceScope, error) {
	scope := &discountSourceScope{Buyer: buyer, Target: request.TargetSubscriptionID}
	var sub model.Subscription
	if err := db.Select("id", "plan_id").Where("id = ? AND user_id = ?", scope.Target, buyer).First(&sub).Error; err != nil {
		return nil, orderResourceError(err, "target_subscription_id", "目标订阅不存在。")
	}
	var sku model.PlanSKU
	if err := db.Select("id", "plan_id", "currency", "billing_unit", "billing_value").First(&sku, request.PlanSKUID).Error; err != nil {
		return nil, orderResourceError(err, "plan_sku_id", "销售规格不存在。")
	}
	if sku.PlanID != sub.PlanID {
		return scope, nil
	}
	ids, err := selectedDiscountSourceIDs(db, buyer, scope.Target, sub.PlanID, sku.Currency, sku.BillingUnit, sku.BillingValue)
	scope.IDs = ids
	return scope, err
}

func discountSourceReadError(err error) error {
	var typed *mysql.MySQLError
	if errors.As(err, &typed) && typed.Number == 3572 {
		return commerce.ErrSubscriptionQuoteChanged
	}
	return err
}

// No ordinary SELECT may precede owner acquisition in this transaction. The
// exact PK locking read is current under InnoDB RR. This matches payment,
// refund and replacement's source-order-before-owner order.
func lockPreparedDiscountSources(tx *gorm.DB, scope *discountSourceScope) error {
	if scope == nil || len(scope.IDs) == 0 {
		return nil
	}
	if tx.Dialector.Name() == "sqlite" {
		if err := tx.Exec("UPDATE orders SET id = id WHERE id = ?", scope.IDs[0]).Error; err != nil {
			return err
		}
	}
	var ids []uint
	if err := tx.Model(&model.Order{}).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id IN ?", scope.IDs).Order("id ASC").Pluck("id", &ids).Error; err != nil {
		return err
	}
	if len(ids) != len(scope.IDs) {
		return commerce.ErrSubscriptionQuoteChanged
	}
	return nil
}

func readDiscountAgreementSource(tx *gorm.DB, order model.Order, sub model.Subscription, scope *discountSourceScope) (*commerce.DiscountSourceSnapshot, error) {
	ids, err := selectedDiscountSourceIDs(tx, order.UserID, sub.ID, order.PlanID, order.Currency, order.BillingUnit, order.BillingValue)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	if scope != nil {
		if scope.Buyer != order.UserID || scope.Target != sub.ID {
			return nil, commerce.ErrSubscriptionQuoteChanged
		}
		allowed := map[uint]bool{}
		for _, id := range scope.IDs {
			allowed[id] = true
		}
		for _, id := range ids {
			if !allowed[id] {
				return nil, commerce.ErrSubscriptionQuoteChanged
			}
		}
	}
	// Sources already held before owner are reentrant here. Internal callers
	// without a prepared scope may only acquire exact source PKs immediately.
	var selected []model.Order
	if err := tx.Clauses(clause.Locking{Strength: "SHARE", Options: "NOWAIT"}).Where("id IN ?", ids).Order("id ASC").Find(&selected).Error; err != nil {
		return nil, discountSourceReadError(err)
	}
	var root model.Order
	for _, row := range selected {
		if row.OrderType == "new" || row.OrderType == "upgrade" {
			if row.ID > root.ID {
				root = row
			}
		}
	}
	if root.ID == 0 {
		return nil, commerce.ErrSubscriptionQuoteChanged
	}
	// Every supported source writer holds owner before modifying commercial
	// evidence: settlement/funding, consumption refund, full replacement and
	// lifecycle closure. The first consistent snapshot is established only after
	// owner acquisition, so this bounded nonlocking evidence scan is stable.
	var series []model.Order
	if err := tx.Where("user_id = ? AND subscription_id = ? AND plan_id = ? AND status = ? AND id >= ? AND order_type IN ?", order.UserID, sub.ID, order.PlanID, "paid", root.ID, []string{"new", "upgrade", "renewal"}).Order("id ASC").Limit(1001).Find(&series).Error; err != nil {
		return nil, err
	}
	if len(series) == 0 || len(series) > 1000 {
		return nil, discountCheckoutError("购买协议系列超出可安全核对范围，请通过工单核实。")
	}
	rootPolicy, rootErr := commerce.DecodePurchaseSnapshot(root.PurchaseSnapshot)
	recurring := rootErr == nil && rootPolicy.Discount != nil && rootPolicy.Discount.Version == 2 && rootPolicy.Discount.Quote.Offer.Agreement != nil && rootPolicy.Discount.Quote.Offer.Agreement.Scope == "recurring"
	if !recurring {
		for _, row := range series {
			var policy commerce.PurchaseSnapshot
			var fields map[string]json.RawMessage
			_ = json.Unmarshal([]byte(row.PurchaseSnapshot), &fields)
			_, evidencePresent := fields["discount"]
			if row.PurchaseSnapshot != "" && json.Unmarshal([]byte(row.PurchaseSnapshot), &policy) != nil {
				return nil, invalidPurchaseSnapshot()
			}
			if evidencePresent || policy.Discount != nil || row.DiscountAmount != 0 || row.PayableAmount != row.AmountCents {
				if !knownInitialPurchaseDiscount(row) {
					return nil, discountCheckoutError("历史优惠协议无法核验，不能按原价回退续费；请通过工单核实。")
				}
			}
		}
		return nil, nil
	}
	if root.OrderType != "new" || root.Currency != order.Currency || root.BillingUnit != order.BillingUnit || root.BillingValue != order.BillingValue || root.AmountCents != order.AmountCents {
		return nil, discountCheckoutError("当前续费周期缺少已购的长期优惠约定，不能回退原价。")
	}
	var previous model.Order
	var rootAgreement commerce.DiscountAgreement
	var provider commerce.DiscountProviderIdentity
	var selection string
	type sourceFact struct {
		ID                                      uint
		Amount, Discount, Payable, Paid, Refund int64
		Snapshot                                string
		EndedAt                                 *time.Time
		EndReason                               string
	}
	facts := []sourceFact{}
	for _, row := range series {
		if row.AmountCents != root.AmountCents || row.Currency != root.Currency || row.BillingUnit != root.BillingUnit || row.BillingValue != root.BillingValue {
			return nil, discountCheckoutError("购买系列存在其他周期，长期优惠需重新核对。")
		}
		policy, err := commerce.DecodePurchaseSnapshot(row.PurchaseSnapshot)
		if err != nil || commerce.ValidateRecurringPurchaseDiscount(commerce.Order(row), policy) != nil || row.PaidAmount != row.PayableAmount || row.RefundAmount != 0 {
			return nil, invalidPurchaseSnapshot()
		}
		if _, err := applyPurchaseSnapshot(row, entitlements.GrantPolicy{}); err != nil {
			return nil, err
		}
		var value commerce.EntitlementValueSnapshot
		if json.Unmarshal([]byte(row.EntitlementValueSnapshot), &value) != nil || value.Version != 1 || !value.PaymentVerified || value.LegacyTransferUnproven || value.OrderID != row.ID || value.SubscriptionID != sub.ID || value.ActualPaidMinor != row.PaidAmount || value.Currency != row.Currency {
			return nil, discountCheckoutError("长期优惠缺少真实已付款权益证据，请通过工单核实。")
		}
		saved := policy.Discount
		if previous.ID == 0 {
			if row.ID != root.ID || saved.Source != nil {
				return nil, invalidPurchaseSnapshot()
			}
			rootAgreement = *saved.Quote.Offer.Agreement
			provider = saved.Quote.Provider
			selection = saved.Context.Selection
		} else {
			src := saved.Source
			if row.OrderType != "renewal" || src == nil || src.RootOrderID != root.ID || src.BaseOrderID != root.ID || src.AgreementOrderID != previous.ID || src.SubscriptionID != sub.ID || src.ProviderID != provider.PluginID || src.SigningKeyFingerprint != provider.SigningKeyFingerprint || src.Selection != selection {
				return nil, invalidPurchaseSnapshot()
			}
		}
		agreement := saved.Quote.Offer.Agreement
		a, _ := json.Marshal(agreement)
		b, _ := json.Marshal(rootAgreement)
		if string(a) != string(b) || saved.Quote.Provider.PluginID != provider.PluginID || saved.Quote.Provider.SigningKeyFingerprint != provider.SigningKeyFingerprint {
			return nil, invalidPurchaseSnapshot()
		}
		facts = append(facts, sourceFact{row.ID, row.AmountCents, row.DiscountAmount, row.PayableAmount, row.PaidAmount, row.RefundAmount, row.PurchaseSnapshot, row.SubscriptionEndedAt, row.SubscriptionEndReason})
		previous = row
	}
	raw, _ := json.Marshal(facts)
	digest := sha256.Sum256(raw)
	return &commerce.DiscountSourceSnapshot{Version: 1, SubscriptionID: sub.ID, RootOrderID: root.ID, BaseOrderID: root.ID, AgreementOrderID: previous.ID, Fingerprint: hex.EncodeToString(digest[:]), EntitlementEpoch: sub.EntitlementEpoch, ProviderID: provider.PluginID, SigningKeyFingerprint: provider.SigningKeyFingerprint, Agreement: rootAgreement, Selection: selection}, nil
}
