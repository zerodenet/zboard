package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/zeroevent"
)

func TestConsumerMetricsOnlyCountSuccessfulCheckpointProgress(t *testing.T) {
	h, credential := accountingBenchmarkFixture(t)
	spool := &checkpointTestSpool{events: accountingBenchmarkEvents(credential, 0, 2), commitErr: errors.New("checkpoint unavailable")}
	for i := range spool.events {
		spool.events[i].ReceivedAt = time.Now().Add(-time.Second)
	}
	var metrics zeroEventConsumerMetrics
	if _, err := h.consumeZeroEventBatchMeasured(context.Background(), spool, 32, &metrics); err == nil {
		t.Fatal("expected checkpoint failure")
	}
	if result := metrics.snapshot(); result.ProcessedEnvelopes != 0 || result.P95UpperMillis != nil {
		t.Fatalf("failed checkpoint counted as progress: %+v", result)
	}
	spool.commitErr = nil
	if _, err := h.consumeZeroEventBatchMeasured(context.Background(), spool, 32, &metrics); err != nil {
		t.Fatal(err)
	}
	result := metrics.snapshot()
	if result.ProcessedEnvelopes != 2 || result.LatencySamples != 2 || result.Batches != 1 || result.P95UpperMillis == nil || *result.P95UpperMillis < 1000 {
		t.Fatalf("missing checkpoint latency evidence: %+v", result)
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 60, subStatusActive)
}

func TestConsumerLatencyHistogramKeepsMissingTimesAndOverflowExplicit(t *testing.T) {
	now := time.Now().UTC()
	events := make([]zeroevent.Envelope, 22)
	for i := range 19 {
		events[i].ReceivedAt = now.Add(-time.Millisecond)
	}
	events[19].ReceivedAt = now.Add(-65 * time.Second)
	events[21].ReceivedAt = now.Add(time.Second)
	var metrics zeroEventConsumerMetrics
	metrics.committed(events, time.Millisecond, now)
	result := metrics.snapshot()
	if result.LatencySamples != 20 || result.MissingReceiveTime != 2 || result.MaximumMillis != 65000 || result.P95UpperMillis == nil || *result.P95UpperMillis != 100 {
		t.Fatalf("histogram=%+v", result)
	}
	var overflow zeroEventConsumerMetrics
	overflow.committed(events[19:20], 0, now)
	result = overflow.snapshot()
	if result.P95UpperMillis == nil || *result.P95UpperMillis != 65000 {
		t.Fatalf("overflow latency silently clipped: %+v", result)
	}
}

func TestAdminRuntimeDiagnosticsAreOptInAndAdminOnly(t *testing.T) {
	f := newTrafficReadFixture(t)
	if err := f.h.db.Create(&model.Installation{ID: 1, InstalledAt: time.Now().UTC()}).Error; err != nil {
		t.Fatal(err)
	}
	closeReads, err := f.h.ConfigureTrafficReads()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = closeReads() })
	path := "/api/v1/admin/system-info"
	var regular, observed adminSystemInfo
	f.get(t, path, true, f.h.AdminSystemInfoHandler, &regular)
	if regular.Runtime != nil {
		t.Fatal("runtime diagnostics added to ordinary metadata reads")
	}
	f.get(t, path+"?include_runtime=true", true, f.h.AdminSystemInfoHandler, &observed)
	if observed.Runtime == nil || observed.Runtime.SharedPool || observed.Runtime.AccountingDB.Maximum != 1 || observed.Runtime.TrafficReadDB.Maximum != 2 || observed.Runtime.EventSpool != nil || observed.Runtime.EventConsumer != nil {
		t.Fatalf("runtime diagnostics=%+v", observed.Runtime)
	}
	for _, item := range []struct {
		token, suffix string
		status        int
	}{{"", "true", 401}, {f.token, "true", 403}, {f.admin, "invalid", 400}} {
		response := httptest.NewRecorder()
		f.h.AdminSystemInfoHandler(response, announcementRequest(http.MethodGet, path+"?include_runtime="+item.suffix, item.token, ""))
		if response.Code != item.status {
			t.Fatalf("runtime access status=%d want=%d", response.Code, item.status)
		}
	}
}
