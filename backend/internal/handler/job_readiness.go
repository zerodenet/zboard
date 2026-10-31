package handler

import (
	"context"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/application"
)

func (h *handlers) jobReadiness(id string) func(context.Context) (bool, error) {
	if id == "registration_messages" {
		return h.services.RegistrationMessages(h.credentialCipher).Pending
	}
	if id == application.FlowStartStatisticsJobID {
		return h.services.FlowStartStatisticsJob().Ready
	}
	if id == application.PrincipalStatisticsJobID {
		return h.services.PrincipalStatisticsJob().Ready
	}
	if id == application.PrincipalScopeRetentionJobID {
		return h.services.PrincipalScopeRetentionJob().Ready
	}
	if id == "traffic_diagnostic_retention" {
		return h.services.DiagnosticMaterialRetentionJob().Ready
	}
	if id == "node_observation_material_retention" {
		return h.services.NodeObservationMaterialRetentionJob().Ready
	}
	if id == application.NodeObservationJobID {
		return h.services.NodeObservationJob().Ready
	}
	if id == application.UsageProjectionJobID {
		return h.services.UsageProjectionJob().Ready
	}
	if id != "admin_tasks" && id != "event_consumer" && !strings.HasPrefix(id, "node_publish_") {
		return nil
	}
	return func(ctx context.Context) (bool, error) {
		if id == "event_consumer" {
			value, ok := zeroEventRuntimeRegistry.Load(h)
			if !ok {
				return false, nil
			}
			return value.(*zeroEventRuntime).spool.Status().PendingEvents > 0, nil
		}
		if id == "admin_tasks" {
			return h.services.BatchLifecycle().Pending(ctx)
		}
		return h.services.NodePublication(nil).Pending(ctx, time.Now().UTC())
	}
}
