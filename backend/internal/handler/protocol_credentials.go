package handler

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/zerodenet/zboard/backend/internal/model"
)

const (
	protocolCredentialStatusActive  = "active"
	protocolCredentialStatusRevoked = "revoked"
)

func protocolUsesSubscriptionCredential(protocol string) bool {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "vless", "vmess", "shadowsocks":
		return true
	default:
		return false
	}
}

func protocolUsesDedicatedCredentialPort(protocol string) bool {
	return strings.EqualFold(strings.TrimSpace(protocol), "shadowsocks")
}

func (h *handlers) ensureSubscriptionCredentials(tx *gorm.DB, subscription model.Subscription) ([]model.ProtocolCredential, error) {
	if tx == nil {
		tx = h.db
	}
	var endpoints []model.ProtocolEndpoint
	if err := tx.Model(&model.ProtocolEndpoint{}).
		Joins("JOIN node_group_endpoints ON node_group_endpoints.protocol_endpoint_id = protocol_endpoints.id").
		Where("node_group_endpoints.node_group_id = ? AND protocol_endpoints.is_active = ?", subscription.NodeGroupID, true).
		Order("protocol_endpoints.sort_order asc, protocol_endpoints.id asc").
		Find(&endpoints).Error; err != nil {
		return nil, err
	}

	credentials := make([]model.ProtocolCredential, 0, len(endpoints))
	for _, endpoint := range endpoints {
		if !protocolUsesSubscriptionCredential(endpoint.Protocol) {
			continue
		}
		var credential model.ProtocolCredential
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("subscription_id = ? AND protocol_endpoint_id = ?", subscription.ID, endpoint.ID).
			First(&credential).Error
		if err == nil {
			updates := map[string]interface{}{
				"user_id":    subscription.UserID,
				"node_id":    endpoint.NodeID,
				"status":     protocolCredentialStatusActive,
				"expires_at": subscription.EndAt,
				"revoked_at": nil,
			}
			if err := tx.Model(&credential).Updates(updates).Error; err != nil {
				return nil, err
			}
			credential.UserID = subscription.UserID
			credential.NodeID = endpoint.NodeID
			credential.Status = protocolCredentialStatusActive
			credential.ExpiresAt = subscription.EndAt
			credential.RevokedAt = nil
			credentials = append(credentials, credential)
			continue
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}

		secret, err := h.newProtocolCredentialSecret(endpoint)
		if err != nil {
			return nil, err
		}
		encryptedSecret, err := h.credentialCipher.Encrypt(secret)
		if err != nil {
			return nil, err
		}
		listenPort, publicPort, err := allocateProtocolCredentialPort(tx, endpoint)
		if err != nil {
			return nil, err
		}
		credential = model.ProtocolCredential{
			SubscriptionID:     subscription.ID,
			UserID:             subscription.UserID,
			ProtocolEndpointID: endpoint.ID,
			NodeID:             endpoint.NodeID,
			CredentialID:       fmt.Sprintf("subscription-%d-endpoint-%d", subscription.ID, endpoint.ID),
			PrincipalKey:       fmt.Sprintf("subscription:%d:endpoint:%d", subscription.ID, endpoint.ID),
			Secret:             encryptedSecret,
			ListenPort:         listenPort,
			PublicPort:         publicPort,
			Status:             protocolCredentialStatusActive,
			ExpiresAt:          subscription.EndAt,
		}
		if err := tx.Create(&credential).Error; err != nil {
			return nil, err
		}
		credentials = append(credentials, credential)
	}
	return credentials, nil
}

func (h *handlers) ensureCredentialsForSubscriptions(subscriptions []model.Subscription) error {
	return h.db.Transaction(func(tx *gorm.DB) error {
		for _, subscription := range subscriptions {
			if _, err := h.ensureSubscriptionCredentials(tx, subscription); err != nil {
				return err
			}
		}
		return nil
	})
}

func (h *handlers) reconcileNodeGroupCredentials(groupID uint) error {
	now := time.Now().UTC()
	return h.db.Transaction(func(tx *gorm.DB) error {
		activeSubscriptionIDs := tx.Model(&model.Subscription{}).
			Select("id").
			Where("node_group_id = ? AND status = ? AND end_at > ? AND flow_used < flow_total", groupID, subStatusActive, now)
		currentEndpointIDs := tx.Model(&model.NodeGroupEndpoint{}).
			Select("protocol_endpoint_id").
			Where("node_group_id = ?", groupID)
		if err := tx.Model(&model.ProtocolCredential{}).
			Where("subscription_id IN (?)", activeSubscriptionIDs).
			Where("protocol_endpoint_id NOT IN (?)", currentEndpointIDs).
			Where("status = ? AND revoked_at IS NULL", protocolCredentialStatusActive).
			Updates(map[string]interface{}{"status": protocolCredentialStatusRevoked, "revoked_at": now}).Error; err != nil {
			return err
		}

		var subscriptions []model.Subscription
		if err := tx.Where("node_group_id = ? AND status = ? AND end_at > ? AND flow_used < flow_total", groupID, subStatusActive, now).
			Order("id asc").
			Find(&subscriptions).Error; err != nil {
			return err
		}
		for _, subscription := range subscriptions {
			if _, err := h.ensureSubscriptionCredentials(tx, subscription); err != nil {
				return err
			}
		}
		return nil
	})
}

