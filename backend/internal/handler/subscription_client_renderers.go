package handler

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gopkg.in/yaml.v2"
	"gorm.io/gorm"
)

const (
	subscriptionRendererShadowrocket = "shadowrocket"
	subscriptionRendererQuantumultX  = "quantumult-x"
	subscriptionRendererV2RayN       = "v2rayn"
)

var builtinClientSubscriptionTemplates = []model.SubscriptionTemplate{
	{
		Name:        "Shadowrocket",
		Slug:        "shadowrocket",
		Description: "Shadowrocket 标准分享链接订阅；支持常用 VMess、VLESS、Trojan、Shadowsocks 与 Hysteria2 节点。",
		Renderer:    subscriptionRendererShadowrocket,
		IsActive:    true,
		SortOrder:   -270,
		Revision:    1,
	},
	{
		Name:        "Quantumult X",
		Slug:        "quantumult-x",
		Description: "Quantumult X server_remote 资源；当前生成 Shadowsocks、VMess 与 Trojan 节点。",
		Renderer:    subscriptionRendererQuantumultX,
		IsActive:    true,
		SortOrder:   -260,
		Revision:    1,
	},
	{
		Name:        "v2rayN",
		Slug:        "v2rayn",
		Description: "v2rayN 标准协议分享链接订阅；支持常用 VMess、VLESS、Trojan、Shadowsocks 与 Hysteria2 节点。",
		Renderer:    subscriptionRendererV2RayN,
		IsActive:    true,
		SortOrder:   -250,
		Revision:    1,
	},
}

func init() {
	subscriptionRendererDefinitions[subscriptionRendererShadowrocket] = subscriptionRendererDefinition{
		contentType: "text/plain",
		render:      renderShadowrocketSubscription,
	}
	subscriptionRendererDefinitions[subscriptionRendererQuantumultX] = subscriptionRendererDefinition{
		contentType: "text/plain",
		render:      renderQuantumultXSubscription,
	}
	subscriptionRendererDefinitions[subscriptionRendererV2RayN] = subscriptionRendererDefinition{
		contentType: "text/plain",
		render:      renderV2RayNSubscription,
	}

	wrapSelectionTargetRenderer(subscriptionRendererZnetSink)
	wrapSelectionTargetRenderer(subscriptionRendererClash)
	wrapSelectionTargetRenderer(subscriptionRendererSingBox)
}

// ReconcileSubscriptionClientTemplateDefaults inserts any missing built-in
// client templates without changing existing records. Startup invokes it only
// through the one-time seed guard, so later administrator edits or deletions
// remain authoritative.
func (h *handlers) ReconcileSubscriptionClientTemplateDefaults() error {
	return h.db.Transaction(func(tx *gorm.DB) error {
		for _, definition := range builtinClientSubscriptionTemplates {
			customization, err := json.Marshal(defaultSubscriptionCustomization(definition.Renderer))
			if err != nil {
				return fmt.Errorf("encode %s subscription defaults: %w", definition.Renderer, err)
			}
			definition.Customization = customization
			var existing model.SubscriptionTemplate
			err = tx.Where("slug = ?", definition.Slug).First(&existing).Error
			switch {
			case err == nil:
				continue
			case !errors.Is(err, gorm.ErrRecordNotFound):
				return fmt.Errorf("inspect subscription template %q: %w", definition.Slug, err)
			}
			if err := tx.Create(&definition).Error; err != nil {
				return fmt.Errorf("create subscription template %q: %w", definition.Slug, err)
			}
		}
		return nil
	})
}

func renderShadowrocketSubscription(data subscriptionTemplateData, _ subscriptionTemplateCustomization) (string, error) {
	return renderEncodedShareLinkSubscription(data, "Shadowrocket")
}

func renderV2RayNSubscription(data subscriptionTemplateData, _ subscriptionTemplateCustomization) (string, error) {
	return renderEncodedShareLinkSubscription(data, "v2rayN")
}

