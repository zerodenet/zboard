package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v2"
)

type clashSubscriptionDocument struct {
	MixedPort     int                          `yaml:"mixed-port,omitempty"`
	AllowLAN      bool                         `yaml:"allow-lan,omitempty"`
	BindAddress   string                       `yaml:"bind-address,omitempty"`
	Mode          string                       `yaml:"mode,omitempty"`
	LogLevel      string                       `yaml:"log-level,omitempty"`
	UnifiedDelay  bool                         `yaml:"unified-delay,omitempty"`
	TCPConcurrent bool                         `yaml:"tcp-concurrent,omitempty"`
	DNS           map[string]interface{}       `yaml:"dns,omitempty"`
	Profile       map[string]interface{}       `yaml:"profile,omitempty"`
	Proxies       []map[string]interface{}     `yaml:"proxies"`
	ProxyGroups   []clashSubscriptionGroup     `yaml:"proxy-groups"`
	RuleProviders map[string]clashRuleProvider `yaml:"rule-providers,omitempty"`
	Rules         []string                     `yaml:"rules"`
}

type clashSubscriptionGroup struct {
	Name      string   `yaml:"name"`
	Type      string   `yaml:"type"`
	Proxies   []string `yaml:"proxies"`
	URL       string   `yaml:"url,omitempty"`
	Interval  int      `yaml:"interval,omitempty"`
	Tolerance int      `yaml:"tolerance,omitempty"`
}

type clashRuleProvider struct {
	Type     string `yaml:"type"`
	URL      string `yaml:"url"`
	Path     string `yaml:"path"`
	Interval int    `yaml:"interval"`
	Behavior string `yaml:"behavior"`
	Format   string `yaml:"format"`
}

func renderZnetSinkSubscription(data subscriptionTemplateData, customization subscriptionTemplateCustomization) (string, error) {
	outbounds := []map[string]interface{}{
		{"tag": "direct", "protocol": map[string]interface{}{"type": "direct"}},
		{"tag": "block", "protocol": map[string]interface{}{"type": "block"}},
		{"tag": "DIRECT", "protocol": map[string]interface{}{"type": "direct"}},
		{"tag": "REJECT", "protocol": map[string]interface{}{"type": "block"}},
	}
	proxyTags := make([]string, 0, len(data.ProtocolEndpoints))
	availableTags := make(map[string]struct{}, len(data.ProtocolEndpoints))
	for _, endpoint := range data.ProtocolEndpoints {
		tag := subscriptionEndpointTag(endpoint)
		protocol, err := zeroOutboundProtocol(endpoint)
		if err != nil {
			return "", err
		}
		proxyTags = append(proxyTags, tag)
		availableTags[tag] = struct{}{}
		outbounds = append(outbounds, map[string]interface{}{
			"tag":      tag,
			"protocol": protocol,
		})
	}

	groupNames := subscriptionPolicyGroupNames(customization.PolicyGroups)
	outboundGroups := make([]map[string]interface{}, 0, len(customization.PolicyGroups))
	for _, group := range customization.PolicyGroups {
		members, err := subscriptionPolicyGroupMembers(group, data.ProtocolEndpoints, availableTags, groupNames)
		if err != nil {
			return "", err
		}
		members = appendPermanentSelectionTargets(group, members)
		rendered := map[string]interface{}{
			"tag": group.Name, "type": zeroSubscriptionPolicyGroupType(group.Type), "outbounds": members,
		}
		switch group.Type {
		case "select":
			if group.DefaultGroup != "" {
				rendered["default"] = groupNames[group.DefaultGroup]
			}
		case "urltest":
			rendered["url"] = group.ProbeURL
			rendered["interval_seconds"] = group.Interval
		}
		outboundGroups = append(outboundGroups, rendered)
	}
	final := zeroSubscriptionFinalAction(customization.Final, groupNames)
	ruleSets := make([]map[string]interface{}, 0, len(customization.RuleSets))
	rules := make([]map[string]interface{}, 0, len(customization.RuleSets))
	for _, item := range customization.RuleSets {
		ruleSets = append(ruleSets, map[string]interface{}{
			"tag": item.Tag, "type": "url", "path": subscriptionRuleSetCachePath(subscriptionRendererZnetSink, item),
			"url": item.URL, "update_interval_seconds": item.Interval, "format": item.Format,
		})
		rules = append(rules, map[string]interface{}{
			"condition": map[string]interface{}{"type": "rule_set", "tag": item.Tag},
			"action":    zeroSubscriptionRuleAction(item.Target, groupNames),
		})
	}
	document := map[string]interface{}{
		"outbounds":       outbounds,
		"outbound_groups": outboundGroups,
		"route": map[string]interface{}{
			"rule_sets": ruleSets,
			"rules":     rules,
			"final":     final,
		},
	}
	if err := applySubscriptionAdvancedOverlay(subscriptionRendererZnetSink, document, customization.AdvancedSource, proxyTags); err != nil {
		return "", err
	}
	if err := validateGeneratedSubscriptionDocument(subscriptionRendererZnetSink, document); err != nil {
		return "", err
	}
	return marshalIndentedJSON(document)
}

