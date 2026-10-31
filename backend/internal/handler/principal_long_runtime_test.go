package handler

import (
	"database/sql"
	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	gormmysql "gorm.io/driver/mysql"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestSQLitePrincipalLongRuntimeGenerationRevisionAndReplay(t *testing.T) {
	h, c := accountingBenchmarkFixture(t)
	assertPrincipalLongRuntime(t, h, c)
}
func TestMySQLPrincipalLongRuntimeGenerationRevisionAndReplay(t *testing.T) {
	h, c := accountingFixtureFromOrder(t, newMySQLBusinessOrderFixture(t))
	assertPrincipalLongRuntime(t, h, c)
}
func assertPrincipalLongRuntime(t *testing.T, h *handlers, c model.ProtocolCredential) {
	t.Helper()
	at := time.Now().UTC().Truncate(time.Millisecond)
	a := strings.Repeat("runtime-", 35) + "A"
	b := strings.Repeat("runtime-", 35) + "B"
	event := metering.PrincipalEvent{CoreInstanceID: a, EventID: "long-first", Sequence: 1, ObservedAt: at}
	observation := metering.PrincipalObservation{PrincipalKey: c.PrincipalKey, ActiveFlows: 5, SessionRegistryRevision: 5, ObservedAt: at}
	observe := func() {
		t.Helper()
		if err := h.services.PrincipalCollection.Observe(t.Context(), c.NodeID, event, observation); err != nil {
			t.Fatal(err)
		}
	}
	scope := func(want uint64) {
		t.Helper()
		var row meteringstore.PrincipalFlowScopeCurrent
		if err := h.db.First(&row, "scope_type = ? AND scope_id = ?", "subscription", c.SubscriptionID).Error; err != nil || row.ActiveFlows != want {
			t.Fatal("scope changed unexpectedly", row, want, err)
		}
	}
	observe()
	scope(5)
	var initial meteringstore.PrincipalFlowNodeGeneration
	if err := h.db.First(&initial, "node_id = ?", c.NodeID).Error; err != nil || initial.CoreInstanceID != a {
		t.Fatal("long generation original lost", initial, err)
	}
	observe()
	scope(5)
	var afterReplay meteringstore.PrincipalFlowNodeGeneration
	if err := h.db.First(&afterReplay, "node_id = ?", c.NodeID).Error; err != nil || !reflect.DeepEqual(initial, afterReplay) {
		t.Fatal("same runtime replay reset its start/window", err)
	}
	event.EventID = "older-revision"
	observation.SessionRegistryRevision = 4
	observation.ActiveFlows = 99
	observe()
	scope(5)
	event.EventID = "long-stop"
	event.ObservedAt = at.Add(time.Second)
	if err := h.services.PrincipalCollection.Boundary(t.Context(), c.NodeID, event, true); err != nil {
		t.Fatal(err)
	}
	scope(0)
	event.EventID = "late-stopped"
	observation.SessionRegistryRevision = 6
	observe()
	scope(0)
	event = metering.PrincipalEvent{CoreInstanceID: b, EventID: "long-restart", Sequence: 1, ObservedAt: at.Add(2 * time.Second)}
	if err := h.services.PrincipalCollection.Boundary(t.Context(), c.NodeID, event, false); err != nil {
		t.Fatal(err)
	}
	event.EventID = "second-runtime-flow"
	observation.ObservedAt = event.ObservedAt
	observation.SessionRegistryRevision = 1
	observation.ActiveFlows = 2
	observe()
	scope(2)
	event.CoreInstanceID = a
	event.EventID = "late-old-instance"
	observation.ObservedAt = at
	observation.SessionRegistryRevision = 10
	observation.ActiveFlows = 100
	observe()
	scope(2)
	// A legitimate short literal resembling a digest is still an independent
	// runtime, never the encoded alias of the original long raw identity.
	literal := "sha256:" + metering.RuntimeIdentityDigest(a)
	event = metering.PrincipalEvent{CoreInstanceID: literal, EventID: "literal-restart", Sequence: 1, ObservedAt: at.Add(3 * time.Second)}
	if err := h.services.PrincipalCollection.Boundary(t.Context(), c.NodeID, event, false); err != nil {
		t.Fatal(err)
	}
	event.EventID = "literal-flow"
	observation.ObservedAt = event.ObservedAt
	observation.SessionRegistryRevision = 1
	observation.ActiveFlows = 7
	observe()
	scope(7)
	var rows []meteringstore.PrincipalFlowObservation
	if err := h.db.Where("node_id = ?", c.NodeID).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.CoreInstanceSHA256 != metering.RuntimeIdentityDigest(row.CoreInstanceID) {
			t.Fatal("digest not bound to preserved raw original", row.ID)
		}
	}
	var current meteringstore.PrincipalFlowCurrent
	if err := h.db.First(&current, "node_id = ? AND principal_key = ?", c.NodeID, c.PrincipalKey).Error; err != nil || current.CoreInstanceID != literal {
		t.Fatal("current identity was substituted by hash", err)
	}
	// Raw diagnostic identities are no longer copied per event. The current
	// full runtime and revision fence still ignore stale snapshot content.
	if len(rows) != 0 {
		t.Fatal("snapshot ingress recreated event history", len(rows))
	}
	event = metering.PrincipalEvent{CoreInstanceID: literal, EventID: "conflicting-snapshot", Sequence: 9, ObservedAt: at.Add(4 * time.Second)}
	observation.ObservedAt = event.ObservedAt
	observation.SessionRegistryRevision = 1
	observation.ActiveFlows = 99
	if err := h.services.PrincipalCollection.Observe(t.Context(), c.NodeID, event, observation); err != nil {
		t.Fatal("stale diagnostic snapshot blocked ingestion", err)
	}
	scope(7)
	var retained meteringstore.PrincipalFlowCurrent
	if err := h.db.First(&retained, "node_id = ? AND principal_key = ?", c.NodeID, c.PrincipalKey).Error; err != nil || !reflect.DeepEqual(current, retained) {
		t.Fatal("rejected collision changed current activity", err)
	}
}