func renderEncodedShareLinkSubscription(data subscriptionTemplateData, clientName string) (string, error) {
	links := make([]string, 0, len(data.ProtocolEndpoints))
	for _, endpoint := range data.ProtocolEndpoints {
		link, supported, err := standardSubscriptionShareLink(endpoint)
		if err != nil {
			return "", err
		}
		if supported {
			links = append(links, link)
		}
	}
	if len(links) == 0 {
		return "", fmt.Errorf("%s 当前没有可导出的兼容节点", clientName)
	}
	return base64.StdEncoding.EncodeToString([]byte(strings.Join(links, "\n"))), nil
}

func standardSubscriptionShareLink(endpoint subscriptionTemplateEndpoint) (string, bool, error) {
	config := normalizedZeroClientConfig(endpoint)
	protocol := strings.ToLower(strings.TrimSpace(endpoint.Protocol))
	switch protocol {
	case "vmess":
		return vmessShareLink(endpoint, config)
	case "vless":
		return vlessShareLink(endpoint, config)
	case "trojan":
		return trojanShareLink(endpoint, config)
	case "shadowsocks":
		return shadowsocksShareLink(endpoint, config)
	case "hysteria2":
		return hysteria2ShareLink(endpoint, config)
	default:
		return "", false, nil
	}
}

func vmessShareLink(endpoint subscriptionTemplateEndpoint, config map[string]interface{}) (string, bool, error) {
	id, err := requiredConfigString(endpoint, config, "id", "uuid")
	if err != nil {
		return "", false, err
	}
	payload := map[string]string{
		"v":    "2",
		"ps":   subscriptionEndpointTag(endpoint),
		"add":  configString(config, "server"),
		"port": strconv.Itoa(subscriptionEndpointPort(endpoint)),
		"id":   id,
		"aid":  "0",
		"scy":  configStringDefault(config, "auto", "cipher", "security"),
		"net":  "tcp",
		"type": "none",
	}
	if ws := configMap(config, "ws"); len(ws) > 0 {
		payload["net"] = "ws"
		payload["path"] = configString(ws, "path")
		payload["host"] = configString(configMap(ws, "headers"), "Host")
		if payload["host"] == "" {
			payload["host"] = configString(configMap(ws, "headers"), "host")
		}
	} else if grpc := configMap(config, "grpc"); len(grpc) > 0 {
		payload["net"] = "grpc"
		payload["path"] = firstGrpcServiceName(grpc)
	}
	if tls := configMap(config, "tls"); len(tls) > 0 {
		payload["tls"] = "tls"
		payload["sni"] = firstConfigString(configString(config, "sni"), configString(tls, "server_name"))
		payload["fp"] = firstConfigString(configString(config, "client_fingerprint"), configString(tls, "client_fingerprint"))
		if insecure, ok := firstConfigBool(config, tls, "insecure"); ok && insecure {
			payload["insecure"] = "1"
		}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", false, fmt.Errorf("encode VMess share link: %w", err)
	}
	return "vmess://" + base64.StdEncoding.EncodeToString(raw), true, nil
}

func vlessShareLink(endpoint subscriptionTemplateEndpoint, config map[string]interface{}) (string, bool, error) {
	id, err := requiredConfigString(endpoint, config, "id", "uuid")
	if err != nil {
		return "", false, err
	}
	u := &url.URL{
		Scheme:   "vless",
		User:     url.User(id),
		Host:     net.JoinHostPort(configString(config, "server"), strconv.Itoa(subscriptionEndpointPort(endpoint))),
		Fragment: subscriptionEndpointTag(endpoint),
	}
	query := url.Values{"encryption": {"none"}, "type": {"tcp"}}
	if flow := configString(config, "flow"); flow != "" {
		query.Set("flow", flow)
	}
	if reality := configMap(config, "reality"); len(reality) > 0 {
		query.Set("security", "reality")
		if value := firstConfigString(configString(config, "sni"), configString(reality, "server_name")); value != "" {
			query.Set("sni", value)
		}
		if value := configString(reality, "public_key"); value != "" {
			query.Set("pbk", value)
		}
		if value := configString(reality, "short_id"); value != "" {
			query.Set("sid", value)
		}
		if value := firstConfigString(configString(config, "client_fingerprint"), configString(reality, "client_fingerprint")); value != "" {
			query.Set("fp", value)
		}
	} else if tls := configMap(config, "tls"); len(tls) > 0 {
		query.Set("security", "tls")
		if value := firstConfigString(configString(config, "sni"), configString(tls, "server_name")); value != "" {
			query.Set("sni", value)
		}
		if value := firstConfigString(configString(config, "client_fingerprint"), configString(tls, "client_fingerprint")); value != "" {
			query.Set("fp", value)
		}
	} else {
		query.Set("security", "none")
	}
	applyShareLinkTransport(query, config)
	u.RawQuery = query.Encode()
	return u.String(), true, nil
}

