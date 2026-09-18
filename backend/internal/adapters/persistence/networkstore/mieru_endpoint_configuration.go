package networkstore

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type MieruEndpointConfigurations struct{ DB *gorm.DB }

func (s MieruEndpointConfigurations) ListMieruEndpointConfigurations(ctx context.Context) ([]network.MieruEndpointConfigurationSnapshot, error) {
	if s.DB == nil {
		return nil, network.ErrMieruEndpointConfigurationUnavailable
	}
	var rows []model.ProtocolEndpoint
	if err := s.DB.WithContext(ctx).Where("LOWER(protocol) = ?", "mieru").Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]network.MieruEndpointConfigurationSnapshot, 0, len(rows))
	for _, row := range rows {
		result = append(result, network.MieruEndpointConfigurationSnapshot{
			ID: row.ID, NodeID: row.NodeID, ServerCiphertext: row.ServerConfig,
			ClientConfig: row.ClientConfig, MieruPrincipalReady: row.MieruPrincipalReady,
		})
	}
	return result, nil
}

func (s MieruEndpointConfigurations) CommitMieruEndpointConfigurations(ctx context.Context, updates []network.MieruEndpointConfigurationUpdate) error {
	if s.DB == nil {
		return network.ErrMieruEndpointConfigurationUnavailable
	}
	if len(updates) == 0 {
		return nil
	}
	ordered := append([]network.MieruEndpointConfigurationUpdate(nil), updates...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, update := range ordered {
			var row model.ProtocolEndpoint
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, update.ID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return network.ErrMieruEndpointConfigurationConflict
				}
				return err
			}
			if !strings.EqualFold(row.Protocol, "mieru") || row.ServerConfig != update.ExpectedServerCiphertext || row.ClientConfig != update.ExpectedClientConfig {
				return network.ErrMieruEndpointConfigurationConflict
			}
			result := tx.Model(&model.ProtocolEndpoint{}).Where("id = ? AND server_config = ? AND client_config = ?", row.ID, update.ExpectedServerCiphertext, update.ExpectedClientConfig).
				Updates(map[string]any{"server_config": update.ServerCiphertext, "client_config": update.ClientConfig})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return network.ErrMieruEndpointConfigurationConflict
			}
		}
		return nil
	})
}
