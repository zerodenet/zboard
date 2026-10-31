package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/zeroevent"
	"gorm.io/gorm"
)

func repeatedFlowEvents(credential model.ProtocolCredential, count int) []zeroevent.Envelope {
	events := accountingBenchmarkEvents(credential, 0, count)
	at := time.Date(2026, 10, 16, 5, 0, 1, 0, time.UTC)
	for i := range events {
		events[i].FlowID = "flood-flow"
		events[i].OccurredAt = at
		events[i].Payload = json.RawMessage(fmt.Sprintf(`{"flow_id":"flood-flow","traffic":{"bytes_up":%d,"bytes_down":%d}}`, i+1, 2*(i+1)))
	}
	return events
}

func TestZeroEventWindowReducesLedgerWritesPreservingRoundingCoverageAndReplay(t *testing.T) {
	h, credential := accountingBenchmarkFixture(t)
	if err := h.db.Model(&model.ProtocolEndpoint{}).Where("id = ?", credential.ProtocolEndpointID).Update("multiplier_milli", 1500).Error; err != nil {
		t.Fatal(err)
	}
	events := repeatedFlowEvents(credential, 1000)
	spool := &checkpointTestSpool{events: events, commitErr: errors.New("checkpoint unavailable")}
	metrics := &zeroEventConsumerMetrics{}
	if _, err := h.consumeZeroEventBatchMeasured(context.Background(), spool, 2000, metrics); err == nil {
		t.Fatal("checkpoint failure not returned")
	}
	// Each 3-byte delta rounds to five billed bytes; rounding 3000 once would be 4500.
	assertAccountingTotal(t, h, credential.SubscriptionID, 5000, subStatusActive)
	if metrics.snapshot().ProcessedEnvelopes != 0 || spool.position != 0 {
		t.Fatal("failed checkpoint reported progress")
	}
	spool.commitErr = nil
	if count, err := h.consumeZeroEventBatchMeasured(context.Background(), spool, 2000, metrics); err != nil || count != 1000 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 5000, subStatusActive)
	var records int64
	if err := h.db.Model(&model.TrafficRecord{}).Count(&records).Error; err != nil || records != 1 {
		t.Fatalf("records=%d err=%v", records, err)
	}
	var record model.TrafficRecord
	if err := h.db.First(&record).Error; err != nil {
		t.Fatal(err)
	}
	var evidence struct {
		Samples  uint64 `json:"accounting_samples"`
		First    uint64 `json:"first_sequence"`
		Rounding int64  `json:"rounding_numerator"`
	}
	if err := json.Unmarshal([]byte(record.Meta), &evidence); err != nil || evidence.Samples != 1000 || evidence.Rounding != 500000 || (record.RawBytes*record.ProtocolMultiplierMilli+evidence.Rounding)/1000 != record.UsedBytes {
		t.Fatalf("unverifiable rounding evidence: %+v %v", evidence, err)
	}
	var coverage fairUseNodeCoverage
	if err := h.db.Where("node_id = ?", credential.NodeID).First(&coverage).Error; err != nil || coverage.LastSequence != 1000 || coverage.GapCount != 0 {
		t.Fatalf("coverage=%+v err=%v", coverage, err)
	}
	got := metrics.snapshot()
	if got.ProjectedFlowSamples != 1 || got.CoalescedFlowUpdates != 999 {
		t.Fatalf("metrics=%+v", got)
	}
}

