package handler

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	capabilityjobs "github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
)

const nodePublishPollInterval = 5 * time.Second

// StartNodePublishWorker always scans durable state on startup, even when no
// request has been enqueued in this process. The shared jobs runtime owns
// polling; producers only persist intent and never signal a private scheduler.
func (h *handlers) StartNodePublishWorker() { h.startNodePublishWorker(h.publishQueuedNode) }

func (h *handlers) publishQueuedNode(ctx context.Context, item model.NodeConfigPublish) error {
	target, active, err := h.services.NodePublicationTargets().Resolve(ctx, publicationCapability(item))
	if err != nil || !active {
		return err
	}
	_, _, err = h.publishNodeConfigForNode(ctx, target.NodeID, target.EndpointID, target.RequestedBy)
	return err
}

func (h *handlers) startNodePublishWorker(publish func(context.Context, model.NodeConfigPublish) error) {
	for index := 0; index < nodeConfigPublishWorkerCount; index++ {
		id := fmt.Sprintf("node_publish_%d", index)
		h.startScheduledJob(id, nodePublishPollInterval, func(ctx context.Context) error {
			item, ok, err := h.services.NodePublication(nil).Claim(ctx, time.Now().UTC())
			if err != nil {
				return err
			}
			if !ok {
				return nil
			}
			modelItem := publicationModel(item)
			if err := h.executeNodePublish(ctx, modelItem, func(ctx context.Context) error { return publish(ctx, modelItem) }); err != nil {
				return err
			}
			return capabilityjobs.ErrContinue
		})
	}
}
func (h *handlers) CloseNodePublishWorker() {
	for index := 0; index < nodeConfigPublishWorkerCount; index++ {
		h.closeScheduledJob(fmt.Sprintf("node_publish_%d", index))
	}
}

func (h *handlers) executeNodePublish(parent context.Context, item model.NodeConfigPublish, publish func(context.Context) error) error {
	finishObservation := h.observeJob("node_publish", "queue", nodePublishPollInterval, nodeConfigPublishWorkerCount)
	executor := publicationExecutorFunc(func(ctx context.Context, _ network.Publication) error { return publish(ctx) })
	failure := h.services.NodePublication(executor).Execute(parent, publicationCapability(item))
	defer func() { finishObservation(failure) }()
	if errors.Is(failure, network.ErrPublicationLeaseLost) || errors.Is(failure, network.ErrPublicationFinalize) {
		log.Printf("retain node publication for lease recovery: node_id=%d error=%v", item.NodeID, failure)
	} else if failure != nil {
		log.Printf("node publication scheduled for retry: node_id=%d error=%v", item.NodeID, failure)
	}
	return failure
}

type publicationExecutorFunc func(context.Context, network.Publication) error

func (f publicationExecutorFunc) PublishNodeConfiguration(ctx context.Context, item network.Publication) error {
	return f(ctx, item)
}
