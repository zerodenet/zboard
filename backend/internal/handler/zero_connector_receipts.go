package handler

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
)

type bufferedConnectorReceipt struct {
	node network.EventNode
	at   time.Time
}

// Coalesce by node, keeping the newest receipt even when request goroutines
// complete out of order. Storage is bounded by the number of reporting nodes.
func (runtime *zeroEventRuntime) enqueueReceipt(receipt bufferedConnectorReceipt) {
	for {
		previous, loaded := runtime.receipts.LoadOrStore(receipt.node.ID, receipt)
		if !loaded || !receipt.at.After(previous.(bufferedConnectorReceipt).at) {
			return
		}
		if runtime.receipts.CompareAndSwap(receipt.node.ID, previous, receipt) {
			return
		}
	}
}

func (h *handlers) flushBufferedConnectorReceipts(ctx context.Context, runtime *zeroEventRuntime) {
	now := time.Now().UTC()
	runtime.receipts.Range(func(key, value any) bool {
		if ctx.Err() != nil {
			return false
		}
		receipt := value.(bufferedConnectorReceipt)
		if previous, ok := runtime.lastReceipt.Load(key); ok && now.Sub(previous.(time.Time)) < zeroConnectorReceiptPersistInterval {
			return true
		}
		// Also bound connection-pool contention. MySQL row contention is handled
		// without waiting by SKIP LOCKED in the repository.
		attempt, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
		applied, err := h.services.NodeActivity.TryReceipt(attempt, receipt.node.ID, receipt.node.Credential, receipt.at)
		cancel()
		if errors.Is(err, network.ErrNodeActivityCredential) {
			runtime.receipts.CompareAndDelete(key, receipt)
			return true
		}
		if err != nil {
			if ctx.Err() == nil {
				log.Printf("Zero connector receipt projection deferred for node %d: %v", receipt.node.ID, err)
			}
			return true
		}
		if applied {
			runtime.lastReceipt.Store(key, now)
			// A newer receipt arriving during this attempt must remain pending.
			runtime.receipts.CompareAndDelete(key, receipt)
		}
		return true
	})
}