func renderClashSubscription(data subscriptionTemplateData, customization subscriptionTemplateCustomization) (string, error) {
	proxies := make([]map[string]interface{}, 0, len(data.ProtocolEndpoints))
	proxyNames := make([]string, 0, len(data.ProtocolEndpoints))
	availableTags := make(map[string]struct{}, len(data.ProtocolEndpoints))
	for _, endpoint := range data.ProtocolEndpoints {
		proxy, err := clashProxy(endpoint)
		if err != nil {
			return "", err
		}
		proxies = append(proxies, proxy)
		tag := subscriptionEndpointTag(endpoint)
		proxyNames = append(proxyNames, tag)
		availableTags[tag] = struct{}{}
	}
	groupNames := subscriptionPolicyGroupNames(customization.PolicyGroups)
	ruleProviders := make(map[string]clashRuleProvider, len(customization.RuleSets))
	rules := make([]string, 0, len(customization.RuleSets)+1)
	for _, item := range customization.RuleSets {
		ruleProviders[item.Tag] = clashRuleProvider{
			Type: "http", URL: item.URL, Path: subscriptionRuleSetCachePath(subscriptionRendererClash, item),
			Interval: item.Interval, Behavior: item.Behavior, Format: item.Format,
		}
		rules = append(rules, fmt.Sprintf("RULE-SET,%s,%s", item.Tag, clashSubscriptionActionTarget(item.Target, groupNames)))
	}
	policyGroups := make([]clashSubscriptionGroup, 0, len(customization.PolicyGroups))
	for _, group := range customization.PolicyGroups {
		members, err := subscriptionPolicyGroupMembers(group, data.ProtocolEndpoints, availableTags, groupNames)
		if err != nil {
			return "", err
		}
		members = appendPermanentSelectionTargets(group, members)
		rendered := clashSubscriptionGroup{
			Name: group.Name, Type: clashSubscriptionPolicyGroupType(group.Type), Proxies: members,
		}
		if group.Type == "urltest" || group.Type == "fallback" {
			rendered.URL = group.ProbeURL
			rendered.Interval = group.Interval
			rendered.Tolerance = group.Tolerance
		}
		policyGroups = append(policyGroups, rendered)
	}
	document := clashSubscriptionDocument{
		Proxies:       proxies,
		ProxyGroups:   policyGroups,
		RuleProviders: ruleProviders,
	}
	rules = append(rules, "MATCH,"+clashSubscriptionActionTarget(customization.Final, groupNames))
	document.Rules = rules
	basePayload, err := yaml.Marshal(document)
	if err != nil {
		return "", fmt.Errorf("encode Clash subscription: %w", err)
	}
	var base interface{}
	if err := yaml.Unmarshal(basePayload, &base); err != nil {
		return "", fmt.Errorf("decode generated Clash subscription: %w", err)
	}
	normalized, err := normalizeYAMLValue(base)
	if err != nil {
		return "", fmt.Errorf("normalize generated Clash subscription: %w", err)
	}
	baseMap, ok := normalized.(map[string]interface{})
	if !ok {
		return "", errors.New("generated Clash subscription is not an object")
	}
	if err := applySubscriptionAdvancedOverlay(subscriptionRendererClash, baseMap, customization.AdvancedSource, proxyNames); err != nil {
		return "", err
	}
	if err := validateGeneratedSubscriptionDocument(subscriptionRendererClash, baseMap); err != nil {
		return "", err
	}
	rendered, err := yaml.Marshal(baseMap)
	if err != nil {
		return "", fmt.Errorf("encode customized Clash subscription: %w", err)
	}
	return string(rendered), nil
}

