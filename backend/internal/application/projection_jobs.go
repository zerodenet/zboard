package application

import (
	"context"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"time"
)

const (
	UsageProjectionJobID   = "usage_projection"
	UsageProjectionJobName = "计量统计投影"
	NodeObservationJobID   = "node_observation"
	NodeObservationJobName = "节点观察投影"
)

// UsageProjectionJob is the existing bounded auxiliary projection stream.
// Returning its definition neither registers work nor opens the startup gate.
// RegisterPeriodicJob remains the single durable policy/identity composer.
func (s *Services) UsageProjectionJob() PeriodicJob {
	return PeriodicJob{
		ID: UsageProjectionJobID, Name: UsageProjectionJobName,
		Interval: time.Second, Timeout: 30 * time.Minute, Immediate: true,
		ReconcileAfterLoss: true, RetryFailures: true,
		Ready: func(ctx context.Context) (bool, error) { return s.UsageProjections.HasPending(ctx) },
		Run: func(ctx context.Context) error {
			n, err := s.UsageProjections.Drain(ctx, 200)
			return projectionBatchOutcome(n, 200, err)
		},
	}
}

// NodeObservationJob keeps observation material ownership and byte limits in
// the existing projection capability. It shares no spool, plugin or observer.
func (s *Services) NodeObservationJob() PeriodicJob {
	return PeriodicJob{
		ID: NodeObservationJobID, Name: NodeObservationJobName,
		Interval: time.Second, Timeout: 30 * time.Minute, Immediate: true,
		ReconcileAfterLoss: true, RetryFailures: true,
		Ready: func(ctx context.Context) (bool, error) { return s.NodeObservations.HasPending(ctx) },
		Run: func(ctx context.Context) error {
			n, err := s.NodeObservations.Drain(ctx, 32, 8*1024*1024)
			return projectionBatchOutcome(n, 32, err)
		},
	}
}

// Material retention is independent of apply and authority. Readiness and every
// retirement use the original bounded DB ownership/recovery executor.
func (s *Services) NodeObservationMaterialRetentionJob() PeriodicJob {
	return PeriodicJob{ID: "node_observation_material_retention", Name: "节点观察材料保留", Interval: time.Second, Timeout: time.Minute, Immediate: true, ReconcileAfterLoss: true, RetryFailures: true,
		Ready: func(ctx context.Context) (bool, error) { return s.NodeObservationMaterialRetention.HasPending(ctx) },
		Run: func(ctx context.Context) error {
			result, err := s.NodeObservationMaterialRetention.Retire(ctx, 32, 8*1024*1024)
			return projectionBatchOutcome(result.Inspected, 32, err)
		}}
}

// A full committed batch yields through the existing durable scheduler. The
// original Ready check still decides whether another claim is eligible; a final
// exactly-full batch does not create an empty execution or a busy retry loop.
// Partial/empty batches keep the original interval, and failures retain their
// original identity instead of being masked as successful continuation.
func projectionBatchOutcome(processed, limit int, err error) error {
	if err != nil {
		return err
	}
	if processed >= limit {
		return jobs.ErrContinue
	}
	return nil
}
