package commerce

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"reflect"
	"sort"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
)

type perpetualSyncLot struct {
	OrderID   uint  `json:"order_id"`
	Quota     int64 `json:"quota"`
	Consumed  int64 `json:"consumed"`
	PaidMinor int64 `json:"paid_minor"`
}

func (l perpetualSyncLot) remaining() int64 { return max(int64(0), l.Quota-l.Consumed) }

type perpetualSyncReplay struct {
	ctx              context.Context
	root             EntitlementValueSnapshot
	state            EntitlementValueState
	lots             []perpetualSyncLot
	epoch            uint64
	firstUnspent     int
	lastID           uint
	lastAt           time.Time
	lastPlanRevision uint64
	unknownOverrun   bool
	totalPaid        int64
	syncCount        int
	sourceDigest     [32]byte
}

// Prepare produces evidence for a non-payment quota mutation. The caller owns
// eligibility, locks, durable QuotaEvent identity and its atomic business write.
// No existing purchase snapshot, actual paid amount or lot consumption is edited.
func PreparePerpetualBaseQuotaSyncContext(ctx context.Context, records []EntitlementValueSnapshot, prior []PerpetualBaseQuotaSyncEvent, current EntitlementValueState, currency string, at time.Time, epoch uint64, newBase int64, planRevision uint64) (PerpetualBaseQuotaSyncDetail, error) {
	var empty PerpetualBaseQuotaSyncDetail
	if newBase <= 0 || planRevision == 0 || current.EndedAt != nil {
		return empty, ErrValueEvidence
	}
	replay, err := replayPerpetualBaseSync(ctx, records, prior, current, currency, at, epoch)
	if err != nil {
		return empty, err
	}
	if replay.unknownOverrun || planRevision <= replay.lastPlanRevision {
		return empty, ErrValueEvidence
	}
	return replay.prepare(current, newBase, planRevision)
}

// Empty sync history uses the previous projector verbatim, including its golden
// quote identities. New evidence is mandatory once a mixed base-sync exists.
func ProjectEntitlementValueWithBaseSyncContext(ctx context.Context, records []EntitlementValueSnapshot, events []PerpetualBaseQuotaSyncEvent, current EntitlementValueState, currency string, at time.Time, epoch uint64) (ValueProjection, error) {
	if len(events) == 0 {
		return ProjectEntitlementValueContext(ctx, records, current, currency, at)
	}
	empty := ValueProjection{Sources: map[uint]int64{}}
	replay, err := replayPerpetualBaseSync(ctx, records, events, current, currency, at, epoch)
	if err != nil {
		return empty, err
	}
	if replay.unknownOverrun {
		return empty, ErrValueEvidence
	}
	out := ValueProjection{ValuationModel: PerpetualBaseQuotaSyncAllocation, RootOrderID: replay.root.OrderID, Sources: map[uint]int64{}, CurrentCycle: ValueCycle{StartAt: replay.root.TermStartAt, EndAt: replay.root.TermEndAt}}
	for _, lot := range replay.lots {
		if err := ctx.Err(); err != nil {
			return empty, err
		}
		if out.CurrentCycle.AmountMinor > math.MaxInt64-lot.PaidMinor {
			return empty, ErrValueEvidence
		}
		out.CurrentCycle.AmountMinor += lot.PaidMinor
		amount := int64(0)
		if lot.Quota > 0 {
			amount = proportionalValue(lot.PaidMinor, lot.remaining(), lot.Quota)
		}
		if amount < 0 || amount > lot.PaidMinor || out.TotalMinor > math.MaxInt64-amount {
			return empty, ErrValueEvidence
		}
		out.TotalMinor += amount
		if amount > 0 {
			out.Sources[lot.OrderID] = amount
		}
	}
	if current.EndedAt != nil && out.TotalMinor != 0 {
		return empty, ErrValueEvidence
	}
	out.CurrentMinor = out.TotalMinor
	fingerprint, err := replay.fingerprint(current, &out)
	if err != nil {
		return empty, err
	}
	out.Fingerprint = fingerprint
	return out, nil
}

