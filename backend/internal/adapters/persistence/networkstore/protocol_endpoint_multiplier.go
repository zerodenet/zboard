package networkstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ProtocolEndpointMultiplier struct {
	DB *gorm.DB
}

func (s ProtocolEndpointMultiplier) UpdateProtocolEndpointMultiplier(ctx context.Context, actorID, endpointID uint, multiplierMilli int64) (out network.ProtocolEndpoint, err error) {
	if s.DB == nil {
		return out, network.ErrProtocolEndpointMultiplierUnavailable
	}
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var endpoint model.ProtocolEndpoint
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&endpoint, endpointID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return network.ErrProtocolEndpointNotFound
			}
			return err
		}
		actor, err := providerAdmin(tx, actorID)
		if err != nil {
			if errors.Is(err, network.ErrProviderPermission) {
				return network.ErrProtocolEndpointMultiplierPermission
			}
			return err
		}
		previous := endpoint.MultiplierMilli
		if previous != multiplierMilli {
			if err := tx.Model(&endpoint).Update("multiplier_milli", multiplierMilli).Error; err != nil {
				return err
			}
			if err := tx.Create(&model.AuditLog{
				UserID: &actor.ID, Actor: actor.Email, Action: "protocol_endpoint.multiplier.update",
				Target: fmt.Sprintf("protocol_endpoint:%d", endpoint.ID), Detail: fmt.Sprintf("from=%d to=%d", previous, multiplierMilli),
			}).Error; err != nil {
				return err
			}
			endpoint.MultiplierMilli = multiplierMilli
		}
		out = protocolEndpointView(endpoint)
		return nil
	})
	return
}

func protocolEndpointView(row model.ProtocolEndpoint) network.ProtocolEndpoint {
	return network.ProtocolEndpoint{
		ID: row.ID, NodeID: row.NodeID, Name: row.Name, Protocol: row.Protocol, Address: row.Address,
		Port: row.Port, PublicPort: row.PublicPort, Cipher: row.Cipher, ParentProtocolID: row.ParentProtocolID,
		MultiplierMilli: row.MultiplierMilli, ManagedPrincipalReady: row.ManagedPrincipalReady,
		MieruPrincipalReady: row.MieruPrincipalReady, ClientConfig: row.ClientConfig,
		OptionalConfig: row.OptionalConfig, Tags: row.Tags, IsActive: row.IsActive, SortOrder: row.SortOrder,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}
