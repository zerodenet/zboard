package jobs

import (
	"context"
	"time"
)

type AttemptView struct {
	Number     int        `json:"attempt_number"`
	Worker     string     `json:"worker"`
	State      State      `json:"state"`
	StartedAt  time.Time  `json:"started_at"`
	ExpiresAt  time.Time  `json:"expires_at"`
	FinishedAt *time.Time `json:"finished_at"`
}

type HistoryRepository interface {
	History(context.Context, int, int) ([]Run, int64, error)
	Attempts(context.Context, string, int, int) ([]AttemptView, int64, error)
}

func (s HistoryQueries) Attempts(ctx context.Context, runID string, limit, offset int) ([]AttemptView, int64, error) {
	if s.Repository == nil || runID == "" || len(runID) > 36 || limit < 1 || limit > 50 || offset < 0 || offset > 1000000 {
		return nil, 0, ErrInvalid
	}
	return s.Repository.Attempts(ctx, runID, limit, offset)
}

type HistoryQueries struct{ Repository HistoryRepository }

func (s HistoryQueries) Page(ctx context.Context, limit, offset int) ([]Run, int64, error) {
	if s.Repository == nil || limit < 1 || limit > 50 || offset < 0 || offset > 1000000 {
		return nil, 0, ErrInvalid
	}
	return s.Repository.History(ctx, limit, offset)
}
