package handler

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/application"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm/clause"
)

func TestMySQLQuotaPartitionsReleaseUnrelatedSubscriptionBeforeWaiting(t *testing.T) {
	h, c1 := accountingFixtureFromOrder(t, newMySQLBusinessOrderFixture(t))
	if e := h.db.Model(&model.ProtocolEndpoint{}).Where("id=?", c1.ProtocolEndpointID).Update("multiplier_milli", 1001).Error; e != nil {
		t.Fatal(e)
	}
	var s2 model.Subscription
	if e := h.db.First(&s2, c1.SubscriptionID).Error; e != nil {
		t.Fatal(e)
	}
	s2.ID = 0
	s2.FlowUsed = 0
	if e := h.db.Create(&s2).Error; e != nil {
		t.Fatal(e)
	}
	n := model.Node{Name: "second-report-node", AssetTag: strings.Repeat("b", 64), Config: "{}", IsEnabled: true}
	if e := h.db.Create(&n).Error; e != nil {
		t.Fatal(e)
	}
	var ep model.ProtocolEndpoint
	if e := h.db.First(&ep, c1.ProtocolEndpointID).Error; e != nil {
		t.Fatal(e)
	}
	ep.ID = 0
	ep.NodeID = n.ID
	ep.Name = "second-endpoint"
	ep.RuntimeKey = "cross-sub-second-endpoint"
	ep.MultiplierMilli = 1500
	if e := h.db.Create(&ep).Error; e != nil {
		t.Fatal(e)
	}
	clone := func(name string, node, endpoint, sub uint) model.ProtocolCredential {
		c := c1
		c.ID = 0
		c.CredentialID = name
		c.PrincipalKey = name
		c.NodeID = node
		c.ProtocolEndpointID = endpoint
		c.SubscriptionID = sub
		c.PrincipalLookupVersion = 0
		c.PrincipalDigest = ""
		c.SecretPrincipalDigest = ""
		if e := h.db.Create(&c).Error; e != nil {
			t.Fatal(e)
		}
		return c
	}
	c2 := clone("cross-sub2-node1", c1.NodeID, c1.ProtocolEndpointID, s2.ID)
	c3 := clone("cross-sub1-node2", n.ID, ep.ID, c1.SubscriptionID)
	c4 := clone("cross-sub2-node2", n.ID, ep.ID, s2.ID)
	old := time.Now().UTC().AddDate(-3, 0, 0)
	sample := func(c model.ProtocolCredential, bytes int64) metering.FlowSample {
		return metering.FlowSample{NodeID: c.NodeID, PrincipalKey: c.PrincipalKey, CoreInstanceID: "cross-node-core", FlowID: c.CredentialID, EventID: "old-" + c.CredentialID, EventType: "flow.updated", Sequence: 1, Revision: 1, BytesUp: bytes, OccurredAt: old}
	}
	batches := [][]metering.FlowSample{{sample(c1, 1), sample(c2, 3)}, {sample(c4, 2), sample(c3, 4)}}
	held := h.db.Begin()
	defer held.Rollback()
	var first model.Subscription
	if e := held.Clauses(clause.Locking{Strength: "UPDATE"}).First(&first, s2.ID).Error; e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	done := make(chan error, 2)
	apply := func(flows []metering.FlowSample) error {
		_, e := h.services.ProjectBufferedEvents(ctx, application.BufferedEventProjection{Flows: flows}, h.credentialCipher)
		return e
	}
	for _, batch := range batches {
		go func(b []metering.FlowSample) { done <- apply(b) }(batch)
	}
	var schema string
	h.db.Raw("SELECT DATABASE()").Scan(&schema)
	deadline := time.Now().Add(10 * time.Second)
	var waits int64
	for time.Now().Before(deadline) {
		if e := h.db.Raw("SELECT COUNT(*) FROM performance_schema.data_lock_waits w JOIN performance_schema.data_locks l ON l.ENGINE_LOCK_ID=w.REQUESTING_ENGINE_LOCK_ID WHERE l.OBJECT_SCHEMA=? AND l.OBJECT_NAME='subscriptions'", schema).Scan(&waits).Error; e != nil {
			t.Fatal(e)
		}
		if waits >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	// The first quota committed even though the second remains busy.
	assertAccountingTotal(t, h, c1.SubscriptionID, 2, subStatusActive)
	probe := h.db.Begin()
	var second model.Subscription
	probeErr := probe.Clauses(clause.Locking{Strength: "UPDATE", Options: "NOWAIT"}).First(&second, c1.SubscriptionID).Error
	probe.Rollback()
	held.Commit()
	for range 2 {
		if e := <-done; e != nil {
			t.Fatal(e)
		}
	}
	if waits < 2 || probeErr != nil {
		t.Fatalf("unrelated quota remained locked while another waited: waits=%d sub1=%v", waits, probeErr)
	}
	assertAccountingTotal(t, h, c1.SubscriptionID, 8, subStatusActive)
	assertAccountingTotal(t, h, s2.ID, 7, subStatusActive)
	// Exact old reports remain duplicate-only, independent of chart retention.
	for _, batch := range batches {
		if e := apply(batch); e != nil {
			t.Fatal("historical replay", e)
		}
	}
	assertAccountingTotal(t, h, c1.SubscriptionID, 8, subStatusActive)
	assertAccountingTotal(t, h, s2.ID, 7, subStatusActive)
	next := sample(c1, 2)
	next.EventID = "higher-old-cumulative"
	next.Sequence, next.Revision = 2, 2
	for range 2 {
		if e := apply([]metering.FlowSample{next}); e != nil {
			t.Fatal(e)
		}
	}
	assertAccountingTotal(t, h, c1.SubscriptionID, 10, subStatusActive)
	var count int64
	h.db.Model(&model.MeteringReportReceipt{}).Count(&count)
	if count != 5 {
		t.Fatal("duplicate report created another contribution", count)
	}
	t.Log(fmt.Sprintf("two nodes, reversed subscription order, 3-year-old replay: quota totals 10/7; 5 unique reports"))
}
