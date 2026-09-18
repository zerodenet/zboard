package jobs

import (
	"context"
	"time"
)

const (
	AdminRuntimeQueue       = "admin_tasks"
	PublicationRuntimeQueue = "node_publish"
)

type RuntimeQueueSummary struct {
	OldestAt *time.Time `json:"oldest_at,omitempty"`
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Pending  int64      `json:"pending"`
	Running  int64      `json:"running"`
	Delayed  int64      `json:"delayed"`
	Stale    int64      `json:"stale"`
	Drafts   int64      `json:"drafts"`
	Failed   int64      `json:"failed"`
}

type RuntimeQueueItem struct {
	ID            uint       `json:"id"`
	Kind          string     `json:"kind"`
	State         string     `json:"state"`
	Attempts      int        `json:"attempts"`
	CreatedAt     time.Time  `json:"created_at"`
	NextAttemptAt *time.Time `json:"next_attempt_at"`
	LeaseUntil    *time.Time `json:"lease_until"`
	LastError     string     `json:"last_error"`
}

type RuntimeQueuePage struct {
	Items []RuntimeQueueItem
	Total int64
}

type RuntimeQueueSource interface {
	Summary(context.Context, time.Time) (RuntimeQueueSummary, error)
	Page(context.Context, time.Time, int, int) (RuntimeQueuePage, error)
}

type RuntimeQueues struct {
	Admin       RuntimeQueueSource
	Publication RuntimeQueueSource
}

func (s RuntimeQueues) Summaries(ctx context.Context, now time.Time) ([]RuntimeQueueSummary, error) {
	if s.Admin == nil || s.Publication == nil {
		return nil, ErrInvalid
	}
	publication, err := s.Publication.Summary(ctx, now.UTC())
	if err != nil {
		return nil, err
	}
	admin, err := s.Admin.Summary(ctx, now.UTC())
	if err != nil {
		return nil, err
	}
	return []RuntimeQueueSummary{publication, admin}, nil
}

func (s RuntimeQueues) Page(ctx context.Context, name string, now time.Time, limit, offset int) (RuntimeQueuePage, error) {
	if limit < 1 || limit > 50 || offset < 0 || offset > 1000000 {
		return RuntimeQueuePage{}, ErrInvalid
	}
	switch name {
	case AdminRuntimeQueue:
		if s.Admin == nil {
			return RuntimeQueuePage{}, ErrInvalid
		}
		return s.Admin.Page(ctx, now.UTC(), limit, offset)
	case PublicationRuntimeQueue:
		if s.Publication == nil {
			return RuntimeQueuePage{}, ErrInvalid
		}
		return s.Publication.Page(ctx, now.UTC(), limit, offset)
	default:
		return RuntimeQueuePage{}, ErrInvalid
	}
}
