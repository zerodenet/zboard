package commerce

import (
	"bytes"
	"encoding/json"
	"io"
	"time"
)

const PerpetualBaseQuotaSyncVersion = 1
const PerpetualBaseQuotaSyncKind = "plan_base_quota_sync"
const PerpetualBaseQuotaSyncAllocation = "perpetual_paid_quota_fifo_base_sync_v1"

// This is a non-payment QuotaEvent.Detail. It changes an existing base lot's
// quota, never its immutable actual paid amount or its historical consumption.
// Row identity/time come only from the separately persisted QuotaEvent envelope.
type PerpetualBaseQuotaSyncDetail struct {
	Version             int                   `json:"version"`
	Kind                string                `json:"kind"`
	SubscriptionID      uint                  `json:"subscription_id"`
	EntitlementEpoch    uint64                `json:"entitlement_epoch"`
	PlanID              uint                  `json:"plan_id"`
	PlanRevision        uint64                `json:"plan_revision"`
	BaseOrderID         uint                  `json:"base_order_id"`
	OldBaseQuota        int64                 `json:"old_base_quota"`
	NewBaseQuota        int64                 `json:"new_base_quota"`
	UsedAtSync          int64                 `json:"used_at_sync"`
	BaseConsumedBytes   int64                 `json:"base_consumed_bytes"`
	BaseRemainingBefore int64                 `json:"base_remaining_before"`
	BaseRemainingAfter  int64                 `json:"base_remaining_after"`
	Before              EntitlementValueState `json:"before"`
	After               EntitlementValueState `json:"after"`
	SourceFingerprint   string                `json:"source_fingerprint"`
}

type PerpetualBaseQuotaSyncEvent struct {
	QuotaEventID uint                         `json:"quota_event_id"`
	QuotaEventAt time.Time                    `json:"quota_event_at"`
	Detail       PerpetualBaseQuotaSyncDetail `json:"detail"`
}

// DecodePerpetualBaseQuotaSyncDetail refuses unknown, missing, duplicate and
// trailing JSON fields. Zero epoch/zero consumption are valid only when present.
func DecodePerpetualBaseQuotaSyncDetail(payload []byte) (PerpetualBaseQuotaSyncDetail, error) {
	var out PerpetualBaseQuotaSyncDetail
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&out); err != nil {
		return out, ErrValueEvidence
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return out, ErrValueEvidence
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(payload, &fields) != nil {
		return out, ErrValueEvidence
	}
	allowed := map[string]bool{}
	for _, key := range []string{"version", "kind", "subscription_id", "entitlement_epoch", "plan_id", "plan_revision", "base_order_id", "old_base_quota", "new_base_quota", "used_at_sync", "base_consumed_bytes", "base_remaining_before", "base_remaining_after", "before", "after", "source_fingerprint"} {
		allowed[key] = true
		if len(fields[key]) == 0 || bytes.Equal(bytes.TrimSpace(fields[key]), []byte("null")) {
			return out, ErrValueEvidence
		}
	}
	for key := range fields {
		if !allowed[key] {
			return out, ErrValueEvidence
		}
	}
	if out.Version != PerpetualBaseQuotaSyncVersion || out.Kind != PerpetualBaseQuotaSyncKind {
		return out, ErrValueEvidence
	}
	for _, stateName := range []string{"before", "after"} {
		var state map[string]json.RawMessage
		if json.Unmarshal(fields[stateName], &state) != nil {
			return out, ErrValueEvidence
		}
		stateAllowed := map[string]bool{"ended_at": true, "end_reason": true, "next_reset_at": true}
		for _, key := range []string{"plan_id", "plan_sku_id", "start_at", "end_at", "status", "flow_total", "flow_used", "cycle_start_used", "reset_quota_bytes", "reset_policy"} {
			stateAllowed[key] = true
			if len(state[key]) == 0 || bytes.Equal(bytes.TrimSpace(state[key]), []byte("null")) {
				return out, ErrValueEvidence
			}
		}
		for key := range state {
			if !stateAllowed[key] {
				return out, ErrValueEvidence
			}
		}
	}
	if !perpetualSyncUniqueJSON(json.NewDecoder(bytes.NewReader(payload))) {
		return out, ErrValueEvidence
	}

	return out, nil
}

// encoding/json ordinarily keeps the last duplicate key, including in nested
// states. Reject ambiguity at every level before accepting financial evidence.
func perpetualSyncUniqueJSON(decoder *json.Decoder) bool {
	token, err := decoder.Token()
	if err != nil {
		return false
	}
	delim, structured := token.(json.Delim)
	if !structured {
		return true
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			token, err := decoder.Token()
			key, ok := token.(string)
			if err != nil || !ok || seen[key] {
				return false
			}
			seen[key] = true
			if !perpetualSyncUniqueJSON(decoder) {
				return false
			}
		}
		end, err := decoder.Token()
		return err == nil && end == json.Delim('}')
	case '[':
		for decoder.More() {
			if !perpetualSyncUniqueJSON(decoder) {
				return false
			}
		}
		end, err := decoder.Token()
		return err == nil && end == json.Delim(']')
	}
	return false
}

// Integration API implemented by value_perpetual_sync.go:
// PreparePerpetualBaseQuotaSyncContext(ctx context.Context,
//   records []EntitlementValueSnapshot, prior []PerpetualBaseQuotaSyncEvent,
//   current EntitlementValueState, currency string, at time.Time,
//   epoch uint64, newBase int64, planRevision uint64,
// ) (PerpetualBaseQuotaSyncDetail, error)
// ProjectEntitlementValueWithBaseSyncContext(ctx context.Context,
//   records []EntitlementValueSnapshot, events []PerpetualBaseQuotaSyncEvent,
//   current EntitlementValueState, currency string, at time.Time, epoch uint64,
// ) (ValueProjection, error)
