package networkstore

import (
	"context"
	"errors"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type EventCredentials struct{ DB *gorm.DB }

func (s EventCredentials) EventNode(ctx context.Context, nodeID uint) (network.EventNode, error) {
	var row model.Node
	err := s.DB.WithContext(ctx).Select("id", "node_credential").Where("id = ? AND node_credential_revoked_at IS NULL AND node_credential <> ''", nodeID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return network.EventNode{}, network.ErrEventCredentialUnavailable
	}
	if err != nil {
		return network.EventNode{}, err
	}
	return network.EventNode{ID: row.ID, Credential: row.NodeCredential}, nil
}
