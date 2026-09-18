package jobs

import (
	"context"
	"time"
	_ "time/tzdata"
)

type MisfirePolicy string

const (
	// MisfireFireOnce collapses all overdue slots into the latest planned slot.
	MisfireFireOnce MisfirePolicy = "fire_once"
	// MisfireSkip advances to the first future slot without creating a Run.
	MisfireSkip MisfirePolicy = "skip"
)

// Definition identifies one periodic execution stream. Revision is a domain
// fencing value (for example a plugin generation plus configuration revision).
type Definition struct {
	ID             string
	Owner          string
	Name           string
	Handler        string
	ExecutionGroup string
	Resource       string
	Revision       string
	Interval       time.Duration
	Timeout        time.Duration
	MaxAttempts    int
	RetryBackoff   time.Duration
	Immediate      bool
	// TimeZone records the IANA zone used by the owner to calculate FirstPlannedAt.
	// Durable planned timestamps are always UTC.
	TimeZone       string
	MisfirePolicy  MisfirePolicy
	FirstPlannedAt time.Time
	DispatchLane   string
	// Only core reconcilers with their own durable/idempotent business state may
	// resume scanning after a lost execution. Plugin tasks default to verification.
	ReconcileAfterLoss bool
	// Ephemeral admission hint from the domain's read-only work-availability
	// probe. Plans remain visible without writing empty execution records.
	SuppressDispatch bool
}

type ScheduleView struct {
	Definition
	NextAt         time.Time
	Runs           uint64
	Failures       uint64
	MissedRuns     uint64
	State          State
	LastState      State
	LastStartedAt  *time.Time
	LastFinishedAt *time.Time
}

type SchedulingStore interface {
	Store
	Schedule(context.Context, Definition) error
	Schedules(context.Context) ([]ScheduleView, error)
	CancelHandler(context.Context, string) error
}
