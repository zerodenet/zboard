package handler

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/zerodenet/zboard/backend/internal/model"
)

const (
	protocolCredentialStatusActive   = "active"
	protocolCredentialStatusPrepared = "prepared"
	protocolCredentialStatusRevoked  = "revoked"
)

func protocolUsesSubscriptionCredential(protocol string) bool {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "vless", "vmess", "shadowsocks", "trojan", "hysteria2":
		return true
	default:
		return false
	}
}

// Endpoint templates describe transport defaults, not a shared account.
// Subscriber secrets are injected only while compiling a node runtime or a
// subscriber client configuration.
func normalizeManagedProtocolTemplates(protocol, serverConfig, clientConfig string) (string, string, error) {
	var server map[string]interface{}
	var client map[string]interface{}
	if err := json.Unmarshal([]byte(serverConfig), &server); err != nil || server == nil {
		return "", "", validationError("协议配置校验失败。", map[string]string{"config": "服务端配置必须是 JSON 对象。"})
	}
	if err := json.Unmarshal([]byte(clientConfig), &client); err != nil || client == nil {
		return "", "", validationError("协议配置校验失败。", map[string]string{"client_config": "客户端配置必须是 JSON 对象。"})
	}

	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "vless":
		var defaults []interface{}
		if configured, ok := server["users"].([]interface{}); ok && len(configured) > 0 {
			if first, ok := configured[0].(map[string]interface{}); ok {
				if flow, ok := first["flow"].(string); ok && strings.TrimSpace(flow) != "" {
					defaults = []interface{}{map[string]interface{}{"flow": strings.TrimSpace(flow)}}
				}
			}
		}
		if defaults == nil {
			defaults = []interface{}{}
		}
		server["users"] = defaults
		delete(client, "id")
	case "trojan", "hysteria2":
		delete(server, "password")
		server["users"] = []interface{}{}
		delete(client, "password")
	default:
		return serverConfig, clientConfig, nil
	}

	normalizedServer, err := json.MarshalIndent(server, "", "  ")
	if err != nil {
		return "", "", err
	}
	normalizedClient, err := json.MarshalIndent(client, "", "  ")
	if err != nil {
		return "", "", err
	}
	return string(normalizedServer), string(normalizedClient), nil
}

func (h *handlers) protocolStoresSubscriptionCredential(protocol string) bool {
	return protocolStoresSubscriptionCredentialWithMieru(protocol, h.zeroMieruAccess)
}

func protocolStoresSubscriptionCredentialWithMieru(protocol string, mieruAccess bool) bool {
	return protocolUsesSubscriptionCredential(protocol) ||
		(mieruAccess && strings.EqualFold(strings.TrimSpace(protocol), "mieru"))
}

func (h *handlers) protocolUsesSubscriptionCredential(protocol string) bool {
	if h.zeroMieruAccess && strings.EqualFold(strings.TrimSpace(protocol), "mieru") {
		return true
	}
	if h.zeroNativeAccess {
		return protocolUsesSubscriptionCredential(protocol)
	}
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "vless", "vmess", "shadowsocks":
		return true
	default:
		return false
	}
}

func (h *handlers) runtimeCredentialProtocols() []string {
	if h.zeroMieruAccess {
		return []string{"vless", "vmess", "shadowsocks", "trojan", "hysteria2", "mieru"}
	}
	if h.zeroNativeAccess {
		return []string{"vless", "vmess", "shadowsocks", "trojan", "hysteria2"}
	}
	return []string{"vless", "vmess", "shadowsocks"}
}

func (h *handlers) desiredProtocolCredentialStatus(endpoint model.ProtocolEndpoint) string {
	return desiredProtocolCredentialStatusWithMieru(endpoint, h.zeroMieruAccess)
}

func desiredProtocolCredentialStatusWithMieru(endpoint model.ProtocolEndpoint, mieruAccess bool) string {
	if strings.EqualFold(strings.TrimSpace(endpoint.Protocol), "mieru") &&
		!mieruAccess && !endpoint.MieruPrincipalReady {
		return protocolCredentialStatusPrepared
	}
	return protocolCredentialStatusActive
}

