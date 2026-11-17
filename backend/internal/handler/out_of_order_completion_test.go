package handler

import (
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/zeroevent"
	"testing"
	"time"
)

func TestLateCompletionPreservesCumulativeHighWaterAndTerminalTail(t *testing.T) {
	h, c := accountingBenchmarkFixture(t)
	assertLateCompletionHighWater(t, h, c)
}
func TestMySQLLateCompletionPreservesCumulativeHighWaterAndTerminalTail(t *testing.T) {
	h, c := accountingFixtureFromOrder(t, newMySQLBusinessOrderFixture(t))
	assertLateCompletionHighWater(t, h, c)
}
func assertLateCompletionHighWater(t *testing.T, h *handlers, c model.ProtocolCredential) {
	t.Helper()
	if e := h.db.Model(&model.ProtocolEndpoint{}).Where("id=?", c.ProtocolEndpointID).Update("multiplier_milli", 1001).Error; e != nil {
		t.Fatal(e)
	}
	at := time.Now().UTC().Add(-72 * time.Hour)
	core, flow := "out-of-order-core", "out-of-order-flow"
	update := func(id string, seq uint64, up, down int64) {
		t.Helper()
		ev := zeroevent.Envelope{ID: id, NodeID: uint64(c.NodeID), Type: "flow.updated", CoreInstanceID: core, PrincipalKey: c.PrincipalKey, Sequence: seq, FlowID: flow, OccurredAt: at, ReceivedAt: time.Now().UTC(), Payload: []byte(fmt.Sprintf(`{"flow_id":%q,"traffic":{"bytes_up":%d,"bytes_down":%d}}`, flow, up, down))}
		if e := h.projectZeroNodeEvents(t.Context(), []zeroevent.Envelope{ev}); e != nil {
			t.Fatal(e)
		}
	}
	complete := func(id string, seq uint64, up, down int64) {
		t.Helper()
		in := metering.CompletedFlow{NodeID: c.NodeID, EventID: id, FlowID: flow, PrincipalKey: c.PrincipalKey, CoreInstanceID: core, EventType: "flow.completed", Sequence: seq, BytesUp: up, BytesDown: down, OccurredAt: at}
		if _, _, e := h.services.CompletionAccounting(h.credentialCipher).Complete(t.Context(), in); e != nil {
			t.Fatal(e)
		}
	}
	check := func(used, raw int64, seq uint64) {
		t.Helper()
		assertAccountingTotal(t, h, c.SubscriptionID, used, subStatusActive)
		var cursor model.FlowUsage
		if e := h.db.Where("node_id=? AND flow_id=?", c.NodeID, metering.FlowUsageKey(core, flow)).First(&cursor).Error; e != nil || cursor.RawBytes != raw || cursor.Revision != seq || cursor.Status != "completed" || cursor.CompletedAt == nil {
			t.Fatalf("cursor %+v err=%v", cursor, e)
		}
		var totals struct{ Raw, Used int64 }
		if e := h.db.Model(&model.TrafficRecord{}).Select("SUM(raw_bytes) AS raw,SUM(used_bytes) AS used").Scan(&totals).Error; e != nil || totals.Raw != raw || totals.Used != used {
			t.Fatalf("ledger %+v %v", totals, e)
		}
	}
	update("update-20", 20, 10, 20)
	complete("late-completion-19", 19, 8, 15)
	check(31, 30, 20)
	complete("late-completion-19", 19, 8, 15)
	check(31, 30, 20)
	update("buffered-tail-21", 21, 20, 30)
	check(31, 30, 20)
	update("buffered-tail-21", 21, 20, 30)
	check(31, 30, 20)
	complete("older-unseen-tail-18", 18, 25, 35)
	check(62, 60, 20)
	update("older-buffered-tail-17", 17, 26, 34)
	check(62, 60, 20)
	update("older-buffered-tail-17", 17, 26, 34)
	complete("older-unseen-tail-18", 18, 25, 35)
	check(62, 60, 20)
	var n int64
	if e := h.db.Model(&model.MeteringReportReceipt{}).Count(&n).Error; e != nil || n != 3 {
		t.Fatalf("receipts=%d error=%v", n, e)
	}
	// A genuinely newer active counter regression still cannot overwrite a floor.
	fresh := accountingBenchmarkEvents(c, 30, 1)
	if err := h.projectZeroNodeEvents(t.Context(), fresh); err != nil {
		t.Fatal(err)
	}
	fresh[0].ID = "invalid-active-regression"
	fresh[0].Sequence++
	fresh[0].Payload = []byte(`{"flow_id":"flow-0","traffic":{"bytes_up":1,"bytes_down":1}}`)
	if err := h.projectZeroNodeEvents(t.Context(), fresh); err == nil {
		t.Fatal("newer active counter regression was accepted")
	}

	assertAccountingTotal(t, h, c.SubscriptionID, 993, subStatusActive)
	// A legacy cursor revision is not a sequence from the new runtime namespace.
	legacy := model.FlowUsage{NodeID: c.NodeID, FlowID: "legacy-adopt", ProtocolCredentialID: c.ID, SubscriptionID: c.SubscriptionID, ProtocolEndpointID: c.ProtocolEndpointID, PrincipalKey: c.PrincipalKey, Revision: 9999, RawBytes: 30, UploadBytes: 10, DownloadBytes: 20, UsedBytes: 31, Status: "active", LastSeenAt: at, LastEventID: "legacy-seed"}
	if e := h.db.Create(&legacy).Error; e != nil {
		t.Fatal(e)
	}
	terminal := metering.CompletedFlow{NodeID: c.NodeID, EventID: "adopt-runtime-1", FlowID: "legacy-adopt", PrincipalKey: c.PrincipalKey, CoreInstanceID: "new-runtime", EventType: "flow.completed", Sequence: 1, BytesUp: 20, BytesDown: 40, OccurredAt: at}
	if _, _, e := h.services.CompletionAccounting(h.credentialCipher).Complete(t.Context(), terminal); e != nil {
		t.Fatal(e)
	}
	var adopted model.FlowUsage
	if e := h.db.Where("node_id=? AND flow_id=?", c.NodeID, metering.FlowUsageKey("new-runtime", "legacy-adopt")).First(&adopted).Error; e != nil || adopted.Revision != 1 || adopted.RawBytes != 60 {
		t.Fatalf("runtime cursor inherited legacy sequence: %+v %v", adopted, e)
	}
	assertAccountingTotal(t, h, c.SubscriptionID, 1024, subStatusActive)
}
