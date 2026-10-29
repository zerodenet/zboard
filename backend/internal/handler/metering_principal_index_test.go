package handler

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/credentialstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestMeteringPrincipalIndexResumesHistoryAndRejectsAmbiguousOwnership(t *testing.T) {
	assertMeteringPrincipalIndex(t, newOrderFixture(t))
}
func TestMySQLMeteringPrincipalIndexResumesHistoryAndRejectsAmbiguousOwnership(t *testing.T) {
	assertMeteringPrincipalIndex(t, newMySQLBusinessOrderFixture(t))
}
func assertMeteringPrincipalIndex(t *testing.T, f orderFixture) {
	h, target := accountingFixtureFromOrder(t, f)
	plain, err := h.credentialCipher.Decrypt(target.Secret)
	if err != nil {
		t.Fatal(err)
	}
	var source model.Subscription
	if err := h.db.First(&source, target.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	// Imported legacy ownership fixtures deliberately carry no fabricated paid
	// order snapshots. One real purchased subscription remains the charge target.
	const history = 5000
	subs := make([]model.Subscription, history)
	rows := make([]model.ProtocolCredential, history)
	for i := range subs {
		subs[i] = source
		subs[i].ID = 0
		subs[i].Status = "ended"
	}
	if err := h.db.CreateInBatches(&subs, 256).Error; err != nil {
		t.Fatal(err)
	}
	for i := range rows {
		rows[i] = model.ProtocolCredential{SubscriptionID: subs[i].ID, UserID: source.UserID, NodeID: target.NodeID, ProtocolEndpointID: target.ProtocolEndpointID, CredentialID: fmt.Sprintf("imported-%d", i), PrincipalKey: fmt.Sprintf("imported-owner-%d", i), Status: "revoked", ExpiresAt: source.EndAt, RetiredPrincipalHash: entitlements.RetiredPrincipalHash(target.NodeID, fmt.Sprintf("imported-secret-%d", i))}
	}
	if err := h.db.CreateInBatches(&rows, 256).Error; err != nil {
		t.Fatal(err)
	}
	const callback = "test:metering-principal-secrecy"
	audit := func(tx *gorm.DB) {
		for _, arg := range tx.Statement.Vars {
			if value, ok := arg.(string); ok && strings.Contains(value, plain) {
				t.Error("wire principal entered SQL parameters")
			}
		}
	}
	for _, processor := range []interface {
		Register(string, func(*gorm.DB)) error
	}{h.db.Callback().Query().After("gorm:query"), h.db.Callback().Row().After("gorm:row"), h.db.Callback().Create().After("gorm:create"), h.db.Callback().Update().After("gorm:update"), h.db.Callback().Raw().After("gorm:raw")} {
		if err := processor.Register(callback, audit); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		h.db.Callback().Query().Remove(callback)
		h.db.Callback().Row().Remove(callback)
		h.db.Callback().Create().Remove(callback)
		h.db.Callback().Update().Remove(callback)
		h.db.Callback().Raw().Remove(callback)
	})
	events := accountingBenchmarkEvents(target, 0, 1)
	events[0].PrincipalKey = plain
	var previous int64 = 1 // The actual issuer already indexed the purchased row.
	settled := false
	for attempt := 0; attempt < 8; attempt++ {
		err := h.projectZeroNodeEvents(t.Context(), events)
		var indexed int64
		if err := h.db.Model(&model.ProtocolCredential{}).Where("principal_lookup_version = ?", 1).Count(&indexed).Error; err != nil {
			t.Fatal(err)
		}
		if indexed <= previous || indexed-previous > 1024 {
			t.Fatalf("legacy backfill failed its independent bound: %d -> %d", previous, indexed)
		}
		previous = indexed
		if err == nil {
			settled = true
			break
		}
		if !errors.Is(err, credentialstore.ErrPrincipalIndexPending) {
			t.Fatal(err)
		}
		assertAccountingTotal(t, h, target.SubscriptionID, 0, subStatusActive)
	}
	if !settled || previous != history+1 {
		t.Fatalf("large retained node never settled: %v %d", settled, previous)
	}
	assertAccountingTotal(t, h, target.SubscriptionID, 30, subStatusActive)
	// Replay and the synchronous completion route share the same indexed owner.
	if err := h.projectZeroNodeEvents(t.Context(), events); err != nil {
		t.Fatal(err)
	}
	completion := metering.CompletedFlow{NodeID: target.NodeID, CoreInstanceID: "benchmark-core", SourceID: "native", EventID: "indexed-completion", EventType: "flow.completed", Sequence: 2, FlowID: "flow-0", PrincipalKey: plain, Revision: 0, BytesUp: 20, BytesDown: 40, OccurredAt: time.Now().UTC()}
	for range 2 {
		if _, _, err := h.services.CompletionAccounting(h.credentialCipher).Complete(t.Context(), completion); err != nil {
			t.Fatal(err)
		}
	}
	assertAccountingTotal(t, h, target.SubscriptionID, 60, subStatusActive)
	// A canonical secret mutation must invalidate coverage automatically. A
	// shared wire identity then rejects ownership rather than selecting a row.
	shared, err := h.credentialCipher.Encrypt(plain)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.db.Model(&rows[0]).Update("secret", shared).Error; err != nil {
		t.Fatal(err)
	}
	var changed model.ProtocolCredential
	if err := h.db.First(&changed, rows[0].ID).Error; err != nil || changed.PrincipalLookupVersion != 0 {
		t.Fatalf("canonical change left a trusted digest: %d %v", changed.PrincipalLookupVersion, err)
	}
	late := accountingBenchmarkEvents(target, 1, 1)
	late[0].PrincipalKey = plain
	if err := h.projectZeroNodeEvents(t.Context(), late); !errors.Is(err, metering.ErrPrincipalAmbiguous) {
		t.Fatalf("ambiguous wire identity settled: %v", err)
	}
	assertAccountingTotal(t, h, target.SubscriptionID, 60, subStatusActive)
	t.Logf("retained_credentials=%d independently_committed_backfill_limit=1024 target_charged=60 ambiguous_extra_charge=0", history+1)
}