func trojanShareLink(endpoint subscriptionTemplateEndpoint, config map[string]interface{}) (string, bool, error) {
	password, err := requiredConfigString(endpoint, config, "password")
	if err != nil {
		return "", false, err
	}
	u := &url.URL{
		Scheme:   "trojan",
		User:     url.User(password),
		Host:     net.JoinHostPort(configString(config, "server"), strconv.Itoa(subscriptionEndpointPort(endpoint))),
		Fragment: subscriptionEndpointTag(endpoint),
	}
	query := url.Values{"security": {"tls"}, "type": {"tcp"}}
	tls := configMap(config, "tls")
	if value := firstConfigString(configString(config, "sni"), configString(tls, "server_name")); value != "" {
		query.Set("sni", value)
	}
	if insecure, ok := firstConfigBool(config, tls, "insecure"); ok && insecure {
		query.Set("allowInsecure", "1")
	}
	applyShareLinkTransport(query, config)
	u.RawQuery = query.Encode()
	return u.String(), true, nil
}

func shadowsocksShareLink(endpoint subscriptionTemplateEndpoint, config map[string]interface{}) (string, bool, error) {
	password, err := requiredConfigString(endpoint, config, "password")
	if err != nil {
		return "", false, err
	}
	method := configStringDefault(config, "chacha20-ietf-poly1305", "cipher", "method")
	credentials := base64.RawURLEncoding.EncodeToString([]byte(method + ":" + password))
	return "ss://" + credentials + "@" + net.JoinHostPort(configString(config, "server"), strconv.Itoa(subscriptionEndpointPort(endpoint))) + "#" + url.PathEscape(subscriptionEndpointTag(endpoint)), true, nil
}

func hysteria2ShareLink(endpoint subscriptionTemplateEndpoint, config map[string]interface{}) (string, bool, error) {
	password, err := requiredConfigString(endpoint, config, "password")
	if err != nil {
		return "", false, err
	}
	u := &url.URL{
		Scheme:   "hysteria2",
		User:     url.User(password),
		Host:     net.JoinHostPort(configString(config, "server"), strconv.Itoa(subscriptionEndpointPort(endpoint))),
		Fragment: subscriptionEndpointTag(endpoint),
	}
	query := url.Values{}
	tls := configMap(config, "tls")
	if value := firstConfigString(configString(config, "sni"), configString(tls, "server_name")); value != "" {
		query.Set("sni", value)
	}
	if insecure, ok := firstConfigBool(config, tls, "insecure"); ok && insecure {
		query.Set("insecure", "1")
	}
	u.RawQuery = query.Encode()
	return u.String(), true, nil
}

func applyShareLinkTransport(query url.Values, config map[string]interface{}) {
	if ws := configMap(config, "ws"); len(ws) > 0 {
		query.Set("type", "ws")
		if value := configString(ws, "path"); value != "" {
			query.Set("path", value)
		}
		headers := configMap(ws, "headers")
		if value := firstConfigString(configString(headers, "Host"), configString(headers, "host")); value != "" {
			query.Set("host", value)
		}
		return
	}
	if grpc := configMap(config, "grpc"); len(grpc) > 0 {
		query.Set("type", "grpc")
		if value := firstGrpcServiceName(grpc); value != "" {
			query.Set("serviceName", value)
		}
	}
}

