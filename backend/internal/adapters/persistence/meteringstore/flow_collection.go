package meteringstore

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type CredentialProjection struct {
	ID                 uint `gorm:"column:id"`
	UserID             uint `gorm:"column:user_id"`
	SubscriptionID     uint `gorm:"column:subscription_id"`
	ProtocolEndpointID uint `gorm:"column:protocol_endpoint_id"`
}

// Historical events map through the latest credential for this node/principal,
// including revoked credentials; current service eligibility is a separate concern.
func ResolveCredential(tx *gorm.DB, node uint, principal string) (CredentialProjection, bool, error) {
	var out CredentialProjection
	err := tx.Table("protocol_credentials").Select("id, user_id, subscription_id, protocol_endpoint_id").Where("node_id = ? AND principal_key = ?", node, principal).Order("id DESC").Take(&out).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return CredentialProjection{}, false, nil
	}
	return out, err == nil, err
}

type FlowCollection struct{ DB *gorm.DB }

func (s FlowCollection) Record(ctx context.Context, event metering.FlowStart) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row := FlowStartRecord{NodeID: event.NodeID, CoreInstanceID: event.CoreInstanceID, EventID: event.EventID, Sequence: event.Sequence, PrincipalKey: event.PrincipalKey, MappingState: "unmapped", OccurredAt: event.OccurredAt, ReceivedAt: event.ReceivedAt, CreatedAt: event.ReceivedAt}
		if event.PrincipalKey != "" {
			credential, found, err := ResolveCredential(tx, event.NodeID, event.PrincipalKey)
			if err != nil {
				return err
			}
			if found {
				row.UserID = credential.UserID
				row.SubscriptionID = credential.SubscriptionID
				row.ProtocolCredentialID = credential.ID
				row.ProtocolEndpointID = credential.ProtocolEndpointID
				if row.SubscriptionID > 0 {
					row.MappingState = "mapped"
				}
			}
		}
		return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error
	})
}