func replayPerpetualBaseSync(ctx context.Context, records []EntitlementValueSnapshot, events []PerpetualBaseQuotaSyncEvent, current EntitlementValueState, currency string, at time.Time, epoch uint64) (*perpetualSyncReplay, error) {
	if ctx == nil || len(records) == 0 || at.IsZero() || !entitlements.IsPerpetualEnd(current.EndAt) {
		return nil, ErrValueEvidence
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	records = append([]EntitlementValueSnapshot(nil), records...)
	sort.Slice(records, func(i, j int) bool { return records[i].QuotaEventID < records[j].QuotaEventID })
	rootIndex := -1
	for i, s := range records {
		if (s.Mode == "new_term" || s.Mode == "recovery_term" || s.Mode == "replacement_term") && s.After.StartAt.Equal(current.StartAt) {
			rootIndex = i
		}
	}
	if rootIndex < 0 {
		return nil, ErrValueEvidence
	}
	records = records[rootIndex:]
	root := records[0]
	if _, err := projectSinglePerpetualQuotaValue(ctx, records[:1], root.After, currency, at); err != nil {
		return nil, err
	}
	rootKindValid := root.Mode == "new_term" && root.Kind == "new" || root.Mode == "recovery_term" && root.Kind == "renewal" || root.Mode == "replacement_term" && root.Kind == "upgrade"
	if !rootKindValid || root.After.PlanID == 0 || root.After.PlanSKUID == 0 || perpetualSyncTimeBefore(at, root.QuotaEventAt) || root.After.EndedAt != nil || root.After.Status != "active" || root.After.FlowUsed < 0 || root.After.CycleStartUsed < 0 {
		return nil, ErrValueEvidence
	}
	replay := &perpetualSyncReplay{ctx: ctx, root: root, state: root.After, epoch: epoch, totalPaid: root.ActualPaidMinor, lastID: root.QuotaEventID, lastAt: root.QuotaEventAt, lots: []perpetualSyncLot{{OrderID: root.OrderID, Quota: root.After.FlowTotal - root.After.CycleStartUsed, PaidMinor: root.ActualPaidMinor}}}
	if err := replay.bindSource("paid_grant", root); err != nil {
		return nil, err
	}
	// IDs are source authority. Require the adapter's sync stream to be ordered;
	// same-millisecond events remain distinct and are never sorted by timestamp.
	for i, event := range events {
		if event.QuotaEventID <= root.QuotaEventID || event.QuotaEventAt.IsZero() || perpetualSyncTimeBefore(at, event.QuotaEventAt) || (i > 0 && event.QuotaEventID <= events[i-1].QuotaEventID) {
			return nil, ErrValueEvidence
		}
	}
	seenOrders := map[uint]bool{root.OrderID: true}
	grantIndex, syncIndex := 1, 0
	for grantIndex < len(records) || syncIndex < len(events) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		useSync := syncIndex < len(events) && (grantIndex == len(records) || events[syncIndex].QuotaEventID < records[grantIndex].QuotaEventID)
		if !useSync {
			s := records[grantIndex]
			if s.QuotaEventID <= replay.lastID || s.QuotaEventAt.IsZero() || perpetualSyncTimeBefore(s.QuotaEventAt, replay.lastAt) || perpetualSyncTimeBefore(at, s.QuotaEventAt) || seenOrders[s.OrderID] {
				return nil, ErrValueEvidence
			}
			if err := replay.grant(s, currency, at); err != nil {
				return nil, err
			}
			seenOrders[s.OrderID] = true
			if err := replay.bindSource("paid_grant", s); err != nil {
				return nil, err
			}
			replay.lastID, replay.lastAt = s.QuotaEventID, s.QuotaEventAt
			grantIndex++
		} else {
			event := events[syncIndex]
			if event.QuotaEventID <= replay.lastID || perpetualSyncTimeBefore(event.QuotaEventAt, replay.lastAt) {
				return nil, ErrValueEvidence
			}
			if err := replay.sync(event); err != nil {
				return nil, err
			}
			if err := replay.bindSource("base_quota_sync", event); err != nil {
				return nil, err
			}
			replay.syncCount++
			replay.lastID, replay.lastAt = event.QuotaEventID, event.QuotaEventAt
			syncIndex++
		}
	}
	final := current
	if final.EndedAt != nil {
		if final.EndReason != "exhausted" || final.Status != "expired" || final.FlowUsed < final.FlowTotal {
			return nil, ErrValueEvidence
		}
		final.EndedAt = nil
		final.EndReason = replay.state.EndReason
	}
	if err := replay.advance(final); err != nil {
		return nil, err
	}
	return replay, nil
}