func TestSQLitePrincipalLongRuntimeReadBudgetPrecedesFullFetch(t *testing.T) {
	h, c := accountingBenchmarkFixture(t)
	assertPrincipalRuntimeReadBudget(t, h, c)
}
func TestMySQLPrincipalLongRuntimeReadBudgetPrecedesFullFetch(t *testing.T) {
	h, c := accountingFixtureFromOrder(t, newMySQLBusinessOrderFixture(t))
	assertPrincipalRuntimeReadBudget(t, h, c)
}
func assertPrincipalRuntimeReadBudget(t *testing.T, h *handlers, c model.ProtocolCredential) {
	t.Helper()
	at := time.Now().UTC().Truncate(time.Millisecond)
	// Owned stored-corruption fixture only; this cannot arrive through the
	// authenticated wire body and does not enlarge its accepted contract.
	row := meteringstore.PrincipalFlowNodeGeneration{NodeID: c.NodeID, CoreInstanceID: strings.Repeat("x", metering.MaxRuntimeIdentityBytes+1), StartedAt: at, UpdatedAt: at}
	if err := h.db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	fullReads := 0
	const callback = "test:principal_runtime_read_budget"
	if err := h.db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "principal_flow_node_generations" && len(tx.Statement.Selects) == 0 {
			fullReads++
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer h.db.Callback().Query().Remove(callback)
	event := metering.PrincipalEvent{CoreInstanceID: "new-valid-runtime", EventID: "after-corruption", Sequence: 1, ObservedAt: at.Add(time.Second)}
	observation := metering.PrincipalObservation{PrincipalKey: c.PrincipalKey, ActiveFlows: 3, SessionRegistryRevision: 1, ObservedAt: event.ObservedAt}
	if err := h.services.PrincipalCollection.Observe(t.Context(), c.NodeID, event, observation); err == nil || fullReads != 0 {
		t.Fatal("oversized identity fetched before metadata refusal", fullReads, err)
	}
	var n int64
	if err := h.db.Model(&meteringstore.PrincipalFlowObservation{}).Where("node_id = ?", c.NodeID).Count(&n).Error; err != nil || n != 0 {
		t.Fatal("rejected source acquired a false activity sample", n, err)
	}
}

func principalClientFoundRowsDB(t *testing.T, h *handlers) *gorm.DB {
	t.Helper()
	dialect, ok := h.db.Dialector.(*gormmysql.Dialector)
	if !ok {
		t.Fatal("real MySQL dialector required")
	}
	cfg, err := mysqldriver.ParseDSN(dialect.Config.DSN)
	if err != nil {
		t.Fatal("isolated MySQL DSN parse failed")
	}
	cfg.ClientFoundRows = true
	db, err := datastore.Open(cfg.FormatDSN())
	if err != nil {
		t.Fatal("open isolated clientFoundRows connection")
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	return db
}
func TestMySQLPrincipalRuntimeClientFoundRowsStillRejectsCorruptDuplicate(t *testing.T) {
	h, c := accountingFixtureFromOrder(t, newMySQLBusinessOrderFixture(t))
	db := principalClientFoundRowsDB(t, h)
	h.services.PrincipalCollection = metering.PrincipalCollection{Repository: meteringstore.PrincipalCollection{DB: db}}
	assertPrincipalLongRuntime(t, h, c)
}
func TestMySQLPrincipalRuntimeDuplicateUsesCurrentReadAfterOldRRSnapshot(t *testing.T) {
	h, c := accountingFixtureFromOrder(t, newMySQLBusinessOrderFixture(t))
	db := principalClientFoundRowsDB(t, h)
	old := db.Begin(&sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if old.Error != nil {
		t.Fatal(old.Error)
	}
	defer old.Rollback()
	var before int64
	if err := old.Model(&meteringstore.PrincipalFlowObservation{}).Where("node_id = ?", c.NodeID).Count(&before).Error; err != nil || before != 0 {
		t.Fatal("failed to establish old snapshot", before, err)
	}
	at := time.Now().UTC().Truncate(time.Millisecond)
	event := metering.PrincipalEvent{CoreInstanceID: strings.Repeat("old-snapshot-runtime-", 15), EventID: "inserted-after-old-snapshot", Sequence: 1, ObservedAt: at}
	observation := metering.PrincipalObservation{PrincipalKey: c.PrincipalKey, ActiveFlows: 5, SessionRegistryRevision: 1, ObservedAt: at}
	if err := h.services.PrincipalCollection.Observe(t.Context(), c.NodeID, event, observation); err != nil {
		t.Fatal("second connection did not commit", err)
	}
	var original meteringstore.PrincipalFlowCurrent
	if err := h.db.First(&original, "node_id = ? AND principal_key = ?", c.NodeID, c.PrincipalKey).Error; err != nil {
		t.Fatal(err)
	}
	// This old consistent snapshot still sees zero immutable rows. Locking reads
	// inside duplicate resolution must nevertheless see the real committed fact.
	var stale int64
	if err := old.Model(&meteringstore.PrincipalFlowObservation{}).Where("node_id = ?", c.NodeID).Count(&stale).Error; err != nil || stale != 0 {
		t.Fatal("fixture did not retain a repeatable-read snapshot", stale, err)
	}
	replay := metering.PrincipalCollection{Repository: meteringstore.PrincipalCollection{DB: old}}
	if err := replay.Observe(t.Context(), c.NodeID, event, observation); err != nil {
		t.Fatal("reasonable duplicate replay resolved against stale snapshot", err)
	}
	if err := old.Commit().Error; err != nil {
		t.Fatal(err)
	}
	var retained meteringstore.PrincipalFlowCurrent
	if err := h.db.First(&retained, "node_id = ? AND principal_key = ?", c.NodeID, c.PrincipalKey).Error; err != nil || !reflect.DeepEqual(original, retained) {
		t.Fatal("duplicate replay rewrote current observation", err)
	}
	var count int64
	if err := h.db.Model(&meteringstore.PrincipalFlowObservation{}).Where("node_id = ?", c.NodeID).Count(&count).Error; err != nil || count != 1 {
		t.Fatal("duplicate acquired a second observation", count, err)
	}
}
