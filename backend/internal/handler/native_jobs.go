package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
)

type nativeJobInput struct {
	Revision    string `json:"revision"`
	OperationID uint   `json:"operation_id"`
	ReviewRunID string `json:"review_run_id,omitempty"`
	Reviewer    uint   `json:"reviewer,omitempty"`
}

func (h *handlers) registerNativeJobs(runtime *jobs.Runtime) {
	if err := h.services.RegisterFairUseJobs(); err != nil {
		log.Printf("register Fair Use jobs: %v", err)
	}
	for _, kind := range []string{"certificate_operation", "dns_operation", "dns_reconcile"} {
		kind := kind
		_ = runtime.Register(jobs.Definition{ID: kind, Handler: kind, Owner: "system", Revision: "1", Timeout: nativeJobTimeout(kind)}, func(ctx context.Context, run jobs.Run) error {
			var input nativeJobInput
			if run.Owner != "system" || json.Unmarshal([]byte(run.Payload), &input) != nil || input.OperationID == 0 {
				return jobs.ErrInvalid
			}
			if kind == "dns_reconcile" {
				return h.services.DNSReconciliation(h).Reconcile(ctx, input.Reviewer, input.ReviewRunID)
			}
			operationKind := network.DNSOperation
			if kind == "certificate_operation" {
				h.executeManagedCertificateOperationContext(ctx, input.OperationID)
				operationKind = network.CertificateOperation
			} else {
				h.executeDNSOperationContext(ctx, input.OperationID)
			}
			state, err := h.services.NativeOperationStatus.Status(ctx, operationKind, input.OperationID)
			if err != nil {
				return err
			}
			if state == "succeeded" {
				return nil
			}
			if state == "failed" {
				return errors.New("resource operation failed; inspect operation history")
			}
			return jobs.ErrUncertain
		})
	}
	_ = runtime.Register(jobs.Definition{ID: network.NodeCleanupHandler, Handler: network.NodeCleanupHandler, Owner: "system", Revision: "1", Timeout: nativeJobTimeout(network.NodeCleanupHandler)}, h.executeNodeCleanupRun)
}

func nativeJobTimeout(kind string) time.Duration {
	if kind == network.NodeCleanupHandler {
		return 3 * time.Minute
	}
	if kind == "dns_operation" || kind == "dns_reconcile" {
		return time.Minute
	}
	return 30 * time.Minute
}