func TestLegacyTrafficReportCannotMixIndependentSubscriptions(t *testing.T) {
	assertLegacyTrafficOwnership(t, newOrderFixture(t))
}
func TestMySQLLegacyTrafficReportCannotMixIndependentSubscriptions(t *testing.T) {
	assertLegacyTrafficOwnership(t, newMySQLBusinessOrderFixture(t))
}
func assertLegacyTrafficOwnership(t *testing.T, f orderFixture) {
	h, first := accountingFixtureFromOrder(t, f)
	if err := h.db.Model(&model.Node{}).Where("id = ?", first.NodeID).Update("traffic_secret", "legacy-fixture-key").Error; err != nil {
		t.Fatal(err)
	}
	report := metering.AuthenticatedNodeReport{NodeID: first.NodeID, UserID: first.UserID, ProtocolEndpointID: first.ProtocolEndpointID, ExpectedCredential: "legacy-fixture-key", Timestamp: time.Now().UTC(), Nonce: "legacy-unique-nonce", ReportID: "legacy-unique-report", DownloadBytes: 10}
	accepted, err := h.services.NodeReports().Record(t.Context(), report)
	if err != nil || accepted.Record.UsedBytes != 10 {
		t.Fatal("unambiguous legacy report failed", err)
	}
	second := f.paid(t, f.create(t, 0).ID)
	replay, err := h.services.NodeReports().Record(t.Context(), report)
	if err != nil || !replay.Duplicate || replay.Record.ID != accepted.Record.ID {
		t.Fatal("known legacy receipt lost idempotency", err)
	}
	report.Nonce = "legacy-ambiguous-nonce"
	report.ReportID = "legacy-ambiguous-report"
	if _, err := h.services.NodeReports().Record(t.Context(), report); !errors.Is(err, metering.ErrNodeReportNeedsPrincipal) {
		t.Fatal("legacy report chose an arbitrary independent subscription", err)
	}
	assertAccountingTotal(t, h, first.SubscriptionID, 10, subStatusActive)
	assertAccountingTotal(t, h, second.SubscriptionID, 0, subStatusActive)
}
