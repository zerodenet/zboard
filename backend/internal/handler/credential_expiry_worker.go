package handler

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	credentialExpiryReconcileInterval  = 20 * time.Second
	credentialExpiryReconcileBatchSize = 200
)

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
	if h.backgroundWorkPaused() {
		return nil
	}
	started := now.UTC()
	if started.IsZero() {
		started = time.Now().UTC()
	}
	expired, err := expireDueSubscriptionCredentials(h.db, started, credentialExpiryReconcileBatchSize)
	if err != nil {
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

// expireDueSubscriptionCredentials handles only time-based expiry. Traffic
// accounting changes a subscription to expired in the transaction that charges
// its final bytes, so scanning every active subscription for quota exhaustion
// here was redundant. Lock and process a bounded due batch instead of issuing
// two global UPDATE statements every worker interval.
func expireDueSubscriptionCredentials(db *gorm.DB, now time.Time, limit int) ([]model.ProtocolCredential, error) {
	if db == nil {
		return nil, fmt.Errorf("database is required")
	}
	if limit <= 0 {
		limit = credentialExpiryReconcileBatchSize
	}
	var expired []model.ProtocolCredential
	err := db.Transaction(func(tx *gorm.DB) error {
		var subscriptions []model.Subscription
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id").
			Where("status = ? AND end_at <= ?", subStatusActive, now).
			Order("end_at asc, id asc").
			Limit(limit).
			Find(&subscriptions).Error; err != nil {
			return err
		}
		if len(subscriptions) == 0 {
			return nil
		}
		ids := make([]uint, 0, len(subscriptions))
		for _, subscription := range subscriptions {
			ids = append(ids, subscription.ID)
		}
		if err := tx.Model(&model.Subscription{}).
			Where("id IN ? AND status = ? AND end_at <= ?", ids, subStatusActive, now).
			Updates(map[string]interface{}{"status": subStatusExpired, "updated_at": now}).Error; err != nil {
			return err
		}
		if err := tx.Where("status IN ? AND subscription_id IN ?", []string{protocolCredentialStatusActive, protocolCredentialStatusPrepared}, ids).
			Find(&expired).Error; err != nil {
			return err
		}
		if len(expired) == 0 {
			return nil
		}
		return tx.Model(&model.ProtocolCredential{}).
			Where("id IN ?", protocolCredentialIDs(expired)).
			Updates(map[string]interface{}{"status": "expired", "updated_at": now}).Error
	})
	if err != nil {
		return nil, err
	}
	return expired, nil
}

func protocolCredentialIDs(credentials []model.ProtocolCredential) []uint {
	ids := make([]uint, 0, len(credentials))
	for _, credential := range credentials {
		ids = append(ids, credential.ID)
	}
	return ids
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
