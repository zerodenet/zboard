package observationstore

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func mailboxFixture(t *testing.T) (*gorm.DB, *gorm.DB, model.Node) {
	a, b, n := ownershipFixture(t)
	if e := a.Exec("ALTER TABLE nodes ADD COLUMN lifecycle_status TEXT NOT NULL DEFAULT 'active'").Error; e != nil {
		t.Fatal(e)
	}
	return a, b, n
}
func mailboxSource(n model.Node, core string, seq uint64, at time.Time, stats bool) network.ObservationSource {
	p := network.EventPosition{AssetTag: n.AssetTag, ID: "report", CoreInstanceID: core, Sequence: seq, OccurredAt: at, ReceivedAt: at}
	o := network.NodeObservation{NodeID: n.ID, Latest: p}
	if stats {
		o.StatsEvent = &p
		o.Stats = network.NodeStats{ActiveSessions: seq, BytesUp: seq * 100, BytesDown: seq * 200}
	}
	return network.ObservationSource{Observation: o, Kind: "flow.updated"}
}
func enqueueMailbox(t *testing.T, db *gorm.DB, s ...network.ObservationSource) {
	t.Helper()
	if e := db.Transaction(func(tx *gorm.DB) error { return EnqueueLatest(tx, s) }); e != nil {
		t.Fatal(e)
	}
}
func drainMailbox(t *testing.T, db *gorm.DB) {
	t.Helper()
	if n, e := (Worker{DB: db}).Drain(context.Background(), 32, MaxDrainBytes); e != nil || n != 1 {
		t.Fatalf("drain = %d %v", n, e)
	}
}

func TestMailboxRemainsBoundedAndUpdatesStatsWithFullMaterialPool(t *testing.T) {
	a, b, node := mailboxFixture(t)
	if e := a.Model(&model.NodeObservationMaterialBudget{}).Where("id = 1").Update("retained_bytes", MaterialCapacityBytes).Error; e != nil {
		t.Fatal(e)
	}
	base := time.Now().UTC()
	sources := make([]network.ObservationSource, 10000)
	for i := range sources {
		sources[i] = mailboxSource(node, "runtime", uint64(i+1), base.Add(time.Duration(i)*time.Second), i%10 == 0)
	}
	// A large envelope body is irrelevant to current counters and must never
	// leak into either persistent projection or a new diagnostic header.
	sources[len(sources)-1].Payload = bytes.Repeat([]byte("x"), 3<<20)
	sources[len(sources)-1].PrincipalKey = strings.Repeat("principal", 100000)
	sources[len(sources)-1].Observation.Latest.ID = strings.Repeat("\x00", 191)
	sources[len(sources)-1].Observation.Latest.SourceID = strings.Repeat("\x00", 191)
	enqueueMailbox(t, a, sources...)
	for _, table := range []string{"node_observation_intents", "node_observation_retirements", "node_observation_parts"} {
		var count int64
		if e := a.Table(table).Count(&count).Error; e != nil || count != 0 {
			t.Fatal(table, count, e)
		}
	}
	var row model.NodeObservationMailbox
	if e := a.First(&row, node.ID).Error; e != nil {
		t.Fatal(e)
	}
	if len(row.Observation) > projectionBytes || !row.Pending {
		t.Fatal("unbounded or lost mailbox")
	}
	// A fresh adapter/connection consumes durable state without event bodies.
	drainMailbox(t, b)
	var got model.Node
	if e := a.First(&got, node.ID).Error; e != nil {
		t.Fatal(e)
	}
	if got.ActiveFlows != 9991 || got.BytesUp != 999100 {
		t.Fatalf("latest statistics not applied: %+v", got)
	}
	var state model.NodeObservationState
	if e := a.Where("node_id = ? AND asset_tag = ?", node.ID, node.AssetTag).First(&state).Error; e != nil {
		t.Fatal(e)
	}
	if !state.CurrentKnown || state.LatestInline == "" || state.StatsInline == "" {
		t.Fatal("missing bounded provenance")
	}
	var count int64
	a.Model(&model.NodeObservationMailbox{}).Count(&count)
	if count != 1 {
		t.Fatal("per-event mailbox growth", count)
	}
}

func TestMailboxSeparatesRuntimeStatsAndCannotRegressOnHistoricalReports(t *testing.T) {
	a, _, node := mailboxFixture(t)
	base := time.Now().UTC()
	enqueueMailbox(t, a, mailboxSource(node, "old-runtime", 100, base, true))
	drainMailbox(t, a)
	// A new runtime's liveness cannot relabel old runtime counters as current.
	enqueueMailbox(t, a, mailboxSource(node, "new-runtime", 1, base.Add(time.Minute), false))
	drainMailbox(t, a)
	var state model.NodeObservationState
	a.Where("node_id = ? AND asset_tag = ?", node.ID, node.AssetTag).First(&state)
	if state.CurrentKnown {
		t.Fatal("cross-runtime statistics presented as current")
	}
	enqueueMailbox(t, a, mailboxSource(node, "new-runtime", 2, base.Add(2*time.Minute), true))
	drainMailbox(t, a)
	// Older receive/event positions and duplicate updates are a no-op, with no
	// durable queue history and no second projection.
	enqueueMailbox(t, a, mailboxSource(node, "old-runtime", 101, base.Add(time.Second), true))
	if ready, e := (Worker{DB: a}).hasMailboxes(context.Background()); e != nil || ready {
		t.Fatal("historical event requeued stale state", ready, e)
	}
	var got model.Node
	a.First(&got, node.ID)
	if got.ActiveFlows != 2 {
		t.Fatal("historical event regressed counters")
	}
}

