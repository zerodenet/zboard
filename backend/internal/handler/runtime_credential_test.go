package handler

import (
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func activeEndpointCredentialsForTest(t *testing.T, h *handlers, endpointID uint, now time.Time) []model.ProtocolCredential {
	t.Helper()
	var credentials []model.ProtocolCredential
	if err := h.db.Model(&model.ProtocolCredential{}).
		Joins("JOIN subscriptions ON subscriptions.id = protocol_credentials.subscription_id").
		Joins(credentialMembershipJoin("node_group_endpoints.node_group_id = subscriptions.node_group_id AND node_group_endpoints.protocol_endpoint_id = protocol_credentials.protocol_endpoint_id")).
		Where("protocol_credentials.protocol_endpoint_id = ? AND protocol_credentials.status = ? AND protocol_credentials.revoked_at IS NULL AND protocol_credentials.expires_at > ?", endpointID, protocolCredentialStatusActive, now).
		Where("subscriptions.status = ? AND subscriptions.end_at > ? AND subscriptions.flow_used < subscriptions.flow_total", subStatusActive, now).
		Order("protocol_credentials.id asc").Find(&credentials).Error; err != nil {
		t.Fatal(err)
	}
	return credentials
}
