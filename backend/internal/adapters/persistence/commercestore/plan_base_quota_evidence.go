package commercestore

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// The owning plan transaction already holds subscription authorities. Financial
// writers acquire order -> account -> plan -> subscription. Never wait for an
// order here in the reverse direction. MySQL NOWAIT is a current locking read;
// SQLite's already-owned write transaction supplies its serialized snapshot.
func planBaseQuotaCurrentRead(tx *gorm.DB) *gorm.DB {
	if tx.Dialector.Name() == "sqlite" {
		return tx
	}
	return tx.Clauses(clause.Locking{Strength: "SHARE", Options: "NOWAIT"})
}

func planBaseQuotaReadError(err error) error {
	var mysqlErr *mysqlDriver.MySQLError
	if errors.As(err, &mysqlErr) && (mysqlErr.Number == 3572 || mysqlErr.Number == 1205 || mysqlErr.Number == 1213) {
		return &commerce.ValidationError{Message: "订阅订单或额度来源正在变更，套餐与订阅均未修改，请重试。", Fields: map[string]string{"sync_traffic_quota": "同步已回滚；请在并发订单处理结束后重新核对并提交。"}}
	}
	return err
}

func subscriptionValueRecords(query *gorm.DB, sub model.Subscription) ([]commerce.EntitlementValueSnapshot, map[uint]model.Order, string, error) {
	orders, err := readOrderEvidence(query, "id", "paid_amount", "refund_amount", "currency", "entitlement_value_snapshot", "subscription_ended_at")
	if err != nil {
		return nil, nil, "", err
	}
	if len(orders) == 0 {
		return nil, nil, "", commerce.ErrSubscriptionValueUnavailable
	}
	records := make([]commerce.EntitlementValueSnapshot, 0, len(orders))
	sources := make(map[uint]model.Order, len(orders))
	currency := ""
	for _, order := range orders {
		if err := query.Statement.Context.Err(); err != nil {
			return nil, nil, "", err
		}
		var fact commerce.EntitlementValueSnapshot
		if json.Unmarshal([]byte(order.EntitlementValueSnapshot), &fact) != nil || fact.OrderID != order.ID || fact.SubscriptionID != sub.ID || fact.ActualPaidMinor != order.PaidAmount || fact.Currency != order.Currency || order.RefundAmount != 0 {
			return nil, nil, "", commerce.ErrSubscriptionValueUnavailable
		}
		if currency == "" {
			currency = order.Currency
		}
		if currency != order.Currency {
			return nil, nil, "", commerce.ErrSubscriptionValueUnavailable
		}
		records = append(records, fact)
		sources[order.ID] = order
	}
	return records, sources, currency, nil
}

func subscriptionValueOrderQuery(tx *gorm.DB, sub model.Subscription) *gorm.DB {
	return tx.Model(&model.Order{}).Where("subscription_id = ? AND status = ? AND (subscription_ended_at IS NULL OR subscription_ended_at > ?)", sub.ID, "paid", sub.StartAt)
}

// The event row, not user JSON or wall-clock order, supplies its exact identity,
// time and epoch. Bound the materialized history by existing evidence budgets.
func readPlanBaseQuotaSyncEvents(tx *gorm.DB, sub model.Subscription, records []commerce.EntitlementValueSnapshot, current bool) ([]commerce.PerpetualBaseQuotaSyncEvent, int64, error) {
	var rootEvent uint
	for _, fact := range records {
		if (fact.Mode == "new_term" || fact.Mode == "recovery_term" || fact.Mode == "replacement_term") && fact.After.StartAt.Equal(sub.StartAt) && fact.QuotaEventID > rootEvent {
			rootEvent = fact.QuotaEventID
		}
	}
	if rootEvent == 0 {
		return nil, 0, commerce.ErrSubscriptionValueUnavailable
	}
	var out []commerce.PerpetualBaseQuotaSyncEvent
	var bodyBytes int64
	after := rootEvent
	strictSeen := false
	for {
		query := tx.Model(&model.QuotaEvent{}).Where("subscription_id = ? AND event_type = ? AND id > ?", sub.ID, "plan_quota_sync", after)
		if current {
			query = planBaseQuotaCurrentRead(query)
		}
		var metadata []struct {
			ID            uint
			EvidenceBytes int64
		}
		lengthSQL := "id, OCTET_LENGTH(detail) AS evidence_bytes"
		if tx.Dialector.Name() == "sqlite" {
			lengthSQL = "id, LENGTH(CAST(detail AS BLOB)) AS evidence_bytes"
		}
		if err := query.Select(lengthSQL).Order("id ASC").Limit(orderEvidencePageSize).Find(&metadata).Error; err != nil {
			return nil, bodyBytes, err
		}
		if len(metadata) == 0 {
			return out, bodyBytes, nil
		}
		ids := make([]uint, len(metadata))
		for i, row := range metadata {
			if row.ID <= after || row.EvidenceBytes < 0 || row.EvidenceBytes > orderEvidenceRowLimit || row.EvidenceBytes > orderEvidenceByteLimit-bodyBytes {
				return nil, bodyBytes, commerce.ErrSubscriptionEvidenceBudget
			}
			ids[i] = row.ID
			after = row.ID
			bodyBytes += row.EvidenceBytes
		}
		query = tx.Model(&model.QuotaEvent{}).Where("id IN ? AND subscription_id = ? AND event_type = ?", ids, sub.ID, "plan_quota_sync")
		if current {
			query = planBaseQuotaCurrentRead(query)
		}
		var rows []model.QuotaEvent
		if err := query.Order("id ASC").Limit(orderEvidencePageSize).Find(&rows).Error; err != nil {
			return nil, bodyBytes, err
		}
		if len(rows) != len(ids) {
			return nil, bodyBytes, commerce.ErrSubscriptionQuoteChanged
		}
		for i, row := range rows {
			if row.ID != ids[i] || int64(len(row.Detail)) != metadata[i].EvidenceBytes {
				return nil, bodyBytes, commerce.ErrSubscriptionQuoteChanged
			}
			var marker struct {
				Version *int   `json:"version"`
				Kind    string `json:"kind"`
			}
			if json.Unmarshal([]byte(row.Detail), &marker) != nil {
				return nil, bodyBytes, commerce.ErrSubscriptionValueUnavailable
			}
			if marker.Version == nil && marker.Kind == "" {
				// Old single-source sync is already covered by the original projector's
				// unchanged compatibility rule. It never becomes a new paid snapshot.
				if strictSeen || !legacyPlanQuotaSyncAudit(row, sub.ID) {
					return nil, bodyBytes, commerce.ErrSubscriptionValueUnavailable
				}
				continue
			}
			detail, err := commerce.DecodePerpetualBaseQuotaSyncDetail([]byte(row.Detail))
			if err != nil || row.EntitlementEpoch != sub.EntitlementEpoch || detail.EntitlementEpoch != row.EntitlementEpoch || detail.SubscriptionID != row.SubscriptionID || detail.PlanID != sub.PlanID || row.ReferenceType != "plan_revision" || row.ReferenceID != fmt.Sprintf("%d:%d", detail.PlanID, detail.PlanRevision) || row.DeltaBytes != detail.After.FlowTotal-detail.Before.FlowTotal || row.BalanceBefore != max(int64(0), detail.Before.FlowTotal-detail.Before.FlowUsed) || row.BalanceAfter != max(int64(0), detail.After.FlowTotal-detail.After.FlowUsed) || row.CreatedAt.IsZero() {
				return nil, bodyBytes, commerce.ErrSubscriptionValueUnavailable
			}
			strictSeen = true
			out = append(out, commerce.PerpetualBaseQuotaSyncEvent{QuotaEventID: row.ID, QuotaEventAt: row.CreatedAt, Detail: detail})
		}
	}
}

