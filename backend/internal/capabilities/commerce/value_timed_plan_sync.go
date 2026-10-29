package commerce

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"math"
	"reflect"
	"sort"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
)

const TimedPlanQuotaSyncKind = "timed_plan_base_quota_sync"
const TimedPlanQuotaSyncAllocation = "timed_free_plan_quota_sync_v1"

// Non-payment evidence in the existing QuotaEvent. No historical paid snapshot
// or source balance is rewritten. Current value is capped at the settled prior
// remainder; future paid allocations retain their original sources and amounts.
type TimedPlanQuotaSyncDetail struct {
	Version            int                   `json:"version"`
	Kind               string                `json:"kind"`
	SubscriptionID     uint                  `json:"subscription_id"`
	EntitlementEpoch   uint64                `json:"entitlement_epoch"`
	PlanID             uint                  `json:"plan_id"`
	PlanRevision       uint64                `json:"plan_revision"`
	OldBaseQuota       int64                 `json:"old_base_quota"`
	NewBaseQuota       int64                 `json:"new_base_quota"`
	AddonQuota         int64                 `json:"addon_quota"`
	Before             EntitlementValueState `json:"before"`
	After              EntitlementValueState `json:"after"`
	CurrentMinorBefore int64                 `json:"current_minor_before,string"`
	CurrentMinorAfter  int64                 `json:"current_minor_after,string"`
	CapturedAt         time.Time             `json:"captured_at"`
	SourceFingerprint  string                `json:"source_fingerprint"`
}

type LegacyTimedPlanQuotaSync struct {
	PlanID             uint   `json:"plan_id"`
	PlanRevision       uint64 `json:"plan_revision"`
	ActorID            uint   `json:"actor_id"`
	PreviousFlowTotal  int64  `json:"previous_flow_total"`
	FlowTotal          int64  `json:"flow_total"`
	FlowUsed           int64  `json:"flow_used"`
	PreviousResetQuota int64  `json:"previous_reset_quota_bytes"`
	ResetQuota         int64  `json:"reset_quota_bytes"`
}

type TimedPlanQuotaSyncEvent struct {
	QuotaEventID     uint                      `json:"quota_event_id"`
	QuotaEventAt     time.Time                 `json:"quota_event_at"`
	EntitlementEpoch uint64                    `json:"entitlement_epoch"`
	Detail           *TimedPlanQuotaSyncDetail `json:"detail,omitempty"`
	Legacy           *LegacyTimedPlanQuotaSync `json:"legacy,omitempty"`
}

func DecodeTimedPlanQuotaSyncDetail(raw []byte) (TimedPlanQuotaSyncDetail, error) {
	var out TimedPlanQuotaSyncDetail
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(&out) != nil || dec.Decode(new(any)) != io.EOF || !perpetualSyncUniqueJSON(json.NewDecoder(bytes.NewReader(raw))) {
		return out, ErrValueEvidence
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 15 {
		return out, ErrValueEvidence
	}
	for _, key := range []string{"version", "kind", "subscription_id", "entitlement_epoch", "plan_id", "plan_revision", "old_base_quota", "new_base_quota", "addon_quota", "before", "after", "current_minor_before", "current_minor_after", "captured_at", "source_fingerprint"} {
		value := fields[key]
		if len(value) == 0 || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return out, ErrValueEvidence
		}
	}
	// There are fifteen named fields. Reject missing and unknown fields without
	// interpreting zero epoch, zero money or zero add-on as absent authority.
	if out.Version != 1 || out.Kind != TimedPlanQuotaSyncKind {
		return out, ErrValueEvidence
	}
	for _, key := range []string{"before", "after"} {
		var state map[string]json.RawMessage
		if json.Unmarshal(fields[key], &state) != nil {
			return out, ErrValueEvidence
		}
		allowed := map[string]bool{"ended_at": true, "end_reason": true, "next_reset_at": true}
		for _, name := range []string{"plan_id", "plan_sku_id", "start_at", "end_at", "status", "flow_total", "flow_used", "cycle_start_used", "reset_quota_bytes", "reset_policy"} {
			allowed[name] = true
			if len(state[name]) == 0 || bytes.Equal(bytes.TrimSpace(state[name]), []byte("null")) {
				return out, ErrValueEvidence
			}
		}
		for name := range state {
			if !allowed[name] {
				return out, ErrValueEvidence
			}
		}
	}
	return out, nil
}

