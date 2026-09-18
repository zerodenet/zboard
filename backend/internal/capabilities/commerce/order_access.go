package commerce

import (
	"context"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
)

type OrderAccessRepository interface {
	Read(context.Context, uint, uint) (entitlements.AccessView, error)
}
type OrderAccess struct{ Repository OrderAccessRepository }

func (s OrderAccess) Read(ctx context.Context, actor, id uint) (entitlements.AccessView, error) {
	if actor == 0 {
		return entitlements.AccessView{}, ErrOrderPermission
	}
	if id == 0 {
		return entitlements.AccessView{}, ErrNotFound
	}
	return s.Repository.Read(ctx, actor, id)
}
