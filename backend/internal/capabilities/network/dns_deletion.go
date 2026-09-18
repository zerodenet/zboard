package network

import (
	"context"
	"errors"
	"time"
)

var (
	ErrDNSDeletionRemoteIdentity = errors.New("DNS remote identity is incomplete or uncertain")
	ErrDNSDeletionProvider       = errors.New("unsupported DNS provider")
)

type DNSDeletionInput struct {
	RecordID, ProviderAccountID      uint
	ProviderZoneID, ProviderRecordID string
	ObservedHash                     string
	LastSyncedAt                     *time.Time
}
type DNSDeletionPreparation struct {
	DeleteRemote         bool
	ProviderKey          string
	CredentialCiphertext string
}
type DNSDeletionRepository interface {
	DNSDeletionAttempts(context.Context, uint) (int64, error)
	DNSDeletionProvider(context.Context, uint) (string, string, error)
}
type DNSDeletion struct{ Repository DNSDeletionRepository }

func (s DNSDeletion) Prepare(ctx context.Context, input DNSDeletionInput) (DNSDeletionPreparation, error) {
	if s.Repository == nil || input.RecordID == 0 || input.ProviderAccountID == 0 {
		return DNSDeletionPreparation{}, ErrDNSDeletionRemoteIdentity
	}
	if input.ProviderZoneID == "" && input.ProviderRecordID == "" {
		attempts, err := s.Repository.DNSDeletionAttempts(ctx, input.RecordID)
		if err != nil {
			return DNSDeletionPreparation{}, err
		}
		if attempts > 0 || input.LastSyncedAt != nil || input.ObservedHash != "" {
			return DNSDeletionPreparation{}, ErrDNSDeletionRemoteIdentity
		}
		return DNSDeletionPreparation{}, nil
	}
	if input.ProviderZoneID == "" || input.ProviderRecordID == "" {
		return DNSDeletionPreparation{}, ErrDNSDeletionRemoteIdentity
	}
	provider, credential, err := s.Repository.DNSDeletionProvider(ctx, input.ProviderAccountID)
	if err != nil {
		return DNSDeletionPreparation{}, err
	}
	if provider != "cloudflare" {
		return DNSDeletionPreparation{}, ErrDNSDeletionProvider
	}
	return DNSDeletionPreparation{DeleteRemote: true, ProviderKey: provider, CredentialCiphertext: credential}, nil
}
