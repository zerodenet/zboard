package networkstore

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/observationstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestNodeObservationIndependentSources(t *testing.T) {
	t.Setenv("ZBOARD_TEST_MYSQL_DSN", "")
	observationIndependentSources(t)
}
func TestNodeObservationOwnedAckRollback(t *testing.T) {
	t.Setenv("ZBOARD_TEST_MYSQL_DSN", "")
	observationOwnedAckRollback(t)
}
func TestNodeObservationUnknownAndByteBudget(t *testing.T) {
	t.Setenv("ZBOARD_TEST_MYSQL_DSN", "")
	observationUnknownAndByteBudget(t)
}
func TestNodeObservationUnknownAssetDoesNotBorrowFloor(t *testing.T) {
	t.Setenv("ZBOARD_TEST_MYSQL_DSN", "")
	observationUnknownAssetDoesNotBorrowFloor(t)
}
func TestMySQLNodeObservationHandoff(t *testing.T) {
	if os.Getenv("ZBOARD_TEST_MYSQL_DSN") == "" {
		t.Skip("actual MySQL DSN required")
	}
	t.Run("independent_sources", observationIndependentSources)
	t.Run("owned_ack_rollback", observationOwnedAckRollback)
	t.Run("unknown_and_byte_budget", observationUnknownAndByteBudget)
	t.Run("unknown_asset_floor", observationUnknownAssetDoesNotBorrowFloor)
}

