package handler

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

const nodePublishPollInterval = 5 * time.Second

type nodePublishScheduler struct {
	mu     sync.Mutex
	wake   chan struct{}
	cancel context.CancelFunc
	done   chan struct{}
}

func newNodePublishScheduler() *nodePublishScheduler {
	return &nodePublishScheduler{wake: make(chan struct{}, 1)}
}
func (s *nodePublishScheduler) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
func (h *handlers) publishScheduler() *nodePublishScheduler {
	h.nodePublishSchedulerOnce.Do(func() {
		if h.nodePublishScheduler == nil {
			h.nodePublishScheduler = newNodePublishScheduler()
		}
	})
	return h.nodePublishScheduler
}

// StartNodePublishWorker always scans durable state on startup, even when no
// request has been enqueued in this process. A single ticker drives idle polling.
func (h *handlers) StartNodePublishWorker() { h.startNodePublishWorker(h.publishQueuedNode) }

func (h *handlers) publishQueuedNode(ctx context.Context, item model.NodeConfigPublish) error {
	var node model.Node
	if err := h.db.WithContext(ctx).Select("id", "lifecycle_status").First(&node, item.NodeID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	if node.LifecycleStatus == resourceStatusDeleting {
		return nil
	}
	// The trigger can have moved or been deleted while this task was pending.
	var endpoint model.ProtocolEndpoint
	read := h.db.WithContext(ctx).Select("id").Where("node_id = ?", item.NodeID).Order("id").Limit(1).Find(&endpoint)
	if read.Error != nil {
		return read.Error
	}
	if endpoint.ID == 0 {
		return errors.New("node publication is waiting for a protocol endpoint")
	}
	if item.RequestedBy != 0 {
		var count int64
		if err := h.db.WithContext(ctx).Model(&model.User{}).Where("id = ?", item.RequestedBy).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			item.RequestedBy = 0
		}
	}
	_, _, err := h.publishNodeConfigForNode(ctx, item.NodeID, endpoint.ID, item.RequestedBy)
	return err
}

func (h *handlers) startNodePublishWorker(publish func(context.Context, model.NodeConfigPublish) error) {
	s := h.publishScheduler()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel, s.done = cancel, make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < nodeConfigPublishWorkerCount; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); h.runNodePublishWorker(ctx, s, publish) }()
	}
	go func() {
		ticker := time.NewTicker(nodePublishPollInterval)
		defer ticker.Stop()
		defer close(s.done)
		s.signal()
		for {
			select {
			case <-ctx.Done():
				wg.Wait()
				return
			case <-ticker.C:
				s.signal()
			}
		}
	}()
}
func (h *handlers) CloseNodePublishWorker() {
	s := h.publishScheduler()
	s.mu.Lock()
	if s.cancel == nil {
		s.mu.Unlock()
		return
	}
	cancel, done := s.cancel, s.done
	s.mu.Unlock()
	cancel()
	<-done
	s.mu.Lock()
	if s.done == done {
		s.cancel = nil
	}
	s.mu.Unlock()
}
func (h *handlers) runNodePublishWorker(ctx context.Context, s *nodePublishScheduler, publish func(context.Context, model.NodeConfigPublish) error) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
			if h.backgroundWorkPaused() {
				continue
			}
			item, ok, err := claimNodeConfigPublish(h.db.WithContext(ctx), time.Now().UTC())
			if err != nil {
				if ctx.Err() == nil {
					log.Printf("claim node publication: %v", err)
				}
				continue
			}
			if !ok {
				continue
			}
			s.signal()
			h.executeNodePublish(ctx, item, func(ctx context.Context) error { return publish(ctx, item) })
			s.signal()
		}
	}
}

func (h *handlers) executeNodePublish(parent context.Context, item model.NodeConfigPublish, publish func(context.Context) error) {
	ctx, cancel := context.WithTimeout(parent, nodeConfigPublishTimeout)
	defer cancel()
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(nodePublishLease / 3)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				// Keep ownership until the executor actually returns, including during shutdown.
				renewalCtx, release := context.WithTimeout(context.Background(), 5*time.Second)
				renewed := h.db.WithContext(renewalCtx).Model(&model.NodeConfigPublish{}).
					Where("node_id = ? AND lease_token = ?", item.NodeID, item.LeaseToken).
					Update("lease_until", time.Now().UTC().Add(nodePublishLease))
				release()
				if renewed.Error != nil || renewed.RowsAffected != 1 {
					cancel()
					return
				}
			}
		}
	}()
	failure := publish(ctx)
	close(stop)
	<-done
	completionCtx, release := context.WithTimeout(context.Background(), 5*time.Second)
	defer release()
	if err := finishNodeConfigPublish(h.db.WithContext(completionCtx), item, time.Now().UTC(), failure); err != nil {
		log.Printf("retain node publication for lease recovery: node_id=%d error=%v", item.NodeID, err)
	} else if failure != nil {
		log.Printf("node publication scheduled for retry: node_id=%d error=%v", item.NodeID, failure)
	}
}

func (h *handlers) scheduleNodeConfigPublish(nodeID, endpointID, requestedBy uint) error {
	if err := enqueueNodeConfigPublish(h.db, nodeID, endpointID, requestedBy); err != nil {
		log.Printf("persist node publication: node_id=%d error=%v", nodeID, err)
		return err
	}
	h.publishScheduler().signal()
	return nil
}
