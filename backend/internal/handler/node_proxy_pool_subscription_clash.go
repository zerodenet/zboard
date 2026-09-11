package handler

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"gopkg.in/yaml.v2"
)

type clashProxyPoolDocument struct {
	Proxies     []map[string]interface{} `yaml:"proxies"`
	ProxyGroups []map[string]interface{} `yaml:"proxy-groups"`
}

func proxyPoolPathFromClash(content []byte) (networkEntryPath, int, error) {
	var document clashProxyPoolDocument
	if err := yaml.Unmarshal(content, &document); err != nil || len(document.Proxies) == 0 {
		return networkEntryPath{}, 0, errors.New("Clash 订阅必须包含 proxies")
	}
	path := networkEntryPath{}
	known := map[string]bool{"direct": true, "block": true}
	path.Outbounds = append(path.Outbounds,
		map[string]interface{}{"tag": "direct", "protocol": map[string]interface{}{"type": "direct"}},
		map[string]interface{}{"tag": "block", "protocol": map[string]interface{}{"type": "block"}},
	)
	for _, source := range document.Proxies {
		outbound, err := clashProxyPoolOutbound(source)
		if err != nil {
			return networkEntryPath{}, 0, err
		}
		tag, _ := outbound["tag"].(string)
		if known[tag] {
			return networkEntryPath{}, 0, fmt.Errorf("订阅代理标识重复：%s", tag)
		}
		known[tag] = true
		path.Outbounds = append(path.Outbounds, outbound)
	}
	for _, source := range document.ProxyGroups {
		if name := clashString(source, "name"); name != "" {
			known[name] = true
		}
	}
	for _, source := range document.ProxyGroups {
		group, ok := clashProxyPoolGroup(source, known)
		if ok {
			path.Groups = append(path.Groups, group)
		}
	}
	if len(path.Groups) > 0 {
		path.Target, _ = path.Groups[0]["tag"].(string)
	} else {
		members := make([]interface{}, 0, len(path.Outbounds)-2)
		for _, outbound := range path.Outbounds[2:] {
			members = append(members, outbound["tag"])
		}
		path.Groups = []map[string]interface{}{{"tag": "subscription", "type": "url_test", "outbounds": members, "interval_seconds": 300, "tolerance_ms": 50}}
		path.Target = "subscription"
	}
	return path, len(path.Outbounds) - 2, nil
}

func clashProxyPoolOutbound(source map[string]interface{}) (map[string]interface{}, error) {
	tag, kind := clashString(source, "name"), strings.ToLower(clashString(source, "type"))
	server, port := clashString(source, "server"), clashInt(source, "port")
	if tag == "" || kind == "" || server == "" || port < 1 || port > 65535 {
		return nil, errors.New("Clash 代理需要 name、type、server 和有效 port")
	}
	protocol := map[string]interface{}{"server": server, "port": port}
	copyString := func(target, sourceKey string) {
		if value := clashString(source, sourceKey); value != "" {
			protocol[target] = value
		}
	}
	switch kind {
	case "ss", "shadowsocks":
		protocol["type"] = "shadowsocks"
		copyString("cipher", "cipher")
		copyString("password", "password")
	case "vmess", "vless":
		protocol["type"] = kind
		copyString("id", "uuid")
		if protocol["id"] == nil {
			copyString("id", "id")
		}
		if kind == "vmess" {
			copyString("cipher", "cipher")
		}
		if flow := clashString(source, "flow"); kind == "vless" && flow != "" {
			protocol["flow"] = flow
		}
		applyClashSecurityAndTransport(protocol, source, kind)
	case "trojan":
		protocol["type"] = kind
		copyString("password", "password")
		copyString("sni", "sni")
		if insecure, ok := clashBool(source, "skip-cert-verify"); ok {
			protocol["insecure"] = insecure
		}
		if fingerprint := firstConfigString(clashString(source, "client-fingerprint"), clashString(source, "fingerprint")); fingerprint != "" {
			protocol["client_fingerprint"] = fingerprint
		}
	case "hysteria2", "hy2":
		protocol["type"] = "hysteria2"
		copyString("password", "password")
		if insecure, ok := clashBool(source, "skip-cert-verify"); ok {
			protocol["insecure"] = insecure
		}
	case "socks", "socks5":
		protocol["type"] = "socks5"
		copyString("username", "username")
		copyString("password", "password")
	case "http":
		protocol["type"] = "http"
		copyString("username", "username")
		copyString("password", "password")
	default:
		return nil, fmt.Errorf("Clash 订阅包含不支持的代理类型：%s", kind)
	}
	return map[string]interface{}{"tag": tag, "protocol": protocol}, nil
}