func (r *perpetualSyncReplay) advance(next EntitlementValueState) error {
	if !samePerpetualStateIdentity(next, r.state) || next.FlowTotal != r.state.FlowTotal || next.ResetQuotaBytes != r.state.ResetQuotaBytes || next.FlowUsed < r.state.FlowUsed || next.FlowUsed < next.CycleStartUsed || (next.Status != "active" && next.Status != "expired") || next.EndedAt != nil || next.EndReason != r.state.EndReason {
		return ErrValueEvidence
	}
	used := next.FlowUsed - r.state.FlowUsed
	for r.firstUnspent < len(r.lots) && used > 0 {
		if err := r.ctx.Err(); err != nil {
			return err
		}
		lot := &r.lots[r.firstUnspent]
		taken := min(used, lot.remaining())
		lot.Consumed += taken
		used -= taken
		if lot.remaining() == 0 {
			r.firstUnspent++
		}
	}
	if used != 0 {
		r.unknownOverrun = true
	}
	r.state = next
	return nil
}

func (r *perpetualSyncReplay) grant(s EntitlementValueSnapshot, currency string, at time.Time) error {
	if s.Version != 1 || s.OrderID == 0 || s.SubscriptionID != r.root.SubscriptionID || s.Currency != currency || !s.PaymentVerified || s.LegacyTransferUnproven || s.ActualPaidMinor < 0 || s.CapturedAt.IsZero() || s.Before == nil || len(s.PaidPeriods) != 0 {
		return ErrValueEvidence
	}
	valid := s.Mode == "perpetual_quota_addition" && s.AllocationModel == PerpetualQuotaFIFOAllocation
	legacy := s.Mode == "unsupported_legacy_effect" && ((s.Kind == "renewal" && s.AllocationModel == "uniform_paid_units_v1") || (s.Kind == "traffic_pack" && s.AllocationModel == "legacy_value_requires_review"))
	if (!valid && !legacy) || (s.Kind != "renewal" && s.Kind != "traffic_pack") || (s.Kind == "renewal" && (s.BillingUnit != "once" || s.BillingValue != 1)) || !s.TermEndAt.Equal(r.root.TermEndAt) || s.TermStartAt.Before(r.root.TermStartAt) || at.Before(s.TermStartAt) {
		return ErrValueEvidence
	}
	before, after := *s.Before, s.After
	if len(r.lots) == 1 && r.syncCount == 0 && before.FlowTotal != r.state.FlowTotal {
		// Preserve the previously accepted purchase-time single-lot sync anchor.
		// It may establish base quota only before the first additional paid lot.
		if before.FlowTotal <= before.CycleStartUsed || !samePerpetualStateIdentity(before, r.state) {
			return ErrValueEvidence
		}
		r.lots[0].Quota = before.FlowTotal - before.CycleStartUsed
		r.state.FlowTotal, r.state.ResetQuotaBytes = before.FlowTotal, before.ResetQuotaBytes
	}
	if err := r.advance(before); err != nil {
		return err
	}
	if after.PlanSKUID == 0 || before.FlowUsed != after.FlowUsed || before.CycleStartUsed != after.CycleStartUsed || before.PlanID != after.PlanID || !before.StartAt.Equal(after.StartAt) || !before.EndAt.Equal(after.EndAt) || after.FlowTotal <= before.FlowTotal || after.ResetPolicy != before.ResetPolicy || after.NextResetAt != nil || after.EndedAt != nil || after.EndReason != "" || after.Status != "active" {
		return ErrValueEvidence
	}
	if r.totalPaid > math.MaxInt64-s.ActualPaidMinor {
		return ErrValueEvidence
	}
	r.totalPaid += s.ActualPaidMinor
	delta := after.FlowTotal - before.FlowTotal
	r.lots = append(r.lots, perpetualSyncLot{OrderID: s.OrderID, Quota: delta, PaidMinor: s.ActualPaidMinor})
	r.state = after
	return nil
}

