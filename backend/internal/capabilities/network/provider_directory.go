package network

import "context"

type ProviderDeletionBlocked struct{ Blockers map[string]int64 }

func (e *ProviderDeletionBlocked) Error() string {
	return "供应商账户仍有未完成或待核验任务"
}

type ProviderDeleted struct {
	ID                      uint `json:"id"`
	Deleted                 bool `json:"deleted"`
	ExternalAccountRetained bool `json:"external_account_retained"`
}
type ProviderDirectoryStore interface {
	ListProviders(context.Context, uint) ([]ProviderAccount, error)
	DeleteProvider(context.Context, uint, uint) error
}
type ProviderDirectory struct{ Store ProviderDirectoryStore }

func (s ProviderDirectory) List(ctx context.Context, actor uint) ([]ProviderAccount, error) {
	if actor == 0 {
		return nil, ErrProviderPermission
	}
	return s.Store.ListProviders(ctx, actor)
}
func (s ProviderDirectory) Delete(ctx context.Context, actor, id uint) (ProviderDeleted, error) {
	if actor == 0 {
		return ProviderDeleted{}, ErrProviderPermission
	}
	if id == 0 {
		return ProviderDeleted{}, ErrProviderNotFound
	}
	if err := s.Store.DeleteProvider(ctx, actor, id); err != nil {
		return ProviderDeleted{}, err
	}
	return ProviderDeleted{ID: id, Deleted: true, ExternalAccountRetained: true}, nil
}
