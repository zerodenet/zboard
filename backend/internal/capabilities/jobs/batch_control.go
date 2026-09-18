package jobs

import "context"

type BatchControlRepository interface {
	Queue(context.Context, uint, uint) error
}
type BatchControl struct{ Repository BatchControlRepository }

func (s BatchControl) Queue(ctx context.Context, actor, id uint) error {
	if actor == 0 {
		return ErrBatchPermission
	}
	if id == 0 {
		return ErrBatchQuery
	}
	return s.Repository.Queue(ctx, actor, id)
}
