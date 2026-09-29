package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/zeroevent"
	"gorm.io/gorm/clause"
)

func receiptFixture(t *testing.T, h *handlers) (model.Node, *zeroEventRuntime) {
	t.Helper()
	credential, err := h.credentialCipher.Encrypt("receipt-test-secret")
	if err != nil {
		t.Fatal(err)
	}
	node := model.Node{Name: "receipt-lock", Config: "{}", NodeCredential: credential, IsEnabled: true}
	if err := h.db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	cfg := zeroevent.DefaultConfig()
	cfg.Directory = t.TempDir()
	cfg.Storage.MinFreeSpace, cfg.Storage.EmergencyReserve = 0, 0
	spool, err := zeroevent.NewFileSpool(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := spool.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	runtime := &zeroEventRuntime{spool: spool, config: cfg.Consumer}
	// Deliberately control consumer attempts while a separate transaction holds
	// the lock. Requests still run the real authentication and durable file path.
	zeroEventRuntimeRegistry.Store(h, runtime)
	t.Cleanup(func() {
		zeroEventRuntimeRegistry.Delete(h)
		if err := spool.Close(); err != nil {
			t.Error(err)
		}
	})
	return node, runtime
}

func receiptRequest(h *handlers, nodeID uint, id string) *httptest.ResponseRecorder {
	event := zeroEventEnvelope{SchemaID: "zero.event.v1", EventID: id, EventType: "stats.sampled", SourceID: fmt.Sprintf("node-%d", nodeID), CoreInstanceID: "receipt-core", Sequence: 1, OccurredAtUnixMillis: time.Now().UnixMilli(), Payload: json.RawMessage(`{"active_sessions":3,"bytes_up":100,"bytes_down":200}`)}
	body, _ := json.Marshal(event)
	req := httptest.NewRequest(http.MethodPost, "/api/zero/events", bytes.NewReader(body))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req = req.WithContext(ctx)
	req.Header.Set("Authorization", "Bearer receipt-test-secret")
	response := httptest.NewRecorder()
	h.ZeroEventHandler(response, req)
	return response
}

func TestMySQLBufferedReceiptsDoNotWaitForLockedNode(t *testing.T) {
	h, _ := newMySQLPublishHandlers(t)
	node, runtime := receiptFixture(t, h)
	lock := h.db.Begin()
	if lock.Error != nil {
		t.Fatal(lock.Error)
	}
	defer lock.Rollback()
	var locked model.Node
	if err := lock.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, node.ID).Error; err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	var wg sync.WaitGroup
	responses := make(chan *httptest.ResponseRecorder, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) { defer wg.Done(); responses <- receiptRequest(h, node.ID, fmt.Sprintf("locked-%d", i)) }(i)
	}
	wg.Wait()
	close(responses)
	for response := range responses {
		if response.Code != 200 {
			t.Fatalf("locked node HTTP: %d %s", response.Code, response.Body.String())
		}
	}
	elapsed := time.Since(started)
	if elapsed > 2*time.Second {
		t.Fatalf("receipts waited for node row lock: %s", elapsed)
	}
	batch, err := runtime.spool.ReadBatch(context.Background(), 64)
	if err != nil || len(batch.Events) != 32 {
		t.Fatalf("durable receipts=%d error=%v", len(batch.Events), err)
	}
	started = time.Now()
	h.flushBufferedConnectorReceipts(context.Background(), runtime)
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("busy row was not skipped: %s", elapsed)
	}
	if _, ok := runtime.receipts.Load(node.ID); !ok {
		t.Fatal("busy receipt discarded")
	}
	if err := lock.Commit().Error; err != nil {
		t.Fatal(err)
	}
	h.flushBufferedConnectorReceipts(context.Background(), runtime)
	if err := h.db.First(&node, node.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !node.IsOnline || node.ConnectorLastSeenAt == nil {
		t.Fatal("receipt did not recover after releasing row lock")
	}
	if _, ok := runtime.receipts.Load(node.ID); ok {
		t.Fatal("applied receipt still pending")
	}
	t.Logf("32 concurrent authenticated durable HTTP receipts with node row locked: %s", elapsed)
}

func TestMySQLBufferedReceiptsDoNotWaitForExhaustedPool(t *testing.T) {
	h, _ := newMySQLPublishHandlers(t)
	node, runtime := receiptFixture(t, h)
	// Warm the existing five-second authentication cache before starving the
	// pool. An uncached authentication read still needs a database connection.
	if response := receiptRequest(h, node.ID, "warm"); response.Code != 200 {
		t.Fatalf("warm: %s", response.Body.String())
	}
	pool, err := h.db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	connection, err := pool.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	started := time.Now()
	response := receiptRequest(h, node.ID, "pool-busy")
	if response.Code != 200 || time.Since(started) > time.Second {
		t.Fatalf("pool delayed durable receipt: %d %s", response.Code, time.Since(started))
	}
	started = time.Now()
	h.flushBufferedConnectorReceipts(context.Background(), runtime)
	if time.Since(started) > time.Second {
		t.Fatal("metadata attempt exceeded bound")
	}
	if _, ok := runtime.receipts.Load(node.ID); !ok {
		t.Fatal("pool contention lost pending receipt")
	}
	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}
	h.flushBufferedConnectorReceipts(context.Background(), runtime)
	if _, ok := runtime.receipts.Load(node.ID); ok {
		t.Fatal("pool recovery did not retry receipt")
	}
}

