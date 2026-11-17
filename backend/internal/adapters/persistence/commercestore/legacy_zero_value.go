package commercestore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const legacyZeroValueKind = "legacy_value_baseline"
const legacyZeroValueBasis = "legacy_zero_payment_migration"

// This is migration evidence, not a historical purchase or payment snapshot.
// Only an all-zero remaining paid basis may enter it. Original orders stay intact.
type legacyZeroValueBaseline struct {
	Version      int                            `json:"version"`
	Kind         string                         `json:"kind"`
	Epoch        uint64                         `json:"epoch"`
	UpperOrderID uint                           `json:"upper_order_id"`
	SourceHash   string                         `json:"source_sha256"`
	RootOrderID  uint                           `json:"root_order_id"`
	Currency     string                         `json:"currency"`
	CapturedAt   time.Time                      `json:"captured_at"`
	State        commerce.EntitlementValueState `json:"subscription"`
	Policy       legacyZeroPolicy               `json:"current_policy"`
}
type legacyZeroPolicy struct {
	PlanName        string `json:"plan_name"`
	DeviceLimit     int    `json:"device_limit"`
	SpeedLimitMbps  int    `json:"speed_limit_mbps"`
	FamilyLimit     int    `json:"family_limit"`
	TrafficCalcMode int16  `json:"traffic_calc_mode"`
}

var legacyValueColumns = []string{"id", "user_id", "subscription_id", "plan_id", "plan_sku_id", "order_type", "status", "amount_cents", "payable_amount", "paid_amount", "refund_amount", "discount_amount", "currency", "billing_unit", "billing_value", "renewal_effect", "traffic_bytes", "paid_at", "fulfilled_at", "created_at", "purchase_snapshot", "entitlement_value_snapshot", "change_snapshot"}