func TestMailboxRollbackAndCounterOverrideFence(t *testing.T) {
	a, _, node := mailboxFixture(t)
	base := time.Now().UTC()
	e := a.Transaction(func(tx *gorm.DB) error {
		if e := EnqueueLatest(tx, []network.ObservationSource{mailboxSource(node, "runtime", 1, base, true)}); e != nil {
			return e
		}
		return context.Canceled
	})
	if e != context.Canceled {
		t.Fatal(e)
	}
	var count int64
	a.Model(&model.NodeObservationMailbox{}).Count(&count)
	if count != 0 {
		t.Fatal("rolled back accounting retained a projection")
	}
	if e := a.Transaction(func(tx *gorm.DB) error {
		return CounterOverride(tx, node.ID, node.AssetTag, "heartbeat", base.Add(time.Minute))
	}); e != nil {
		t.Fatal(e)
	}
	enqueueMailbox(t, a, mailboxSource(node, "runtime", 1, base, true))
	drainMailbox(t, a)
	var got model.Node
	a.First(&got, node.ID)
	if got.ActiveFlows != node.ActiveFlows {
		t.Fatal("receiver-time override was overwritten")
	}
	conflict := mailboxSource(node, "runtime", 1, base, true)
	conflict.Observation.Stats.BytesUp++
	enqueueMailbox(t, a, conflict)
	drainMailbox(t, a)
	var state model.NodeObservationState
	a.Where("node_id = ? AND asset_tag = ?", node.ID, node.AssetTag).First(&state)
	var mailbox model.NodeObservationMailbox
	a.First(&mailbox, node.ID)
	if mailbox.Reason != "stats_position_conflict" || mailbox.Pending {
		t.Fatal("conflict was not surfaced independently", mailbox)
	}
}

func TestMailboxDamagedPendingStateDoesNotBlockAccounting(t *testing.T) {
	a, _, node := mailboxFixture(t)
	at := time.Now().UTC()
	enqueueMailbox(t, a, mailboxSource(node, "runtime", 10, at, true))
	drainMailbox(t, a)
	if e := a.Model(&model.NodeObservationMailbox{}).Where("node_id = ?", node.ID).Updates(map[string]any{"observation": "{broken", "pending": true}).Error; e != nil {
		t.Fatal(e)
	}
	drainMailbox(t, a)
	var row model.NodeObservationMailbox
	a.First(&row, node.ID)
	if row.Pending || row.Reason != "invalid_mailbox" {
		t.Fatal(row)
	}
	// Recovering an older pending position cannot regress applied counters.
	enqueueMailbox(t, a, mailboxSource(node, "runtime", 9, at.Add(-time.Minute), true))
	drainMailbox(t, a)
	var got model.Node
	a.First(&got, node.ID)
	if got.ActiveFlows != 10 {
		t.Fatal("recovery regressed counters", got.ActiveFlows)
	}
	enqueueMailbox(t, a, mailboxSource(node, "runtime", 11, at.Add(time.Minute), true))
	drainMailbox(t, a)
	a.First(&got, node.ID)
	if got.ActiveFlows != 11 {
		t.Fatal("recovery failed", got.ActiveFlows)
	}
}

func TestMailboxLongRuntimeSurvivesLegacyTransition(t *testing.T) {
	a, _, node := mailboxFixture(t)
	at := time.Now().UTC()
	core := strings.Repeat("runtime", 100)
	old := mailboxSource(node, core, 100, at, true)
	if _, e := Enqueue(a, old); e != nil {
		t.Fatal(e)
	}
	drainMailbox(t, a)
	// Same runtime uses sequence ordering even when its clock moves backwards.
	next := mailboxSource(node, core, 101, at.Add(-time.Hour), true)
	next.Observation.Latest.ReceivedAt = at.Add(time.Minute)
	next.Observation.StatsEvent.ReceivedAt = at.Add(time.Minute)
	enqueueMailbox(t, a, next)
	drainMailbox(t, a)
	var got model.Node
	a.First(&got, node.ID)
	if got.ActiveFlows != 101 {
		t.Fatal("legacy runtime identity comparison changed", got.ActiveFlows)
	}
}

func TestMailboxCoalescingPreservesQueueAge(t *testing.T) {
	a, _, node := mailboxFixture(t)
	at := clockNow()
	enqueueMailbox(t, a, mailboxSource(node, "runtime", 1, at, true))
	since := at.Add(-time.Minute)
	if err := a.Model(&model.NodeObservationMailbox{}).Where("node_id = ?", node.ID).UpdateColumn("pending_since", since).Error; err != nil {
		t.Fatal(err)
	}
	enqueueMailbox(t, a, mailboxSource(node, "runtime", 2, at.Add(time.Second), true))
	var row model.NodeObservationMailbox
	if err := a.First(&row, node.ID).Error; err != nil || !row.PendingSince.Equal(since) {
		t.Fatal("coalescing reset oldest pending age", row.PendingSince, err)
	}
	drainMailbox(t, a)
	enqueueMailbox(t, a, mailboxSource(node, "runtime", 3, at.Add(2*time.Second), true))
	if err := a.First(&row, node.ID).Error; err != nil || !row.PendingSince.After(since) {
		t.Fatal("new queue round retained old age", row.PendingSince, err)
	}
}