func protocolUsesDedicatedCredentialPort(protocol string) bool {
	return strings.EqualFold(strings.TrimSpace(protocol), "shadowsocks")
}

func (h *handlers) ensureSubscriptionCredentials(tx *gorm.DB, subscription model.Subscription) ([]model.ProtocolCredential, error) {
	return h.ensureSubscriptionCredentialsWithMieru(tx, subscription, h.zeroMieruAccess)
}

func (h *handlers) ensureSubscriptionCredentialsWithMieru(tx *gorm.DB, subscription model.Subscription, mieruAccess bool) ([]model.ProtocolCredential, error) {
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
		if !protocolStoresSubscriptionCredentialWithMieru(endpoint.Protocol, mieruAccess) {
			continue
		}
		targetStatus := desiredProtocolCredentialStatusWithMieru(endpoint, mieruAccess)
		var credential model.ProtocolCredential
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("subscription_id = ? AND protocol_endpoint_id = ?", subscription.ID, endpoint.ID).
			First(&credential).Error
		if err == nil {
			updates := map[string]interface{}{
				"user_id":    subscription.UserID,
				"node_id":    endpoint.NodeID,
				"status":     targetStatus,
				"expires_at": subscription.EndAt,
				"revoked_at": nil,
			}
			if err := tx.Model(&credential).Updates(updates).Error; err != nil {
				return nil, err
			}
			credential.UserID = subscription.UserID
			credential.NodeID = endpoint.NodeID
			credential.Status = targetStatus
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
			Status:             targetStatus,
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
	return h.ensureCredentialsForSubscriptionsWithMieru(subscriptions, h.zeroMieruAccess)
}

func (h *handlers) ensureCredentialsForSubscriptionsWithMieru(subscriptions []model.Subscription, mieruAccess bool) error {
	for _, subscription := range orderedProtocolCredentialSubscriptions(subscriptions) {
		subscription := subscription
		if err := h.runProtocolCredentialTransaction(func(tx *gorm.DB) error {
			_, err := h.ensureSubscriptionCredentialsWithMieru(tx, subscription, mieruAccess)
			return err
		}); err != nil {
			return err
		}
	}
	return nil
}

func (h *handlers) reconcileNodeGroupCredentials(groupID uint) error {
	now := time.Now().UTC()
	var subscriptions []model.Subscription
	if err := h.runProtocolCredentialTransaction(func(tx *gorm.DB) error {
		activeSubscriptionIDs := tx.Model(&model.Subscription{}).
			Select("id").
			Where("node_group_id = ? AND status = ? AND end_at > ? AND flow_used < flow_total", groupID, subStatusActive, now)
		currentEndpointIDs := tx.Model(&model.NodeGroupEndpoint{}).
			Select("protocol_endpoint_id").
			Where("node_group_id = ?", groupID)
		if err := tx.Model(&model.ProtocolCredential{}).
			Where("subscription_id IN (?)", activeSubscriptionIDs).
			Where("protocol_endpoint_id NOT IN (?)", currentEndpointIDs).
			Where("status IN ? AND revoked_at IS NULL", []string{protocolCredentialStatusActive, protocolCredentialStatusPrepared}).
			Updates(map[string]interface{}{"status": protocolCredentialStatusRevoked, "revoked_at": now}).Error; err != nil {
			return err
		}

		return tx.Where("node_group_id = ? AND status = ? AND end_at > ? AND flow_used < flow_total", groupID, subStatusActive, now).
			Order("id asc").
			Find(&subscriptions).Error
	}); err != nil {
		return err
	}
	return h.ensureCredentialsForSubscriptions(subscriptions)
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
	case "trojan", "hysteria2", "mieru":
		entropy := make([]byte, 32)
		if _, err := rand.Read(entropy); err != nil {
			return "", err
		}
		return base64.RawURLEncoding.EncodeToString(entropy), nil
	default:
		return "", fmt.Errorf("protocol %s does not support subscription credentials", endpoint.Protocol)
	}
}

