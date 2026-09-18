package metering

import (
	"context"
	"time"
)

type EvaluationSnapshot struct {
	Policy Policy
	Source PolicySource
	State  State
}
type EvaluationSource interface {
	Snapshot(context.Context, uint) (EvaluationSnapshot, error)
	Candidates(context.Context, time.Time) ([]uint, error)
}
