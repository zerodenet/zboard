package handler

import (
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func (h *handlers) validateEndpointCredentialProjection(endpointID uint, now time.Time) error {
	var activeSubscriptions int64
	if err := h.db.Model(&model.Subscription{}).
		Joins("JOIN node_group_endpoints ON node_group_endpoints.node_group_id = subscriptions.node_group_id").
		Where("node_group_endpoints.protocol_endpoint_id = ?", endpointID).
		Where("subscriptions.status = ? AND subscriptions.end_at > ? AND subscriptions.flow_used < subscriptions.flow_total", subStatusActive, now).
		Where("NOT EXISTS (SELECT 1 FROM subscription_fair_use_restrictions fur WHERE fur.subscription_id = subscriptions.id AND fur.active = ? AND fur.restricted_until > ?)", true, now.UTC()).
		Distinct("subscriptions.id").
		Count(&activeSubscriptions).Error; err != nil {
		return err
	}
	return validateEndpointCredentialProjectionCount(endpointID, activeSubscriptions, 0)
}

func validateEndpointCredentialProjectionCount(endpointID uint, activeSubscriptions int64, activeCredentials int) error {
	if activeSubscriptions > 0 && activeCredentials == 0 {
		return fmt.Errorf("protocol endpoint %d has %d active subscriptions but no active credentials after reconciliation", endpointID, activeSubscriptions)
	}
	return nil
}