func observationUnknownAssetDoesNotBorrowFloor(t *testing.T) {
	a, _ := administrationFixture(t)
	node := model.Node{Name: "unknown-asset", Config: "{}", IsEnabled: true, ActiveFlows: 17}
	if err := a.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	// A recorded0059 node may legitimately lack attribution. Model create-only
	// identity must not be backfilled just to make an observation appear known.
	if err := a.Exec("UPDATE nodes SET asset_tag = '' WHERE id = ?", node.ID).Error; err != nil {
		t.Fatal(err)
	}
	at := time.Unix(1700000000, 0).UTC()
	floor := model.NodeObservationState{NodeID: node.ID, AssetTag: "", Deleted: true, CurrentSource: "deleted", CounterWrittenAt: &at, UpdatedAt: at}
	if err := a.Create(&floor).Error; err != nil {
		t.Fatal(err)
	}
	var row model.Node
	if err := withObservationEvidence(a.Model(&model.Node{})).Where("nodes.id = ?", node.ID).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	evidence := nodeCounterEvidence(row)
	if row.ActiveFlows != 17 || evidence.Known || evidence.Source != "unknown" || evidence.CounterReceivedAt != nil || evidence.LatestReference != "" || evidence.StatsReference != "" {
		t.Fatal("unknown asset borrowed legacy floor evidence", row.ActiveFlows, evidence)
	}
}
func observationSource(node model.Node, id, core string, sequence uint64, stats bool) network.ObservationSource {
	at := time.Unix(1700000000, 0).UTC()
	p := network.EventPosition{AssetTag: node.AssetTag, ID: id, CoreInstanceID: core, Sequence: sequence, OccurredAt: at, ReceivedAt: at}
	source := network.ObservationSource{Observation: network.NodeObservation{NodeID: node.ID, Latest: p}, Kind: "flow.updated", Payload: []byte(`{"first_sequence":7,"accounting_samples":[1,2]}`)}
	if stats {
		source.Kind = "stats.sampled"
		source.Observation.StatsEvent = &p
		source.Observation.Stats = network.NodeStats{ActiveSessions: 3, BytesUp: 100, BytesDown: 200}
	}
	return source
}
func saveObservation(t *testing.T, db *gorm.DB, s network.ObservationSource) string {
	t.Helper()
	var id string
	if err := db.Transaction(func(tx *gorm.DB) error { var err error; id, err = observationstore.Enqueue(tx, s); return err }); err != nil {
		t.Fatal(err)
	}
	return id
}
func drainObservations(t *testing.T, db *gorm.DB) {
	t.Helper()
	if _, err := (observationstore.Worker{DB: db}).Drain(context.Background(), 32, observationstore.MaxDrainBytes); err != nil {
		t.Fatal(err)
	}
}
func observationState(t *testing.T, db *gorm.DB, node model.Node) model.NodeObservationState {
	t.Helper()
	var s model.NodeObservationState
	if err := db.First(&s, "node_id = ? AND asset_tag = ?", node.ID, node.AssetTag).Error; err != nil {
		t.Fatal(err)
	}
	return s
}
func observationIndependentSources(t *testing.T) {
	a, b := administrationFixture(t)
	node := model.Node{Name: "independent", Config: "{}", IsEnabled: true}
	if err := a.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	flow := observationSource(node, "flow100", "runtime-a", 100, false)
	flowID := saveObservation(t, a, flow)
	drainObservations(t, b)
	state := observationState(t, b, node)
	if state.LatestReference != flowID || state.StatsReference != "" || state.CurrentKnown {
		t.Fatal("flow fabricated Stats", state)
	}
	stats := observationSource(node, "stats99", "runtime-a", 99, true)
	statsID := saveObservation(t, a, stats)
	drainObservations(t, b)
	state = observationState(t, b, node)
	var after model.Node
	if err := b.First(&after, node.ID).Error; err != nil {
		t.Fatal(err)
	}
	if state.LatestReference != flowID || state.StatsReference != statsID || !state.CurrentKnown || after.ActiveFlows != 3 || after.BytesUp != 100 || after.BytesDown != 200 {
		t.Fatal("flow100 swallowed stats99", state, after.ActiveFlows)
	}
	record := nodeAdministrationRecord(after)
	if record.Observation.Known {
		t.Fatal("unjoined Node read invented evidence")
	}
	if err := withObservationEvidence(b.Model(&model.Node{})).Where("nodes.id = ?", node.ID).First(&after).Error; err != nil {
		t.Fatal(err)
	}
	record = nodeAdministrationRecord(after)
	if !record.Observation.Known || !record.Observation.StatsSourceKnown || record.Observation.StatsReference != statsID {
		t.Fatal("read model lost current source", record.Observation)
	}
	flowB := observationSource(node, "flow-b", "runtime-b", 101, false)
	flowB.Observation.Latest.OccurredAt = flowB.Observation.Latest.OccurredAt.Add(time.Second)
	saveObservation(t, a, flowB)
	drainObservations(t, b)
	state = observationState(t, b, node)
	if state.CurrentKnown || state.StatsReference != statsID {
		t.Fatal("Latest runtime borrowed historical Stats identity", state)
	}
	// Heartbeat counters are actual writes, but have no buffered position. A
	// liveness-only heartbeat does not change the independently retained source.
	if err := a.Model(&model.Node{}).Where("id = ?", node.ID).Update("node_credential", "activity").Error; err != nil {
		t.Fatal(err)
	}
	count := uint64(9)
	if err := (NodeActivity{DB: a}).RecordNodeActivity(context.Background(), node.ID, "activity", network.NodeActivityUpdate{At: time.Now().UTC(), ActiveFlows: &count, CounterWriter: network.CounterWriterHeartbeat}); err != nil {
		t.Fatal(err)
	}
	state = observationState(t, b, node)
	if state.CurrentKnown || state.CurrentSource != "heartbeat" || state.StatsReference != statsID {
		t.Fatal("heartbeat fabricated or erased source", state)
	}
	// Delete/reuse cannot admit the old asset's queued counters. Use the same
	// node→state tombstone primitive as the actual administrative deletion path.
	if err := a.Transaction(func(tx *gorm.DB) error {
		if err := observationstore.Tombstone(tx, node.ID, node.AssetTag); err != nil {
			return err
		}
		return tx.Delete(&node).Error
	}); err != nil {
		t.Fatal(err)
	}
	replacement := model.Node{ID: node.ID, Name: "replacement", Config: "{}", IsEnabled: true}
	if err := a.Create(&replacement).Error; err != nil {
		t.Fatal(err)
	}
	old := observationSource(node, "queued-before-delete", "runtime-a", 102, true)
	oldID := saveObservation(t, a, old)
	drainObservations(t, b)
	var h model.NodeObservationIntent
	if err := b.First(&h, "id = ?", oldID).Error; err != nil || h.Status != "held" || h.Reason != "asset_mismatch" {
		t.Fatal("reused ID accepted old counters", h, err)
	}
	if err := b.First(&replacement, replacement.ID).Error; err != nil || replacement.ActiveFlows != 0 {
		t.Fatal("replacement inherited old statistics", err)
	}
}
func observationOwnedAckRollback(t *testing.T) {
	a, b := administrationFixture(t)
	node := model.Node{Name: "ack", Config: "{}", IsEnabled: true}
	if err := a.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	source := observationSource(node, "ack-stats", "runtime", 1, true)
	id := saveObservation(t, a, source)
	injected := errors.New("owned acknowledgement unavailable")
	const hook = "test:observation_ack"
	if err := a.Callback().Update().Before("gorm:update").Register(hook, func(tx *gorm.DB) {
		if tx.Statement.Table != "node_observation_intents" {
			return
		}
		values, ok := tx.Statement.Dest.(map[string]any)
		if ok && values["status"] == "applied" {
			tx.AddError(injected)
		}
	}); err != nil {
		t.Fatal(err)
	}
	_, err := (observationstore.Worker{DB: a}).Drain(context.Background(), 32, observationstore.MaxDrainBytes)
	_ = a.Callback().Update().Remove(hook)
	if !errors.Is(err, injected) {
		t.Fatal("missing owned ACK failure", err)
	}
	var current model.Node
	if err := b.First(&current, node.ID).Error; err != nil || current.ActiveFlows != 0 {
		t.Fatal("failed ACK committed counters", err)
	}
	var count int64
	if err := b.Model(&model.NodeObservationState{}).Where("node_id = ? AND asset_tag = ?", node.ID, node.AssetTag).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("failed ACK committed position", err)
	}
	var header model.NodeObservationIntent
	if err := b.First(&header, "id = ?", id).Error; err != nil || header.Status != "pending" || header.Reason != "projection_failed" {
		t.Fatal("failure did not retain retry intent", header, err)
	}
	if err := b.Model(&header).Update("available_at", time.Now().UTC().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	drainObservations(t, b)
	state := observationState(t, b, node)
	if !state.CurrentKnown {
		t.Fatal("independent-pool replay failed")
	}
	var before int64
	if err := b.Model(&model.NodeObservationPart{}).Where("intent_id = ?", id).Count(&before).Error; err != nil || before == 0 {
		t.Fatal("applied reference lost material", err)
	}
	if replay := saveObservation(t, a, source); replay != id {
		t.Fatal("exact replay created new identity")
	}
	drainObservations(t, b)
	if observationState(t, b, node).StatsReference != id {
		t.Fatal("replay changed accepted source")
	}
	// An item-specific persistence failure still permits another node to advance
	// within this same bounded round; the error remains visible to the scheduler.
	bad, good := model.Node{Name: "bad-node", Config: "{}", IsEnabled: true}, model.Node{Name: "other-node", Config: "{}", IsEnabled: true}
	if err := a.Create(&bad).Error; err != nil {
		t.Fatal(err)
	}
	if err := a.Create(&good).Error; err != nil {
		t.Fatal(err)
	}
	badSource := observationSource(bad, "bad-write", "other-runtime", 1, true)
	badSource.Observation.Stats.ActiveSessions = 9
	saveObservation(t, a, badSource)
	goodID := saveObservation(t, a, observationSource(good, "good-write", "other-runtime", 1, true))
	const badHook = "test:observation_one_bad_node"
	if err := a.Callback().Update().Before("gorm:update").Register(badHook, func(tx *gorm.DB) {
		if tx.Statement.Table != "nodes" {
			return
		}
		if values, ok := tx.Statement.Dest.(map[string]any); ok && values["active_flows"] == uint64(9) {
			tx.AddError(injected)
		}
	}); err != nil {
		t.Fatal(err)
	}
	_, err = (observationstore.Worker{DB: a}).Drain(context.Background(), 32, observationstore.MaxDrainBytes)
	_ = a.Callback().Update().Remove(badHook)
	if !errors.Is(err, injected) {
		t.Fatal("specific node failure disappeared", err)
	}
	if state := observationState(t, b, good); !state.CurrentKnown || state.StatsReference != goodID {
		t.Fatal("bad node blocked another node in same round", state)
	}

}
func observationUnknownAndByteBudget(t *testing.T) {
	a, b := administrationFixture(t)
	node := model.Node{Name: "budget", Config: "{}", IsEnabled: true}
	if err := a.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	// A routing identity close to the existing 1MiB wire budget is usable;
	// there is no VARCHAR(128) identity projection or truncation.
	long := observationSource(node, "wire-sized-core", strings.Repeat("c", 1024*1024-2048), 1, true)
	longID := saveObservation(t, a, long)
	drainObservations(t, b)
	if state := observationState(t, b, node); state.StatsReference != longID || !state.CurrentKnown {
		t.Fatal("wire-sized core was narrowed or rejected", state)
	}
	unknown := observationSource(node, "legacy", "legacy", 1, true)
	unknown.Observation.Latest.AssetTag = ""
	unknown.Observation.StatsEvent.AssetTag = ""
	unknownID := saveObservation(t, a, unknown)
	// Optional material beyond2MiB is admitted as a durable compact gap.
	// This never rejects a financial sample or truncates accepted financial proof.
	large := observationSource(node, "large", strings.Repeat("c", 1024*1024), 2, true)
	large.Payload = bytes.Repeat([]byte("x"), int(observationstore.MaxApplyBytes))
	largeID := saveObservation(t, a, large)
	good := observationSource(node, "good", "runtime", 3, true)
	goodID := saveObservation(t, a, good)
	drainObservations(t, b)
	for id, reason := range map[string]string{unknownID: "unknown_source"} {
		var h model.NodeObservationIntent
		if err := b.First(&h, "id = ?", id).Error; err != nil || h.Status != "held" || h.Reason != reason {
			t.Fatal("bad source blocked or vanished", h, err)
		}
	}
	if observationState(t, b, node).StatsReference != goodID {
		t.Fatal("held source blocked another source")
	}
	var header model.NodeObservationIntent
	var receipt model.NodeObservationRetirement
	if e := b.First(&header, "id = ?", largeID).Error; e != nil {
		t.Fatal(e)
	}
	if e := b.First(&receipt, "intent_id = ?", largeID).Error; e != nil {
		t.Fatal(e)
	}
	var retained int64
	if e := b.Model(&model.NodeObservationPart{}).Where("intent_id = ?", largeID).Count(&retained).Error; e != nil || retained != 0 || header.Status != "retired" || header.MaterialRetiredAt == nil || receipt.State != "complete" || receipt.Cause != "source_size_limit" || receipt.ContentVerified {
		t.Fatal("oversize auxiliary source lost its explicit gap", header, receipt, retained, e)
	}
	if got := saveObservation(t, a, large); got != largeID {
		t.Fatal("oversize replay changed source identity", got)
	}

	pending, err := (observationstore.Worker{DB: b}).HasPending(context.Background())
	if err != nil || pending {
		t.Fatal("held evidence remained runnable", err)
	}
}