func (h *handlers) newProtocolCredentialSecret(endpoint model.ProtocolEndpoint) (string, error) {
	switch strings.ToLower(strings.TrimSpace(endpoint.Protocol)) {
	case "vless", "vmess":
		return uuid.NewString(), nil
	case "shadowsocks":
		keyBytes := 32
		rawTemplate, err := h.credentialCipher.Decrypt(endpoint.ServerConfig)
		if err != nil {
			return "", err
		}
		var server map[string]interface{}
		if err := json.Unmarshal([]byte(rawTemplate), &server); err != nil {
			return "", err
		}
		cipherName, _ := server["cipher"].(string)
		cipherName = strings.ToLower(strings.TrimSpace(cipherName))
		if strings.Contains(cipherName, "2022") && strings.Contains(cipherName, "aes-128") {
			keyBytes = 16
		}
		entropy := make([]byte, keyBytes)
		if _, err := rand.Read(entropy); err != nil {
			return "", err
		}
		if strings.Contains(cipherName, "2022") {
			return base64.StdEncoding.EncodeToString(entropy), nil
		}
		return base64.RawURLEncoding.EncodeToString(entropy), nil
	default:
		return "", fmt.Errorf("protocol %s does not support subscription credentials", endpoint.Protocol)
	}
}

func allocateProtocolCredentialPort(tx *gorm.DB, endpoint model.ProtocolEndpoint) (int, int, error) {
	if !protocolUsesDedicatedCredentialPort(endpoint.Protocol) {
		return endpoint.Port, endpoint.PublicPort, nil
	}
	var used []int
	if err := tx.Model(&model.ProtocolCredential{}).
		Where("node_id = ? AND listen_port >= ?", endpoint.NodeID, endpoint.Port).
		Pluck("listen_port", &used).Error; err != nil {
		return 0, 0, err
	}
	occupied := make(map[int]struct{}, len(used))
	for _, port := range used {
		occupied[port] = struct{}{}
	}
	var endpointPorts []struct {
		ID   uint
		Port int
	}
	if err := tx.Model(&model.ProtocolEndpoint{}).
		Select("id, port").
		Where("node_id = ? AND is_active = ?", endpoint.NodeID, true).
		Scan(&endpointPorts).Error; err != nil {
		return 0, 0, err
	}
	for _, candidate := range endpointPorts {
		if candidate.ID != endpoint.ID {
			occupied[candidate.Port] = struct{}{}
		}
	}
	for port := endpoint.Port; port <= 65535; port++ {
		if _, exists := occupied[port]; exists {
			continue
		}
		publicPort := endpoint.PublicPort + (port - endpoint.Port)
		if publicPort > 65535 {
			break
		}
		return port, publicPort, nil
	}
	return 0, 0, errors.New("no free Shadowsocks credential port is available")
}

func migrateProtocolEndpointCredentials(tx *gorm.DB, previous, next model.ProtocolEndpoint) error {
	if previous.ID == 0 || previous.ID != next.ID {
		return nil
	}
	var credentials []model.ProtocolCredential
	if err := tx.Where("protocol_endpoint_id = ?", next.ID).Order("id asc").Find(&credentials).Error; err != nil {
		return err
	}
	if len(credentials) == 0 {
		return nil
	}
	if !protocolUsesDedicatedCredentialPort(next.Protocol) {
		return tx.Model(&model.ProtocolCredential{}).
			Where("protocol_endpoint_id = ?", next.ID).
			Updates(map[string]interface{}{
				"node_id": next.NodeID, "listen_port": next.Port, "public_port": next.PublicPort,
			}).Error
	}

	occupied := make(map[int]struct{})
	var usedCredentialPorts []int
	if err := tx.Model(&model.ProtocolCredential{}).
		Where("node_id = ? AND protocol_endpoint_id <> ?", next.NodeID, next.ID).
		Pluck("listen_port", &usedCredentialPorts).Error; err != nil {
		return err
	}
	for _, port := range usedCredentialPorts {
		occupied[port] = struct{}{}
	}
	var endpointPorts []int
	if err := tx.Model(&model.ProtocolEndpoint{}).
		Where("node_id = ? AND id <> ? AND is_active = ?", next.NodeID, next.ID, true).
		Pluck("port", &endpointPorts).Error; err != nil {
		return err
	}
	for _, port := range endpointPorts {
		occupied[port] = struct{}{}
	}

	candidate := next.Port
	for index := range credentials {
		assigned := false
		for candidate <= 65535 {
			publicPort := next.PublicPort + (candidate - next.Port)
			if publicPort > 65535 {
				break
			}
			if _, exists := occupied[candidate]; exists {
				candidate++
				continue
			}
			if err := tx.Model(&credentials[index]).Updates(map[string]interface{}{
				"node_id": next.NodeID, "listen_port": candidate, "public_port": publicPort,
			}).Error; err != nil {
				return err
			}
			occupied[candidate] = struct{}{}
			candidate++
			assigned = true
			break
		}
		if !assigned {
			return validationError("协议服务迁移失败。", map[string]string{
				"node_id": "目标节点没有足够的可用端口容纳现有 Shadowsocks 凭证。",
			})
		}
	}
	return nil
}