func (h *handlers) prepareMieruEndpointConfigs(endpointID uint, serverRaw, clientRaw string) (string, string, error) {
	var server map[string]interface{}
	if err := json.Unmarshal([]byte(serverRaw), &server); err != nil || server == nil {
		return "", "", validationError("协议配置校验失败。", map[string]string{"config": "服务端配置必须是 JSON 对象。"})
	}
	var client map[string]interface{}
	if err := json.Unmarshal([]byte(clientRaw), &client); err != nil || client == nil {
		return "", "", validationError("协议配置校验失败。", map[string]string{"client_config": "客户端配置必须是 JSON 对象。"})
	}

	password := ""
	if endpointID != 0 {
		var endpoint model.ProtocolEndpoint
		if err := h.db.First(&endpoint, endpointID).Error; err != nil {
			return "", "", err
		}
		existingRaw, err := h.credentialCipher.Decrypt(endpoint.ServerConfig)
		if err != nil {
			return "", "", err
		}
		var existing map[string]interface{}
		if json.Unmarshal([]byte(existingRaw), &existing) == nil {
			password = mieruEndpointPassword(existing)
		}
	}
	if password == "" {
		entropy := make([]byte, 32)
		if _, err := rand.Read(entropy); err != nil {
			return "", "", err
		}
		password = base64.RawURLEncoding.EncodeToString(entropy)
	}

	server["type"] = "mieru"
	server["users"] = []interface{}{map[string]interface{}{"password": password}}
	client["type"] = "mieru"
	delete(client, "username")
	delete(client, "password")

	normalizedServer, err := json.Marshal(server)
	if err != nil {
		return "", "", err
	}
	normalizedClient, err := json.Marshal(client)
	if err != nil {
		return "", "", err
	}
	return string(normalizedServer), string(normalizedClient), nil
}

func mieruEndpointPassword(server map[string]interface{}) string {
	users, _ := server["users"].([]interface{})
	if len(users) == 0 {
		return ""
	}
	user, _ := users[0].(map[string]interface{})
	password, _ := user["password"].(string)
	return strings.TrimSpace(password)
}

func (h *handlers) endpointSubscriptionClientConfig(endpoint model.ProtocolEndpoint) (json.RawMessage, error) {
	if !strings.EqualFold(endpoint.Protocol, "mieru") {
		if !json.Valid([]byte(endpoint.ClientConfig)) {
			return nil, errors.New("endpoint client config is invalid")
		}
		return json.RawMessage(endpoint.ClientConfig), nil
	}

	var client map[string]interface{}
	if err := json.Unmarshal([]byte(endpoint.ClientConfig), &client); err != nil || client == nil {
		return nil, errors.New("endpoint client config is invalid")
	}
	serverRaw, err := h.credentialCipher.Decrypt(endpoint.ServerConfig)
	if err != nil {
		return nil, err
	}
	var server map[string]interface{}
	if err := json.Unmarshal([]byte(serverRaw), &server); err != nil || server == nil {
		return nil, errors.New("endpoint server config is invalid")
	}
	password := mieruEndpointPassword(server)
	if password == "" {
		return nil, errors.New("Mieru endpoint credential is unavailable")
	}
	client["type"] = "mieru"
	client["password"] = password
	delete(client, "username")
	payload, err := json.Marshal(client)
	return json.RawMessage(payload), err
}

func redactMieruEndpointAdminConfigs(serverRaw, clientRaw string) (string, string) {
	var server map[string]interface{}
	if json.Unmarshal([]byte(serverRaw), &server) == nil && server != nil {
		server["users"] = []interface{}{}
		if payload, err := json.Marshal(server); err == nil {
			serverRaw = string(payload)
		}
	}
	var client map[string]interface{}
	if json.Unmarshal([]byte(clientRaw), &client) == nil && client != nil {
		delete(client, "username")
		delete(client, "password")
		if payload, err := json.Marshal(client); err == nil {
			clientRaw = string(payload)
		}
	}
	return serverRaw, clientRaw
}

