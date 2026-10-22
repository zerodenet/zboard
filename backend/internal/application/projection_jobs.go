package application

import (
	"context"
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
		Run:   func(ctx context.Context) error { _, err := s.UsageProjections.Drain(ctx, 200); return err },
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
		Run:   func(ctx context.Context) error { _, err := s.NodeObservations.Drain(ctx, 32, 8*1024*1024); return err },
	}
}
