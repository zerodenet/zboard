package handler

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
)

const credentialExpiryReconcileInterval = 20 * time.Second

var credentialExpiryWorkerRegistry sync.Map

type credentialExpiryWorkerRuntime struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func (h *handlers) StartCredentialExpiryWorker() {
	if h == nil || h.db == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	runtime := &credentialExpiryWorkerRuntime{cancel: cancel, done: make(chan struct{})}
	if _, loaded := credentialExpiryWorkerRegistry.LoadOrStore(h, runtime); loaded {
		cancel()
		return
	}
	go func() {
		defer close(runtime.done)
		runCredentialExpiryWorker(ctx, credentialExpiryReconcileInterval, func(now time.Time) error {
			return h.runExpiredCredentialReconciliation(now)
		}, func(err error) {
			log.Printf("credential expiry reconciliation failed: %v", err)
		})
	}()
}

func (h *handlers) CloseCredentialExpiryWorker() {
	value, ok := credentialExpiryWorkerRegistry.LoadAndDelete(h)
	if !ok {
		return
	}
	runtime := value.(*credentialExpiryWorkerRuntime)
	runtime.cancel()
	<-runtime.done
}

func (h *handlers) runExpiredCredentialReconciliation(now time.Time) error {
	started := now.UTC()
	if started.IsZero() {
		started = time.Now().UTC()
	}
	if err := expireSubscriptions(h.db, 0, started); err != nil {
		return err
	}
	var expired []model.ProtocolCredential
	if err := h.db.Where("status = ? AND updated_at >= ?", "expired", started).Find(&expired).Error; err != nil {
		return err
	}
	seen := map[uint]struct{}{}
	for _, credential := range expired {
		if _, exists := seen[credential.NodeID]; exists {
			continue
		}
		seen[credential.NodeID] = struct{}{}
		h.scheduleNodeConfigPublish(credential.NodeID, credential.ProtocolEndpointID, 0)
	}
	return nil
}

func runCredentialExpiryWorker(ctx context.Context, interval time.Duration, reconcile func(time.Time) error, onError func(error)) {
	if ctx == nil || reconcile == nil {
		return
	}
	if interval <= 0 {
		interval = credentialExpiryReconcileInterval
	}
	run := func() {
		if err := reconcile(time.Now().UTC()); err != nil && onError != nil {
			onError(err)
		}
	}

	run()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}
