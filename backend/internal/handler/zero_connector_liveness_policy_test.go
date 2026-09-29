package handler

import (
	"context"
	"github.com/zerodenet/zboard/backend/internal/model"
	"os"
	"strings"
	"testing"
)

func TestBufferedZeroEventsRefreshConnectorLivenessOnReceipt(t *testing.T) {
	h, _ := accountingBenchmarkFixture(t)
	node, runtime := receiptFixture(t, h)
	response := receiptRequest(h, node.ID, "receipt-timestamp")
	if response.Code != 200 {
		t.Fatalf("receipt: %d %s", response.Code, response.Body.String())
	}
	batch, err := runtime.spool.ReadBatch(context.Background(), 1)
	if err != nil || len(batch.Events) != 1 {
		t.Fatalf("durable receipt: %v", err)
	}
	value, ok := runtime.receipts.Load(node.ID)
	if !ok || !value.(bufferedConnectorReceipt).at.Equal(batch.Events[0].ReceivedAt) {
		t.Fatal("pending heartbeat must use durable server receipt time")
	}
	var current model.Node
	if err := h.db.First(&current, node.ID).Error; err != nil {
		t.Fatal(err)
	}
	if current.ConnectorLastSeenAt != nil {
		t.Fatal("HTTP receipt synchronously wrote node metadata")
	}
	h.flushBufferedConnectorReceipts(context.Background(), runtime)
	if err := h.db.First(&current, node.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !current.IsOnline || current.ConnectorLastSeenAt == nil || !current.ConnectorLastSeenAt.Equal(batch.Events[0].ReceivedAt) {
		t.Fatal("consumer did not persist original receipt time")
	}
}

func TestBufferedProjectionDoesNotRegressReceiptLivenessToEventTime(t *testing.T) {
	source, err := os.ReadFile("zero_event_runtime.go")
	if err != nil {
		t.Fatalf("read zero_event_runtime.go: %v", err)
	}
	text := string(source)
	start := strings.Index(text, "func zeroNodeObservation")
	if start < 0 {
		t.Fatal("zeroNodeObservation source is missing")
	}
	end := strings.Index(text[start:], "func zeroEventNewer")
	if end < 0 {
		t.Fatal("zeroNodeObservation boundary is missing")
	}
	projectionSource := text[start : start+end]
	if strings.Contains(projectionSource, "connector_last_seen_at") || strings.Contains(projectionSource, "\"is_online\"") {
		t.Fatal("spool consumption must project event facts without overwriting receipt-time Connector liveness")
	}
}
