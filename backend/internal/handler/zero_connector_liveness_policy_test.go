package handler

import (
	"os"
	"strings"
	"testing"
)

func TestBufferedZeroEventsRefreshConnectorLivenessOnReceipt(t *testing.T) {
	source, err := os.ReadFile("zero_event_runtime.go")
	if err != nil {
		t.Fatalf("read zero_event_runtime.go: %v", err)
	}
	text := string(source)
	appendStart := strings.Index(text, "func (h *handlers) appendBufferedZeroEvent")
	appendEnd := strings.Index(text[appendStart:], "func (h *handlers) recordBufferedZeroConnectorReceipt")
	if appendStart < 0 || appendEnd < 0 {
		t.Fatal("buffered event receipt functions are missing")
	}
	appendSource := text[appendStart : appendStart+appendEnd]
	persistAt := strings.Index(appendSource, ".spool.Append(ctx, envelope)")
	touchAt := strings.Index(appendSource, "recordBufferedZeroConnectorReceipt(node, runtime)")
	if persistAt < 0 || touchAt < 0 || touchAt <= persistAt {
		t.Fatal("connector liveness must be refreshed only after the event is durably appended")
	}
}

func TestBufferedProjectionDoesNotRegressReceiptLivenessToEventTime(t *testing.T) {
	source, err := os.ReadFile("zero_event_runtime.go")
	if err != nil {
		t.Fatalf("read zero_event_runtime.go: %v", err)
	}
	text := string(source)
	start := strings.Index(text, "func projectZeroNode")
	end := strings.Index(text[start:], "func zeroEventNewer")
	if start < 0 || end < 0 {
		t.Fatal("projectZeroNode source is missing")
	}
	projectionSource := text[start : start+end]
	if strings.Contains(projectionSource, "connector_last_seen_at") || strings.Contains(projectionSource, "\"is_online\"") {
		t.Fatal("spool consumption must project event facts without overwriting receipt-time Connector liveness")
	}
}
