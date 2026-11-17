package handler

import (
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestSQLiteCompletedFlowStorageKeepsHistoricalAccountingAuthority(t *testing.T) {
	h, c := accountingBenchmarkFixture(t)
	completedFlowStorageAcceptance(t, h, c)
}

func TestMySQLCompletedFlowStorageKeepsHistoricalAccountingAuthority(t *testing.T) {
	h, c := accountingFixtureFromOrder(t, newMySQLBusinessOrderFixture(t))
	completedFlowStorageAcceptance(t, h, c)
}

func completedFlowStorageAcceptance(t *testing.T, h *handlers, c model.ProtocolCredential) {
	t.Helper()
	if err := h.db.Model(&model.ProtocolEndpoint{}).Where("id = ?", c.ProtocolEndpointID).Update("multiplier_milli", 1001).Error; err != nil {
		t.Fatal(err)
	}
	store := h.services.CompletionAccounting(h.credentialCipher)
	in := metering.CompletedFlow{NodeID: c.NodeID, PrincipalKey: c.PrincipalKey, FlowID: "Original", EventID: "storage-original", EventType: "flow.completed", Revision: 3, BytesUp: 100, OccurredAt: time.Now().UTC().AddDate(-20, 0, 0)}
	if _, _, err := store.Complete(t.Context(), in); err != nil {
		t.Fatal(err)
	}
	// Exercise MySQL's DATETIME(3) rounding boundary deterministically. The
	// first compaction must run even when the source clock rounds upward.
	compactionNow := time.Now().UTC().Truncate(time.Second).Add(750 * time.Microsecond)
	old := compactionNow.Add(-48 * time.Hour)
	if err := h.db.Model(&model.FlowUsage{}).Where("node_id = ? AND flow_id = ?", c.NodeID, in.FlowID).Update("completed_at", old).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := (meteringstore.FlowCursorCompaction{DB: h.db, Now: func() time.Time { return compactionNow }}).Drain(t.Context(), 200); err != nil {
		t.Fatal(err)
	}
	assertQuota := func(want int64) {
		t.Helper()
		var sub model.Subscription
		if err := h.db.First(&sub, c.SubscriptionID).Error; err != nil || sub.FlowUsed != want {
			t.Fatal("historical quota differs", sub.FlowUsed, want, err)
		}
	}
	assertQuota(101)
	var cold int64
	if err := h.db.Model(&model.FlowCursorCheckpoint{}).Count(&cold).Error; err != nil || cold != 1 {
		t.Fatal("completed authority not transferred", cold, err)
	}
	if _, _, err := store.Complete(t.Context(), in); err != nil {
		t.Fatal("twenty-year-old receipt replay failed", err)
	}
	assertQuota(101)
	if !datastore.IsSQLite(h.db) {
		alias := in
		alias.EventID = "storage-alias"
		alias.FlowID = "ORIGINAL"
		alias.Revision = 4
		alias.BytesUp = 130
		if _, _, err := store.Complete(t.Context(), alias); err == nil {
			t.Fatal("collation alias created a second accounting identity")
		}
		assertQuota(101)
	}
	in.EventID = "storage-late"
	in.Revision = 4
	in.BytesUp = 130
	if _, _, err := store.Complete(t.Context(), in); err != nil {
		t.Fatal("historical completion delta rejected", err)
	}
	assertQuota(132)
	if _, _, err := store.Complete(t.Context(), in); err != nil {
		t.Fatal(err)
	}
	assertQuota(132)
	if err := datastore.RunMigrations(h.db); err != nil {
		t.Fatal("restart lost compact authority preparation", err)
	}
}