func subscriptionEndpointPort(endpoint subscriptionTemplateEndpoint) int {
	if endpoint.PublicPort > 0 {
		return endpoint.PublicPort
	}
	return endpoint.Port
}

func renderQuantumultXSubscription(data subscriptionTemplateData, _ subscriptionTemplateCustomization) (string, error) {
	lines := make([]string, 0, len(data.ProtocolEndpoints))
	for _, endpoint := range data.ProtocolEndpoints {
		line, supported, err := quantumultXServerLine(endpoint)
		if err != nil {
			return "", err
		}
		if supported {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return "# 当前没有 Quantumult X 兼容节点\n", nil
	}
	return strings.Join(lines, "\n"), nil
}

func quantumultXServerLine(endpoint subscriptionTemplateEndpoint) (string, bool, error) {
	config := normalizedZeroClientConfig(endpoint)
	protocol := strings.ToLower(strings.TrimSpace(endpoint.Protocol))
	server := net.JoinHostPort(configString(config, "server"), strconv.Itoa(subscriptionEndpointPort(endpoint)))
	tag := sanitizeQuantumultXValue(subscriptionEndpointTag(endpoint))
	parts := []string{}
	switch protocol {
	case "shadowsocks":
		password, err := requiredConfigString(endpoint, config, "password")
		if err != nil {
			return "", false, err
		}
		parts = append(parts,
			"shadowsocks="+server,
			"method="+sanitizeQuantumultXValue(configStringDefault(config, "chacha20-ietf-poly1305", "cipher", "method")),
			"password="+sanitizeQuantumultXValue(password),
		)
		applyQuantumultXWebSocket(&parts, config, false)
	case "vmess":
		id, err := requiredConfigString(endpoint, config, "id", "uuid")
		if err != nil {
			return "", false, err
		}
		parts = append(parts,
			"vmess="+server,
			"method="+sanitizeQuantumultXValue(configStringDefault(config, "none", "cipher", "security")),
			"password="+sanitizeQuantumultXValue(id),
		)
		applyQuantumultXWebSocket(&parts, config, len(configMap(config, "tls")) > 0)
		if len(configMap(config, "ws")) == 0 && len(configMap(config, "tls")) > 0 {
			parts = append(parts, "obfs=over-tls")
			appendQuantumultXTLS(&parts, config)
		}
	case "trojan":
		password, err := requiredConfigString(endpoint, config, "password")
		if err != nil {
			return "", false, err
		}
		parts = append(parts, "trojan="+server, "password="+sanitizeQuantumultXValue(password), "over-tls=true")
		appendQuantumultXTLS(&parts, config)
	default:
		return "", false, nil
	}
	parts = append(parts, "fast-open=false", "udp-relay=true", "tag="+tag)
	return strings.Join(parts, ", "), true, nil
}

func applyQuantumultXWebSocket(parts *[]string, config map[string]interface{}, tlsEnabled bool) {
	ws := configMap(config, "ws")
	if len(ws) == 0 {
		return
	}
	if tlsEnabled {
		*parts = append(*parts, "obfs=wss")
	} else {
		*parts = append(*parts, "obfs=ws")
	}
	if headers := configMap(ws, "headers"); len(headers) > 0 {
		if host := firstConfigString(configString(headers, "Host"), configString(headers, "host")); host != "" {
			*parts = append(*parts, "obfs-host="+sanitizeQuantumultXValue(host))
		}
	}
	if path := configString(ws, "path"); path != "" {
		*parts = append(*parts, "obfs-uri="+sanitizeQuantumultXValue(path))
	}
	if tlsEnabled {
		appendQuantumultXTLS(parts, config)
	}
}

func appendQuantumultXTLS(parts *[]string, config map[string]interface{}) {
	tls := configMap(config, "tls")
	if host := firstConfigString(configString(config, "sni"), configString(tls, "server_name")); host != "" {
		*parts = append(*parts, "tls-host="+sanitizeQuantumultXValue(host))
	}
	verification := true
	if insecure, ok := firstConfigBool(config, tls, "insecure"); ok {
		verification = !insecure
	}
	*parts = append(*parts, "tls-verification="+strconv.FormatBool(verification))
}

func sanitizeQuantumultXValue(value string) string {
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, ",", " ")
	return strings.TrimSpace(value)
}

