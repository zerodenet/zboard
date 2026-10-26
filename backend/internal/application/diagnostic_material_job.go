package application

import (
	"context"
	"time"
)

func (s *Services) DiagnosticMaterialRetentionJob() PeriodicJob {
	return PeriodicJob{ID: "traffic_diagnostic_retention", Name: "流量诊断材料保留", Interval: time.Second, Timeout: time.Minute, Immediate: true, ReconcileAfterLoss: true, RetryFailures: true,
		Ready: func(ctx context.Context) (bool, error) { return s.DiagnosticMaterialRetention.HasPending(ctx) },
		Run: func(ctx context.Context) error {
			_, err := s.DiagnosticMaterialRetention.Retire(ctx, 32, 256*1024)
			return err
		},
	}
}
