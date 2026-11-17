package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/zeroevent"
	"time"
)

func TestBufferedFlowKeepsIngressAcrossDurableReduction(t *testing.T) {
	h, _ := accountingBenchmarkFixture(t)
	node, runtime := receiptFixture(t, h)
	for _, raw := range []string{
		`{"flow_id":"f1","inbound_tag":"endpoint-31","record":{"flow_id":"f1","inbound":{"tag":"endpoint-31"},"traffic":{"bytes_up":7,"bytes_down":9}}}`,
		`{"flow_id":"f2","inbound":{"tag":"endpoint-32"},"traffic":{"bytes_up":1,"bytes_down":2}}`,
		`{"flow_id":"f3","traffic":{"bytes_up":3,"bytes_down":4}}`,
	} {
		var p map[string]interface{}
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			t.Fatal(err)
		}
		e := zeroEventEnvelope{EventID: p["flow_id"].(string), EventType: "flow.updated", PrincipalKey: "original-principal", Payload: []byte(raw)}
		if _, err := h.appendBufferedZeroEvent(t.Context(), network.EventNode{ID: node.ID, AssetTag: node.AssetTag}, e); err != nil {
			t.Fatal(err)
		}
	}
	b, err := runtime.spool.ReadBatch(context.Background(), 10)
	if err != nil || len(b.Events) != 3 {
		t.Fatal(len(b.Events), err)
	}
	samples, err := zeroFlowSamples(b.Events)
	if err != nil || len(samples) != 3 {
		t.Fatal(len(samples), err)
	}
	for i, expected := range []string{"endpoint-31", "endpoint-32", ""} {
		if samples[i].InboundTag != expected {
			t.Fatalf("durable ingress %q, want %q", samples[i].InboundTag, expected)
		}
	}
}

func TestFlowIngressRejectsContradictoryPayloadWithoutGuessing(t *testing.T) {
	for _, raw := range []string{
		`{"flow_id":"f","inbound_tag":"endpoint-1","record":{"inbound":{"tag":"endpoint-2"},"traffic":{"bytes_up":1,"bytes_down":1}}}`,
		`{"flow_id":"f","inbound_tag":7,"traffic":{"bytes_up":1,"bytes_down":1}}`,
	} {
		if _, err := parseZeroFlowProjection(zeroEventEnvelope{PrincipalKey: "principal", Payload: []byte(raw)}); err == nil {
			t.Fatal("invalid ingress accepted")
		}
	}
}

func TestFlowIngressCannotBillAnotherEndpointMultiplier(t *testing.T) {
	h, c := accountingBenchmarkFixture(t)
	assertFlowIngressAccounting(t, h, c)
}
func TestMySQLFlowIngressCannotBillAnotherEndpointMultiplier(t *testing.T) {
	h, c := accountingFixtureFromOrder(t, newMySQLBusinessOrderFixture(t))
	assertFlowIngressAccounting(t, h, c)
}
func assertFlowIngressAccounting(t *testing.T, h *handlers, c model.ProtocolCredential) {
	t.Helper()
	events := accountingBenchmarkEvents(c, 0, 1)
	events[0].Payload = []byte(fmt.Sprintf(`{"flow_id":"flow-0","inbound_tag":"endpoint-%d","traffic":{"bytes_up":10,"bytes_down":20}}`, c.ProtocolEndpointID+1))
	if err := h.projectZeroNodeEvents(t.Context(), events); !errors.Is(err, metering.ErrFlowIngressMismatch) {
		t.Fatal("mismatched ingress", err)
	}
	assertAccountingTotal(t, h, c.SubscriptionID, 0, subStatusActive)
	events[0].Payload = []byte(fmt.Sprintf(`{"flow_id":"flow-0","inbound_tag":"endpoint-%d","traffic":{"bytes_up":10,"bytes_down":20}}`, c.ProtocolEndpointID))
	if err := h.projectZeroNodeEvents(t.Context(), events); err != nil {
		t.Fatal(err)
	}
	// A dominated replay without a receipt must not bypass the ingress guard.
	events[0].ID = "unreceipted-wrong-ingress"
	events[0].Payload = []byte(fmt.Sprintf(`{"flow_id":"flow-0","inbound_tag":"endpoint-%d","traffic":{"bytes_up":1,"bytes_down":2}}`, c.ProtocolEndpointID+1))
	if err := h.projectZeroNodeEvents(t.Context(), events); !errors.Is(err, metering.ErrFlowIngressMismatch) {
		t.Fatal(err)
	}
	assertAccountingTotal(t, h, c.SubscriptionID, 30, subStatusActive)
	final := metering.CompletedFlow{NodeID: c.NodeID, CoreInstanceID: "final-ingress-core", EventID: "wrong-final-ingress", FlowID: "final-ingress-flow", PrincipalKey: c.PrincipalKey, EventType: "flow.completed", Sequence: 1, BytesUp: 10, BytesDown: 20, OccurredAt: time.Now().UTC(), InboundTag: fmt.Sprintf("endpoint-%d", c.ProtocolEndpointID+1)}
	if _, _, err := h.services.CompletionAccounting(h.credentialCipher).Complete(t.Context(), final); !errors.Is(err, metering.ErrFlowIngressMismatch) {
		t.Fatal("final ingress mismatch", err)
	}
	assertAccountingTotal(t, h, c.SubscriptionID, 30, subStatusActive)
	final.InboundTag = fmt.Sprintf("endpoint-%d", c.ProtocolEndpointID)
	if _, _, err := h.services.CompletionAccounting(h.credentialCipher).Complete(t.Context(), final); err != nil {
		t.Fatal(err)
	}
	assertAccountingTotal(t, h, c.SubscriptionID, 60, subStatusActive)
	var count int64
	if err := h.db.Model(&model.MeteringReportReceipt{}).Count(&count).Error; err != nil || count != 2 {
		t.Fatal(count, err)
	}
}

func TestFlowWindowDoesNotMergeAcrossIngressChanges(t *testing.T) {
	_, c := accountingBenchmarkFixture(t)
	events := repeatedFlowEvents(c, 2)
	for i := range events {
		events[i].Payload = []byte(fmt.Sprintf(`{"flow_id":"same","inbound_tag":"endpoint-%d","traffic":{"bytes_up":%d,"bytes_down":0}}`, i+1, i+1))
	}
	samples, err := zeroFlowSamplesWithMerge([]zeroevent.Envelope{events[0], events[1]}, true)
	if err != nil || len(samples) != 2 {
		t.Fatal("ingress boundary merged", len(samples), err)
	}
}