func TestZeroEventWindowRetainsCheckpointAfterPartialAuthorityFailure(t *testing.T) {
	h, credential := accountingBenchmarkFixture(t)
	spool := &checkpointTestSpool{events: accountingBenchmarkEvents(credential, 0, 65)}
	batches := 0
	injected := errors.New("second authority batch failed")
	const callback = "test:fail_second_accounting_batch"
	if err := h.db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Schema == nil || tx.Statement.Schema.Table != "traffic_records" {
			return
		}
		n := 1
		if tx.Statement.ReflectValue.Kind() == reflect.Slice {
			n = tx.Statement.ReflectValue.Len()
		}
		if n > zeroEventSQLiteBatchLimit {
			t.Errorf("oversized authority insert: %d", n)
		}
		batches++
		if batches == 2 {
			tx.AddError(injected)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.consumeZeroEventBatch(context.Background(), spool, 2000); !errors.Is(err, injected) || spool.position != 0 {
		t.Fatalf("position=%d error=%v", spool.position, err)
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 32*30, subStatusActive)
	h.db.Callback().Create().Remove(callback)
	if _, err := h.consumeZeroEventBatch(context.Background(), spool, 2000); err != nil {
		t.Fatal(err)
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 65*30, subStatusActive)
}

func TestZeroFlowWindowPreservesIdentityMinuteAndCounterBoundaries(t *testing.T) {
	h, credential := accountingBenchmarkFixture(t)
	_ = h
	for name, change := range map[string]func(*zeroevent.Envelope){
		"generation":         func(e *zeroevent.Envelope) { e.CoreInstanceID = "next" },
		"config":             func(e *zeroevent.Envelope) { e.ConfigRevision++ },
		"minute":             func(e *zeroevent.Envelope) { e.OccurredAt = e.OccurredAt.Add(time.Minute) },
		"missing_generation": func(e *zeroevent.Envelope) { e.CoreInstanceID = "" },
		"missing_sequence":   func(e *zeroevent.Envelope) { e.Sequence = 0 },
		"duplicate_sequence": func(e *zeroevent.Envelope) { e.Sequence = 1 },
		"principal":          func(e *zeroevent.Envelope) { e.PrincipalKey = "changed" },
		"revision": func(e *zeroevent.Envelope) {
			e.Payload = []byte(`{"flow_id":"flood-flow","revision":9,"traffic":{"bytes_up":2,"bytes_down":4}}`)
		},
		"regression": func(e *zeroevent.Envelope) {
			e.Payload = []byte(`{"flow_id":"flood-flow","traffic":{"bytes_up":0,"bytes_down":0}}`)
		},
	} {
		t.Run(name, func(t *testing.T) {
			events := repeatedFlowEvents(credential, 2)
			change(&events[1])
			samples, err := zeroFlowSamples(events)
			if err != nil || len(samples) != 2 {
				t.Fatalf("samples=%+v err=%v", samples, err)
			}
		})
	}
}

func TestBufferedEventsPersistOnlyPanelFieldsAndResolvedPrincipal(t *testing.T) {
	h, _ := accountingBenchmarkFixture(t)
	node, runtime := receiptFixture(t, h)
	for _, event := range []zeroEventEnvelope{
		{EventID: "slim-flow", EventType: "flow.updated", PrincipalKey: "wrong", Payload: json.RawMessage(`{"record":{"flow_id":"f","auth":{"principal_key":"correct"},"traffic":{"bytes_up":9007199254740993,"bytes_down":1},"destination":"private.example","diagnostics":"` + strings.Repeat("x", 100000) + `"}}`)},
		{EventID: "slim-stats", EventType: "stats.sampled", Payload: json.RawMessage(`{"active_sessions":3,"bytes_up":9007199254740993,"bytes_down":1,"diagnostics":"` + strings.Repeat("x", 100000) + `"}`)},
	} {
		if buffered, err := h.appendBufferedZeroEvent(context.Background(), network.EventNode{ID: node.ID}, event); err != nil || !buffered {
			t.Fatalf("buffer=%t err=%v", buffered, err)
		}
	}
	batch, err := runtime.spool.ReadBatch(context.Background(), 10)
	if err != nil || len(batch.Events) != 2 {
		t.Fatalf("read err=%v", err)
	}
	for _, event := range batch.Events {
		if len(event.Payload) > 250 || strings.Contains(string(event.Payload), "diagnostics") {
			t.Fatal("persisted unused kernel diagnostics")
		}
	}
	flow, err := parseZeroFlowProjection(zeroBufferedEnvelopeAsEvent(batch.Events[0]))
	if err != nil || flow.PrincipalKey != "correct" || flow.BytesUp != 9007199254740993 {
		t.Fatalf("flow=%+v err=%v", flow, err)
	}
	stats, err := parseZeroStatsProjection(batch.Events[1].Payload)
	if err != nil || stats.BytesUp != 9007199254740993 {
		t.Fatalf("stats=%+v err=%v", stats, err)
	}
}

func TestMySQLZeroEventWindowPreservesRoundedAccountingAndReplay(t *testing.T) {
	h, credential := accountingFixtureFromOrder(t, newMySQLBusinessOrderFixture(t))
	if err := h.db.Model(&model.ProtocolEndpoint{}).Where("id = ?", credential.ProtocolEndpointID).Update("multiplier_milli", 1500).Error; err != nil {
		t.Fatal(err)
	}
	events := repeatedFlowEvents(credential, 1000)
	if err := h.projectZeroNodeEvents(context.Background(), events); err != nil {
		t.Fatal(err)
	}
	if err := h.projectZeroNodeEvents(context.Background(), events); err != nil {
		t.Fatal(err)
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 5000, subStatusActive)
	var count int64
	if err := h.db.Model(&model.TrafficRecord{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("records=%d err=%v", count, err)
	}
}

func TestZeroEventWindowRejectsRegressedCountersWithoutCheckpointAdvance(t *testing.T) {
	h, credential := accountingBenchmarkFixture(t)
	events := repeatedFlowEvents(credential, 3)
	events[1].Payload = []byte(`{"flow_id":"flood-flow","traffic":{"bytes_up":0,"bytes_down":0}}`)
	spool := &checkpointTestSpool{events: events}
	if _, err := h.consumeZeroEventBatch(context.Background(), spool, 2000); err == nil || spool.position != 0 {
		t.Fatalf("regression advanced checkpoint: %d %v", spool.position, err)
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 0, subStatusActive)
}

func TestMySQLZeroWindowFallsBackForDatabaseEquivalentReportIDs(t *testing.T) {
	h, credential := accountingFixtureFromOrder(t, newMySQLBusinessOrderFixture(t))
	if err := h.db.Model(&model.ProtocolEndpoint{}).Where("id = ?", credential.ProtocolEndpointID).Update("multiplier_milli", 1500).Error; err != nil {
		t.Fatal(err)
	}
	events := repeatedFlowEvents(credential, 3)
	events[0].ID = "Case-Report"
	events[1].ID = "case-report"
	events[2].ID = "next-report"
	if err := h.projectZeroNodeEvents(context.Background(), events); err != nil {
		t.Fatal(err)
	}
	// The equivalent second report is ignored by the database: ceil(3*1.5)+ceil(6*1.5).
	assertAccountingTotal(t, h, credential.SubscriptionID, 14, subStatusActive)
}

func TestZeroWindowKeepsExistingReportIdentityBeforeCollapsing(t *testing.T) {
	h, credential := accountingBenchmarkFixture(t)
	first := repeatedFlowEvents(credential, 1)
	first[0].FlowID = "other-flow"
	first[0].Payload = []byte(`{"flow_id":"other-flow","traffic":{"bytes_up":1,"bytes_down":2}}`)
	if err := h.projectZeroNodeEvents(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	events := repeatedFlowEvents(credential, 3)
	if err := h.projectZeroNodeEvents(context.Background(), events); err != nil {
		t.Fatal(err)
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 12, subStatusActive)
	var count int64
	if err := h.db.Model(&model.MeteringReportReceipt{}).Count(&count).Error; err != nil || count != 3 {
		t.Fatalf("report identity lost: rows=%d err=%v", count, err)
	}
}

func TestBufferedWarningStormRetainsUnknownSourcesWithoutBilling(t *testing.T) {
	h, _ := accountingBenchmarkFixture(t)
	node, runtime := receiptFixture(t, h)
	for i := 0; i < 100; i++ {
		event := zeroEventEnvelope{EventID: fmt.Sprintf("warning-%d", i), EventType: "engine.warning", SourceID: fmt.Sprintf("node-%d", node.ID), CoreInstanceID: "warnings", Sequence: uint64(i + 1), Payload: json.RawMessage(`{"message":"` + strings.Repeat("x", 10000) + `"}`)}
		if buffered, err := h.appendBufferedZeroEvent(context.Background(), network.EventNode{ID: node.ID}, event); err != nil || !buffered {
			t.Fatalf("buffered=%t err=%v", buffered, err)
		}
	}
	batch, err := runtime.spool.ReadBatch(context.Background(), 2000)
	if err != nil || len(batch.Events) != 100 {
		t.Fatalf("batch=%d err=%v", len(batch.Events), err)
	}
	for _, event := range batch.Events {
		if string(event.Payload) != "{}" {
			t.Fatal("unused diagnostic payload was retained")
		}
	}
	if err := h.projectZeroNodeEvents(context.Background(), batch.Events); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		if _, err := h.services.NodeObservations.Drain(context.Background(), 32, 8*1024*1024); err != nil {
			t.Fatal(err)
		}
	}
	var held int64
	if err := h.db.Table("node_observation_intents").Where("node_id = ? AND status = ? AND reason = ?", node.ID, "held", "unknown_source").Count(&held).Error; err != nil || held != 100 {
		t.Fatalf("unknown sources lost: held=%d err=%v", held, err)
	}
	var count int64
	if err := h.db.Model(&model.TrafficRecord{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("warning storm produced accounting rows: %d %v", count, err)
	}
}
