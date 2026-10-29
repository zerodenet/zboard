package commercestore

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

// Use the actual event envelope and bounded exact-ID pages. Legacy details are
// compatibility evidence, not paid grants; new typed details must bind the same
// subscription, epoch, revision, before/after balances and database event time.
func readTimedPlanQuotaSyncEvents(tx *gorm.DB, sub model.Subscription, records []commerce.EntitlementValueSnapshot, current bool) ([]commerce.TimedPlanQuotaSyncEvent, int64, error) {
	var root uint
	for _, s := range records {
		if (s.Mode == "new_term" || s.Mode == "recovery_term" || s.Mode == "replacement_term") && s.After.StartAt.Equal(sub.StartAt) && s.QuotaEventID > root {
			root = s.QuotaEventID
		}
	}
	if root == 0 {
		return nil, 0, commerce.ErrSubscriptionValueUnavailable
	}
	var out []commerce.TimedPlanQuotaSyncEvent
	var used int64
	after := root
	strictSeen := false
	for {
		query := tx.Model(&model.QuotaEvent{}).Where("subscription_id=? AND event_type=? AND id>?", sub.ID, "plan_quota_sync", after)
		if current {
			query = planBaseQuotaCurrentRead(query)
		}
		length := "id,OCTET_LENGTH(detail) AS evidence_bytes"
		if tx.Dialector.Name() == "sqlite" {
			length = "id,LENGTH(CAST(detail AS BLOB)) AS evidence_bytes"
		}
		var meta []struct {
			ID            uint
			EvidenceBytes int64
		}
		if err := query.Select(length).Order("id ASC").Limit(orderEvidencePageSize).Find(&meta).Error; err != nil {
			return nil, used, err
		}
		if len(meta) == 0 {
			return out, used, nil
		}
		ids := make([]uint, len(meta))
		for i, m := range meta {
			if m.ID <= after || m.EvidenceBytes < 0 || m.EvidenceBytes > orderEvidenceRowLimit || m.EvidenceBytes > orderEvidenceByteLimit-used {
				return nil, used, commerce.ErrSubscriptionEvidenceBudget
			}
			ids[i] = m.ID
			after = m.ID
			used += m.EvidenceBytes
		}
		query = tx.Model(&model.QuotaEvent{}).Where("id IN ? AND subscription_id=? AND event_type=?", ids, sub.ID, "plan_quota_sync")
		if current {
			query = planBaseQuotaCurrentRead(query)
		}
		var rows []model.QuotaEvent
		if err := query.Order("id ASC").Limit(orderEvidencePageSize).Find(&rows).Error; err != nil {
			return nil, used, err
		}
		if len(rows) != len(ids) {
			return nil, used, commerce.ErrSubscriptionQuoteChanged
		}
		for i, row := range rows {
			if err := tx.Statement.Context.Err(); err != nil {
				return nil, used, err
			}
			if row.ID != ids[i] || int64(len(row.Detail)) != meta[i].EvidenceBytes || row.CreatedAt.IsZero() {
				return nil, used, commerce.ErrSubscriptionQuoteChanged
			}
			var marker struct {
				Version *int   `json:"version"`
				Kind    string `json:"kind"`
			}
			if json.Unmarshal([]byte(row.Detail), &marker) != nil {
				return nil, used, commerce.ErrSubscriptionValueUnavailable
			}
			event := commerce.TimedPlanQuotaSyncEvent{QuotaEventID: row.ID, QuotaEventAt: row.CreatedAt, EntitlementEpoch: row.EntitlementEpoch}
			if marker.Version == nil && marker.Kind == "" {
				if strictSeen || !legacyPlanQuotaSyncAudit(row, sub.ID) || (row.EntitlementEpoch != 0 && row.EntitlementEpoch != sub.EntitlementEpoch) {
					return nil, used, commerce.ErrSubscriptionValueUnavailable
				}
				legacy, err := commerce.DecodeLegacyTimedPlanQuotaSync([]byte(row.Detail))
				if err != nil {
					return nil, used, commerce.ErrSubscriptionValueUnavailable
				}
				event.Legacy = &legacy
			} else {
				d, err := commerce.DecodeTimedPlanQuotaSyncDetail([]byte(row.Detail))
				if err != nil || row.EntitlementEpoch != sub.EntitlementEpoch || d.EntitlementEpoch != row.EntitlementEpoch || d.SubscriptionID != row.SubscriptionID || d.PlanID != sub.PlanID || row.ReferenceType != "plan_revision" || row.ReferenceID != fmt.Sprintf("%d:%d", d.PlanID, d.PlanRevision) || row.DeltaBytes != d.After.FlowTotal-d.Before.FlowTotal || row.BalanceBefore != max(int64(0), d.Before.FlowTotal-d.Before.FlowUsed) || row.BalanceAfter != max(int64(0), d.After.FlowTotal-d.After.FlowUsed) || d.CapturedAt.IsZero() || d.CapturedAt.Sub(row.CreatedAt) > time.Millisecond || row.CreatedAt.Sub(d.CapturedAt) > time.Millisecond {
					return nil, used, commerce.ErrSubscriptionValueUnavailable
				}
				event.Detail = &d
				strictSeen = true
			}
			out = append(out, event)
		}
	}
}

func prepareTimedPlanQuotaSync(tx *gorm.DB, sub model.Subscription, plan commerce.Plan, at time.Time) (*commerce.TimedPlanQuotaSyncDetail, int64, int64, error) {
	query := planBaseQuotaCurrentRead(subscriptionValueOrderQuery(tx, sub))
	// Metadata-only historical subscriptions retain the ordinary quota update;
	// absence of a paid root never becomes a manufactured refundable value.
	var existence []struct{ ID uint }
	if err := query.Session(&gorm.Session{}).Select("id").Limit(1).Find(&existence).Error; err != nil {
		return nil, 0, 0, planBaseQuotaReadError(err)
	}
	if len(existence) == 0 {
		if sub.ResetQuotaBytes <= 0 || sub.FlowTotal-sub.CycleStartUsed != sub.ResetQuotaBytes {
			return nil, 0, 0, commerce.ErrSubscriptionValueUnavailable
		}
		next, err := commerce.SynchronizedPlanQuota(sub.CycleStartUsed, sub.FlowUsed, sub.FlowTotal, plan.TrafficBytes)
		return nil, next, 0, err
	}
	records, _, currency, err := subscriptionValueRecords(query, sub)
	if err != nil {
		return nil, 0, 0, planBaseQuotaReadError(err)
	}
	events, used, err := readTimedPlanQuotaSyncEvents(tx, sub, records, true)
	if err != nil {
		return nil, 0, 0, planBaseQuotaReadError(err)
	}
	d, err := commerce.PrepareTimedPlanQuotaSyncContext(tx.Statement.Context, records, events, entitlementValueState(sub), currency, at, sub.EntitlementEpoch, plan.TrafficBytes, plan.Revision)
	if err != nil {
		return nil, 0, used, err
	}
	return &d, d.After.FlowTotal, used, nil
}