func (h *handlers) activeEndpointCredentials(endpointID uint, now time.Time) ([]model.ProtocolCredential, error) {
	var credentials []model.ProtocolCredential
	err := h.db.Model(&model.ProtocolCredential{}).
		Joins("JOIN subscriptions ON subscriptions.id = protocol_credentials.subscription_id").
		Joins("JOIN node_group_endpoints ON node_group_endpoints.node_group_id = subscriptions.node_group_id AND node_group_endpoints.protocol_endpoint_id = protocol_credentials.protocol_endpoint_id").
		Where("protocol_credentials.protocol_endpoint_id = ? AND protocol_credentials.status = ? AND protocol_credentials.revoked_at IS NULL AND protocol_credentials.expires_at > ?", endpointID, protocolCredentialStatusActive, now).
		Where("subscriptions.status = ? AND subscriptions.end_at > ? AND subscriptions.flow_used < subscriptions.flow_total", subStatusActive, now).
		Order("protocol_credentials.id asc").
		Find(&credentials).Error
	return credentials, err
}

func (h *handlers) runtimeInboundsForEndpoint(endpoint model.ProtocolEndpoint, protocol map[string]interface{}, now time.Time) ([]map[string]interface{}, error) {
	if !protocolUsesSubscriptionCredential(endpoint.Protocol) {
		return []map[string]interface{}{runtimeInbound(endpoint, fmt.Sprintf("endpoint-%d", endpoint.ID), endpoint.Port, protocol)}, nil
	}
	credentials, err := h.activeEndpointCredentials(endpoint.ID, now)
	if err != nil {
		return nil, err
	}
	if len(credentials) == 0 {
		return nil, nil
	}

	if strings.EqualFold(endpoint.Protocol, "vless") || strings.EqualFold(endpoint.Protocol, "vmess") {
		users := make([]interface{}, 0, len(credentials))
		defaultCipher := "aes-128-gcm"
		if configured, ok := protocol["users"].([]interface{}); ok && len(configured) > 0 {
			if first, ok := configured[0].(map[string]interface{}); ok {
				if value, ok := first["cipher"].(string); ok && strings.TrimSpace(value) != "" {
					defaultCipher = value
				}
			}
		}
		for _, credential := range credentials {
			secret, err := h.credentialCipher.Decrypt(credential.Secret)
			if err != nil {
				return nil, fmt.Errorf("decrypt protocol credential %d: %w", credential.ID, err)
			}
			user := map[string]interface{}{
				"id":            secret,
				"credential_id": credential.CredentialID,
				"principal_key": credential.PrincipalKey,
			}
			if strings.EqualFold(endpoint.Protocol, "vmess") {
				user["cipher"] = defaultCipher
			}
			users = append(users, user)
		}
		protocol["users"] = users
		return []map[string]interface{}{runtimeInbound(endpoint, fmt.Sprintf("endpoint-%d", endpoint.ID), endpoint.Port, protocol)}, nil
	}

	inbounds := make([]map[string]interface{}, 0, len(credentials))
	for _, credential := range credentials {
		secret, err := h.credentialCipher.Decrypt(credential.Secret)
		if err != nil {
			return nil, fmt.Errorf("decrypt protocol credential %d: %w", credential.ID, err)
		}
		copyPayload, err := json.Marshal(protocol)
		if err != nil {
			return nil, err
		}
		var credentialProtocol map[string]interface{}
		if err := json.Unmarshal(copyPayload, &credentialProtocol); err != nil {
			return nil, err
		}
		credentialProtocol["password"] = secret
		tag := fmt.Sprintf("endpoint-%d-credential-%d", endpoint.ID, credential.ID)
		inbounds = append(inbounds, runtimeInbound(endpoint, tag, credential.ListenPort, credentialProtocol))
	}
	return inbounds, nil
}

func runtimeInbound(endpoint model.ProtocolEndpoint, tag string, port int, protocol map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"tag":      tag,
		"listen":   map[string]interface{}{"address": "0.0.0.0", "port": port},
		"protocol": protocol,
	}
}

func (h *handlers) credentialClientConfig(endpoint model.ProtocolEndpoint, credential model.ProtocolCredential) (json.RawMessage, error) {
	var client map[string]interface{}
	if err := json.Unmarshal([]byte(endpoint.ClientConfig), &client); err != nil || client == nil {
		return nil, errors.New("endpoint client config is invalid")
	}
	secret, err := h.credentialCipher.Decrypt(credential.Secret)
	if err != nil {
		return nil, err
	}
	client["server"] = endpoint.Address
	client["port"] = credential.PublicPort
	switch strings.ToLower(endpoint.Protocol) {
	case "vless", "vmess":
		client["id"] = secret
	case "shadowsocks":
		client["password"] = secret
	}
	payload, err := json.Marshal(client)
	return json.RawMessage(payload), err
}