func TestMySQLBufferedReceiptRevalidatesCredentialAfterContention(t *testing.T) {
	h, _ := newMySQLPublishHandlers(t)
	node, runtime := receiptFixture(t, h)
	lock := h.db.Begin()
	if lock.Error != nil {
		t.Fatal(lock.Error)
	}
	defer lock.Rollback()
	var locked model.Node
	if err := lock.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, node.ID).Error; err != nil {
		t.Fatal(err)
	}
	if response := receiptRequest(h, node.ID, "before-revoke"); response.Code != 200 {
		t.Fatalf("receipt: %s", response.Body.String())
	}
	h.flushBufferedConnectorReceipts(context.Background(), runtime)
	if err := lock.Model(&node).Updates(map[string]any{"node_credential_revoked_at": time.Now(), "is_online": false}).Error; err != nil {
		t.Fatal(err)
	}
	if err := lock.Commit().Error; err != nil {
		t.Fatal(err)
	}
	h.flushBufferedConnectorReceipts(context.Background(), runtime)
	if err := h.db.First(&node, node.ID).Error; err != nil {
		t.Fatal(err)
	}
	if node.IsOnline || node.ConnectorLastSeenAt != nil {
		t.Fatal("revoked queued receipt resurrected node")
	}
	if _, ok := runtime.receipts.Load(node.ID); ok {
		t.Fatal("revoked receipt must be discarded")
	}
}

func TestBufferedReceiptCannotUndoLaterStopAndDeletedNodesAreDiscarded(t *testing.T) {
	h, _ := accountingBenchmarkFixture(t)
	node, runtime := receiptFixture(t, h)
	at := time.Now().UTC()
	runtime.enqueueReceipt(bufferedConnectorReceipt{node: network.EventNode{ID: node.ID, Credential: node.NodeCredential}, at: at})
	if err := h.services.NodeActivity.Record(context.Background(), node.ID, node.NodeCredential, network.NodeActivityUpdate{At: at.Add(time.Second), Online: false}); err != nil {
		t.Fatal(err)
	}
	h.flushBufferedConnectorReceipts(context.Background(), runtime)
	if err := h.db.First(&node, node.ID).Error; err != nil {
		t.Fatal(err)
	}
	if node.IsOnline || node.ConnectorLastSeenAt != nil {
		t.Fatal("old receipt undid later stop")
	}
	runtime.enqueueReceipt(bufferedConnectorReceipt{node: network.EventNode{ID: 999999, Credential: "deleted"}, at: at})
	h.flushBufferedConnectorReceipts(context.Background(), runtime)
	if _, ok := runtime.receipts.Load(uint(999999)); ok {
		t.Fatal("deleted node receipt retained indefinitely")
	}
}

func TestBufferedReceiptCoalescesConcurrentOutOfOrderArrivals(t *testing.T) {
	runtime := &zeroEventRuntime{}
	base := time.Now().UTC()
	var wg sync.WaitGroup
	for i := 0; i < 256; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			runtime.enqueueReceipt(bufferedConnectorReceipt{node: network.EventNode{ID: 1, Credential: "credential"}, at: base.Add(time.Duration(i) * time.Second)})
		}(i)
	}
	wg.Wait()
	value, ok := runtime.receipts.Load(uint(1))
	if !ok || !value.(bufferedConnectorReceipt).at.Equal(base.Add(255*time.Second)) {
		t.Fatal("concurrent queue regressed latest receipt")
	}
}

type failedReceiptSpool struct{ zeroevent.EventSpool }

func (failedReceiptSpool) Append(context.Context, zeroevent.Envelope) error {
	return errors.New("fixture storage unavailable")
}

func TestFailedDurableAppendDoesNotQueueReceipt(t *testing.T) {
	h, _ := accountingBenchmarkFixture(t)
	node, runtime := receiptFixture(t, h)
	runtime.spool = failedReceiptSpool{runtime.spool}
	response := receiptRequest(h, node.ID, "append-failed")
	if response.Code != 500 {
		t.Fatalf("append failure must remain retryable: %d", response.Code)
	}
	if _, ok := runtime.receipts.Load(node.ID); ok {
		t.Fatal("failed append queued successful receipt")
	}
}

func TestMySQLBufferedReceiptCannotUndoStopWithinSameMillisecond(t *testing.T) {
	h, _ := newMySQLPublishHandlers(t)
	node, runtime := receiptFixture(t, h)
	base := time.Now().UTC().Truncate(time.Millisecond)
	runtime.enqueueReceipt(bufferedConnectorReceipt{node: network.EventNode{ID: node.ID, Credential: node.NodeCredential}, at: base.Add(100 * time.Microsecond)})
	if err := h.services.NodeActivity.Record(context.Background(), node.ID, node.NodeCredential, network.NodeActivityUpdate{At: base.Add(200 * time.Microsecond), Online: false}); err != nil {
		t.Fatal(err)
	}
	h.flushBufferedConnectorReceipts(context.Background(), runtime)
	if err := h.db.First(&node, node.ID).Error; err != nil {
		t.Fatal(err)
	}
	if node.IsOnline || node.ConnectorLastSeenAt != nil {
		t.Fatal("receipt resurrected stopped node through DATETIME(3) rounding")
	}
}
