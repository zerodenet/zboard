package networkstore

import (
	"context"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type NativeOperationStatus struct{ DB *gorm.DB }

func (s NativeOperationStatus) NativeOperationStatus(ctx context.Context, kind network.OperationKind, id uint) (string, error) {
	var row struct{ Status string }
	query := s.DB.WithContext(ctx)
	if kind == network.CertificateOperation {
		query = query.Model(&model.CertificateOperation{})
	} else {
		query = query.Model(&model.ProviderOperation{})
	}
	err := query.Select("status").Where("id = ?", id).Take(&row).Error
	return row.Status, err
}

var _ network.NativeOperationStatusRepository = NativeOperationStatus{}
