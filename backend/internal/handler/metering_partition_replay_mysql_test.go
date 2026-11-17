package handler

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/application"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestMySQLQuotaPartitionFailureReplaysCommittedPrefixExactlyOnce(t *testing.T) {
	h, c := accountingFixtureFromOrder(t, newMySQLBusinessOrderFixture(t))
	var sub model.Subscription
	if e := h.db.First(&sub, c.SubscriptionID).Error; e != nil {
		t.Fatal(e)
	}
	sub.ID = 0
	sub.FlowUsed = 0
	if e := h.db.Create(&sub).Error; e != nil {
		t.Fatal(e)
	}
	other := c
	other.ID = 0
	other.SubscriptionID = sub.ID
	other.CredentialID = "partition-other"
	other.PrincipalKey = "partition-other"
	other.PrincipalLookupVersion = 0
	other.PrincipalDigest = ""
	other.SecretPrincipalDigest = ""
	if e := h.db.Create(&other).Error; e != nil {
		t.Fatal(e)
	}
	events := accountingBenchmarkEvents(c, 0, 3)
	events[1].PrincipalKey = other.PrincipalKey
	for i := range events {
		events[i].OccurredAt = time.Now().UTC().AddDate(-3, 0, 0)
	}
	spool := &checkpointTestSpool{events: events}
	injected := errors.New("second quota write unavailable")
	var failed atomic.Bool
	const hook = "test:partition-prefix"
	if e := h.db.Callback().Create().Before("gorm:create").Register(hook, func(q *gorm.DB) {
		if r, ok := q.Statement.Dest.(*model.TrafficRecord); ok && r.SubscriptionID == sub.ID && failed.CompareAndSwap(false, true) {
			q.AddError(injected)
		}
	}); e != nil {
		t.Fatal(e)
	}
	defer h.db.Callback().Create().Remove(hook)
	if count, e := h.consumeZeroEventBatch(t.Context(), spool, 32); !errors.Is(e, injected) || count != 0 || spool.position != 0 {
		t.Fatal("partial failure acknowledged durable source", count, spool.position, e)
	}
	assertAccountingTotal(t, h, c.SubscriptionID, 30, subStatusActive)
	assertAccountingTotal(t, h, sub.ID, 0, subStatusActive)
	// Replay the whole durable source after the partial failure, then simulate
	// a lost checkpoint acknowledgement. Every accepted report charges once.
	for range 2 {
		spool.position = 0
		if count, e := h.consumeZeroEventBatch(t.Context(), spool, 32); e != nil || count != 3 || spool.position != 3 {
			t.Fatal(count, spool.position, e)
		}
	}
	assertAccountingTotal(t, h, c.SubscriptionID, 60, subStatusActive)
	assertAccountingTotal(t, h, sub.ID, 30, subStatusActive)
	var receipts int64
	if e := h.db.Model(&model.MeteringReportReceipt{}).Count(&receipts).Error; e != nil || receipts != 3 {
		t.Fatal(receipts, e)
	}
}

func TestMySQLQuotaPartitionKeepsDatabaseEquivalentReportOrder(t *testing.T) {
	h, c := accountingFixtureFromOrder(t, newMySQLBusinessOrderFixture(t))
	var sub model.Subscription
	h.db.First(&sub, c.SubscriptionID)
	sub.ID = 0
	sub.FlowUsed = 0
	if e := h.db.Create(&sub).Error; e != nil {
		t.Fatal(e)
	}
	other := c
	other.ID = 0
	other.SubscriptionID = sub.ID
	other.CredentialID = "equiv-other"
	other.PrincipalKey = "equiv-other"
	other.PrincipalLookupVersion = 0
	other.PrincipalDigest = ""
	other.SecretPrincipalDigest = ""
	if e := h.db.Create(&other).Error; e != nil {
		t.Fatal(e)
	}
	samples := []metering.FlowSample{}
	for i, credential := range []model.ProtocolCredential{c, other, c} {
		samples = append(samples, metering.FlowSample{NodeID: c.NodeID, PrincipalKey: credential.PrincipalKey, CoreInstanceID: "equiv-partition", FlowID: string(rune('a' + i)), EventID: []string{"first", "same-report", "SAME-REPORT"}[i], EventType: "flow.updated", BytesUp: 5, Sequence: uint64(i + 1), OccurredAt: time.Now().UTC()})
	}
	if _, e := h.services.ProjectBufferedEvents(context.Background(), application.BufferedEventProjection{Flows: samples}, h.credentialCipher); e != nil {
		t.Fatal(e)
	}
	assertAccountingTotal(t, h, c.SubscriptionID, 5, subStatusActive)
	assertAccountingTotal(t, h, sub.ID, 5, subStatusActive)
}
