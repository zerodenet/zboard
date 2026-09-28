package menustore

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/navigation"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Store struct{ DB *gorm.DB }

func (s Store) Read(ctx context.Context, surface string) (navigation.Snapshot, error) {
	return Read(s.DB.WithContext(ctx), surface)
}
func (s Store) Save(ctx context.Context, actor uint, surface string, snapshot navigation.Snapshot) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user model.User
		result := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND is_admin = ? AND status = ?", actor, true, "active").Limit(1).Find(&user)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return navigation.ErrPermission
		}
		return Replace(tx, surface, snapshot.Revision, snapshot.Nodes, user.Email, user.ID)
	})
}

func (s Store) HasDocuments(ctx context.Context) (bool, error) {
	var configs []model.SystemConfig
	if err := s.DB.WithContext(ctx).Where("config_key IN ?", []string{"site_policy_documents", "site_terms_content", "site_privacy_content", "site_refund_content"}).Find(&configs).Error; err != nil {
		return false, err
	}
	for _, config := range configs {
		if config.ConfigKey == "site_policy_documents" && strings.TrimSpace(config.Value) != "null" && strings.TrimSpace(config.Value) != "" {
			var docs []json.RawMessage
			if json.Unmarshal([]byte(config.Value), &docs) == nil {
				return len(docs) > 0, nil
			}
		}
	}
	for _, config := range configs {
		if config.ConfigKey != "site_policy_documents" && strings.TrimSpace(config.Value) != "" {
			return true, nil
		}
	}
	return false, nil
}