func TestMySQLObservationBusyNodeReleasesClaimBeforeNode(t *testing.T) {
	if os.Getenv("ZBOARD_TEST_MYSQL_DSN") == "" {
		t.Skip("actual MySQL DSN required")
	}
	a, b := administrationFixture(t)
	node := model.Node{Name: "locked-node", Config: "{}", IsEnabled: true}
	if err := a.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	source := observationSource(node, "busy", "runtime", 1, true)
	id := saveObservation(t, a, source)
	lock := a.Begin()
	if lock.Error != nil {
		t.Fatal(lock.Error)
	}
	defer lock.Rollback()
	if err := lock.Raw("SELECT * FROM nodes WHERE id = ? FOR UPDATE", node.ID).Scan(&model.Node{}).Error; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if done, err := (observationstore.Worker{DB: b}).Drain(ctx, 1, observationstore.MaxDrainBytes); err != nil || done != 0 {
		t.Fatal("busy node waited while owning claim", done, err)
	}
	var h model.NodeObservationIntent
	if err := b.First(&h, "id = ?", id).Error; err != nil || h.Status != "pending" || h.Reason != "node_busy" {
		t.Fatal("busy source not deferred", h, err)
	}
	// The financial producer has no nodes FK: another intent can commit while
	// deletion/administration owns the node row. No queue→node wait is retained.
	second := observationSource(node, "producer-during-node-lock", "runtime", 2, false)
	if err := b.WithContext(ctx).Transaction(func(tx *gorm.DB) error { _, err := observationstore.Enqueue(tx, second); return err }); err != nil {
		t.Fatal("node lock blocked auxiliary append", err)
	}
	if err := lock.Commit().Error; err != nil {
		t.Fatal(err)
	}
	if err := b.Model(&model.NodeObservationIntent{}).Where("id = ?", id).Update("available_at", time.Now().UTC().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	drainObservations(t, b)
	if !observationState(t, b, node).CurrentKnown {
		t.Fatal("retry lost known Stats")
	}
}

func TestMySQLObservationClaimLossRollsBackNodeAndState(t *testing.T) {
	if os.Getenv("ZBOARD_TEST_MYSQL_DSN") == "" {
		t.Skip("actual MySQL DSN required")
	}
	for _, reclaimed := range []bool{false, true} {
		name := "expired"
		if reclaimed {
			name = "reclaimed"
		}
		t.Run(name, func(t *testing.T) {
			a, b := administrationFixture(t)
			node := model.Node{Name: "lost-claim", Config: "{}", IsEnabled: true}
			if err := a.Create(&node).Error; err != nil {
				t.Fatal(err)
			}
			id := saveObservation(t, a, observationSource(node, "lost-claim", "runtime", 1, true))
			var before []model.NodeObservationPart
			if err := b.Where("intent_id = ?", id).Order("ordinal").Find(&before).Error; err != nil || len(before) != 1 {
				t.Fatal("missing immutable source", err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			entered, resume, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
			const hook = "test:observation_claim_loss"
			if err := a.Callback().Update().Before("gorm:update").Register(hook, func(tx *gorm.DB) {
				if tx.Statement.Table != "nodes" {
					return
				}
				close(entered)
				select {
				case <-resume:
				case <-ctx.Done():
					tx.AddError(ctx.Err())
				}
			}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = a.Callback().Update().Remove(hook) })
			type outcome struct {
				done int
				err  error
			}
			result := make(chan outcome, 1)
			go func() {
				defer close(finished)
				done, err := (observationstore.Worker{DB: a}).Drain(ctx, 1, observationstore.MaxDrainBytes)
				result <- outcome{done, err}
			}()
			defer func() {
				cancel()
				select {
				case <-finished:
				case <-time.After(2 * time.Second):
					t.Error("old worker did not leave its transaction")
				}
			}()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("worker did not reach node application", ctx.Err())
			}
			// Another connection changes the lease while the original apply owns the
			// node/state locks. The original short claim owns no queue row lock here.
			if err := b.WithContext(ctx).Model(&model.NodeObservationIntent{}).Where("id = ?", id).Update("lease_until", time.Now().UTC().Add(-time.Second)).Error; err != nil {
				t.Fatal("node application still owns its queue claim lock", err)
			}
			if reclaimed {
				if done, err := (observationstore.Worker{DB: b}).Drain(ctx, 1, observationstore.MaxDrainBytes); err != nil || done != 0 {
					t.Fatal("replacement worker could not reclaim/defer busy node", done, err)
				}
			}
			close(resume)
			out := <-result
			if out.done != 0 || out.err != nil {
				t.Fatal("lost ownership was acknowledged", out)
			}
			_ = a.Callback().Update().Remove(hook)
			var current model.Node
			if err := b.First(&current, node.ID).Error; err != nil || current.ActiveFlows != 0 || current.BytesUp != 0 || current.BytesDown != 0 {
				t.Fatal("lost claim committed node counters", err)
			}
			var count int64
			if err := b.Model(&model.NodeObservationState{}).Where("node_id = ? AND asset_tag = ?", node.ID, node.AssetTag).Count(&count).Error; err != nil || count != 0 {
				t.Fatal("lost claim committed a source position", count, err)
			}
			var after []model.NodeObservationPart
			if err := b.Where("intent_id = ?", id).Order("ordinal").Find(&after).Error; err != nil || len(after) != 1 || !bytes.Equal(after[0].Data, before[0].Data) {
				t.Fatal("lost ownership discarded or changed material", err)
			}
			if err := b.Model(&model.NodeObservationIntent{}).Where("id = ?", id).Updates(map[string]any{"available_at": time.Now().UTC().Add(-time.Second), "lease_until": time.Now().UTC().Add(-time.Second)}).Error; err != nil {
				t.Fatal(err)
			}
			drainObservations(t, b)
			if state := observationState(t, b, node); !state.CurrentKnown || state.StatsReference != id {
				t.Fatal("new owner failed to recover exact source", state)
			}
		})
	}
}