func applyClashSecurityAndTransport(protocol, source map[string]interface{}, kind string) {
	if tlsEnabled, _ := clashBool(source, "tls"); tlsEnabled {
		tls := map[string]interface{}{}
		if sni := firstConfigString(clashString(source, "servername"), clashString(source, "sni")); sni != "" {
			tls["server_name"] = sni
		}
		if insecure, ok := clashBool(source, "skip-cert-verify"); ok {
			tls["insecure"] = insecure
		}
		if fingerprint := firstConfigString(clashString(source, "client-fingerprint"), clashString(source, "fingerprint")); fingerprint != "" {
			tls["client_fingerprint"] = fingerprint
		}
		protocol["tls"] = tls
	}
	if network := strings.ToLower(clashString(source, "network")); network == "ws" {
		ws := map[string]interface{}{"path": "/"}
		if options, ok := source["ws-opts"].(map[interface{}]interface{}); ok {
			if value, ok := options["path"].(string); ok && value != "" {
				ws["path"] = value
			}
			if headers, ok := options["headers"].(map[interface{}]interface{}); ok {
				normalized := map[string]interface{}{}
				for key, value := range headers {
					if name, ok := key.(string); ok {
						normalized[name] = value
					}
				}
				ws["headers"] = normalized
			}
		}
		protocol["ws"] = ws
	}
	if network := strings.ToLower(clashString(source, "network")); network == "grpc" {
		if options, ok := source["grpc-opts"].(map[interface{}]interface{}); ok {
			for _, key := range []string{"grpc-service-name", "service-name", "serviceName"} {
				if service, ok := options[key].(string); ok && strings.TrimSpace(service) != "" {
					protocol["grpc"] = map[string]interface{}{"service_names": []string{strings.TrimSpace(service)}}
					break
				}
			}
		}
	}
	if kind == "vless" {
		if options, ok := source["reality-opts"].(map[interface{}]interface{}); ok {
			reality := map[string]interface{}{}
			for sourceKey, targetKey := range map[string]string{"public-key": "public_key", "short-id": "short_id"} {
				if value, ok := options[sourceKey].(string); ok && value != "" {
					reality[targetKey] = value
				}
			}
			if sni := firstConfigString(clashString(source, "servername"), clashString(source, "sni")); sni != "" {
				reality["server_name"] = sni
			}
			if len(reality) > 0 {
				delete(protocol, "tls")
				protocol["reality"] = reality
			}
		}
	}
}

func clashProxyPoolGroup(source map[string]interface{}, known map[string]bool) (map[string]interface{}, bool) {
	tag, kind := clashString(source, "name"), strings.ToLower(clashString(source, "type"))
	mapped := ""
	switch kind {
	case "select", "selector":
		mapped = "selector"
	case "url-test", "url_test":
		mapped = "url_test"
	case "relay":
		mapped = "relay"
	default:
		return nil, false
	}
	rawMembers, _ := source["proxies"].([]interface{})
	members := make([]interface{}, 0, len(rawMembers))
	for _, raw := range rawMembers {
		member, _ := raw.(string)
		switch strings.ToUpper(member) {
		case "DIRECT":
			member = "direct"
		case "REJECT", "REJECT-DROP":
			member = "block"
		}
		if known[member] {
			members = append(members, member)
		}
	}
	if tag == "" || len(members) == 0 {
		return nil, false
	}
	key := "outbounds"
	if mapped == "relay" {
		key = "proxies"
	}
	group := map[string]interface{}{"tag": tag, "type": mapped, key: members}
	if mapped == "url_test" {
		applySubscriptionProbeURL(group, clashString(source, "url"))
		if value := clashInt(source, "interval"); value > 0 {
			group["interval_seconds"] = value
		}
	}
	return group, true
}

func clashString(source map[string]interface{}, key string) string {
	value, _ := source[key].(string)
	return strings.TrimSpace(value)
}

func clashInt(source map[string]interface{}, key string) int {
	switch value := source[key].(type) {
	case int:
		return value
	case float64:
		return int(value)
	case string:
		result, _ := strconv.Atoi(value)
		return result
	default:
		return 0
	}
}

func clashBool(source map[string]interface{}, key string) (bool, bool) {
	value, ok := source[key].(bool)
	return value, ok
}

// Zero currently probes plain HTTP only. Use its default probe for HTTPS
// subscriptions; never downgrade a publisher URL that may contain credentials.
func applySubscriptionProbeURL(group map[string]interface{}, raw string) {
	if raw == "" {
		return
	}
	parsed, err := url.Parse(raw)
	if err == nil && strings.EqualFold(parsed.Scheme, "https") && parsed.Hostname() != "" {
		return
	}
	group["url"] = raw
}
