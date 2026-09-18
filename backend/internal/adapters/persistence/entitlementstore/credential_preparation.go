package entitlementstore

import (
	"context"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type CredentialPreparation struct {
	DB     *gorm.DB
	Issuer entitlements.CredentialIssuer
}

func (s CredentialPreparation) EnsureCredentialSubscriptions(ctx context.Context, subscriptions []entitlements.CredentialSubscription) error {
	rows := make([]model.Subscription, 0, len(subscriptions))
	for _, row := range subscriptions {
		rows = append(rows, model.Subscription{ID: row.ID, UserID: row.UserID, NodeGroupID: row.NodeGroupID, EndAt: row.EndAt})
	}
	return (GroupCredentialReconciliation{DB: s.DB, Issuer: s.Issuer}).ensureSubscriptions(ctx, rows)
}

func (s CredentialPreparation) ListActiveMieruCredentialSubscriptions(ctx context.Context, now time.Time) ([]entitlements.CredentialSubscription, error) {
	var rows []model.Subscription
	err := s.DB.WithContext(ctx).Model(&model.Subscription{}).
		Select("DISTINCT subscriptions.*").
		Joins("JOIN node_group_endpoints ON node_group_endpoints.node_group_id = subscriptions.node_group_id").
		Joins("JOIN protocol_endpoints ON protocol_endpoints.id = node_group_endpoints.protocol_endpoint_id").
		Where("LOWER(protocol_endpoints.protocol) = ? AND subscriptions.status = ? AND subscriptions.end_at > ? AND subscriptions.flow_used < subscriptions.flow_total", "mieru", "active", now).
		Order("subscriptions.id asc").Find(&rows).Error
	if err != nil {
		return nil, err
	}
	result := make([]entitlements.CredentialSubscription, 0, len(rows))
	for _, row := range rows {
		result = append(result, entitlements.CredentialSubscription{ID: row.ID, UserID: row.UserID, NodeGroupID: row.NodeGroupID, EndAt: row.EndAt})
	}
	return result, nil
}

var _ entitlements.CredentialPreparationRepository = CredentialPreparation{}
