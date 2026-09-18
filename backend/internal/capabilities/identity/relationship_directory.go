package identity

import (
	"context"
	"errors"
)

var ErrRelationshipDirectoryUnavailable = errors.New("identity relationship directory unavailable")

type RelationshipDirectoryRepository interface {
	Account(context.Context, uint) (PublicAccount, error)
	Bindings(context.Context, uint) ([]ExternalBinding, error)
}

// RelationshipDirectory is the read boundary shared by plugin identity and
// operator adapters. It returns account state without granting authentication.
type RelationshipDirectory struct {
	Repository RelationshipDirectoryRepository
}

func (s RelationshipDirectory) Account(ctx context.Context, id uint) (PublicAccount, error) {
	if s.Repository == nil || id == 0 {
		return PublicAccount{}, ErrRelationshipDirectoryUnavailable
	}
	return s.Repository.Account(ctx, id)
}

func (s RelationshipDirectory) Bindings(ctx context.Context, userID uint) ([]ExternalBinding, error) {
	if s.Repository == nil || userID == 0 {
		return nil, ErrRelationshipDirectoryUnavailable
	}
	return s.Repository.Bindings(ctx, userID)
}
