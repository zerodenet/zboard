package application

import (
	"context"

	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"gorm.io/gorm"
)

type BufferedEventProjection struct {
	Nodes    []network.NodeObservation
	Flows    []metering.FlowSample
	Coverage []metering.NodeCoverageEvent
}

type BufferedEventProjectionResult struct {
	Exhausted   []metering.FlowAccountingResult
	CoverageErr error
}

// ProjectBufferedEvents keeps node observation and billable flow settlement in
// one authority transaction. Coverage is auxiliary observability state and is
// intentionally committed separately so it remains fail-open.
func (s *Services) ProjectBufferedEvents(ctx context.Context, input BufferedEventProjection, cipher metering.CredentialDecryptor) (BufferedEventProjectionResult, error) {
	result := BufferedEventProjectionResult{}
	err := s.Identity.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, observation := range input.Nodes {
			if err := ProjectNodeObservation(tx, observation); err != nil {
				return err
			}
		}
		var err error
		result.Exhausted, err = ApplyFlowBatch(tx, input.Flows, cipher)
		return err
	})
	if err != nil {
		return result, err
	}
	result.CoverageErr = s.Identity.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return ProjectFairUseCoverage(tx, input.Coverage)
	})
	return result, nil
}
