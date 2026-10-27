package handler

import (
	"gorm.io/gorm"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestSQLiteCoverageLongRuntimePreservesIdentityAndAccounting(t *testing.T) {
	h, c := accountingBenchmarkFixture(t)
	assertCoverageLongRuntime(t, h, c)
}
func TestMySQLCoverageLongRuntimePreservesIdentityAndAccounting(t *testing.T) {
	h, c := accountingFixtureFromOrder(t, newMySQLBusinessOrderFixture(t))
	assertCoverageLongRuntime(t, h, c)
}
func assertCoverageLongRuntime(t *testing.T, h *handlers, c model.ProtocolCredential) {
	t.Helper()
	at := time.Now().UTC().Truncate(time.Millisecond)
	coreA := strings.Repeat("r", 189) + "A"
	coreB := strings.Repeat("r", 189) + "B"
	// Use the actual buffered collector, including its independently committed
	// auxiliary projection. A swallowed coverage error must fail this test.
	events := accountingBenchmarkEvents(c, 0, 2)
	for i := range events {
		events[i].CoreInstanceID = coreA
		events[i].OccurredAt = at.Add(time.Duration(i) * time.Second)
		events[i].ReceivedAt = events[i].OccurredAt
	}
	if err := h.projectZeroNodeEvents(t.Context(), events); err != nil {
		t.Fatal(err)
	}
	assertAccountingTotal(t, h, c.SubscriptionID, 60, subStatusActive)
	var first meteringstore.CoverageRecord
	if err := h.db.First(&first, "node_id = ?", c.NodeID).Error; err != nil {
		t.Fatal("accepted facts lost coverage", err)
	}
	if first.CoreInstanceID != coreA || first.LastSequence != 2 || first.GapCount != 0 {
		t.Fatal("long identity was truncated, hashed or not projected", first)
	}
	// A contiguous follow-up preserves the same continuous window. A replay
	// cannot turn either ledger values or the established generation backwards.
	advance := func(core, id string, seq uint64, occurred time.Time) {
		t.Helper()
		err := h.services.ObserveFairUseCoverage(t.Context(), metering.NodeCoverageEvent{NodeID: c.NodeID, ReceivedAt: at.Add(3 * time.Second), Event: metering.CoverageEvent{CoreInstanceID: core, EventID: id, EventType: "flow.updated", Sequence: seq, OccurredAt: occurred}})
		if err != nil {
			t.Fatal(err)
		}
	}
	advance(coreA, "contiguous", 3, at.Add(2*time.Second))
	var continuous meteringstore.CoverageRecord
	if err := h.db.First(&continuous, "node_id = ?", c.NodeID).Error; err != nil {
		t.Fatal(err)
	}
	if continuous.CoreInstanceID != coreA || continuous.LastSequence != 3 || !continuous.ContinuousSinceAt.Equal(first.ContinuousSinceAt) {
		t.Fatal("same long runtime looked like restart", continuous)
	}
	advance(coreB, "next-runtime", 1, at.Add(4*time.Second))
	var next meteringstore.CoverageRecord
	if err := h.db.First(&next, "node_id = ?", c.NodeID).Error; err != nil {
		t.Fatal(err)
	}
	if next.CoreInstanceID != coreB || next.LastSequence != 1 {
		t.Fatal("shared 189-character prefix aliased distinct generations", next)
	}
	advance(coreA, "late-old", 99, at)
	var after meteringstore.CoverageRecord
	if err := h.db.First(&after, "node_id = ?", c.NodeID).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(next, after) {
		t.Fatal("late prior generation replaced current coverage")
	}
	if err := h.services.FairUseFlowCollection.Record(t.Context(), metering.FlowStart{NodeID: c.NodeID, CoreInstanceID: coreA, EventID: "long-flow-start", Sequence: 1, PrincipalKey: c.PrincipalKey, OccurredAt: at, ReceivedAt: at}); err != nil {
		t.Fatal(err)
	}
	var flow meteringstore.FlowStartRecord
	if err := h.db.Where("node_id = ? AND event_id = ?", c.NodeID, "long-flow-start").First(&flow).Error; err != nil || flow.CoreInstanceID != coreA {
		t.Fatal("long flow-start auxiliary identity lost", err)
	}
	if err := h.projectZeroNodeEvents(t.Context(), events); err != nil {
		t.Fatal(err)
	}
	assertAccountingTotal(t, h, c.SubscriptionID, 60, subStatusActive)
	var facts []model.TrafficRecord
	if err := h.db.Where("subscription_id = ?", c.SubscriptionID).Find(&facts).Error; err != nil || len(facts) != 2 {
		t.Fatal("replay changed authoritative facts", len(facts), err)
	}
	for _, fact := range facts {
		if !strings.Contains(fact.Meta, coreA) {
			t.Fatal("financial proof core was rewritten")
		}
	}
}

func TestSQLiteCoverageIdentityBudgetPrecedesFullReadAndDoesNotBlockFacts(t *testing.T) {
	h, c := accountingBenchmarkFixture(t)
	assertCoverageIdentityReadBudget(t, h, c)
}
func TestMySQLCoverageIdentityBudgetPrecedesFullReadAndDoesNotBlockFacts(t *testing.T) {
	h, c := accountingFixtureFromOrder(t, newMySQLBusinessOrderFixture(t))
	assertCoverageIdentityReadBudget(t, h, c)
}
func assertCoverageIdentityReadBudget(t *testing.T, h *handlers, c model.ProtocolCredential) {
	t.Helper()
	at := time.Now().UTC().Truncate(time.Millisecond)
	row := meteringstore.CoverageRecord{NodeID: c.NodeID, CoreInstanceID: strings.Repeat("x", 2*1024*1024+1), LastSequence: 1, LastEventID: "oversized-owned-corruption", ContinuousSinceAt: at, LastReceivedAt: at, LastEventOccurredAt: at, UpdatedAt: at}
	// Only this owned fixture injects an impossible >wire identity to exercise
	// corruption handling; ordinary authenticated admission stays unchanged.
	if err := h.db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	fullReads := 0
	const callback = "test:coverage_identity_budget"
	if err := h.db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "fair_use_node_coverage" && len(tx.Statement.Selects) == 0 {
			fullReads++
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer h.db.Callback().Query().Remove(callback)
	err := h.services.ObserveFairUseCoverage(t.Context(), metering.NodeCoverageEvent{NodeID: c.NodeID, ReceivedAt: at, Event: metering.CoverageEvent{CoreInstanceID: "valid-next", Sequence: 2, EventID: "next", OccurredAt: at}})
	if err == nil || fullReads != 0 {
		t.Fatal("oversized persisted identity reached an unbounded full read", fullReads, err)
	}
	if err := h.projectZeroNodeEvents(t.Context(), accountingBenchmarkEvents(c, 0, 1)); err != nil {
		t.Fatal("auxiliary evidence failure blocked accepted accounting", err)
	}
	assertAccountingTotal(t, h, c.SubscriptionID, 30, subStatusActive)
	if fullReads != 0 {
		t.Fatal("accepted accounting retried oversized raw coverage", fullReads)
	}
}