func renderSingBoxSubscription(data subscriptionTemplateData, customization subscriptionTemplateCustomization) (string, error) {
	outbounds := make([]map[string]interface{}, 0, len(data.ProtocolEndpoints)+2)
	proxyTags := make([]string, 0, len(data.ProtocolEndpoints)+1)
	availableTags := make(map[string]struct{}, len(data.ProtocolEndpoints))
	for _, endpoint := range data.ProtocolEndpoints {
		// sing-box does not provide a Mieru outbound. Other formats keep the
		// endpoint; this export omits it instead of emitting an invalid type.
		if strings.EqualFold(endpoint.Protocol, "mieru") {
			continue
		}
		outbound, err := singBoxOutbound(endpoint)
		if err != nil {
			return "", err
		}
		outbounds = append(outbounds, outbound)
		tag := subscriptionEndpointTag(endpoint)
		proxyTags = append(proxyTags, tag)
		availableTags[tag] = struct{}{}
	}
	outbounds = append(outbounds,
		map[string]interface{}{"type": "direct", "tag": "direct"},
		map[string]interface{}{"type": "direct", "tag": "DIRECT"},
		map[string]interface{}{"type": "block", "tag": "REJECT"},
	)
	groupNames := subscriptionPolicyGroupNames(customization.PolicyGroups)
	for _, group := range customization.PolicyGroups {
		members, err := subscriptionPolicyGroupMembers(group, data.ProtocolEndpoints, availableTags, groupNames)
		if err != nil {
			return "", err
		}
		members = appendPermanentSelectionTargets(group, members)
		rendered := map[string]interface{}{
			"type": singBoxSubscriptionPolicyGroupType(group.Type), "tag": group.Name, "outbounds": members,
		}
		if group.Type == "select" && group.DefaultGroup != "" {
			rendered["default"] = groupNames[group.DefaultGroup]
		}
		if group.Type == "urltest" {
			rendered["url"] = group.ProbeURL
			rendered["interval"] = fmt.Sprintf("%ds", group.Interval)
			rendered["tolerance"] = group.Tolerance
		}
		outbounds = append(outbounds, rendered)
	}
	finalTag := singBoxSubscriptionActionTarget(customization.Final, groupNames)
	ruleSets := make([]map[string]interface{}, 0, len(customization.RuleSets))
	rules := make([]map[string]interface{}, 0, len(customization.RuleSets))
	for _, item := range customization.RuleSets {
		ruleSets = append(ruleSets, map[string]interface{}{
			"type": "remote", "tag": item.Tag, "format": item.Format, "url": item.URL,
			"update_interval": fmt.Sprintf("%ds", item.Interval),
		})
		rule := map[string]interface{}{"rule_set": []string{item.Tag}}
		if item.Target == subscriptionTargetReject {
			rule["action"] = "reject"
		} else {
			rule["action"] = "route"
			rule["outbound"] = singBoxSubscriptionActionTarget(item.Target, groupNames)
		}
		rules = append(rules, rule)
	}
	document := map[string]interface{}{
		"outbounds": outbounds,
		"route":     map[string]interface{}{"rules": rules, "rule_set": ruleSets, "final": finalTag},
	}
	if err := applySubscriptionAdvancedOverlay(subscriptionRendererSingBox, document, customization.AdvancedSource, proxyTags); err != nil {
		return "", err
	}
	if err := validateGeneratedSubscriptionDocument(subscriptionRendererSingBox, document); err != nil {
		return "", err
	}
	return marshalIndentedJSON(document)
}

