package networkstore

import (
	"context"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type DNSDeletion struct{ DB *gorm.DB }

func (s DNSDeletion) DNSDeletionAttempts(ctx context.Context, recordID uint) (int64, error) {
	var attempts int64
	err := s.DB.WithContext(ctx).Model(&model.ProviderOperation{}).
		Where("resource_type = ? AND resource_id = ? AND phase IN ?", "dns_record", recordID, []string{"applying_record", "persisting", "completed"}).Count(&attempts).Error
	return attempts, err
}

func (s DNSDeletion) DNSDeletionProvider(ctx context.Context, accountID uint) (string, string, error) {
	var row model.ProviderAccount
	err := s.DB.WithContext(ctx).Select("provider_key", "credential_ciphertext").First(&row, accountID).Error
	return row.ProviderKey, row.CredentialCiphertext, err
}

var _ network.DNSDeletionRepository = DNSDeletion{}
