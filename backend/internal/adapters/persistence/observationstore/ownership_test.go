package observationstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/libtnb/sqlite"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
)

// This narrow store fixture exercises actual transactions/claims without
// importing datastore back into its dependency. Full owned-schema and MySQL
// contracts live in datastore/networkstore/handler tests.
func ownershipFixture(t *testing.T) (*gorm.DB, *gorm.DB, model.Node) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ownership.db")
	open := func() *gorm.DB {
		db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
		if err != nil {
			t.Fatal(err)
		}
		pool, _ := db.DB()
		pool.SetMaxOpenConns(1)
		t.Cleanup(func() { _ = pool.Close() })
		return db
	}
	a, b := open(), open()
	if err := a.Exec("CREATE TABLE nodes (id INTEGER PRIMARY KEY, asset_tag TEXT NOT NULL, is_enabled INTEGER NOT NULL, node_credential_revoked_at DATETIME NULL, active_flows INTEGER NOT NULL DEFAULT 0, bytes_up INTEGER NOT NULL DEFAULT 0, bytes_down INTEGER NOT NULL DEFAULT 0, updated_at DATETIME NULL)").Error; err != nil {
		t.Fatal(err)
	}
	if err := a.AutoMigrate(&model.NodeObservationIntent{}, &model.NodeObservationPart{}, &model.NodeObservationState{}); err != nil {
		t.Fatal(err)
	}
	raw, err := migrations.Files.ReadFile("sqlite/0061_node_observation_material_retention.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range strings.Split(string(raw), ";") {
		if strings.TrimSpace(statement) != "" {
			if err := a.Exec(statement).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	// Explicit owned0067DDL, not AutoMigrate ownership of new authority.
	raw67, err := migrations.Files.ReadFile("sqlite/0067_node_observation_retirement_budget.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range strings.Split(string(raw67), ";") {
		if strings.TrimSpace(statement) != "" {
			if err := a.Exec(statement).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := a.Create(&model.NodeObservationMaterialBudget{ID: 1, Revision: 1}).Error; err != nil {
		t.Fatal(err)
	}
	node := model.Node{ID: 8, AssetTag: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", IsEnabled: true}
	if err := a.Table("nodes").Create(map[string]any{"id": node.ID, "asset_tag": node.AssetTag, "is_enabled": true}).Error; err != nil {
		t.Fatal(err)
	}
	return a, b, node
}
func TestObservationExpiredClaimCannotAcknowledgeReplacement(t *testing.T) {
	a, b, node := ownershipFixture(t)
	at := time.Unix(1700000000, 0).UTC()
	p := network.EventPosition{AssetTag: node.AssetTag, ID: "source", CoreInstanceID: "runtime", Sequence: 1, OccurredAt: at, ReceivedAt: at}
	source := network.ObservationSource{Observation: network.NodeObservation{NodeID: node.ID, Latest: p, StatsEvent: &p, Stats: network.NodeStats{ActiveSessions: 7}}, Kind: "stats.sampled"}
	if err := a.Transaction(func(tx *gorm.DB) error { _, err := Enqueue(tx, source); return err }); err != nil {
		t.Fatal(err)
	}
	wa, wb := Worker{DB: a}, Worker{DB: b}
	old, found, err := wa.claim(context.Background())
	if err != nil || !found {
		t.Fatal("claim", err)
	}
	// Committed short claim leaves no queue lock; a second independent pool can
	// update/reclaim while the old host has not entered node application.
	if err := b.Model(&model.NodeObservationIntent{}).Where("id = ?", old.ID).Update("lease_until", clockNow().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	replacement, found, err := wb.claim(context.Background())
	if err != nil || !found || replacement.Token == old.Token {
		t.Fatal("expired claim not replaced", err)
	}
	budget := MaxDrainBytes
	if err := wa.apply(context.Background(), old, &budget); !errors.Is(err, ErrOwnership) {
		t.Fatal("stale token acknowledged replacement", err)
	}
	var current model.Node
	if err := b.First(&current, node.ID).Error; err != nil || current.ActiveFlows != 0 {
		t.Fatal("lost token committed counters", err)
	}
	var n int64
	if err := b.Model(&model.NodeObservationState{}).Count(&n).Error; err != nil || n != 0 {
		t.Fatal("lost token committed position", err)
	}
	budget = MaxDrainBytes
	if err := wb.apply(context.Background(), replacement, &budget); err != nil {
		t.Fatal(err)
	}
	if err := a.First(&current, node.ID).Error; err != nil || current.ActiveFlows != 7 {
		t.Fatal("owner failed to apply", err)
	}
	var header model.NodeObservationIntent
	if err := a.First(&header, "id = ?", old.ID).Error; err != nil || header.Status != "applied" {
		t.Fatal("exact owned ACK absent", err)
	}
}

func TestObservationEmptyTagStateKeepsFloorDuringOverrideAndDeletion(t *testing.T) {
	a, b, node := ownershipFixture(t)
	floor := model.NodeObservationState{NodeID: node.ID, AssetTag: "", FloorReference: strings.Repeat("f", 64), CurrentSource: "unattributed_floor", UpdatedAt: clockNow()}
	if err := a.Create(&floor).Error; err != nil {
		t.Fatal(err)
	}
	at := time.Unix(1700000000, 123456789).UTC()
	if err := a.Transaction(func(tx *gorm.DB) error { return CounterOverride(tx, node.ID, "", "heartbeat", at) }); err != nil {
		t.Fatal("empty-tag existing state was inserted instead of updated", err)
	}
	var after model.NodeObservationState
	if err := b.First(&after, "node_id = ? AND asset_tag = ?", node.ID, "").Error; err != nil || after.FloorReference != floor.FloorReference || after.CurrentKnown || after.CurrentSource != "heartbeat" || after.CounterWrittenStamp != at.Format(time.RFC3339Nano) {
		t.Fatal("override lost exact floor or receiver time", after, err)
	}
	if err := a.Transaction(func(tx *gorm.DB) error { return Tombstone(tx, node.ID, "") }); err != nil {
		t.Fatal("empty-tag deletion failed", err)
	}
	if err := b.First(&after, "node_id = ? AND asset_tag = ?", node.ID, "").Error; err != nil || !after.Deleted || after.CurrentSource != "deleted" || after.FloorReference != floor.FloorReference {
		t.Fatal("deletion lost unattributed floor", after, err)
	}
	var count int64
	if err := b.Model(&model.NodeObservationState{}).Where("node_id = ?", node.ID).Count(&count).Error; err != nil || count != 1 {
		t.Fatal("empty-tag state duplicated", count, err)
	}
}

func TestObservationCachedTimesKeepExactReceiverFenceAndMaterial(t *testing.T) {
	a, b, node := ownershipFixture(t)
	occurred := time.Unix(1700000000, 123456789).UTC()
	received := occurred.Add(time.Second + 33*time.Nanosecond)
	p := network.EventPosition{AssetTag: node.AssetTag, ID: "ns-source", CoreInstanceID: "runtime", Sequence: 1, OccurredAt: occurred, ReceivedAt: received}
	source := network.ObservationSource{Observation: network.NodeObservation{NodeID: node.ID, Latest: p, StatsEvent: &p, Stats: network.NodeStats{ActiveSessions: 4}}, Kind: "stats.sampled"}
	var id string
	if err := a.Transaction(func(tx *gorm.DB) error { var err error; id, err = Enqueue(tx, source); return err }); err != nil {
		t.Fatal(err)
	}
	if _, err := (Worker{DB: b}).Drain(context.Background(), 32, MaxDrainBytes); err != nil {
		t.Fatal(err)
	}
	var state model.NodeObservationState
	if err := b.First(&state, "node_id = ? AND asset_tag = ?", node.ID, node.AssetTag).Error; err != nil {
		t.Fatal(err)
	}
	if state.CounterWrittenStamp != received.Format(time.RFC3339Nano) {
		t.Fatal("receiver fence lost nanoseconds", state.CounterWrittenStamp)
	}
	for _, pair := range []struct {
		got  *time.Time
		want time.Time
	}{
		{state.LatestOccurredAt, occurred}, {state.StatsOccurredAt, occurred},
		{state.LatestReceivedAt, received}, {state.StatsReceivedAt, received}, {state.CounterWrittenAt, received},
	} {
		if pair.got == nil || !pair.got.Equal(pair.want.Truncate(time.Millisecond)) {
			t.Fatal("cached source time was replaced instead of normalized", pair)
		}
	}
	if state.UpdatedAt.Nanosecond()%int(time.Millisecond) != 0 {
		t.Fatal("implicit ORM time broke portable state precision", state.UpdatedAt)
	}
	retained, err := readReference(b, id)
	if err != nil || !retained.Observation.Latest.OccurredAt.Equal(occurred) || !retained.Observation.Latest.ReceivedAt.Equal(received) || retained.Observation.StatsEvent == nil || !retained.Observation.StatsEvent.ReceivedAt.Equal(received) {
		t.Fatal("retained source lost original nanoseconds", err)
	}
}
func TestObservationClaimExpirationRollsBackNodeAndPosition(t *testing.T) {
	a, _, node := ownershipFixture(t)
	at := time.Unix(1700000000, 0).UTC()
	p := network.EventPosition{AssetTag: node.AssetTag, ID: "expiring", CoreInstanceID: "runtime", Sequence: 1, OccurredAt: at, ReceivedAt: at}
	if err := a.Transaction(func(tx *gorm.DB) error {
		_, err := Enqueue(tx, network.ObservationSource{Observation: network.NodeObservation{NodeID: node.ID, Latest: p, StatsEvent: &p, Stats: network.NodeStats{ActiveSessions: 9}}, Kind: "stats.sampled"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	w := Worker{DB: a}
	h, found, err := w.claim(context.Background())
	if err != nil || !found {
		t.Fatal(err)
	}
	if err := a.Model(&model.NodeObservationIntent{}).Where("id = ?", h.ID).Update("lease_until", clockNow().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	budget := MaxDrainBytes
	if err := w.apply(context.Background(), h, &budget); !errors.Is(err, ErrOwnership) {
		t.Fatal("expired lease committed", err)
	}
	var current model.Node
	if err := a.First(&current, node.ID).Error; err != nil || current.ActiveFlows != 0 {
		t.Fatal("expired lease wrote counters", err)
	}
}

func TestObservationForgedSmallHeaderDoesNotFetchLargePart(t *testing.T) {
	a, _, node := ownershipFixture(t)
	at := time.Unix(1700000000, 0).UTC()
	position := network.EventPosition{AssetTag: node.AssetTag, ID: "forged-small", CoreInstanceID: "runtime", Sequence: 1, OccurredAt: at, ReceivedAt: at}
	source := network.ObservationSource{Observation: network.NodeObservation{NodeID: node.ID, Latest: position}, Kind: "flow.updated", Payload: bytes.Repeat([]byte("x"), PartBytes-1000)}
	var id string
	if err := a.Transaction(func(tx *gorm.DB) error { var err error; id, err = Enqueue(tx, source); return err }); err != nil {
		t.Fatal(err)
	}
	var header model.NodeObservationIntent
	if err := a.First(&header, "id = ?", id).Error; err != nil || header.PartCount != 1 || header.MaterialBytes < PartBytes-1000 {
		t.Fatal("fixture is not one large material part", header, err)
	}
	if err := a.Model(&model.NodeObservationIntent{}).Where("id = ?", id).Update("material_bytes", uint64(1)).Error; err != nil {
		t.Fatal(err)
	}
	blobSelects := 0
	const hook = "test:observation_prefetch_bound"
	if err := a.Callback().Query().Before("gorm:query").Register(hook, func(tx *gorm.DB) {
		selectSQL := strings.Join(tx.Statement.Selects, " ") + fmt.Sprint(tx.Statement.Clauses["SELECT"].Expression)
		if strings.Contains(selectSQL, "substr(data") {
			blobSelects++
		}
	}); err != nil {
		t.Fatal(err)
	}
	done, err := (Worker{DB: a}).Drain(context.Background(), 200, MaxDrainBytes)
	_ = a.Callback().Query().Remove(hook)
	if err != nil || done != 1 {
		t.Fatal("corrupt source was not retained held", done, err)
	}
	if blobSelects != 0 {
		t.Fatal("forged tiny header caused an uncharged blob SELECT", blobSelects)
	}
	if err := a.First(&header, "id = ?", id).Error; err != nil || header.Status != "held" || header.Reason != "invalid_material" {
		t.Fatal("bad source disposition missing", header, err)
	}
	var parts int64
	if err := a.Model(&model.NodeObservationPart{}).Where("intent_id = ?", id).Count(&parts).Error; err != nil || parts != 1 {
		t.Fatal("bad material discarded", err)
	}
}
