package handler

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/model"
)

type networkEntryPath struct {
	Outbounds []map[string]interface{} `json:"outbounds"`
	Groups    []map[string]interface{} `json:"outbound_groups"`
	Target    string                   `json:"target"`
}

func networkEntryTag(id uint) string { return fmt.Sprintf("entry-%d", id) }

// Prefix only graph references, never protocol-private strings (including credentials).
func (path networkEntryPath) appendTo(config map[string]interface{}, entry model.NetworkEntry) error {
	if entry.Network != "tcp" {
		if err := path.validateDatagramPath(); err != nil {
			return err
		}
	}
	target, err := path.appendGraph(config, networkEntryTag(entry.ID)+"/")
	if err != nil {
		return err
	}
	appendEntryRoute(config, entry.ID, map[string]interface{}{"type": "route", "outbound": target})
	return nil
}

func appendEntryRoute(config map[string]interface{}, entryID uint, action map[string]interface{}) {
	route := config["route"].(map[string]interface{})
	rules, _ := route["rules"].([]interface{})
	route["rules"] = append(rules, map[string]interface{}{"condition": map[string]interface{}{"type": "inbound", "values": []string{networkEntryTag(entryID)}}, "action": action})
}

func (path networkEntryPath) appendGraph(config map[string]interface{}, prefix string) (string, error) {
	names := map[string]string{}
	for _, list := range [][]map[string]interface{}{path.Outbounds, path.Groups} {
		for _, item := range list {
			tag, _ := item["tag"].(string)
			if tag == "" || names[tag] != "" {
				return "", fmt.Errorf("代理路径的 tag 必须非空且唯一")
			}
			names[tag] = prefix + tag
		}
	}
	if names[path.Target] == "" {
		return "", fmt.Errorf("请选择代理路径中存在的 target")
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
			return "", fmt.Errorf("代理组必须包含 outbounds")
		}
		for i, value := range members {
			name, _ := value.(string)
			if names[name] == "" {
				return "", fmt.Errorf("代理组引用了不存在的出口")
			}
			members[i] = names[name]
		}
		for _, key := range []string{"selected", "default"} {
			if value, exists := item[key]; exists {
				name, _ := value.(string)
				if names[name] == "" {
					return "", fmt.Errorf("代理组选择了不存在的出口")
				}
				item[key] = names[name]
			}
		}

		groups = append(groups, item)
	}
	config["outbound_groups"] = groups
	return names[path.Target], nil
}

// Called after B's access filtering. Retaining B's identity also preserves group order and billing.
func preserveNetworkEntryPeerIdentity(config map[string]interface{}, address, protocol string, port int) {
	authority := address
	if port > 0 {
		authority = net.JoinHostPort(address, strconv.Itoa(port))
	}
	// HTTP transport authority must continue to name B even when the dial target is A.
	if ws, ok := config["ws"].(map[string]interface{}); ok {
		headers, _ := ws["headers"].(map[string]interface{})
		if headers == nil {
			headers = map[string]interface{}{}
		}
		hasHost := false
		for key := range headers {
			hasHost = hasHost || strings.EqualFold(key, "Host")
		}
		if !hasHost {
			headers["Host"] = authority
		}
		ws["headers"] = headers
	}
	for _, key := range []string{"h2"} {
		if transport, ok := config[key].(map[string]interface{}); ok {
			if value, _ := transport["host"].(string); value == "" {
				transport["host"] = authority
			}
		}
	}
	peer := firstConfigString(configString(config, "sni"), configString(configMap(config, "tls"), "server_name"), configString(configMap(config, "reality"), "server_name"), configString(configMap(config, "quic"), "server_name"), address)
	for _, key := range []string{"tls", "reality", "quic"} {
		if section, ok := config[key].(map[string]interface{}); ok {
			if value, _ := section["server_name"].(string); value == "" {
				section["server_name"] = peer
			}
		}
	}
	if protocol == "trojan" || protocol == "hysteria2" {
		if value, _ := config["sni"].(string); value == "" {
			config["sni"] = peer
		}
	}
}