func legacyValueHash(rows []model.Order) string {
	// Explicit selected fields exclude mutable UpdatedAt and later lifecycle closure.
	// Preserve the original evidence schema independently of public JSON aliases.
	type evidenceOrder model.Order
	evidence := make([]evidenceOrder, len(rows))
	for i := range rows {
		evidence[i] = evidenceOrder(rows[i])
	}
	raw, _ := json.Marshal(evidence)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func legacyZeroValueRecords(query *gorm.DB, sub model.Subscription, at time.Time) ([]commerce.EntitlementValueSnapshot, map[uint]model.Order, string, error) {
	fail := func() ([]commerce.EntitlementValueSnapshot, map[uint]model.Order, string, error) {
		return nil, nil, "", commerce.ErrSubscriptionValueUnavailable
	}
	tx := query.Session(&gorm.Session{NewDB: true})
	if _, ok := tx.Statement.ConnPool.(gorm.TxCommitter); !ok {
		return fail()
	}
	// A current source read must follow the subscription fence. NOWAIT avoids
	// reversing an order -> subscription writer's ownership while quoting.
	var fresh model.Subscription
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "NOWAIT"}).First(&fresh, sub.ID).Error; err != nil {
		return nil, nil, "", planBaseQuotaReadError(err)
	}
	if fresh.UserID != sub.UserID || fresh.EntitlementEpoch != sub.EntitlementEpoch || fresh.PlanID != sub.PlanID || fresh.PlanSKUID != sub.PlanSKUID || !fresh.StartAt.Equal(sub.StartAt) || !fresh.EndAt.Equal(sub.EndAt) || fresh.FlowTotal != sub.FlowTotal || fresh.CycleStartUsed != sub.CycleStartUsed || fresh.ResetPolicy != sub.ResetPolicy || fresh.ResetQuotaBytes != sub.ResetQuotaBytes {
		return nil, nil, "", commerce.ErrSubscriptionQuoteChanged
	}
	currentQuery := planBaseQuotaCurrentRead(subscriptionValueOrderQuery(tx, fresh))
	orders, err := readOrderEvidence(currentQuery, legacyValueColumns...)
	if err != nil {
		return nil, nil, "", planBaseQuotaReadError(err)
	}
	if len(orders) == 0 {
		return fail()
	}
	// A zero order cannot conceal an already captured monetary contribution.
	for start := 0; start < len(orders); start += orderEvidencePageSize {
		ids := []uint{}
		for _, o := range orders[start:min(start+orderEvidencePageSize, len(orders))] {
			if o.PaidAmount == 0 {
				ids = append(ids, o.ID)
			}
		}
		if len(ids) == 0 {
			continue
		}
		var receipt model.PaymentReceipt
		if err := tx.Select("id").Where("order_id IN ? AND amount_minor>0", ids).Limit(1).Find(&receipt).Error; err != nil {
			return nil, nil, "", err
		}
		if receipt.ID != 0 {
			return fail()
		}
		var funding model.OrderFunding
		if err := tx.Select("order_id").Where("order_id IN ? AND captured_at IS NOT NULL AND (amount_minor>0 OR entitlement_minor>0)", ids).Limit(1).Find(&funding).Error; err != nil {
			return nil, nil, "", err
		}
		if funding.OrderID != 0 {
			return fail()
		}
	}
	var event model.QuotaEvent
	ref := fmt.Sprintf("%d", fresh.EntitlementEpoch)
	var header struct {
		ID            uint
		EvidenceBytes int64
	}
	length := "OCTET_LENGTH(detail)"
	if tx.Dialector.Name() == "sqlite" {
		length = "LENGTH(CAST(detail AS BLOB))"
	}
	q := planBaseQuotaCurrentRead(tx.Model(&model.QuotaEvent{})).Select("id,"+length+" AS evidence_bytes").Where("subscription_id=? AND event_type=? AND reference_type=? AND reference_id=?", fresh.ID, legacyZeroValueKind, "legacy_zero_migration", ref).Find(&header)
	if q.Error != nil {
		return nil, nil, "", q.Error
	}
	var baseline legacyZeroValueBaseline
	if header.ID != 0 {
		if header.EvidenceBytes < 1 || header.EvidenceBytes > 64<<10 {
			return fail()
		}
		if e := planBaseQuotaCurrentRead(tx).First(&event, header.ID).Error; e != nil {
			return nil, nil, "", e
		}
		if int64(len(event.Detail)) != header.EvidenceBytes {
			return fail()
		}
	}
	if header.ID == 0 {
		// Create only once for this epoch; an existing malformed baseline is refused.
		if fresh.EndedAt != nil || (fresh.Status != "active" && fresh.Status != "expired") || at.Before(fresh.StartAt) || !fresh.EndAt.After(at) || fresh.FlowUsed < fresh.CycleStartUsed || fresh.CycleStartUsed < 0 || fresh.FlowTotal < fresh.CycleStartUsed {
			return fail()
		}
		root, currency, e := proveLegacyZeroRemaining(tx, fresh, orders, at)
		if e != nil {
			return nil, nil, "", e
		}
		var plan model.Plan
		if e := tx.Select("id,name").First(&plan, fresh.PlanID).Error; e != nil {
			return nil, nil, "", e
		}
		baseline = legacyZeroValueBaseline{Version: 1, Kind: legacyZeroValueBasis, Epoch: fresh.EntitlementEpoch, UpperOrderID: orders[len(orders)-1].ID, SourceHash: legacyValueHash(orders), RootOrderID: root, Currency: currency, CapturedAt: at.UTC().Truncate(time.Millisecond), State: entitlementValueState(fresh), Policy: legacyZeroPolicy{PlanName: plan.Name, DeviceLimit: fresh.DeviceLimit, SpeedLimitMbps: fresh.SpeedLimitMbps, FamilyLimit: fresh.FamilyLimit, TrafficCalcMode: fresh.TrafficCalcMode}}
		raw, e := json.Marshal(baseline)
		if e != nil {
			return nil, nil, "", e
		}
		balance := max(int64(0), fresh.FlowTotal-fresh.FlowUsed)
		event = model.QuotaEvent{SubscriptionID: fresh.ID, EntitlementEpoch: fresh.EntitlementEpoch, EventType: legacyZeroValueKind, ReferenceType: "legacy_zero_migration", ReferenceID: ref, DeltaBytes: 0, BalanceBefore: balance, BalanceAfter: balance, Detail: string(raw), CreatedAt: baseline.CapturedAt}
		if e := tx.Create(&event).Error; e != nil {
			return nil, nil, "", e
		}
	}
	if json.Unmarshal([]byte(event.Detail), &baseline) != nil || baseline.Version != 1 || baseline.Kind != legacyZeroValueBasis || baseline.Epoch != fresh.EntitlementEpoch || event.EntitlementEpoch != fresh.EntitlementEpoch || baseline.State.PlanID != fresh.PlanID || !baseline.State.StartAt.Equal(fresh.StartAt) || baseline.CapturedAt.IsZero() || at.Before(baseline.CapturedAt) || !event.CreatedAt.Equal(baseline.CapturedAt) || event.DeltaBytes != 0 || event.BalanceBefore != event.BalanceAfter || event.BalanceBefore != max(int64(0), baseline.State.FlowTotal-baseline.State.FlowUsed) {
		return fail()
	}
	original := []model.Order{}
	later := []model.Order{}
	sources := map[uint]model.Order{}
	for _, o := range orders {
		sources[o.ID] = o
		var fact commerce.EntitlementValueSnapshot
		if o.EntitlementValueSnapshot != "" && json.Unmarshal([]byte(o.EntitlementValueSnapshot), &fact) == nil && fact.QuotaEventID > event.ID {
			later = append(later, o)
		} else if o.ID <= baseline.UpperOrderID {
			original = append(original, o)
		} else {
			return fail()
		}
	}
	if legacyValueHash(original) != baseline.SourceHash {
		return fail()
	}
	root, ok := sources[baseline.RootOrderID]
	if !ok || root.ID > baseline.UpperOrderID || root.PaidAmount != 0 || root.PayableAmount != 0 || root.Currency != baseline.Currency {
		return fail()
	}
	synthetic := commerce.EntitlementValueSnapshot{Version: 1, OrderID: root.ID, SubscriptionID: fresh.ID, Currency: baseline.Currency, PaymentVerified: true, ActualPaidMinor: 0, PaymentBasis: legacyZeroValueBasis, Kind: "legacy_migration", Mode: "new_term", CapturedAt: baseline.CapturedAt, QuotaEventID: event.ID, QuotaEventAt: event.CreatedAt, TermStartAt: baseline.State.StartAt, TermEndAt: baseline.State.EndAt, After: baseline.State, AllocationModel: "uniform_paid_units_v1", BillingUnit: "month", BillingValue: 1, PaidPeriods: []commerce.PaidValuePeriod{{StartAt: baseline.State.StartAt, EndAt: baseline.State.EndAt, AmountMinor: 0}}}
	if entitlements.IsPerpetualEnd(baseline.State.EndAt) {
		synthetic.AllocationModel = commerce.PerpetualQuotaAllocation
		synthetic.BillingUnit = "once"
		synthetic.PaidPeriods = nil
	}
	records := []commerce.EntitlementValueSnapshot{synthetic}
	for _, o := range later {
		var fact commerce.EntitlementValueSnapshot
		if json.Unmarshal([]byte(o.EntitlementValueSnapshot), &fact) != nil || fact.OrderID != o.ID || fact.SubscriptionID != fresh.ID || fact.ActualPaidMinor != o.PaidAmount || fact.Currency != baseline.Currency || o.RefundAmount != 0 || fact.QuotaEventID <= event.ID {
			return fail()
		}
		records = append(records, fact)
	}
	return records, sources, baseline.Currency, nil
}

