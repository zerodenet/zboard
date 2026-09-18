package application

import (
	"context"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"gorm.io/gorm"
	"time"
)

// ApplyFairUseEvaluation persists an observation produced by the internal
// evaluator. This method is not an external or plugin capability endpoint.
func (s *Services) ApplyFairUseEvaluation(ctx context.Context, subscription uint, policy metering.Policy, observation metering.EvaluationObservation, now time.Time) (metering.EvaluationChange, error) {
	return (meteringstore.EvaluationWriter{DB: s.Identity.db}).Apply(ctx, subscription, policy, observation, now)
}

func (s *Services) ObserveFairUseCoverage(ctx context.Context, event metering.NodeCoverageEvent) error {
	return (meteringstore.CoverageWriter{DB: s.Identity.db}).Observe(ctx, event)
}

// ProjectFairUseCoverage participates in the existing ingestion transaction.
func ProjectFairUseCoverage(tx *gorm.DB, events []metering.NodeCoverageEvent) error {
	return meteringstore.ProjectCoverageBatch(tx, events)
}
