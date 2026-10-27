package commercestore

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

// A zero-money termination never needs to deserialize a paid source's large
// commercial body. Pending funding still requires its normal bounded proof;
// this does not bypass reservation validation or release unknown money.
func readZeroTerminationOrders(tx *gorm.DB, sub model.Subscription) ([]model.Order, error) {
	paid, _, err := readPaidTerminationMetadata(tx, sub)
	if err != nil {
		return nil, err
	}
	mutable, err := readOrderEvidence(terminationOrders(tx, sub.UserID, sub.ID, sub.StartAt).Where("status <> ?", "paid"))
	if err != nil {
		return nil, err
	}
	out := append(paid, mutable...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func readPaidTerminationMetadata(tx *gorm.DB, sub model.Subscription) ([]model.Order, map[uint]int64, error) {
	var rows []model.Order
	lengths := map[uint]int64{}
	query := terminationOrders(tx, sub.UserID, sub.ID, sub.StartAt).Where("status = ?", "paid")
	err := visitOrderMetadata(query, false, func(page []orderEvidenceIdentity) error {
		ids := make([]uint, len(page))
		for i, row := range page {
			ids[i] = row.ID
			lengths[row.ID] = row.EvidenceBytes
		}
		var items []model.Order
		if err := query.Session(&gorm.Session{}).Omit("raw_callback", "purchase_snapshot", "entitlement_value_snapshot", "change_snapshot").Where("id IN ?", ids).Order("id ASC").Limit(orderEvidencePageSize).Find(&items).Error; err != nil {
			return err
		}
		if len(items) != len(page) {
			return commerce.ErrSubscriptionQuoteChanged
		}
		for i, row := range items {
			if row.ID != ids[i] {
				return commerce.ErrSubscriptionQuoteChanged
			}
		}
		rows = append(rows, items...)
		return nil
	})
	return rows, lengths, err
}

// Resource-limited valuation is explicitly unknown, not a zero residual. Its
// operational quote binds current rights and immutable monetary metadata; it
// makes no assertion about the deliberately unread commercial JSON content.
func unpricedBudgetQuote(tx *gorm.DB, sub model.Subscription) (commerce.AdministrativeSubscriptionValueQuote, error) {
	paid, lengths, err := readPaidTerminationMetadata(tx, sub)
	if err != nil {
		return commerce.AdministrativeSubscriptionValueQuote{}, err
	}
	mutable, err := readOrderEvidence(terminationOrders(tx, sub.UserID, sub.ID, sub.StartAt).Where("status <> ?", "paid"))
	if err != nil {
		return commerce.AdministrativeSubscriptionValueQuote{}, err
	}
	orders := append(paid, mutable...)
	sort.Slice(orders, func(i, j int) bool { return orders[i].ID < orders[j].ID })
	type boundary struct {
		ID, Owner, Subscription  uint
		Target                   *uint
		Status, Currency, Reason string
		Paid, Refund             int64
		EndedAt                  *time.Time
		UpdatedAt                time.Time
		MutableEvidence          string
	}
	boundaries := make([]boundary, 0, len(orders))
	for _, o := range orders {
		proof := ""
		if o.Status != "paid" {
			proof = walletKey(o.PurchaseSnapshot + "\x00" + o.EntitlementValueSnapshot + "\x00" + o.ChangeSnapshot)
		}
		boundaries = append(boundaries, boundary{o.ID, o.UserID, o.SubscriptionID, o.TargetSubscriptionID, o.Status, o.Currency, o.SubscriptionEndReason, o.PaidAmount, o.RefundAmount, o.SubscriptionEndedAt, o.UpdatedAt, proof})
	}
	raw, err := json.Marshal(struct {
		Version                    string
		Owner, Subscription, Group uint
		Epoch                      uint64
		State                      commerce.EntitlementValueState
		Orders                     []boundary
		EvidenceBytes              map[uint]int64
	}{"admin_zero_evidence_budget_v1", sub.UserID, sub.ID, sub.NodeGroupID, sub.EntitlementEpoch, entitlementValueState(sub), boundaries, lengths})
	if err != nil {
		return commerce.AdministrativeSubscriptionValueQuote{}, err
	}
	return commerce.AdministrativeSubscriptionValueQuote{SubscriptionID: sub.ID, UserID: sub.UserID, Fingerprint: walletKey(string(raw)), ValuationStatus: "unavailable", Message: "成交正文超过单次核对预算，默认残值及可退实付保持未知；只能明确输入0元终止，未读取超额正文。"}, nil
}
