package navigation

import "context"

// Repository is the host-owned storage hook. Consumers never receive DB handles.
type Repository interface {
	Read(context.Context, string) (Snapshot, error)
	Save(context.Context, uint, string, Snapshot) error
	HasDocuments(context.Context) (bool, error)
}
type Service struct{ Repository Repository }

func (s Service) Read(ctx context.Context, surface string) (Snapshot, error) {
	return s.Repository.Read(ctx, surface)
}
func (s Service) Save(ctx context.Context, actor uint, surface string, snapshot Snapshot) error {
	return s.Repository.Save(ctx, actor, surface, snapshot)
}
func (s Service) HasDocuments(ctx context.Context) (bool, error) {
	return s.Repository.HasDocuments(ctx)
}
