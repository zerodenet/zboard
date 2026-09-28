package commerce

import "context"

type CatalogDeletionRepository interface {
	DeletePlan(context.Context, uint, uint) error
	DeleteSKU(context.Context, uint, uint) error
}
type CatalogDeletion struct{ Repository CatalogDeletionRepository }

func (s CatalogDeletion) DeletePlan(ctx context.Context, actor, id uint) error {
	if actor == 0 {
		return ErrPermission
	}
	if id == 0 {
		return ErrNotFound
	}
	return s.Repository.DeletePlan(ctx, actor, id)
}
func (s CatalogDeletion) DeleteSKU(ctx context.Context, actor, id uint) error {
	if actor == 0 {
		return ErrPermission
	}
	if id == 0 {
		return ErrNotFound
	}
	return s.Repository.DeleteSKU(ctx, actor, id)
}