func subscriptionPolicyGroupNames(groups []subscriptionPolicyGroup) map[string]string {
	names := make(map[string]string, len(groups))
	for _, group := range groups {
		names[group.ID] = group.Name
	}
	return names
}

func subscriptionPolicyGroupMembers(
	group subscriptionPolicyGroup,
	endpoints []subscriptionTemplateEndpoint,
	availableTags map[string]struct{},
	groupNames map[string]string,
) ([]string, error) {
	include, err := compileOptionalSubscriptionPattern(group.IncludePattern)
	if err != nil {
		return nil, fmt.Errorf("策略组 %q 包含正则无效: %w", group.Name, err)
	}
	exclude, err := compileOptionalSubscriptionPattern(group.ExcludePattern)
	if err != nil {
		return nil, fmt.Errorf("策略组 %q 排除正则无效: %w", group.Name, err)
	}
	members := make([]string, 0, len(group.IncludeGroups)+len(endpoints))
	if group.DefaultGroup != "" {
		members = append(members, groupNames[group.DefaultGroup])
	}
	for _, groupID := range group.IncludeGroups {
		if groupID == group.DefaultGroup {
			continue
		}
		members = append(members, groupNames[groupID])
	}
	for _, endpoint := range endpoints {
		tag := subscriptionEndpointTag(endpoint)
		if _, exists := availableTags[tag]; !exists {
			continue
		}
		name := strings.TrimSpace(endpoint.Name)
		if include != nil && !include.MatchString(name) {
			continue
		}
		if exclude != nil && exclude.MatchString(name) {
			continue
		}
		members = append(members, tag)
	}
	members = uniqueSubscriptionTargets(members)
	if len(members) == 0 {
		return nil, fmt.Errorf("策略组 %q 没有匹配任何协议配置名称，也没有包含其他策略组", group.Name)
	}
	return members, nil
}

func compileOptionalSubscriptionPattern(pattern string) (*regexp.Regexp, error) {
	if strings.TrimSpace(pattern) == "" {
		return nil, nil
	}
	return regexp.Compile(pattern)
}

