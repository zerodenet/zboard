package metering

import "context"

// UsageProjections advances auxiliary daily statistics after authoritative
// metering facts and their durable projection intents have committed.
type UsageProjections struct{ Repository UsageProjectionRepository }

type UsageProjectionRepository interface {
	HasPending(context.Context) (bool, error)
	Drain(context.Context, int) (int, error)
}

func (s UsageProjections) HasPending(ctx context.Context) (bool, error) {
	return s.Repository.HasPending(ctx)
}

func (s UsageProjections) Drain(ctx context.Context, limit int) (int, error) {
	return s.Repository.Drain(ctx, limit)
}