// ReconcileMieruEndpointCredentials upgrades the fallback endpoint credential,
// prepares per-subscription credentials, and republishes only when endpoint
// readiness differs from the selected kernel contract. The subscription
// credential remains undisclosed until a principal-aware publication succeeds.
func (h *handlers) ReconcileMieruEndpointCredentials() error {
	var endpoints []model.ProtocolEndpoint
	if err := h.db.Where("LOWER(protocol) = ?", "mieru").Order("id asc").Find(&endpoints).Error; err != nil {
		return err
	}
	changedNodes := make(map[uint]uint)
	for _, endpoint := range endpoints {
		serverRaw, err := h.credentialCipher.Decrypt(endpoint.ServerConfig)
		if err != nil {
			return fmt.Errorf("decrypt Mieru endpoint %d: %w", endpoint.ID, err)
		}
		normalizedServer, normalizedClient, err := h.prepareMieruEndpointConfigs(endpoint.ID, serverRaw, endpoint.ClientConfig)
		if err != nil {
			return fmt.Errorf("normalize Mieru endpoint %d: %w", endpoint.ID, err)
		}
		if normalizedServer == serverRaw && normalizedClient == endpoint.ClientConfig {
			continue
		}
		encryptedServer, err := h.credentialCipher.Encrypt(normalizedServer)
		if err != nil {
			return fmt.Errorf("encrypt Mieru endpoint %d: %w", endpoint.ID, err)
		}
		if err := h.db.Model(&model.ProtocolEndpoint{}).Where("id = ?", endpoint.ID).Updates(map[string]interface{}{
			"server_config": encryptedServer,
			"client_config": normalizedClient,
		}).Error; err != nil {
			return fmt.Errorf("persist Mieru endpoint %d: %w", endpoint.ID, err)
		}
		if h.zeroMieruAccess || endpoint.MieruPrincipalReady {
			if _, exists := changedNodes[endpoint.NodeID]; !exists {
				changedNodes[endpoint.NodeID] = endpoint.ID
			}
		}
	}
	var managedEndpoints []model.ProtocolEndpoint
	if err := h.db.Where("LOWER(protocol) IN ?", []string{"trojan", "hysteria2"}).Order("id asc").Find(&managedEndpoints).Error; err != nil {
		return err
	}
	for _, endpoint := range managedEndpoints {
		if _, exists := changedNodes[endpoint.NodeID]; !exists {
			changedNodes[endpoint.NodeID] = endpoint.ID
		}
	}
	if !h.zeroMieruAccess {
		for _, endpoint := range endpoints {
			if !endpoint.MieruPrincipalReady {
				continue
			}
			if _, exists := changedNodes[endpoint.NodeID]; !exists {
				changedNodes[endpoint.NodeID] = endpoint.ID
			}
		}
		for nodeID, endpointID := range changedNodes {
			h.scheduleNodeConfigPublish(nodeID, endpointID, 0)
		}
		return nil
	}
	now := time.Now().UTC()
	var subscriptions []model.Subscription
	if err := h.db.Model(&model.Subscription{}).
		Select("DISTINCT subscriptions.*").
		Joins("JOIN node_group_endpoints ON node_group_endpoints.node_group_id = subscriptions.node_group_id").
		Joins("JOIN protocol_endpoints ON protocol_endpoints.id = node_group_endpoints.protocol_endpoint_id").
		Where("LOWER(protocol_endpoints.protocol) = ? AND subscriptions.status = ? AND subscriptions.end_at > ? AND subscriptions.flow_used < subscriptions.flow_total", "mieru", subStatusActive, now).
		Order("subscriptions.id asc").
		Find(&subscriptions).Error; err != nil {
		return fmt.Errorf("load active Mieru subscriptions: %w", err)
	}
	if err := h.ensureCredentialsForSubscriptions(subscriptions); err != nil {
		return fmt.Errorf("prepare per-subscription Mieru credentials: %w", err)
	}
	for _, endpoint := range endpoints {
		if endpoint.MieruPrincipalReady == h.zeroMieruAccess {
			continue
		}
		if _, exists := changedNodes[endpoint.NodeID]; !exists {
			changedNodes[endpoint.NodeID] = endpoint.ID
		}
	}
	for nodeID, endpointID := range changedNodes {
		h.scheduleNodeConfigPublish(nodeID, endpointID, 0)
	}
	return nil
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

func (h *handlers) migrateProtocolEndpointCredentials(tx *gorm.DB, previous, next model.ProtocolEndpoint) error {
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
	if !strings.EqualFold(strings.TrimSpace(previous.Protocol), strings.TrimSpace(next.Protocol)) {
		for index := range credentials {
			secret, err := h.newProtocolCredentialSecret(next)
			if err != nil {
				return err
			}
			encrypted, err := h.credentialCipher.Encrypt(secret)
			if err != nil {
				return err
			}
			if err := tx.Model(&credentials[index]).Update("secret", encrypted).Error; err != nil {
				return err
			}
		}
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

func (h *handlers) runtimeInboundsForEndpoint(endpoint model.ProtocolEndpoint, protocol map[string]interface{}, now time.Time, suppressMieruFallback, nativeAccess, mieruAccess bool) ([]map[string]interface{}, error) {
	usesCredential := endpointUsesRuntimeCredentials(endpoint.Protocol, nativeAccess, mieruAccess)
	if !usesCredential {
		return []map[string]interface{}{runtimeInbound(endpoint, fmt.Sprintf("endpoint-%d", endpoint.ID), endpoint.Port, protocol)}, nil
	}
	credentials, err := h.activeEndpointCredentials(endpoint.ID, now)
	if err != nil {
		return nil, err
	}
	if len(credentials) == 0 {
		return nil, nil
	}
	if !nativeAccess {
		return h.legacyRuntimeInboundsForEndpoint(endpoint, protocol, credentials)
	}
	contexts, err := h.runtimeCredentialContexts(credentials, now)
	if err != nil {
		return nil, err
	}

	if strings.EqualFold(endpoint.Protocol, "vless") || strings.EqualFold(endpoint.Protocol, "vmess") {
		users := make([]interface{}, 0, len(credentials))
		defaultCipher := "aes-128-gcm"
		defaultFlow := ""
		if configured, ok := protocol["users"].([]interface{}); ok && len(configured) > 0 {
			if first, ok := configured[0].(map[string]interface{}); ok {
				if value, ok := first["cipher"].(string); ok && strings.TrimSpace(value) != "" {
					defaultCipher = value
				}
				if value, ok := first["flow"].(string); ok {
					defaultFlow = strings.TrimSpace(value)
				}
			}
		}
		for _, context := range contexts {
			credential := context.Credential
			secret, err := h.credentialCipher.Decrypt(credential.Secret)
			if err != nil {
				return nil, fmt.Errorf("decrypt protocol credential %d: %w", credential.ID, err)
			}
			user, err := managedAccessUserFields(context)
			if err != nil {
				return nil, err
			}
			user["id"] = secret
			if strings.EqualFold(endpoint.Protocol, "vmess") {
				user["cipher"] = defaultCipher
			} else if defaultFlow != "" {
				user["flow"] = defaultFlow
			}
			users = append(users, user)
		}
		protocol["users"] = users
		return []map[string]interface{}{runtimeInbound(endpoint, fmt.Sprintf("endpoint-%d", endpoint.ID), endpoint.Port, protocol)}, nil
	}

	if strings.EqualFold(endpoint.Protocol, "trojan") || strings.EqualFold(endpoint.Protocol, "hysteria2") {
		users := make([]interface{}, 0, len(contexts))
		for _, context := range contexts {
			secret, err := h.credentialCipher.Decrypt(context.Credential.Secret)
			if err != nil {
				return nil, fmt.Errorf("decrypt protocol credential %d: %w", context.Credential.ID, err)
			}
			user, err := managedAccessUserFields(context)
			if err != nil {
				return nil, err
			}
			user["password"] = secret
			users = append(users, user)
		}
		delete(protocol, "password")
		protocol["users"] = users
		return []map[string]interface{}{runtimeInbound(endpoint, fmt.Sprintf("endpoint-%d", endpoint.ID), endpoint.Port, protocol)}, nil
	}

	if strings.EqualFold(endpoint.Protocol, "mieru") {
		users, err := h.managedMieruUsers(contexts)
		if err != nil {
			return nil, err
		}
		if includeMieruMigrationFallback(endpoint, suppressMieruFallback) {
			fallback, err := mieruMigrationFallbackUser(endpoint.ID, protocol)
			if err != nil {
				return nil, err
			}
			users = append(users, fallback)
		}
		protocol["users"] = users
		return []map[string]interface{}{runtimeInbound(endpoint, fmt.Sprintf("endpoint-%d", endpoint.ID), endpoint.Port, protocol)}, nil
	}

	inbounds := make([]map[string]interface{}, 0, len(credentials))
	for _, context := range contexts {
		credential := context.Credential
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
		user, err := managedAccessUserFields(context)
		if err != nil {
			return nil, err
		}
		user["password"] = secret
		if cipherName, _ := credentialProtocol["cipher"].(string); strings.HasPrefix(strings.ToLower(strings.TrimSpace(cipherName)), "2022-") {
			identity, _ := credentialProtocol["password"].(string)
			identity = strings.TrimSpace(identity)
			if identity == "" {
				return nil, fmt.Errorf("protocol endpoint %d requires a stable Shadowsocks 2022 identity password", endpoint.ID)
			}
			if identity == secret {
				return nil, fmt.Errorf("protocol credential %d reuses the Shadowsocks 2022 identity password", credential.ID)
			}
			credentialProtocol["identity_password"] = identity
		}
		delete(credentialProtocol, "password")
		credentialProtocol["users"] = []interface{}{user}
		tag := fmt.Sprintf("endpoint-%d-credential-%d", endpoint.ID, credential.ID)
		inbounds = append(inbounds, runtimeInbound(endpoint, tag, credential.ListenPort, credentialProtocol))
	}
	return inbounds, nil
}

func endpointUsesRuntimeCredentials(protocol string, nativeAccess, mieruAccess bool) bool {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "vless", "vmess", "shadowsocks":
		return true
	case "trojan", "hysteria2":
		return nativeAccess
	case "mieru":
		return mieruAccess
	default:
		return false
	}
}

func includeMieruMigrationFallback(endpoint model.ProtocolEndpoint, suppress bool) bool {
	return !endpoint.MieruPrincipalReady && !suppress
}

func mieruMigrationFallbackUser(endpointID uint, protocol map[string]interface{}) (map[string]interface{}, error) {
	password := mieruEndpointPassword(protocol)
	if password == "" {
		return nil, fmt.Errorf("Mieru endpoint %d fallback credential is unavailable", endpointID)
	}
	return map[string]interface{}{
		"username":      password,
		"password":      password,
		"principal_key": fmt.Sprintf("migration:endpoint:%d", endpointID),
	}, nil
}

func (h *handlers) managedMieruUsers(contexts []runtimeCredentialContext) ([]interface{}, error) {
	users := make([]interface{}, 0, len(contexts))
	for _, context := range contexts {
		secret, err := h.credentialCipher.Decrypt(context.Credential.Secret)
		if err != nil {
			return nil, fmt.Errorf("decrypt protocol credential %d: %w", context.Credential.ID, err)
		}
		principalKey := strings.TrimSpace(context.Credential.PrincipalKey)
		if principalKey == "" {
			return nil, fmt.Errorf("protocol credential %d has no principal_key", context.Credential.ID)
		}
		user := map[string]interface{}{"principal_key": principalKey}
		user["username"] = secret
		user["password"] = secret
		users = append(users, user)
	}
	return users, nil
}

func (h *handlers) legacyRuntimeInboundsForEndpoint(endpoint model.ProtocolEndpoint, protocol map[string]interface{}, credentials []model.ProtocolCredential) ([]map[string]interface{}, error) {
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

type runtimeCredentialContext struct {
	Credential           model.ProtocolCredential
	Subscription         model.Subscription
	SoleActiveCredential bool
}

func (h *handlers) runtimeCredentialContexts(credentials []model.ProtocolCredential, now time.Time) ([]runtimeCredentialContext, error) {
	subscriptionIDs := make([]uint, 0, len(credentials))
	seen := make(map[uint]struct{}, len(credentials))
	for _, credential := range credentials {
		if _, exists := seen[credential.SubscriptionID]; exists {
			continue
		}
		seen[credential.SubscriptionID] = struct{}{}
		subscriptionIDs = append(subscriptionIDs, credential.SubscriptionID)
	}
	subscriptions := make([]model.Subscription, 0, len(subscriptionIDs))
	if err := h.db.Where("id IN ?", subscriptionIDs).Find(&subscriptions).Error; err != nil {
		return nil, err
	}
	subscriptionByID := make(map[uint]model.Subscription, len(subscriptions))
	for _, subscription := range subscriptions {
		subscriptionByID[subscription.ID] = subscription
	}
	type countRow struct {
		SubscriptionID uint
		Count          int64
	}
	counts := make([]countRow, 0, len(subscriptionIDs))
	if err := h.db.Model(&model.ProtocolCredential{}).
		Select("subscription_id, COUNT(*) AS count").
		Joins("JOIN protocol_endpoints ON protocol_endpoints.id = protocol_credentials.protocol_endpoint_id").
		Where("subscription_id IN ? AND status = ? AND revoked_at IS NULL AND expires_at > ?", subscriptionIDs, protocolCredentialStatusActive, now).
		Where("LOWER(protocol_endpoints.protocol) IN ?", h.runtimeCredentialProtocols()).
		Group("subscription_id").
		Scan(&counts).Error; err != nil {
		return nil, err
	}
	countBySubscription := make(map[uint]int64, len(counts))
	for _, count := range counts {
		countBySubscription[count.SubscriptionID] = count.Count
	}
	contexts := make([]runtimeCredentialContext, 0, len(credentials))
	for _, credential := range credentials {
		subscription, exists := subscriptionByID[credential.SubscriptionID]
		if !exists {
			return nil, fmt.Errorf("protocol credential %d references a missing subscription", credential.ID)
		}
		contexts = append(contexts, runtimeCredentialContext{
			Credential:           credential,
			Subscription:         subscription,
			SoleActiveCredential: countBySubscription[credential.SubscriptionID] == 1,
		})
	}
	return contexts, nil
}

func managedAccessUserFields(context runtimeCredentialContext) (map[string]interface{}, error) {
	subscription := context.Subscription
	policyRevision := uint64(subscription.ID)
	if timestamp := subscription.UpdatedAt.UTC().UnixMilli(); timestamp > 0 {
		policyRevision = uint64(timestamp)
	}
	user := map[string]interface{}{
		"principal_key":   context.Credential.PrincipalKey,
		"policy_revision": policyRevision,
	}
	// Zero enforces policy per principal in one process. Project subscription-wide
	// limits only when this is the subscription's sole active credential, otherwise
	// each node would independently grant the full allowance.
	if context.SoleActiveCredential && subscription.SpeedLimitMbps > 0 {
		speedMbps := uint64(subscription.SpeedLimitMbps)
		if speedMbps > math.MaxUint64/125_000 {
			return nil, fmt.Errorf("subscription %d speed limit exceeds the Zero policy range", subscription.ID)
		}
		bytesPerSecond := speedMbps * 125_000
		user["up_bps"] = bytesPerSecond
		user["down_bps"] = bytesPerSecond
	}
	if context.SoleActiveCredential && subscription.DeviceLimit > 0 {
		if uint64(subscription.DeviceLimit) > math.MaxUint32 {
			return nil, fmt.Errorf("subscription %d device limit exceeds the Zero policy range", subscription.ID)
		}
		user["device_limit"] = uint32(subscription.DeviceLimit)
	}
	return user, nil
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
	case "shadowsocks", "trojan", "hysteria2", "mieru":
		client["password"] = secret
	}
	payload, err := json.Marshal(client)
	return json.RawMessage(payload), err
}
