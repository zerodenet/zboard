package commercestore

import (
	"errors"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

// The last purchase/replacement starts the current price agreement. Renewals
// and discounts never replace that agreement. Read actual order snapshots;
// live SKU prices and a subscription's possibly legacy price cannot establish
// a missing historical currency or billing period.
func renewalPurchase(tx *gorm.DB, subscriptionID, buyer, planID uint) (model.Order, error) {
	var source model.Order
	err := tx.Select("id", "amount_cents", "currency", "billing_unit", "billing_value").
		Where("subscription_id = ? AND user_id = ? AND plan_id = ? AND status = ? AND order_type IN ?", subscriptionID, buyer, planID, "paid", []string{"new", "upgrade"}).
		Order("id desc").First(&source).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return source, quoteError("原订阅缺少可核对的购买价格快照，请通过工单核实，不能按当前价格自动续费。")
	}
	return source, err
}
func compatibleRenewalPurchase(source, order model.Order) bool {
	return source.AmountCents >= 0 && source.Currency != "" && source.BillingUnit != "" && source.BillingValue > 0 &&
		source.Currency == order.Currency && source.BillingUnit == order.BillingUnit && source.BillingValue == order.BillingValue
}
