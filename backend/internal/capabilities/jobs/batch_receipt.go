package jobs

import "time"

// BatchReceipt is the transport-neutral projection of a persisted operations batch.
type BatchReceipt struct {
	ID             uint       `json:"id"`
	Type           string     `json:"type"`
	Scope          string     `json:"scope"`
	Content        string     `json:"content"`
	Status         int16      `json:"status"`
	Errors         string     `json:"errors"`
	Total          int64      `json:"total"`
	Current        int64      `json:"current"`
	IdempotencyKey string     `json:"idempotency_key"`
	Priority       int        `json:"priority"`
	ScheduledAt    *time.Time `json:"scheduled_at"`
	StartedAt      *time.Time `json:"started_at"`
	FinishedAt     *time.Time `json:"finished_at"`
	Attempts       int        `json:"attempts"`
	MaxAttempts    int        `json:"max_attempts"`
	LockedBy       string     `json:"locked_by"`
	LockedUntil    *time.Time `json:"locked_until"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}
