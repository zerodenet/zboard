package zero

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
)

const genericConnectorSince = "0.0.15-rc.2"

var runtimeVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z][0-9A-Za-z.-]*)?$`)

type RuntimeConfigurationRenderRequest struct {
	NodeID                uint
	APIKey                string
	ZeroVersion           string
	NativeConnector       bool
	SuppressMieruFallback bool
	Now                   time.Time
	Snapshot              network.RuntimeConfigurationSnapshot
}

type RuntimeConfigurationRenderer struct {
	Cipher Cipher
}

func (r RuntimeConfigurationRenderer) Render(request RuntimeConfigurationRenderRequest) ([]byte, string, error) {
	if r.Cipher == nil || request.NodeID == 0 || request.Now.IsZero() {
		return nil, "", errors.New("Zero runtime configuration renderer is unavailable")
	}
	panelURL := strings.TrimRight(strings.TrimSpace(request.Snapshot.SiteURL), "/")
	parsedURL, err := url.Parse(panelURL)
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Host == "" {
		return nil, "", errors.New("site_url must be an absolute HTTP(S) URL reachable by the VPS before installing Zero")
	}
	if request.APIKey == "" {
		return nil, "", errors.New("Zero connector credential is unavailable")
	}
	now := request.Now.UTC()
	inbounds := make([]map[string]interface{}, 0, len(request.Snapshot.Endpoints))
	outbounds := make([]interface{}, 0)
	routeRules := make([]interface{}, 0)
	for _, endpoint := range request.Snapshot.Endpoints {
		if !supportedRuntimeProtocol(endpoint.Protocol) {
			return nil, "", fmt.Errorf("protocol endpoint %d cannot be published: 面板无法识别该协议。", endpoint.ID)
		}
		rawConfig, err := r.Cipher.Decrypt(endpoint.ServerConfig)
		if err != nil {
			return nil, "", fmt.Errorf("decrypt protocol endpoint %d config: %w", endpoint.ID, err)
		}
		var protocol map[string]interface{}
		if err := json.Unmarshal([]byte(rawConfig), &protocol); err != nil {
			return nil, "", fmt.Errorf("protocol endpoint %d config is invalid JSON: %w", endpoint.ID, err)
		}
		if kind, _ := protocol["type"].(string); !strings.EqualFold(strings.TrimSpace(kind), endpoint.Protocol) {
			return nil, "", fmt.Errorf("protocol endpoint %d config type must be %s", endpoint.ID, endpoint.Protocol)
		}
		if endpoint.Certificate != nil {
			if err := applyRuntimeCertificate(protocol, endpoint.Protocol, *endpoint.Certificate, now); err != nil {
				return nil, "", fmt.Errorf("protocol endpoint %d managed certificate: %w", endpoint.ID, err)
			}
		}
		if endpoint.Port <= 0 || endpoint.Port > 65535 {
			return nil, "", fmt.Errorf("protocol endpoint %d listen port is invalid", endpoint.ID)
		}
		endpointInbounds, err := r.renderEndpoint(endpoint, protocol, request.SuppressMieruFallback)
		if err != nil {
			return nil, "", fmt.Errorf("compile protocol endpoint %d: %w", endpoint.ID, err)
		}
		inbounds = append(inbounds, endpointInbounds...)
		if len(endpointInbounds) > 0 && strings.TrimSpace(endpoint.EgressConfig) != "" {
			rawEgress, err := r.Cipher.Decrypt(endpoint.EgressConfig)
			if err != nil {
				return nil, "", fmt.Errorf("decrypt protocol endpoint %d egress config: %w", endpoint.ID, err)
			}
			_, canonical, err := network.NormalizeProtocolEndpointEgressConfig(rawEgress)
			if err != nil {
				return nil, "", fmt.Errorf("protocol endpoint %d egress config is invalid: %w", endpoint.ID, err)
			}
			if canonical == "" {
				return nil, "", fmt.Errorf("protocol endpoint %d egress config must contain a proxy outbound", endpoint.ID)
			}
			var egressProtocol map[string]interface{}
			if err := json.Unmarshal([]byte(canonical), &egressProtocol); err != nil {
				return nil, "", fmt.Errorf("protocol endpoint %d egress config is invalid JSON: %w", endpoint.ID, err)
			}
			tag := fmt.Sprintf("endpoint-%d-egress", endpoint.ID)
			outbounds = append(outbounds, map[string]interface{}{"tag": tag, "protocol": egressProtocol})
			routeRules = append(routeRules, map[string]interface{}{
				"condition": map[string]interface{}{"type": "inbound", "values": []string{fmt.Sprintf("endpoint-%d", endpoint.ID)}},
				"action":    map[string]interface{}{"type": "route", "outbound": tag},
			})
		}
	}

	config := map[string]interface{}{
		"inbounds":  inbounds,
		"outbounds": outbounds,
		"mode":      map[string]interface{}{"type": "rule"},
		"route":     map[string]interface{}{"rules": routeRules, "final": map[string]interface{}{"type": "direct"}},
	}
	if err := r.appendNetworkEntries(config, request.Snapshot.NetworkEntries); err != nil {
		return nil, "", err
	}
	inbounds = config["inbounds"].([]map[string]interface{})
	if len(inbounds) == 0 {
		inbounds = append(inbounds, BootstrapControlInbound())
	}
	config["inbounds"] = inbounds
	if request.NativeConnector || UsesGenericConnector(request.ZeroVersion) {
		config["api"] = ConnectorAPIConfig(panelURL, request.NodeID, request.APIKey, parsedURL.Scheme == "http")
	} else {
		config["api"] = LegacyEventAPIConfig(panelURL, request.NodeID, parsedURL.Scheme == "http")
		config["push"] = map[string]interface{}{
			"url": panelURL, "node_id": strconv.FormatUint(uint64(request.NodeID), 10),
			"api_key_env": "ZERO_PANEL_API_KEY", "heartbeat_interval_seconds": 30,
			"pull_commands": true, "command_poll_interval_seconds": 10,
		}
	}
	payload, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil, "", err
	}
	payload = append(payload, '\n')
	digest := sha256.Sum256(payload)
	return payload, hex.EncodeToString(digest[:]), nil
}

func supportedRuntimeProtocol(protocol string) bool {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "vmess", "vless", "trojan", "shadowsocks", "hysteria2", "mieru":
		return true
	default:
		return false
	}
}

func applyRuntimeCertificate(protocol map[string]interface{}, protocolType string, certificate network.RuntimeConfigurationCertificate, now time.Time) error {
	if certificate.NotAfter == nil || !certificate.NotAfter.After(now) {
		return errors.New("certificate is expired")
	}
	if certificate.Status != "active" && certificate.Status != "failed" {
		return fmt.Errorf("certificate status is %s", certificate.Status)
	}
	if strings.TrimSpace(certificate.CertPath) == "" || strings.TrimSpace(certificate.KeyPath) == "" {
		return errors.New("certificate file paths are unavailable")
	}
	switch strings.ToLower(strings.TrimSpace(protocolType)) {
	case "vless":
		tls, ok := protocol["tls"].(map[string]interface{})
		if !ok || tls == nil {
			return errors.New("VLESS endpoint is not configured for TLS")
		}
		tls["cert_path"], tls["key_path"] = certificate.CertPath, certificate.KeyPath
	case "vmess", "trojan":
		tls, _ := protocol["tls"].(map[string]interface{})
		if tls == nil {
			tls = make(map[string]interface{})
			protocol["tls"] = tls
		}
		tls["cert_path"], tls["key_path"] = certificate.CertPath, certificate.KeyPath
	case "hysteria2":
		protocol["cert_path"], protocol["key_path"] = certificate.CertPath, certificate.KeyPath
	default:
		return fmt.Errorf("%s does not support managed certificates", protocolType)
	}
	return nil
}

func (r RuntimeConfigurationRenderer) renderEndpoint(endpoint network.RuntimeConfigurationEndpoint, protocol map[string]interface{}, suppressMieruFallback bool) ([]map[string]interface{}, error) {
	credentials := endpoint.Credentials
	if endpoint.ActiveSubscriptionCount > 0 && len(credentials) == 0 {
		return nil, fmt.Errorf("protocol endpoint %d has %d active subscriptions but no active credentials after reconciliation", endpoint.ID, endpoint.ActiveSubscriptionCount)
	}
	kind := strings.ToLower(strings.TrimSpace(endpoint.Protocol))
	switch kind {
	case "shadowsocks":
		if err := r.configureShadowsocks(protocol, credentials); err != nil {
			return nil, err
		}
		return []map[string]interface{}{runtimeInbound(endpoint.ID, endpoint.Port, fmt.Sprintf("endpoint-%d", endpoint.ID), protocol)}, nil
	case "vless", "vmess":
		users := make([]interface{}, 0, len(credentials))
		defaultCipher, defaultFlow := "aes-128-gcm", ""
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
		for _, credential := range credentials {
			secret, err := r.decryptCredential(credential)
			if err != nil {
				return nil, err
			}
			user, err := managedAccessUserFields(credential)
			if err != nil {
				return nil, err
			}
			user["id"] = secret
			if kind == "vmess" {
				user["cipher"] = defaultCipher
			} else if defaultFlow != "" {
				user["flow"] = defaultFlow
			}
			users = append(users, user)
		}
		protocol["users"] = users
		return []map[string]interface{}{runtimeInbound(endpoint.ID, endpoint.Port, fmt.Sprintf("endpoint-%d", endpoint.ID), protocol)}, nil
	case "trojan", "hysteria2":
		users := make([]interface{}, 0, len(credentials))
		for _, credential := range credentials {
			secret, err := r.decryptCredential(credential)
			if err != nil {
				return nil, err
			}
			user, err := managedAccessUserFields(credential)
			if err != nil {
				return nil, err
			}
			user["password"] = secret
			users = append(users, user)
		}
		if kind == "hysteria2" && len(users) == 0 {
			return []map[string]interface{}{}, nil
		}
		delete(protocol, "password")
		protocol["users"] = users
		return []map[string]interface{}{runtimeInbound(endpoint.ID, endpoint.Port, fmt.Sprintf("endpoint-%d", endpoint.ID), protocol)}, nil
	case "mieru":
		users := make([]interface{}, 0, len(credentials)+1)
		for _, credential := range credentials {
			secret, err := r.decryptCredential(credential)
			if err != nil {
				return nil, err
			}
			principalKey := strings.TrimSpace(credential.PrincipalKey)
			if principalKey == "" {
				return nil, fmt.Errorf("protocol credential %d has no principal_key", credential.ID)
			}
			users = append(users, map[string]interface{}{"principal_key": principalKey, "username": secret, "password": secret})
		}
		if !endpoint.MieruPrincipalReady && !suppressMieruFallback {
			password := mieruEndpointPassword(protocol)
			if password == "" {
				return nil, fmt.Errorf("Mieru endpoint %d fallback credential is unavailable", endpoint.ID)
			}
			users = append(users, map[string]interface{}{"username": password, "password": password, "principal_key": fmt.Sprintf("migration:endpoint:%d", endpoint.ID)})
		}
		if len(users) == 0 {
			return []map[string]interface{}{}, nil
		}
		protocol["users"] = users
		return []map[string]interface{}{runtimeInbound(endpoint.ID, endpoint.Port, fmt.Sprintf("endpoint-%d", endpoint.ID), protocol)}, nil
	default:
		return []map[string]interface{}{runtimeInbound(endpoint.ID, endpoint.Port, fmt.Sprintf("endpoint-%d", endpoint.ID), protocol)}, nil
	}
}

func (r RuntimeConfigurationRenderer) decryptCredential(credential network.RuntimeConfigurationCredential) (string, error) {
	secret, err := r.Cipher.Decrypt(credential.Secret)
	if err != nil {
		return "", fmt.Errorf("decrypt protocol credential %d: %w", credential.ID, err)
	}
	return secret, nil
}

func managedAccessUserFields(credential network.RuntimeConfigurationCredential) (map[string]interface{}, error) {
	policyRevision := uint64(credential.SubscriptionID)
	if timestamp := credential.SubscriptionUpdatedAt.UTC().UnixMilli(); timestamp > 0 {
		policyRevision = uint64(timestamp)
	}
	user := map[string]interface{}{"principal_key": credential.PrincipalKey, "policy_revision": policyRevision}
	if credential.SoleActiveCredential && credential.SpeedLimitMbps > 0 {
		speedMbps := uint64(credential.SpeedLimitMbps)
		if speedMbps > math.MaxUint64/125_000 {
			return nil, fmt.Errorf("subscription %d speed limit exceeds the Zero policy range", credential.SubscriptionID)
		}
		user["up_bps"], user["down_bps"] = speedMbps*125_000, speedMbps*125_000
	}
	if credential.SoleActiveCredential && credential.DeviceLimit > 0 {
		if uint64(credential.DeviceLimit) > math.MaxUint32 {
			return nil, fmt.Errorf("subscription %d device limit exceeds the Zero policy range", credential.SubscriptionID)
		}
		user["device_limit"] = uint32(credential.DeviceLimit)
	}
	return user, nil
}

func (r RuntimeConfigurationRenderer) configureShadowsocks(protocol map[string]interface{}, credentials []network.RuntimeConfigurationCredential) error {
	cipherName, _ := protocol["cipher"].(string)
	if strings.EqualFold(strings.TrimSpace(cipherName), "2022-blake3-chacha20-poly1305") && len(credentials) > 1 {
		return fmt.Errorf("Shadowsocks cipher %s cannot identify multiple managed users on one endpoint port; use a legacy AEAD cipher or a 2022 AES cipher", strings.TrimSpace(cipherName))
	}
	users := make([]interface{}, 0, len(credentials))
	for _, credential := range credentials {
		secret, err := r.decryptCredential(credential)
		if err != nil {
			return err
		}
		user, err := managedAccessUserFields(credential)
		if err != nil {
			return err
		}
		user["password"] = secret
		users = append(users, user)
	}
	if shadowsocksUsesIdentity(cipherName) {
		identity, _ := protocol["password"].(string)
		identity = strings.TrimSpace(identity)
		if identity == "" {
			return fmt.Errorf("Shadowsocks cipher %s requires a stable endpoint identity password", strings.TrimSpace(cipherName))
		}
		for _, rawUser := range users {
			user, _ := rawUser.(map[string]interface{})
			if password, _ := user["password"].(string); password == identity {
				return errors.New("managed Shadowsocks user reuses the endpoint identity password")
			}
		}
		protocol["identity_password"] = identity
	} else {
		delete(protocol, "identity_password")
	}
	delete(protocol, "password")
	protocol["users"] = users
	return nil
}

func shadowsocksUsesIdentity(cipher string) bool {
	switch strings.ToLower(strings.TrimSpace(cipher)) {
	case "2022-blake3-aes-128-gcm", "2022-blake3-aes-256-gcm":
		return true
	default:
		return false
	}
}

func mieruEndpointPassword(protocol map[string]interface{}) string {
	users, _ := protocol["users"].([]interface{})
	if len(users) == 0 {
		return ""
	}
	user, _ := users[0].(map[string]interface{})
	password, _ := user["password"].(string)
	return strings.TrimSpace(password)
}

func runtimeInbound(_ uint, port int, tag string, protocol map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{"tag": tag, "listen": map[string]interface{}{"address": "0.0.0.0", "port": port}, "protocol": protocol}
}

func BootstrapControlInbound() map[string]interface{} {
	return map[string]interface{}{
		"tag": "zboard-control-bootstrap", "listen": map[string]interface{}{"address": "127.0.0.1", "port": 0},
		"protocol": map[string]interface{}{"type": "direct"},
	}
}

type runtimeNetworkPath struct {
	Outbounds []map[string]interface{} `json:"outbounds"`
	Groups    []map[string]interface{} `json:"outbound_groups"`
	Target    string                   `json:"target"`
}

func networkEntryTag(id uint) string { return fmt.Sprintf("entry-%d", id) }

func appendEntryRoute(config map[string]interface{}, entryID uint, action map[string]interface{}) {
	route := config["route"].(map[string]interface{})
	rules, _ := route["rules"].([]interface{})
	route["rules"] = append(rules, map[string]interface{}{"condition": map[string]interface{}{"type": "inbound", "values": []string{networkEntryTag(entryID)}}, "action": action})
}

func (path runtimeNetworkPath) appendTo(config map[string]interface{}, entryID uint, networkKind string) error {
	if networkKind != "tcp" {
		if err := path.validateDatagram(); err != nil {
			return err
		}
	}
	target, err := path.appendGraph(config, networkEntryTag(entryID)+"/")
	if err != nil {
		return err
	}
	appendEntryRoute(config, entryID, map[string]interface{}{"type": "route", "outbound": target})
	return nil
}

func (path runtimeNetworkPath) appendGraph(config map[string]interface{}, prefix string) (string, error) {
	names := map[string]string{}
	for _, list := range [][]map[string]interface{}{path.Outbounds, path.Groups} {
		for _, item := range list {
			tag, _ := item["tag"].(string)
			if tag == "" || names[tag] != "" {
				return "", errors.New("代理路径的 tag 必须非空且唯一")
			}
			names[tag] = prefix + tag
		}
	}
	if names[path.Target] == "" {
		return "", errors.New("请选择代理路径中存在的 target")
	}
	outbounds, _ := config["outbounds"].([]interface{})
	if outbounds == nil {
		outbounds = []interface{}{}
	}
	for _, item := range path.Outbounds {
		item["tag"] = names[item["tag"].(string)]
		outbounds = append(outbounds, item)
	}
	config["outbounds"] = outbounds
	groups, _ := config["outbound_groups"].([]interface{})
	if groups == nil {
		groups = []interface{}{}
	}
	for _, item := range path.Groups {
		item["tag"] = names[item["tag"].(string)]
		memberKey := "outbounds"
		if item["type"] == "relay" {
			memberKey = "proxies"
		}
		members, ok := item[memberKey].([]interface{})
		if !ok || len(members) == 0 {
			return "", errors.New("代理组必须包含 outbounds")
		}
		for index, value := range members {
			name, _ := value.(string)
			if names[name] == "" {
				return "", errors.New("代理组引用了不存在的出口")
			}
			members[index] = names[name]
		}
		for _, key := range []string{"selected", "default"} {
			if value, exists := item[key]; exists {
				name, _ := value.(string)
				if names[name] == "" {
					return "", errors.New("代理组选择了不存在的出口")
				}
				item[key] = names[name]
			}
		}
		groups = append(groups, item)
	}
	config["outbound_groups"] = groups
	return names[path.Target], nil
}

func (path runtimeNetworkPath) validateDatagram() error {
	nodes := map[string]map[string]interface{}{}
	for _, list := range [][]map[string]interface{}{path.Outbounds, path.Groups} {
		for _, item := range list {
			tag, _ := item["tag"].(string)
			nodes[tag] = item
		}
	}
	visited := map[string]bool{}
	var visit func(string, bool) error
	visit = func(tag string, finalHop bool) error {
		key := fmt.Sprintf("%s/%t", tag, finalHop)
		if visited[key] {
			return nil
		}
		visited[key] = true
		item := nodes[tag]
		if item == nil {
			return nil
		}
		if protocol, ok := item["protocol"].(map[string]interface{}); ok {
			kind, _ := protocol["type"].(string)
			if kind == "http" {
				return errors.New("HTTP CONNECT 出口不支持原始 UDP，请选择支持 UDP 的出口")
			}
			if finalHop && (kind == "socks5" || kind == "direct" || kind == "block") {
				return fmt.Errorf("%s 不支持作为 UDP 代理链最后一跳；例如可使用 SOCKS5 → Shadowsocks", kind)
			}
			if kind == "vless" && protocol["flow"] == "xtls-rprx-vision" {
				return errors.New("VLESS Vision 不支持 UDP 转发，请使用兼容 UDP 的路径")
			}
			return nil
		}
		kind, _ := item["type"].(string)
		memberKey := "outbounds"
		if kind == "relay" {
			memberKey = "proxies"
		}
		members, _ := item[memberKey].([]interface{})
		if kind == "relay" {
			if len(members) > 0 {
				last, _ := members[len(members)-1].(string)
				return visit(last, true)
			}
			return nil
		}
		for _, member := range members {
			tag, _ := member.(string)
			if err := visit(tag, finalHop); err != nil {
				return err
			}
		}
		return nil
	}
	return visit(path.Target, false)
}

func (r RuntimeConfigurationRenderer) appendNetworkEntries(config map[string]interface{}, entries []network.RuntimeConfigurationNetworkEntry) error {
	inbounds := config["inbounds"].([]map[string]interface{})
	ports := map[int]bool{}
	for _, inbound := range inbounds {
		listen, _ := inbound["listen"].(map[string]interface{})
		port, _ := listen["port"].(int)
		ports[port] = true
	}
	poolTargets := map[uint]string{}
	for _, entry := range entries {
		if !entry.Landing.EndpointExists {
			return fmt.Errorf("入口 %d 的落地协议不存在", entry.ID)
		}
		if !entry.Landing.NodeExists {
			return fmt.Errorf("入口 %d 的落地节点不存在", entry.ID)
		}
		if !entry.Landing.Active || !entry.Landing.NodeEnabled || entry.Landing.NodeLifecycleStatus == "deleting" {
			continue
		}
		if ports[entry.Port] {
			return fmt.Errorf("入口 %d 的监听端口 %d 已占用", entry.ID, entry.Port)
		}
		ports[entry.Port] = true
		port := entry.Landing.PublicPort
		if port == 0 {
			port = entry.Landing.Port
		}
		if entry.Network == "tcp" && entry.Landing.Protocol == "hysteria2" {
			return fmt.Errorf("入口 %d 的 Hysteria2 落地需要 TCP/UDP 转发", entry.ID)
		}
		inbounds = append(inbounds, map[string]interface{}{
			"tag": networkEntryTag(entry.ID), "listen": map[string]interface{}{"address": "0.0.0.0", "port": entry.Port},
			"udp":      map[string]interface{}{"enabled": entry.Network != "tcp"},
			"protocol": map[string]interface{}{"type": "direct", "target": entry.Landing.Address, "port": port},
		})
		if entry.ProxyPoolID != nil {
			if entry.ProxyPool == nil || entry.ProxyPool.NodeID != entry.NodeID {
				return errors.New("请选择入口节点 A 自己的共享代理池")
			}
			path, err := r.decryptNetworkPath(entry.ProxyPool.Config, "代理池配置无法解密", "代理池配置无效")
			if err != nil {
				return err
			}
			if entry.Network != "tcp" {
				if err := path.validateDatagram(); err != nil {
					return err
				}
			}
			target := poolTargets[*entry.ProxyPoolID]
			if target == "" {
				target, err = path.appendGraph(config, fmt.Sprintf("pool-%d/", *entry.ProxyPoolID))
				if err != nil {
					return err
				}
				poolTargets[*entry.ProxyPoolID] = target
			}
			appendEntryRoute(config, entry.ID, map[string]interface{}{"type": "route", "outbound": target})
		} else if entry.PathConfig != "" {
			path, err := r.decryptNetworkPath(entry.PathConfig, fmt.Sprintf("解密入口 %d 代理路径失败", entry.ID), fmt.Sprintf("入口 %d 代理路径无效", entry.ID))
			if err != nil {
				return err
			}
			if err := path.appendTo(config, entry.ID, entry.Network); err != nil {
				return fmt.Errorf("入口 %d: %w", entry.ID, err)
			}
		} else {
			appendEntryRoute(config, entry.ID, map[string]interface{}{"type": "direct"})
		}
	}
	config["inbounds"] = inbounds
	return nil
}

func (r RuntimeConfigurationRenderer) decryptNetworkPath(ciphertext, decryptMessage, invalidMessage string) (runtimeNetworkPath, error) {
	var path runtimeNetworkPath
	raw, err := r.Cipher.Decrypt(ciphertext)
	if err != nil {
		return path, errors.New(decryptMessage)
	}
	if err := json.Unmarshal([]byte(raw), &path); err != nil {
		return path, errors.New(invalidMessage)
	}
	return path, nil
}

func ConnectorAPIConfig(panelURL string, nodeID uint, apiKey string, allowInsecure bool) map[string]interface{} {
	eventSink := map[string]interface{}{
		"tag": "zboard", "type": "webhook", "url": strings.TrimRight(panelURL, "/") + "/api/zero/events",
		"events":    []string{"engine.started", "engine.stopped", "engine.warning", "config.changed", "stats.sampled", "flow.started", "flow.updated", "flow.completed"},
		"source_id": fmt.Sprintf("node-%d", nodeID), "headers": map[string]string{"authorization": "Bearer " + apiKey},
	}
	if allowInsecure {
		eventSink["allow_insecure"] = true
	}
	return map[string]interface{}{"event_sinks": []interface{}{eventSink}, "outbox_path": "/var/lib/zerodenet/event-outbox.jsonl"}
}

func LegacyEventAPIConfig(panelURL string, nodeID uint, allowInsecure bool) map[string]interface{} {
	eventSink := map[string]interface{}{
		"tag": "zboard", "type": "webhook", "url": strings.TrimRight(panelURL, "/") + "/api/zero/events",
		"events": []string{"flow.updated", "flow.completed"}, "source_id": fmt.Sprintf("node-%d", nodeID), "api_key_env": "ZERO_PANEL_API_KEY",
	}
	if allowInsecure {
		eventSink["allow_insecure"] = true
	}
	return map[string]interface{}{"event_sinks": []interface{}{eventSink}}
}

func UsesGenericConnector(version string) bool {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	if !runtimeVersionPattern.MatchString(version) {
		return false
	}
	return (compareRuntimeVersions(version, "0.0.1") >= 0 && compareRuntimeVersions(version, "0.0.4-0") < 0) || compareRuntimeVersions(version, genericConnectorSince) >= 0
}

func compareRuntimeVersions(left, right string) int {
	type parsedVersion struct {
		core       [3]int
		prerelease []string
	}
	parse := func(raw string) (parsedVersion, bool) {
		var result parsedVersion
		versionParts := strings.SplitN(strings.TrimPrefix(strings.TrimSpace(raw), "v"), "-", 2)
		parts := strings.Split(versionParts[0], ".")
		if len(parts) != 3 {
			return result, false
		}
		for index, part := range parts {
			value, err := strconv.Atoi(part)
			if err != nil || value < 0 {
				return result, false
			}
			result.core[index] = value
		}
		if len(versionParts) == 2 {
			result.prerelease = strings.Split(versionParts[1], ".")
		}
		return result, true
	}
	l, lok := parse(left)
	r, rok := parse(right)
	if !lok || !rok {
		return 0
	}
	for index := range l.core {
		if l.core[index] < r.core[index] {
			return -1
		}
		if l.core[index] > r.core[index] {
			return 1
		}
	}
	if len(l.prerelease) == 0 && len(r.prerelease) == 0 {
		return 0
	}
	if len(l.prerelease) == 0 {
		return 1
	}
	if len(r.prerelease) == 0 {
		return -1
	}
	limit := len(l.prerelease)
	if len(r.prerelease) < limit {
		limit = len(r.prerelease)
	}
	for index := 0; index < limit; index++ {
		li, lerr := strconv.Atoi(l.prerelease[index])
		ri, rerr := strconv.Atoi(r.prerelease[index])
		switch {
		case lerr == nil && rerr == nil && li < ri:
			return -1
		case lerr == nil && rerr == nil && li > ri:
			return 1
		case lerr == nil && rerr != nil:
			return -1
		case lerr != nil && rerr == nil:
			return 1
		case l.prerelease[index] < r.prerelease[index]:
			return -1
		case l.prerelease[index] > r.prerelease[index]:
			return 1
		}
	}
	if len(l.prerelease) < len(r.prerelease) {
		return -1
	}
	if len(l.prerelease) > len(r.prerelease) {
		return 1
	}
	return 0
}
