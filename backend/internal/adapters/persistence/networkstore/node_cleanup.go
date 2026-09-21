package networkstore

import (
	"context"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type NodeCleanupGuard struct{ DB *gorm.DB }

func (s NodeCleanupGuard) TargetInUse(ctx context.Context, host string, port int, fingerprint string) (bool, error) {
	query := s.DB.WithContext(ctx).Model(&model.Node{}).Where("ssh_host = ? AND ssh_port = ?", host, port)
	if fingerprint != "" {
		query = query.Or("ssh_host_key_fingerprint = ?", fingerprint)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return false, err
	}
	return count != 0, nil
}
