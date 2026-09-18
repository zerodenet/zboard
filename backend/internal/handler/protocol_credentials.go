package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/application"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	networkcap "github.com/zerodenet/zboard/backend/internal/capabilities/network"

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

func (h *handlers) storedSubscriptionCredentialProtocols() []string {
	protocols := make([]string, 0, 6)
	for _, protocol := range []string{"vless", "vmess", "shadowsocks", "trojan", "hysteria2", "mieru"} {
		if h.protocolStoresSubscriptionCredential(protocol) {
			protocols = append(protocols, protocol)
		}
	}
	return protocols
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

func protocolCredentialsCurrentForEndpoints(subscription model.Subscription, endpoints []model.ProtocolEndpoint, credentials []model.ProtocolCredential, mieruAccess bool) bool {
	credentialsByEndpoint := make(map[uint]model.ProtocolCredential, len(credentials))
	for _, credential := range credentials {
		credentialsByEndpoint[credential.ProtocolEndpointID] = credential
	}
	for _, endpoint := range endpoints {
		if !protocolStoresSubscriptionCredentialWithMieru(endpoint.Protocol, mieruAccess) {
			continue
		}
		credential, exists := credentialsByEndpoint[endpoint.ID]
		if !exists ||
			credential.UserID != subscription.UserID ||
			credential.NodeID != endpoint.NodeID ||
			credential.ListenPort != endpoint.Port ||
			credential.PublicPort != endpoint.PublicPort ||
			credential.Status != desiredProtocolCredentialStatusWithMieru(endpoint, mieruAccess) ||
			!credential.ExpiresAt.Equal(subscription.EndAt) ||
			credential.RevokedAt != nil {
			return false
		}
	}
	return true
}

func (h *handlers) ensureCredentialsForSubscriptions(subscriptions []model.Subscription) error {
	return h.ensureCredentialsForSubscriptionsWithMieru(subscriptions, h.zeroMieruAccess)
}

func (h *handlers) ensureCredentialsForSubscriptionsWithMieru(subscriptions []model.Subscription, mieruAccess bool) error {
	targets := make([]entitlements.CredentialSubscription, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		targets = append(targets, entitlements.CredentialSubscription{ID: subscription.ID, UserID: subscription.UserID, NodeGroupID: subscription.NodeGroupID, EndAt: subscription.EndAt})
	}
	return h.services.CredentialPreparation(h.credentialCipher, mieruAccess).Ensure(context.Background(), targets)
}

func (h *handlers) reconcileNodeGroupCredentials(groupID uint) error {
	return h.reconcileNodeGroupCredentialsContext(context.Background(), groupID)
}

func (h *handlers) reconcileNodeGroupCredentialsContext(ctx context.Context, groupID uint) error {
	return h.services.GroupCredentialReconciliation(h.credentialCipher, h.zeroMieruAccess).ReconcileGroup(ctx, groupID)
}

func (h *handlers) newProtocolCredentialSecret(endpoint model.ProtocolEndpoint) (string, error) {
	return application.NewSubscriptionSecret(h.credentialCipher, endpoint.Protocol, endpoint.ServerConfig)
}

func prepareMieruEndpointConfigsWithExisting(serverRaw, clientRaw, existingRaw string) (string, string, error) {
	server, client, err := networkcap.NormalizeMieruEndpointConfigs(serverRaw, clientRaw, existingRaw)
	var validation *networkcap.ProtocolEndpointMutationValidation
	if errors.As(err, &validation) {
		return "", "", validationError(validation.Message, validation.Fields)
	}
	return server, client, err
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
	reconciliation, err := h.services.MieruEndpointConfigurations(h.credentialCipher).Reconcile(context.Background())
	if err != nil {
		return err
	}
	endpoints := reconciliation.Endpoints
	changedEndpointIDs := make(map[uint]bool, len(reconciliation.ChangedEndpointIDs))
	for _, endpointID := range reconciliation.ChangedEndpointIDs {
		changedEndpointIDs[endpointID] = true
	}
	changedNodes := make(map[uint]uint)
	for _, endpoint := range endpoints {
		if !changedEndpointIDs[endpoint.ID] {
			continue
		}
		if h.zeroMieruAccess || endpoint.MieruPrincipalReady {
			if _, exists := changedNodes[endpoint.NodeID]; !exists {
				changedNodes[endpoint.NodeID] = endpoint.ID
			}
		}
	}
	managedTargets, err := h.services.ManagedPublicationInventory.Targets(context.Background(), []string{"trojan", "hysteria2"})
	if err != nil {
		return err
	}
	for _, target := range managedTargets {
		if _, exists := changedNodes[target.NodeID]; !exists {
			changedNodes[target.NodeID] = target.TriggerEndpointID
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
		if err := h.queueMieruEndpointPublications(changedNodes); err != nil {
			return err
		}
		return nil
	}
	now := time.Now().UTC()
	preparation := h.services.CredentialPreparation(h.credentialCipher, h.zeroMieruAccess)
	subscriptions, err := preparation.ActiveMieru(context.Background(), now)
	if err != nil {
		return fmt.Errorf("load active Mieru subscriptions: %w", err)
	}
	if err := preparation.Ensure(context.Background(), subscriptions); err != nil {
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
	if err := h.queueMieruEndpointPublications(changedNodes); err != nil {
		return err
	}
	return nil
}

func (h *handlers) queueMieruEndpointPublications(targets map[uint]uint) error {
	requests := make([]networkcap.PublicationRequest, 0, len(targets))
	for nodeID, endpointID := range targets {
		requests = append(requests, networkcap.PublicationRequest{NodeID: nodeID, TriggerEndpointID: endpointID})
	}
	if err := h.services.PublicationRequests.Queue(context.Background(), requests); err != nil {
		return err
	}
	return nil
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
	client["port"] = protocolCredentialClientPort(endpoint, credential)
	switch strings.ToLower(endpoint.Protocol) {
	case "vless", "vmess":
		client["id"] = secret
	case "shadowsocks", "trojan", "hysteria2", "mieru":
		client["password"] = secret
	}
	payload, err := json.Marshal(client)
	return json.RawMessage(payload), err
}
