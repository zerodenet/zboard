package commerce

import (
	"context"
	"errors"
)

var ErrOrderPermission = errors.New("order operation is not authorized")
var ErrOrderNotCancelable = errors.New("order not cancelable")

type OrderCancellationRepository interface {
	Cancel(context.Context, uint, uint, bool, bool) (Order, error)
}
type OrderCancellation struct{ Repository OrderCancellationRepository }

func (s OrderCancellation) Owned(ctx context.Context, actor, id uint) (Order, error) {
	if actor == 0 {
		return Order{}, ErrOrderPermission
	}
	if id == 0 {
		return Order{}, ErrNotFound
	}
	return s.Repository.Cancel(ctx, actor, id, false, false)
}
func (s OrderCancellation) Administrative(ctx context.Context, actor, id uint, force bool) (Order, error) {
	if actor == 0 {
		return Order{}, ErrOrderPermission
	}
	if id == 0 {
		return Order{}, ErrNotFound
	}
	return s.Repository.Cancel(ctx, actor, id, true, force)
}
