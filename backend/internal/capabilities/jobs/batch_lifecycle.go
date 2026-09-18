package jobs

import "context"

// BatchLifecycle is called by the admitted host runtime, never directly by a
// plugin or integration. It owns persistent lifecycle state, not worker creation.
type BatchLifecycleRepository interface {
	Pending(context.Context) (bool, error)
	Ready(context.Context, int) ([]uint, error)
	Renew(context.Context, uint, string) error
	Finish(context.Context, uint, string, []string) error
}
type BatchLifecycle struct{ Repository BatchLifecycleRepository }

func (s BatchLifecycle) Pending(ctx context.Context) (bool, error) {
	if s.Repository == nil {
		return false, ErrBatchQuery
	}
	return s.Repository.Pending(ctx)
}

func (s BatchLifecycle) Ready(ctx context.Context, limit int) ([]uint, error) {
	if limit < 1 || limit > 100 {
		return nil, ErrBatchQuery
	}
	return s.Repository.Ready(ctx, limit)
}
func (s BatchLifecycle) Renew(ctx context.Context, id uint, token string) error {
	if id == 0 || token == "" {
		return ErrLeaseLost
	}
	return s.Repository.Renew(ctx, id, token)
}
func (s BatchLifecycle) Finish(ctx context.Context, id uint, token string, failures []string) error {
	if id == 0 || token == "" {
		return ErrLeaseLost
	}
	if len(failures) > 20 {
		failures = failures[:20]
	}
	return s.Repository.Finish(ctx, id, token, failures)
}
