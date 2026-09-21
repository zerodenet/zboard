package jobs

import "context"

type ResourceFenceRepository interface {
	HasUnresolvedResource(context.Context, string) (bool, error)
}

type ResourceFence struct{ Repository ResourceFenceRepository }

func (f ResourceFence) Pending(ctx context.Context, resource string) (bool, error) {
	if f.Repository == nil || resource == "" {
		return false, ErrInvalid
	}
	return f.Repository.HasUnresolvedResource(ctx, resource)
}
