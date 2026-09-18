package identitystore

import (
	"context"
	"errors"

	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type RelationshipDirectory struct{ DB *gorm.DB }

func (s RelationshipDirectory) Account(ctx context.Context, id uint) (identity.PublicAccount, error) {
	var row model.User
	err := s.DB.WithContext(ctx).Select("id", "email", "is_admin", "status").First(&row, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return identity.PublicAccount{}, identity.ErrUnavailable
	}
	return publicAccount(row), err
}

func (s RelationshipDirectory) Bindings(ctx context.Context, userID uint) ([]identity.ExternalBinding, error) {
	var rows []model.ExternalIdentity
	if err := s.DB.WithContext(ctx).Where("user_id = ?", userID).Order("created_at desc").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]identity.ExternalBinding, 0, len(rows))
	for _, row := range rows {
		out = append(out, identity.ExternalBinding{ID: row.ID, UserID: row.UserID, Authority: row.PluginID, Publisher: row.Publisher, Issuer: row.Issuer, Subject: row.Subject, CreatedAt: row.CreatedAt})
	}
	return out, nil
}

var _ identity.RelationshipDirectoryRepository = RelationshipDirectory{}