func legacyPlanQuotaSyncAudit(row model.QuotaEvent, subscription uint) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(row.Detail), &fields) != nil || len(fields) != 8 {
		return false
	}
	for _, key := range []string{"plan_id", "plan_revision", "actor_id", "previous_flow_total", "flow_total", "flow_used", "previous_reset_quota_bytes", "reset_quota_bytes"} {
		if fields[key] == nil {
			return false
		}
	}
	var old struct {
		Plan     uint   `json:"plan_id"`
		Revision uint64 `json:"plan_revision"`
		Before   int64  `json:"previous_flow_total"`
		After    int64  `json:"flow_total"`
		Used     int64  `json:"flow_used"`
	}
	if json.Unmarshal([]byte(row.Detail), &old) != nil {
		return false
	}
	return row.SubscriptionID == subscription && old.Plan != 0 && old.Revision != 0 && old.Used >= 0 && old.Before >= 0 && old.After >= 0 && row.ReferenceType == "plan_revision" && row.ReferenceID == fmt.Sprintf("%d:%d", old.Plan, old.Revision) && row.DeltaBytes == old.After-old.Before && row.BalanceBefore == max(int64(0), old.Before-old.Used) && row.BalanceAfter == max(int64(0), old.After-old.Used)
}

func prepareMixedPlanBaseQuotaSync(tx *gorm.DB, sub model.Subscription, plan commerce.Plan, at time.Time) (commerce.PerpetualBaseQuotaSyncDetail, int64, error) {
	var empty commerce.PerpetualBaseQuotaSyncDetail
	if !entitlements.IsPerpetualEnd(sub.EndAt) {
		return empty, 0, commerce.ErrSubscriptionValueUnavailable
	}
	query := planBaseQuotaCurrentRead(subscriptionValueOrderQuery(tx, sub))
	records, _, currency, err := subscriptionValueRecords(query, sub)
	if err != nil {
		return empty, 0, planBaseQuotaReadError(err)
	}
	events, historyBytes, err := readPlanBaseQuotaSyncEvents(tx, sub, records, true)
	if err != nil {
		return empty, 0, planBaseQuotaReadError(err)
	}
	detail, err := commerce.PreparePerpetualBaseQuotaSyncContext(tx.Statement.Context, records, events, entitlementValueState(sub), currency, at, sub.EntitlementEpoch, plan.TrafficBytes, plan.Revision)
	return detail, historyBytes, err
}

// MySQL JSON stores a normalized representation, which can be wider than the
// Go JSON input. Check the stored byte count inside the same transaction so a
// successful edit never leaves a newly unreadable over-budget proof history.
func verifyStoredPlanBaseQuotaBudget(tx *gorm.DB, eventID uint, priorBytes int64) error {
	lengthSQL := "OCTET_LENGTH(detail)"
	if tx.Dialector.Name() == "sqlite" {
		lengthSQL = "LENGTH(CAST(detail AS BLOB))"
	}
	var stored int64
	if err := tx.Model(&model.QuotaEvent{}).Select(lengthSQL).Where("id = ?", eventID).Scan(&stored).Error; err != nil {
		return err
	}
	if stored <= 0 || stored > orderEvidenceRowLimit || priorBytes < 0 || stored > orderEvidenceByteLimit-priorBytes {
		return &commerce.ValidationError{Message: "订阅额度来源超过安全读取预算，套餐与订阅均未修改。", Fields: map[string]string{"sync_traffic_quota": "同步已回滚；请先通过工单核对完整的权益来源记录。"}}
	}
	return nil
}