func (r *perpetualSyncReplay) prepare(before EntitlementValueState, newBase int64, revision uint64) (PerpetualBaseQuotaSyncDetail, error) {
	var empty PerpetualBaseQuotaSyncDetail
	if newBase <= 0 || revision == 0 || revision <= r.lastPlanRevision || r.unknownOverrun || before.EndedAt != nil {
		return empty, ErrValueEvidence
	}
	fingerprint, err := r.fingerprint(before, nil)
	if err != nil {
		return empty, err
	}
	base := r.lots[0]
	remaining := max(int64(0), newBase-base.Consumed)
	totalRemaining := remaining
	for _, lot := range r.lots[1:] {
		if err := r.ctx.Err(); err != nil {
			return empty, err
		}
		if totalRemaining > math.MaxInt64-lot.remaining() {
			return empty, ErrValueEvidence
		}
		totalRemaining += lot.remaining()
	}
	if before.FlowUsed < 0 || before.FlowUsed > math.MaxInt64-totalRemaining {
		return empty, ErrValueEvidence
	}
	after := before
	after.FlowTotal = before.FlowUsed + totalRemaining
	after.ResetQuotaBytes = newBase
	// Eligibility/terminal checks remain with the existing plan-sync authority.
	// This is its established nonterminal quota-status normalization.
	after.Status = "active"
	return PerpetualBaseQuotaSyncDetail{Version: PerpetualBaseQuotaSyncVersion, Kind: PerpetualBaseQuotaSyncKind, SubscriptionID: r.root.SubscriptionID, EntitlementEpoch: r.epoch, PlanID: before.PlanID, PlanRevision: revision, BaseOrderID: base.OrderID, OldBaseQuota: base.Quota, NewBaseQuota: newBase, UsedAtSync: before.FlowUsed, BaseConsumedBytes: base.Consumed, BaseRemainingBefore: base.remaining(), BaseRemainingAfter: remaining, Before: before, After: after, SourceFingerprint: fingerprint}, nil
}

func (r *perpetualSyncReplay) sync(event PerpetualBaseQuotaSyncEvent) error {
	detail := event.Detail
	if detail.Version != PerpetualBaseQuotaSyncVersion || detail.Kind != PerpetualBaseQuotaSyncKind || detail.SubscriptionID != r.root.SubscriptionID || detail.EntitlementEpoch != r.epoch || detail.PlanID != r.state.PlanID || detail.PlanRevision <= r.lastPlanRevision || detail.BaseOrderID != r.root.OrderID || detail.UsedAtSync != detail.Before.FlowUsed || detail.NewBaseQuota <= 0 {
		return ErrValueEvidence
	}
	if err := r.advance(detail.Before); err != nil {
		return err
	}
	expected, err := r.prepare(detail.Before, detail.NewBaseQuota, detail.PlanRevision)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(expected, detail) {
		return ErrValueEvidence
	}
	r.lots[0].Quota = detail.NewBaseQuota
	// A previously exhausted base can receive new unspent quota. Reset traversal
	// to the oldest lot without undoing consumption already assigned to add-ons.
	r.firstUnspent = 0
	r.state = detail.After
	r.lastPlanRevision = detail.PlanRevision
	return nil
}

// Every immutable source is included once in an ordered hash chain. Replaying
// many revisions must not repeatedly reserialize the entire history prefix.
func (r *perpetualSyncReplay) bindSource(kind string, source any) error {
	if err := r.ctx.Err(); err != nil {
		return err
	}
	payload, err := json.Marshal(struct {
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
	r.sourceDigest = sha256.Sum256(payload)
	return nil
}

func (r *perpetualSyncReplay) fingerprint(current EntitlementValueState, quote *ValueProjection) (string, error) {
	if err := r.ctx.Err(); err != nil {
		return "", err
	}
	payload, err := json.Marshal(struct {
		Version      int
		Epoch        uint64
		SourceDigest string
		State        EntitlementValueState
		Lots         []perpetualSyncLot
		Quote        *ValueProjection
		Sources      map[uint]int64
	}{1, r.epoch, hex.EncodeToString(r.sourceDigest[:]), current, r.lots, quote, perpetualSyncProjectionSources(quote)})
	if err != nil {
		return "", err
	}
	if err := r.ctx.Err(); err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func perpetualSyncProjectionSources(quote *ValueProjection) map[uint]int64 {
	if quote == nil {
		return nil
	}
	return quote.Sources
}

// Row datetime(3) can round while older immutable JSON retains nanoseconds.
// Event IDs and proved state transitions establish ordering. Permit only the
// known one-millisecond representation uncertainty in the weaker clock check.
func perpetualSyncTimeBefore(next, previous time.Time) bool {
	return next.Add(time.Millisecond).Before(previous)
}