// Positive legacy money is never erased by a zero-price order. Only the narrow
// original timed new -> additive extension history can prove it already expired.
func proveLegacyZeroRemaining(tx *gorm.DB, sub model.Subscription, rows []model.Order, at time.Time) (uint, string, error) {
	fail := func() (uint, string, error) { return 0, "", commerce.ErrSubscriptionValueUnavailable }
	currency := rows[0].Currency
	var root uint
	positive := false
	for _, o := range rows {
		if o.UserID != sub.UserID || o.SubscriptionID != sub.ID || o.Currency != currency || o.PaidAmount < 0 || o.RefundAmount != 0 || o.ChangeSnapshot != "" {
			return fail()
		}
		if o.PaidAmount == 0 {
			if o.PayableAmount != 0 {
				return fail()
			}
			root = o.ID
		} else {
			positive = true
		}
		if o.EntitlementValueSnapshot != "" {
			var f commerce.EntitlementValueSnapshot
			if json.Unmarshal([]byte(o.EntitlementValueSnapshot), &f) != nil || f.Version != 1 || f.Currency != currency || f.OrderID != o.ID || f.SubscriptionID != sub.ID || f.ActualPaidMinor != o.PaidAmount || f.LegacyTransferUnproven || !f.PaymentVerified {
				return fail()
			}
		}
	}
	if root == 0 || currency == "" {
		return fail()
	}
	if !positive {
		return root, currency, nil
	}
	// Replay dated grants against their original immutable quota-event balances,
	// not current catalog prices, reset policy or a guessed order-created clock.
	cursor := sub.StartAt
	total := int64(0)
	var lastEvent uint
	var lastUsed int64
	for i, o := range rows {
		if o.EntitlementValueSnapshot != "" || o.PaidAt == nil || o.FulfilledAt == nil || o.PaidAt.IsZero() || !o.PaidAt.Equal(*o.FulfilledAt) || o.PayableAmount != o.PaidAmount || o.DiscountAmount != 0 || o.TrafficBytes <= 0 || (i == 0 && (o.OrderType != "new" || !o.PaidAt.Equal(sub.StartAt))) || (i > 0 && o.OrderType != "upgrade" && o.OrderType != "renewal") || (i > 0 && o.RenewalEffect != "extend_only") {
			return fail()
		}
		var events []model.QuotaEvent
		if e := planBaseQuotaCurrentRead(tx).Select("id", "event_type", "entitlement_epoch", "created_at", "delta_bytes", "balance_before", "balance_after").Where("subscription_id=? AND reference_type='order' AND reference_id=?", sub.ID, fmt.Sprint(o.ID)).Order("id").Limit(2).Find(&events).Error; e != nil {
			return 0, "", planBaseQuotaReadError(e)
		}
		if len(events) != 1 {
			return fail()
		}
		e := events[0]
		expected := "upgrade"
		if i == 0 {
			expected = "purchase"
		} else if o.OrderType == "renewal" {
			expected = "renewal"
		}
		if e.EventType != expected || e.EntitlementEpoch != sub.EntitlementEpoch || e.ID <= lastEvent || (e.CreatedAt.Before(o.FulfilledAt.Truncate(time.Millisecond)) || e.CreatedAt.After(o.FulfilledAt.Add(time.Minute))) || e.DeltaBytes != o.TrafficBytes || e.BalanceBefore < 0 || e.BalanceBefore > total || e.BalanceAfter-e.BalanceBefore != o.TrafficBytes {
			return fail()
		}
		used := total - e.BalanceBefore
		if used < lastUsed {
			return fail()
		}
		lastUsed = used
		if total > int64(^uint64(0)>>1)-o.TrafficBytes {
			return fail()
		}
		total += o.TrafficBytes
		lastEvent = e.ID
		if o.PaidAt.After(cursor) {
			cursor = *o.PaidAt
		}
		end, e2 := entitlements.AddBillingPeriod(cursor, o.BillingUnit, o.BillingValue)
		if e2 != nil {
			return fail()
		}
		if o.PaidAmount > 0 && end.After(at) {
			return fail()
		}
		cursor = end
	}
	if !cursor.Equal(sub.EndAt) || total != sub.FlowTotal || sub.FlowUsed < lastUsed || sub.CycleStartUsed != 0 || sub.ResetQuotaBytes < 0 || (sub.ResetPolicy != 0 && sub.ResetPolicy != 5) || sub.NextResetAt != nil {
		return fail()
	}
	// Other quota adjustments invalidate this narrow reconstruction.
	var extra model.QuotaEvent
	q := tx.Select("id").Where("subscription_id=? AND event_type NOT IN ?", sub.ID, []string{"purchase", "upgrade", "renewal", legacyZeroValueKind}).Limit(1).Find(&extra)
	if q.Error != nil {
		return 0, "", q.Error
	}
	if q.RowsAffected != 0 {
		return fail()
	}
	return root, currency, nil
}