func wrapSelectionTargetRenderer(renderer string) {
	definition, ok := subscriptionRendererDefinitions[renderer]
	if !ok || definition.render == nil {
		return
	}
	baseRender := definition.render
	definition.render = func(data subscriptionTemplateData, customization subscriptionTemplateCustomization) (string, error) {
		rendered, err := baseRender(data, customization)
		if err != nil || !selectionTargetOverrideNeeded(customization) {
			return rendered, err
		}
		return applySelectionTargetOverrides(renderer, rendered, customization)
	}
	subscriptionRendererDefinitions[renderer] = definition
}

func selectionTargetOverrideNeeded(customization subscriptionTemplateCustomization) bool {
	for _, group := range customization.PolicyGroups {
		if group.Type == "select" && (!subscriptionPolicyGroupIncludesDirect(group) || !subscriptionPolicyGroupIncludesReject(group)) {
			return true
		}
	}
	return false
}

func applySelectionTargetOverrides(renderer, rendered string, customization subscriptionTemplateCustomization) (string, error) {
	preferences := make(map[string]subscriptionPolicyGroup, len(customization.PolicyGroups))
	for _, group := range customization.PolicyGroups {
		preferences[group.Name] = group
	}

	var document map[string]interface{}
	switch renderer {
	case subscriptionRendererClash:
		var raw interface{}
		if err := yaml.Unmarshal([]byte(rendered), &raw); err != nil {
			return "", fmt.Errorf("parse generated Clash subscription: %w", err)
		}
		normalized, err := normalizeYAMLValue(raw)
		if err != nil {
			return "", err
		}
		document, _ = normalized.(map[string]interface{})
		groups, _ := document["proxy-groups"].([]interface{})
		for _, item := range groups {
			group, _ := item.(map[string]interface{})
			applySelectionMembers(group, "name", "proxies", preferences)
		}
		if err := validateGeneratedSubscriptionDocument(renderer, document); err != nil {
			return "", err
		}
		payload, err := yaml.Marshal(document)
		return string(payload), err
	case subscriptionRendererZnetSink, subscriptionRendererSingBox:
		if err := json.Unmarshal([]byte(rendered), &document); err != nil {
			return "", fmt.Errorf("parse generated %s subscription: %w", renderer, err)
		}
		if renderer == subscriptionRendererZnetSink {
			groups, _ := document["outbound_groups"].([]interface{})
			for _, item := range groups {
				group, _ := item.(map[string]interface{})
				applySelectionMembers(group, "tag", "outbounds", preferences)
			}
		} else {
			outbounds, _ := document["outbounds"].([]interface{})
			for _, item := range outbounds {
				group, _ := item.(map[string]interface{})
				if subscriptionOptionalString(group, "type") == "selector" {
					applySelectionMembers(group, "tag", "outbounds", preferences)
				}
			}
		}
		if err := validateGeneratedSubscriptionDocument(renderer, document); err != nil {
			return "", err
		}
		payload, err := json.MarshalIndent(document, "", "  ")
		if err != nil {
			return "", err
		}
		return string(payload) + "\n", nil
	default:
		return rendered, nil
	}
}

func applySelectionMembers(group map[string]interface{}, nameField, membersField string, preferences map[string]subscriptionPolicyGroup) {
	name := strings.TrimSpace(subscriptionOptionalString(group, nameField))
	preference, ok := preferences[name]
	if !ok || preference.Type != "select" {
		return
	}
	members, err := subscriptionStringList(group[membersField], membersField)
	if err != nil {
		return
	}
	filtered := make([]string, 0, len(members))
	for _, member := range members {
		switch member {
		case "DIRECT":
			if !subscriptionPolicyGroupIncludesDirect(preference) {
				continue
			}
		case "REJECT":
			if !subscriptionPolicyGroupIncludesReject(preference) {
				continue
			}
		}
		filtered = append(filtered, member)
	}
	group[membersField] = filtered
}
