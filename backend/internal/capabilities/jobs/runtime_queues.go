package jobs

import (
	"context"
	"errors"
	"sync"
	"time"
)

const (
	AdminRuntimeQueue       = "admin_tasks"
	PublicationRuntimeQueue = "node_publish"
)

var ErrRuntimeQueueConflict = errors.New("runtime queue item cannot be retried in its current state")

type RuntimeQueueRetrySource interface {
	Retry(context.Context, uint, uint, string, time.Time) error
}

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
	now = now.UTC()
	var publication, admin RuntimeQueueSummary
	var publicationErr, adminErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		publication, publicationErr = s.Publication.Summary(ctx, now)
	}()
	go func() {
		defer wg.Done()
		admin, adminErr = s.Admin.Summary(ctx, now)
	}()
	wg.Wait()
	result := make([]RuntimeQueueSummary, 0, 2)
	if publicationErr == nil {
		result = append(result, publication)
	}
	if adminErr == nil {
		result = append(result, admin)
	}
	return result, errors.Join(publicationErr, adminErr)
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

func (s RuntimeQueues) RetryPublication(ctx context.Context, nodeID, actorID uint, actor string, now time.Time) error {
	if nodeID == 0 || actorID == 0 {
		return ErrInvalid
	}
	source, ok := s.Publication.(RuntimeQueueRetrySource)
	if !ok {
		return ErrInvalid
	}
	return source.Retry(ctx, nodeID, actorID, actor, now.UTC())
}