func uniqueSubscriptionTargets(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	result := make([]string, 0, len(items))
	for _, item := range items {
		if item == "" {
			continue
		}
		if _, exists := seen[item]; exists {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}

func appendPermanentSelectionTargets(group subscriptionPolicyGroup, members []string) []string {
	if group.Type != "select" {
		return members
	}
	return uniqueSubscriptionTargets(append(members, "DIRECT", "REJECT"))
}

func zeroSubscriptionPolicyGroupType(groupType string) string {
	switch groupType {
	case "select":
		return "selector"
	case "urltest":
		return "url_test"
	default:
		return groupType
	}
}

func clashSubscriptionPolicyGroupType(groupType string) string {
	if groupType == "urltest" {
		return "url-test"
	}
	return groupType
}

func singBoxSubscriptionPolicyGroupType(groupType string) string {
	if groupType == "select" {
		return "selector"
	}
	return groupType
}

func subscriptionTargetGroupName(target string, groupNames map[string]string) string {
	return groupNames[strings.TrimPrefix(target, subscriptionTargetGroupPrefix)]
}

func zeroSubscriptionFinalAction(target string, groupNames map[string]string) map[string]interface{} {
	if target == subscriptionTargetDirect {
		return map[string]interface{}{"type": "direct"}
	}
	return map[string]interface{}{"type": "route", "outbound": subscriptionTargetGroupName(target, groupNames)}
}

func zeroSubscriptionRuleAction(target string, groupNames map[string]string) map[string]interface{} {
	switch target {
	case subscriptionTargetDirect:
		return map[string]interface{}{"type": "direct"}
	case subscriptionTargetReject:
		return map[string]interface{}{"type": "reject"}
	default:
		return map[string]interface{}{"type": "route", "outbound": subscriptionTargetGroupName(target, groupNames)}
	}
}

func clashSubscriptionActionTarget(target string, groupNames map[string]string) string {
	switch target {
	case subscriptionTargetDirect:
		return "DIRECT"
	case subscriptionTargetReject:
		return "REJECT"
	default:
		return subscriptionTargetGroupName(target, groupNames)
	}
}

func singBoxSubscriptionActionTarget(target string, groupNames map[string]string) string {
	if target == subscriptionTargetDirect {
		return "direct"
	}
	return subscriptionTargetGroupName(target, groupNames)
}

func zeroOutboundProtocol(endpoint subscriptionTemplateEndpoint) (map[string]interface{}, error) {
	config := normalizedZeroClientConfig(endpoint)
	protocol := strings.ToLower(strings.TrimSpace(endpoint.Protocol))
	outbound := map[string]interface{}{
		"type":   protocol,
		"server": config["server"],
		"port":   config["port"],
	}
	switch protocol {
	case "vless":
		id, err := requiredConfigString(endpoint, config, "id", "uuid")
		if err != nil {
			return nil, err
		}
		outbound["id"] = id
		for _, key := range []string{
			"flow", "mux_concurrency", "xudp_concurrency", "mux_idle_timeout_secs",
			"mux_response_backlog_frames", "mux_response_backlog_bytes",
		} {
			copyConfigValue(outbound, config, key, key)
		}
		copySanitizedMap(outbound, config, "tls", []string{
			"server_name", "disable_sni", "ca_cert_path", "insecure", "alpn", "client_fingerprint",
		})
		copySanitizedMap(outbound, config, "reality", []string{"public_key", "short_id", "server_name", "cipher_suites", "client_fingerprint"})
		copyZeroWebSocket(outbound, config)
		copyZeroGrpc(outbound, config)
		copySanitizedMap(outbound, config, "h2", []string{"host", "path"})
		copySanitizedMap(outbound, config, "http_upgrade", []string{"host", "path"})
		copySanitizedMap(outbound, config, "split_http", []string{"host", "path", "mode"})
		copySanitizedMap(outbound, config, "quic", []string{"server_name", "ca_cert_path", "insecure"})
	case "vmess":
		id, err := requiredConfigString(endpoint, config, "id", "uuid")
		if err != nil {
			return nil, err
		}
		outbound["id"] = id
		outbound["cipher"] = configStringDefault(config, "aes-128-gcm", "cipher", "security")
		for _, key := range []string{
			"mux_concurrency", "mux_idle_timeout_secs", "mux_response_backlog_frames", "mux_response_backlog_bytes",
		} {
			copyConfigValue(outbound, config, key, key)
		}
		copySanitizedMap(outbound, config, "tls", []string{
			"server_name", "disable_sni", "ca_cert_path", "insecure", "alpn", "client_fingerprint",
		})
		copyZeroWebSocket(outbound, config)
		copyZeroGrpc(outbound, config)
	case "trojan", "hysteria2":
		password, err := requiredConfigString(endpoint, config, "password")
		if err != nil {
			return nil, err
		}
		outbound["password"] = password
		copyConfigValue(outbound, config, "insecure", "insecure")
		copyConfigValue(outbound, config, "client_fingerprint", "client_fingerprint")
	case "shadowsocks":
		password, err := requiredConfigString(endpoint, config, "password")
		if err != nil {
			return nil, err
		}
		outbound["password"] = password
		outbound["cipher"] = configStringDefault(config, "chacha20-ietf-poly1305", "cipher", "method")
	case "mieru":
		password, err := requiredConfigString(endpoint, config, "password")
		if err != nil {
			return nil, err
		}
		outbound["password"] = password
		copyConfigValue(outbound, config, "username", "username")
	default:
		return nil, fmt.Errorf("endpoint %d uses unsupported Zero protocol %q", endpoint.ID, endpoint.Protocol)
	}
	return outbound, nil
}

func clashProxy(endpoint subscriptionTemplateEndpoint) (map[string]interface{}, error) {
	config := normalizedZeroClientConfig(endpoint)
	protocol := strings.ToLower(strings.TrimSpace(endpoint.Protocol))
	proxyType := protocol
	if protocol == "shadowsocks" {
		proxyType = "ss"
	}
	proxy := map[string]interface{}{
		"name":   subscriptionEndpointTag(endpoint),
		"type":   proxyType,
		"server": config["server"],
		"port":   config["port"],
	}
	switch protocol {
	case "vless":
		id, err := requiredConfigString(endpoint, config, "id", "uuid")
		if err != nil {
			return nil, err
		}
		proxy["uuid"] = id
		proxy["udp"] = true
		copyConfigValue(proxy, config, "flow", "flow")
	case "vmess":
		id, err := requiredConfigString(endpoint, config, "id", "uuid")
		if err != nil {
			return nil, err
		}
		proxy["uuid"] = id
		proxy["alterId"] = 0
		proxy["cipher"] = configStringDefault(config, "auto", "cipher", "security")
		proxy["udp"] = true
	case "trojan":
		password, err := requiredConfigString(endpoint, config, "password")
		if err != nil {
			return nil, err
		}
		proxy["password"] = password
		proxy["udp"] = true
	case "shadowsocks":
		password, err := requiredConfigString(endpoint, config, "password")
		if err != nil {
			return nil, err
		}
		proxy["cipher"] = configStringDefault(config, "chacha20-ietf-poly1305", "cipher", "method")
		proxy["password"] = password
		proxy["udp"] = true
	case "hysteria2":
		password, err := requiredConfigString(endpoint, config, "password")
		if err != nil {
			return nil, err
		}
		proxy["password"] = password
	case "mieru":
		password, err := requiredConfigString(endpoint, config, "password")
		if err != nil {
			return nil, err
		}
		proxy["username"] = configStringDefault(config, password, "username")
		proxy["password"] = password
		proxy["transport"] = strings.ToUpper(configStringDefault(config, "TCP", "transport"))
		proxy["multiplexing"] = configStringDefault(config, "MULTIPLEXING_LOW", "multiplexing")
	default:
		return nil, fmt.Errorf("endpoint %d uses unsupported Clash protocol %q", endpoint.ID, endpoint.Protocol)
	}
	addClashTLS(proxy, config, protocol)
	addClashTransport(proxy, config)
	return proxy, nil
}

func singBoxOutbound(endpoint subscriptionTemplateEndpoint) (map[string]interface{}, error) {
	config := normalizedZeroClientConfig(endpoint)
	protocol := strings.ToLower(strings.TrimSpace(endpoint.Protocol))
	outbound := map[string]interface{}{
		"type":        protocol,
		"tag":         subscriptionEndpointTag(endpoint),
		"server":      config["server"],
		"server_port": config["port"],
	}
	switch protocol {
	case "vless":
		id, err := requiredConfigString(endpoint, config, "id", "uuid")
		if err != nil {
			return nil, err
		}
		outbound["uuid"] = id
		copyConfigValue(outbound, config, "flow", "flow")
	case "vmess":
		id, err := requiredConfigString(endpoint, config, "id", "uuid")
		if err != nil {
			return nil, err
		}
		outbound["uuid"] = id
		outbound["security"] = configStringDefault(config, "auto", "cipher", "security")
	case "trojan":
		password, err := requiredConfigString(endpoint, config, "password")
		if err != nil {
			return nil, err
		}
		outbound["password"] = password
	case "shadowsocks":
		password, err := requiredConfigString(endpoint, config, "password")
		if err != nil {
			return nil, err
		}
		outbound["method"] = configStringDefault(config, "chacha20-ietf-poly1305", "cipher", "method")
		outbound["password"] = password
	case "hysteria2":
		password, err := requiredConfigString(endpoint, config, "password")
		if err != nil {
			return nil, err
		}
		outbound["password"] = password
	default:
		return nil, fmt.Errorf("endpoint %d uses unsupported sing-box protocol %q", endpoint.ID, endpoint.Protocol)
	}
	if tls := singBoxTLS(config, protocol); len(tls) > 0 {
		outbound["tls"] = tls
	}
	if transport := singBoxTransport(config); len(transport) > 0 {
		outbound["transport"] = transport
	}
	return outbound, nil
}

func normalizedZeroClientConfig(endpoint subscriptionTemplateEndpoint) map[string]interface{} {
	config := cloneStringMap(endpoint.Config)
	config["type"] = strings.ToLower(strings.TrimSpace(endpoint.Protocol))
	config["server"] = endpoint.Address
	port := endpoint.PublicPort
	if port <= 0 {
		port = endpoint.Port
	}
	config["port"] = port
	return config
}

func addClashTLS(proxy, config map[string]interface{}, protocol string) {
	tls := configMap(config, "tls")
	reality := configMap(config, "reality")
	if (protocol == "vless" || protocol == "vmess") && (len(tls) > 0 || len(reality) > 0) {
		proxy["tls"] = true
	}
	serverName := firstConfigString(
		configString(config, "sni"),
		configString(tls, "server_name"),
		configString(reality, "server_name"),
	)
	if serverName != "" {
		proxy["servername"] = serverName
		if protocol == "trojan" || protocol == "hysteria2" {
			proxy["sni"] = serverName
		}
	}
	if insecure, ok := firstConfigBool(config, tls, "insecure"); ok {
		proxy["skip-cert-verify"] = insecure
	}
	fingerprint := firstConfigString(
		configString(config, "client_fingerprint"),
		configString(tls, "client_fingerprint"),
		configString(reality, "client_fingerprint"),
	)
	if fingerprint != "" {
		proxy["client-fingerprint"] = fingerprint
	}
	if len(reality) > 0 {
		options := map[string]interface{}{}
		copyConfigValue(options, reality, "public-key", "public_key")
		copyConfigValue(options, reality, "short-id", "short_id")
		if len(options) > 0 {
			proxy["reality-opts"] = options
		}
	}
}

func addClashTransport(proxy, config map[string]interface{}) {
	if ws := configMap(config, "ws"); len(ws) > 0 {
		proxy["network"] = "ws"
		options := map[string]interface{}{}
		copyConfigValue(options, ws, "path", "path")
		copyConfigValue(options, ws, "headers", "headers")
		proxy["ws-opts"] = options
		return
	}
	if grpc := configMap(config, "grpc"); len(grpc) > 0 {
		proxy["network"] = "grpc"
		options := map[string]interface{}{}
		copyConfigValue(options, grpc, "grpc-service-name", "service_name")
		proxy["grpc-opts"] = options
	}
}

func singBoxTLS(config map[string]interface{}, protocol string) map[string]interface{} {
	source := configMap(config, "tls")
	reality := configMap(config, "reality")
	required := protocol == "trojan" || protocol == "hysteria2"
	if !required && len(source) == 0 && len(reality) == 0 {
		return nil
	}
	tls := map[string]interface{}{"enabled": true}
	serverName := firstConfigString(
		configString(config, "sni"),
		configString(source, "server_name"),
		configString(reality, "server_name"),
	)
	if serverName != "" {
		tls["server_name"] = serverName
	}
	if insecure, ok := firstConfigBool(config, source, "insecure"); ok {
		tls["insecure"] = insecure
	}
	fingerprint := firstConfigString(
		configString(config, "client_fingerprint"),
		configString(source, "client_fingerprint"),
		configString(reality, "client_fingerprint"),
	)
	if fingerprint == "" && len(reality) > 0 {
		// sing-box requires uTLS for a Reality client. "chrome" matches the
		// established client default when the endpoint did not specify one.
		fingerprint = "chrome"
	}
	if fingerprint != "" {
		tls["utls"] = map[string]interface{}{"enabled": true, "fingerprint": fingerprint}
	}
	if len(reality) > 0 {
		options := map[string]interface{}{"enabled": true}
		copyConfigValue(options, reality, "public_key", "public_key")
		copyConfigValue(options, reality, "short_id", "short_id")
		tls["reality"] = options
	}
	return tls
}

func singBoxTransport(config map[string]interface{}) map[string]interface{} {
	if ws := configMap(config, "ws"); len(ws) > 0 {
		transport := map[string]interface{}{"type": "ws"}
		copyConfigValue(transport, ws, "path", "path")
		copyConfigValue(transport, ws, "headers", "headers")
		return transport
	}
	if grpc := configMap(config, "grpc"); len(grpc) > 0 {
		transport := map[string]interface{}{"type": "grpc"}
		copyConfigValue(transport, grpc, "service_name", "service_name")
		return transport
	}
	return nil
}

func requiredConfigString(endpoint subscriptionTemplateEndpoint, config map[string]interface{}, keys ...string) (string, error) {
	for _, key := range keys {
		if value := configString(config, key); value != "" {
			return value, nil
		}
	}
	return "", fmt.Errorf("endpoint %d %s client config is missing %s", endpoint.ID, endpoint.Protocol, strings.Join(keys, " or "))
}

func subscriptionEndpointTag(endpoint subscriptionTemplateEndpoint) string {
	name := strings.TrimSpace(endpoint.Name)
	if name == "" {
		name = strings.ToUpper(strings.TrimSpace(endpoint.Protocol))
	}
	return name
}

func cloneStringMap(source map[string]interface{}) map[string]interface{} {
	cloned := make(map[string]interface{}, len(source)+3)
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

func configMap(source map[string]interface{}, key string) map[string]interface{} {
	value, _ := source[key].(map[string]interface{})
	return value
}

func configString(source map[string]interface{}, key string) string {
	value, _ := source[key].(string)
	return strings.TrimSpace(value)
}

func configStringDefault(source map[string]interface{}, fallback string, keys ...string) string {
	for _, key := range keys {
		if value := configString(source, key); value != "" {
			return value
		}
	}
	return fallback
}

func firstConfigString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func firstConfigBool(primary, secondary map[string]interface{}, key string) (bool, bool) {
	if value, ok := primary[key].(bool); ok {
		return value, true
	}
	value, ok := secondary[key].(bool)
	return value, ok
}

func copyConfigValue(target, source map[string]interface{}, targetKey, sourceKey string) {
	if value, ok := source[sourceKey]; ok && value != nil {
		target[targetKey] = value
	}
}

func copySanitizedMap(target, source map[string]interface{}, key string, allowed []string) {
	sourceMap := configMap(source, key)
	if len(sourceMap) == 0 {
		return
	}
	sanitized := make(map[string]interface{}, len(allowed))
	for _, field := range allowed {
		copyConfigValue(sanitized, sourceMap, field, field)
	}
	if len(sanitized) > 0 {
		target[key] = sanitized
	}
}

func copyZeroGrpc(target, source map[string]interface{}) {
	grpc := configMap(source, "grpc")
	if len(grpc) == 0 {
		return
	}
	if value, ok := grpc["service_names"]; ok {
		target["grpc"] = map[string]interface{}{"service_names": value}
		return
	}
	if value, ok := grpc["service_name"]; ok {
		target["grpc"] = map[string]interface{}{"service_names": value}
	}
}

func copyZeroWebSocket(target, source map[string]interface{}) {
	ws := configMap(source, "ws")
	if len(ws) == 0 {
		return
	}
	sanitized := map[string]interface{}{}
	copyConfigValue(sanitized, ws, "path", "path")
	if headers := configMap(ws, "headers"); len(headers) > 0 {
		cleanHeaders := make(map[string]interface{}, len(headers))
		for key, value := range headers {
			switch strings.ToLower(strings.TrimSpace(key)) {
			case "host", "connection", "upgrade", "sec-websocket-key", "sec-websocket-version",
				"sec-websocket-protocol", "sec-websocket-extensions", "sec-websocket-accept":
				continue
			default:
				cleanHeaders[key] = value
			}
		}
		if len(cleanHeaders) > 0 {
			sanitized["headers"] = cleanHeaders
		}
	}
	if len(sanitized) > 0 {
		target["ws"] = sanitized
	}
}

func marshalIndentedJSON(value interface{}) (string, error) {
	rendered, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", err
	}
	return string(rendered) + "\n", nil
}