func DecodeLegacyTimedPlanQuotaSync(raw []byte) (LegacyTimedPlanQuotaSync, error) {
	var out LegacyTimedPlanQuotaSync
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&out) != nil || decoder.Decode(new(any)) != io.EOF || !perpetualSyncUniqueJSON(json.NewDecoder(bytes.NewReader(raw))) {
		return out, ErrValueEvidence
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 8 {
		return out, ErrValueEvidence
	}
	for _, key := range []string{"plan_id", "plan_revision", "actor_id", "previous_flow_total", "flow_total", "flow_used", "previous_reset_quota_bytes", "reset_quota_bytes"} {
		if len(fields[key]) == 0 || bytes.Equal(bytes.TrimSpace(fields[key]), []byte("null")) {
			return out, ErrValueEvidence
		}
	}
	return out, nil
}

func timedPlanRecords(ctx context.Context, records []EntitlementValueSnapshot, current EntitlementValueState, at time.Time) ([]EntitlementValueSnapshot, error) {
	if ctx == nil || len(records) == 0 || at.IsZero() || entitlements.IsPerpetualEnd(current.EndAt) {
		return nil, ErrValueEvidence
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	records = append([]EntitlementValueSnapshot(nil), records...)
	sort.Slice(records, func(i, j int) bool { return records[i].QuotaEventID < records[j].QuotaEventID })
	root := -1
	for i, s := range records {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if (s.Mode == "new_term" || s.Mode == "recovery_term" || s.Mode == "replacement_term") && s.After.StartAt.Equal(current.StartAt) {
			root = i
		}
	}
	if root < 0 {
		return nil, ErrValueEvidence
	}
	return records[root:], nil
}

func ProjectTimedEntitlementValueWithPlanSyncContext(ctx context.Context, records []EntitlementValueSnapshot, events []TimedPlanQuotaSyncEvent, current EntitlementValueState, currency string, at time.Time, epoch uint64) (ValueProjection, error) {
	records, err := timedPlanRecords(ctx, records, current, at)
	if err != nil {
		return ValueProjection{}, err
	}
	out, _, err := projectTimedValueEvents(ctx, records, current, currency, at, events, epoch, true)
	return out, err
}
func PrepareTimedPlanQuotaSyncContext(ctx context.Context, records []EntitlementValueSnapshot, events []TimedPlanQuotaSyncEvent, current EntitlementValueState, currency string, at time.Time, epoch uint64, newBase int64, revision uint64) (TimedPlanQuotaSyncDetail, error) {
	records, err := timedPlanRecords(ctx, records, current, at)
	if err != nil {
		return TimedPlanQuotaSyncDetail{}, err
	}
	if !at.Before(current.EndAt) {
		return TimedPlanQuotaSyncDetail{}, ErrValueEvidence
	}
	quote, replay, err := projectTimedValueEvents(ctx, records, current, currency, at, events, epoch, true)
	if err != nil {
		return TimedPlanQuotaSyncDetail{}, err
	}
	return replay.prepare(current, at, newBase, revision, quote.Fingerprint)
}

type timedQuotaReplay struct {
	ctx          context.Context
	epoch        uint64
	events       []TimedPlanQuotaSyncEvent
	sourceDigest [32]byte
	frame        *valueFrame
	root         EntitlementValueSnapshot
	state        EntitlementValueState
	cycleStart   time.Time
	base, addon  int64
	lastID       uint
	lastAt       time.Time
	lastRevision uint64
	next         int
	typedSeen    bool
}

func (r *timedQuotaReplay) cycle(at time.Time) (ValueCycle, error) {
	cycles, _, err := r.frame.cycles(r.state.ResetPolicy)
	if err != nil {
		return ValueCycle{}, err
	}
	effective := at
	if !effective.Before(r.frame.end) {
		effective = r.frame.end.Add(-time.Nanosecond)
	}
	for _, c := range cycles {
		if !effective.Before(c.StartAt) && effective.Before(c.EndAt) {
			return c, nil
		}
	}
	return ValueCycle{}, ErrValueEvidence
}
func (r *timedQuotaReplay) seed(s EntitlementValueSnapshot) error {
	r.root = s
	r.state = s.After
	r.lastID = s.QuotaEventID
	r.lastAt = s.QuotaEventAt
	q, err := quotaForValue(s.After)
	if err != nil || s.After.ResetQuotaBytes < 0 {
		return ErrValueEvidence
	}
	r.base = q
	c, err := r.cycle(s.QuotaEventAt)
	if err != nil {
		return err
	}
	r.cycleStart = c.StartAt
	for i, e := range r.events {
		if e.QuotaEventID <= r.lastID || e.QuotaEventAt.IsZero() || (i > 0 && e.QuotaEventID <= r.events[i-1].QuotaEventID) || (e.Detail == nil) == (e.Legacy == nil) {
			return ErrValueEvidence
		}
	}
	return r.bind("paid_grant", s)
}
func (r *timedQuotaReplay) advance(next EntitlementValueState, at time.Time) error {
	if err := r.ctx.Err(); err != nil {
		return err
	}
	old := r.state
	if next.PlanID != old.PlanID || next.PlanSKUID != old.PlanSKUID || !next.StartAt.Equal(old.StartAt) || !next.EndAt.Equal(old.EndAt) || next.ResetPolicy != old.ResetPolicy || next.FlowUsed < old.FlowUsed || next.CycleStartUsed < 0 || next.FlowUsed < next.CycleStartUsed || next.FlowTotal < next.CycleStartUsed || next.ResetQuotaBytes != old.ResetQuotaBytes || next.EndedAt != nil || next.EndReason != old.EndReason || (next.Status != "active" && next.Status != "expired") {
		return ErrValueEvidence
	}
	c, err := r.cycle(at)
	if err != nil {
		return err
	}
	if c.StartAt.Equal(r.cycleStart) {
		if next.CycleStartUsed != old.CycleStartUsed || next.FlowTotal != old.FlowTotal || !sameValueReset(next.NextResetAt, old.NextResetAt) {
			return ErrValueEvidence
		}
	} else {
		resetQuota := old.ResetQuotaBytes
		if resetQuota == 0 {
			resetQuota = r.base
		}
		if old.NextResetAt == nil || at.Before(*old.NextResetAt) || next.CycleStartUsed < old.CycleStartUsed || next.CycleStartUsed > math.MaxInt64-resetQuota {
			return ErrValueEvidence
		}
		// A scheduled reset supplies one new base allocation; prior-cycle add-ons
		// expire. Cumulative usage never decreases, including delayed reset catch-up.
		expected := max(next.CycleStartUsed+resetQuota, next.FlowUsed)
		effective := at
		if !effective.Before(next.EndAt) {
			effective = next.EndAt.Add(-time.Nanosecond)
		}
		expectedNext := entitlements.NextTrafficResetAfter(next.StartAt, next.ResetPolicy, effective)
		if next.FlowTotal != expected || !sameValueReset(next.NextResetAt, expectedNext) {
			return ErrValueEvidence
		}
		r.base, r.addon = resetQuota, 0
		r.cycleStart = c.StartAt
	}
	r.state = next
	return nil
}
func (r *timedQuotaReplay) beforeGrant(records []EntitlementValueSnapshot, s EntitlementValueSnapshot) error {
	if s.QuotaEventID <= r.lastID || perpetualSyncTimeBefore(s.QuotaEventAt, r.lastAt) {
		return ErrValueEvidence
	}
	if err := r.applyBefore(records, s.QuotaEventID, s.QuotaEventAt); err != nil {
		return err
	}
	return r.advance(*s.Before, s.QuotaEventAt)
}

// An exact core-captured plan-sync instant can lie after a later payment's
// rounded datetime(3) row within the same millisecond. Only that known
// representation overlap may use the payment's already-captured exact clock;
// IDs remain ordered authority and no historic no-sync calculation is changed.
func (r *timedQuotaReplay) grantAt(s EntitlementValueSnapshot) (time.Time, error) {
	at := s.QuotaEventAt.UTC()
	if r.typedSeen && r.frame.basis != nil && at.Before(r.frame.basis.Basis.At) {
		if r.frame.basis.Basis.At.Sub(at) > time.Millisecond || s.CapturedAt.Before(r.frame.basis.Basis.At) || s.CapturedAt.Sub(at) > time.Millisecond || at.Sub(s.CapturedAt) > time.Millisecond {
			return time.Time{}, ErrValueEvidence
		}
		return s.CapturedAt.UTC(), nil
	}
	return at, nil
}
func (r *timedQuotaReplay) acceptGrant(s EntitlementValueSnapshot) error {
	switch s.Mode {
	case "timed_quota_addition":
		delta := s.After.FlowTotal - s.Before.FlowTotal
		if delta <= 0 || r.addon > math.MaxInt64-delta {
			return ErrValueEvidence
		}
		r.addon += delta
	case "paid_reset":
		q, err := quotaForValue(s.After)
		if err != nil || q <= 0 {
			return ErrValueEvidence
		}
		r.base, r.addon = q, 0
	case "extended_term": // Time extension does not resize the current base.
	default:
		return ErrValueEvidence
	}
	r.state = s.After
	r.lastID = s.QuotaEventID
	r.lastAt = s.QuotaEventAt
	c, err := r.cycle(s.QuotaEventAt)
	if err != nil {
		return err
	}
	r.cycleStart = c.StartAt
	return r.bind("paid_grant", s)
}
func (r *timedQuotaReplay) finish(records []EntitlementValueSnapshot, current EntitlementValueState, at time.Time) error {
	if err := r.applyBefore(records, ^uint(0), at); err != nil {
		return err
	}
	if r.next != len(r.events) || perpetualSyncTimeBefore(at, r.lastAt) {
		return ErrValueEvidence
	}
	if at.Before(current.EndAt) {
		return r.advance(current, at)
	}
	next := current
	if current.EndedAt != nil {
		// Only the existing exact natural-expiry marker can be normalized for this
		// read. Actual terminal fields remain in the quote; no write or revival.
		if current.Status != "expired" || current.EndReason != "expired" || !current.EndedAt.Equal(current.EndAt) {
			return ErrValueEvidence
		}
		next.EndedAt = nil
		next.EndReason = r.state.EndReason
	}
	// A stopped reset/expiry worker cannot create refundable future service.
	// Verify the last ACTUALLY executed cycle, then project at the real expired
	// instant. Identical source fields need no invented intermediate reset.
	if next.FlowTotal == r.state.FlowTotal && next.CycleStartUsed == r.state.CycleStartUsed && next.ResetQuotaBytes == r.state.ResetQuotaBytes && sameValueReset(next.NextResetAt, r.state.NextResetAt) {
		return r.advance(next, r.cycleStart)
	}
	// A partial reset's persisted next boundary locates that executed cycle.
	// advance still checks the exact schedule, base quota, accounting baseline,
	// monotonic used and source identity; an arbitrary date is not authority.
	if next.NextResetAt == nil {
		return ErrValueEvidence
	}
	observedCycle := next.NextResetAt.Add(-time.Nanosecond)
	if !observedCycle.Before(next.EndAt) {
		observedCycle = next.EndAt.Add(-time.Nanosecond)
	}
	return r.advance(next, observedCycle)
}

func (r *timedQuotaReplay) applyBefore(records []EntitlementValueSnapshot, id uint, at time.Time) error {
	for r.next < len(r.events) && r.events[r.next].QuotaEventID < id {
		e := r.events[r.next]
		if e.QuotaEventID <= r.lastID || perpetualSyncTimeBefore(e.QuotaEventAt, r.lastAt) || perpetualSyncTimeBefore(at, e.QuotaEventAt) {
			return ErrValueEvidence
		}
		if e.Detail != nil {
			d := *e.Detail
			eventAt := d.CapturedAt
			if eventAt.IsZero() || eventAt.Sub(e.QuotaEventAt) > time.Millisecond || e.QuotaEventAt.Sub(eventAt) > time.Millisecond {
				return ErrValueEvidence
			}
			if e.EntitlementEpoch != r.epoch || d.EntitlementEpoch != r.epoch || d.SubscriptionID != r.root.SubscriptionID || d.PlanID != r.state.PlanID {
				return ErrValueEvidence
			}
			if err := r.advance(d.Before, eventAt); err != nil {
				return err
			}
			before, err := r.frame.project(d.Before, eventAt)
			if err != nil {
				return err
			}
			before.RootOrderID = r.root.OrderID
			before.ValuationModel = "remaining_value_basis_v1"
			if r.typedSeen {
				before.ValuationModel = TimedPlanQuotaSyncAllocation
				before, err = r.fingerprint(d.Before, before)
			} else {
				before, err = fingerprintValueProjection(r.ctx, records, d.Before, before)
			}
			if err != nil {
				return err
			}
			expected, err := r.prepare(d.Before, eventAt, d.NewBaseQuota, d.PlanRevision, before.Fingerprint)
			if err != nil || !reflect.DeepEqual(expected, d) {
				return ErrValueEvidence
			}
			if !before.CurrentCycle.EndAt.IsZero() {
				shares, err := currentProjectionSources(before, *r.frame, d.Before, eventAt)
				if err != nil {
					return err
				}
				shares, err = proportionValueSources(d.CurrentMinorAfter, shares)
				if err != nil {
					return err
				}
				remaining := max(int64(0), d.After.FlowTotal-d.After.FlowUsed)
				quota, err := quotaForValue(d.After)
				if err != nil {
					return err
				}
				r.frame.basis = &valueBasisEvidence{CycleStart: before.CurrentCycle.StartAt, TotalQuota: quota, Sources: shares, Basis: RemainingValueBasis{At: eventAt.UTC(), EndAt: before.CurrentCycle.EndAt, AmountMinor: d.CurrentMinorAfter, UsedAtBasis: d.After.FlowUsed, QuotaBytes: remaining}}
			}
			r.state = d.After
			r.base = d.NewBaseQuota
			r.lastRevision = d.PlanRevision
			r.typedSeen = true
		} else {
			d := e.Legacy
			if r.typedSeen || d.ActorID == 0 || d.PlanID != r.state.PlanID || d.PlanRevision <= r.lastRevision || d.PreviousFlowTotal < 0 || d.FlowUsed < r.state.FlowUsed || d.PreviousResetQuota <= 0 || d.ResetQuota <= 0 || r.addon != 0 {
				return ErrValueEvidence
			}
			before := r.state
			before.FlowUsed = d.FlowUsed
			if d.PreviousFlowTotal < d.PreviousResetQuota {
				return ErrValueEvidence
			}
			before.FlowTotal = d.PreviousFlowTotal
			before.CycleStartUsed = d.PreviousFlowTotal - d.PreviousResetQuota
			cycle, err := r.cycle(e.QuotaEventAt)
			if err != nil {
				return err
			}
			if !cycle.StartAt.Equal(r.cycleStart) {
				before.NextResetAt = entitlements.NextTrafficResetAfter(before.StartAt, before.ResetPolicy, e.QuotaEventAt)
			}
			if err := r.advance(before, e.QuotaEventAt); err != nil {
				return err
			}
			if d.PreviousFlowTotal != before.FlowTotal || d.PreviousResetQuota != before.ResetQuotaBytes || before.CycleStartUsed > math.MaxInt64-d.ResetQuota || d.FlowTotal != before.CycleStartUsed+d.ResetQuota {
				return ErrValueEvidence
			}
			r.state.FlowTotal = d.FlowTotal
			r.state.ResetQuotaBytes = d.ResetQuota
			r.state.Status = "active"
			r.base = d.ResetQuota
			r.lastRevision = d.PlanRevision
		}
		if err := r.bind("plan_quota_sync", e); err != nil {
			return err
		}
		r.lastID = e.QuotaEventID
		r.lastAt = e.QuotaEventAt
		r.next++
	}
	if r.next < len(r.events) && r.events[r.next].QuotaEventID == id {
		return ErrValueEvidence
	}
	return nil
}
func (r *timedQuotaReplay) prepare(before EntitlementValueState, at time.Time, newBase int64, revision uint64, fingerprint string) (TimedPlanQuotaSyncDetail, error) {
	var empty TimedPlanQuotaSyncDetail
	if newBase <= 0 || revision == 0 || revision <= r.lastRevision || before.EndedAt != nil || fingerprint == "" || before.CycleStartUsed > math.MaxInt64-newBase || before.CycleStartUsed+newBase > math.MaxInt64-r.addon {
		return empty, ErrValueEvidence
	}
	after := before
	after.FlowTotal = before.CycleStartUsed + newBase + r.addon
	after.ResetQuotaBytes = newBase
	after.Status = "active"
	old, err := r.frame.project(before, at)
	if err != nil {
		return empty, err
	}
	if !old.CurrentCycle.EndAt.IsZero() && old.CurrentCycle.EndAt.Unix() <= at.Unix() {
		return empty, ErrValueEvidence
	}
	changed, err := r.frame.project(after, at)
	if err != nil {
		return empty, err
	}
	if old.FutureMinor != changed.FutureMinor {
		return empty, ErrValueEvidence
	}
	capped := min(old.CurrentMinor, changed.CurrentMinor)
	return TimedPlanQuotaSyncDetail{Version: 1, Kind: TimedPlanQuotaSyncKind, SubscriptionID: r.root.SubscriptionID, EntitlementEpoch: r.epoch, PlanID: before.PlanID, PlanRevision: revision, OldBaseQuota: r.base, NewBaseQuota: newBase, AddonQuota: r.addon, Before: before, After: after, CurrentMinorBefore: old.CurrentMinor, CurrentMinorAfter: capped, CapturedAt: at.UTC(), SourceFingerprint: fingerprint}, nil
}

// Ordered single-pass binding avoids reserializing all paid/event prefixes at
// each later sync. First no-sync/legacy quotes keep their existing fingerprint.
func (r *timedQuotaReplay) bind(kind string, source any) error {
	if err := r.ctx.Err(); err != nil {
		return err
	}
	raw, err := json.Marshal(struct {
		Version  int
		Epoch    uint64
		Previous string
		Kind     string
		Source   any
	}{1, r.epoch, hex.EncodeToString(r.sourceDigest[:]), kind, source})
	if err != nil {
		return err
	}
	if err := r.ctx.Err(); err != nil {
		return err
	}
	r.sourceDigest = sha256.Sum256(raw)
	return nil
}
func (r *timedQuotaReplay) fingerprint(state EntitlementValueState, out ValueProjection) (ValueProjection, error) {
	if err := r.ctx.Err(); err != nil {
		return ValueProjection{}, err
	}
	raw, err := json.Marshal(struct {
		Version      int
		Epoch        uint64
		SourceDigest string
		State        EntitlementValueState
		Quote        ValueProjection
		Sources      map[uint]int64
	}{1, r.epoch, hex.EncodeToString(r.sourceDigest[:]), state, out, out.Sources})
	if err != nil {
		return ValueProjection{}, err
	}
	if err := r.ctx.Err(); err != nil {
		return ValueProjection{}, err
	}
	sum := sha256.Sum256(raw)
	out.Fingerprint = hex.EncodeToString(sum[:])
	return out, nil
}
