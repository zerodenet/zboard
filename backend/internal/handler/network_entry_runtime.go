package handler

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

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
	prefix := networkEntryTag(entry.ID) + "/"
	names := map[string]string{}
	for _, list := range [][]map[string]interface{}{path.Outbounds, path.Groups} {
		for _, item := range list {
			tag, _ := item["tag"].(string)
			if tag == "" || names[tag] != "" {
				return fmt.Errorf("代理路径的 tag 必须非空且唯一")
			}
			names[tag] = prefix + tag
		}
	}
	if names[path.Target] == "" {
		return fmt.Errorf("请选择代理路径中存在的 target")
	}
	outbounds, _ := config["outbounds"].([]interface{})
	for _, item := range path.Outbounds {
		item["tag"] = names[item["tag"].(string)]
		outbounds = append(outbounds, item)
	}
	config["outbounds"] = outbounds
	groups, _ := config["outbound_groups"].([]interface{})
	for _, item := range path.Groups {
		item["tag"] = names[item["tag"].(string)]
		memberKey := "outbounds"
		if item["type"] == "relay" {
			memberKey = "proxies"
		}
		members, ok := item[memberKey].([]interface{})
		if !ok || len(members) == 0 {
			return fmt.Errorf("代理组必须包含 outbounds")
		}
		for i, value := range members {
			name, _ := value.(string)
			if names[name] == "" {
				return fmt.Errorf("代理组引用了不存在的出口")
			}
			members[i] = names[name]
		}
		for _, key := range []string{"selected", "default"} {
			if value, exists := item[key]; exists {
				name, _ := value.(string)
				if names[name] == "" {
					return fmt.Errorf("代理组选择了不存在的出口")
				}
				item[key] = names[name]
			}
		}

		groups = append(groups, item)
	}
	config["outbound_groups"] = groups
	route := config["route"].(map[string]interface{})
	rules, _ := route["rules"].([]interface{})
	route["rules"] = append(rules, map[string]interface{}{"condition": map[string]interface{}{"type": "inbound", "values": []string{networkEntryTag(entry.ID)}}, "action": map[string]interface{}{"type": "route", "outbound": names[path.Target]}})
	return nil
}

func (h *handlers) appendNetworkEntryRuntime(config map[string]interface{}, nodeID uint) error {
	var entries []model.NetworkEntry
	if err := h.db.Where("node_id = ? AND enabled = ?", nodeID, true).Order("id").Find(&entries).Error; err != nil {
		return err
	}
	inbounds := config["inbounds"].([]map[string]interface{})
	ports := map[int]bool{}
	for _, inbound := range inbounds {
		listen, _ := inbound["listen"].(map[string]interface{})
		port, _ := listen["port"].(int)
		ports[port] = true
	}
	for _, entry := range entries {
		var endpoint model.ProtocolEndpoint
		if err := h.db.First(&endpoint, entry.EndpointID).Error; err != nil {
			return fmt.Errorf("入口 %d 的落地协议不存在", entry.ID)
		}
		var landing model.Node
		if err := h.db.First(&landing, endpoint.NodeID).Error; err != nil {
			return err
		}
		if !endpoint.IsActive || !landing.IsEnabled || landing.LifecycleStatus == resourceStatusDeleting {
			continue
		}
		if ports[entry.Port] {
			return fmt.Errorf("入口 %d 的监听端口 %d 已占用", entry.ID, entry.Port)
		}
		ports[entry.Port] = true
		port := endpoint.PublicPort
		if port == 0 {
			port = endpoint.Port
		}
		if entry.Network == "tcp" && endpoint.Protocol == "hysteria2" {
			return fmt.Errorf("入口 %d 的 Hysteria2 落地需要 TCP/UDP 转发", entry.ID)
		}
		protocol := map[string]interface{}{"type": "direct", "target": endpoint.Address, "port": port}
		inbounds = append(inbounds, map[string]interface{}{"tag": networkEntryTag(entry.ID), "listen": map[string]interface{}{"address": "0.0.0.0", "port": entry.Port}, "udp": map[string]interface{}{"enabled": entry.Network != "tcp"}, "protocol": protocol})
		if entry.PathConfig != "" {
			raw, err := h.credentialCipher.Decrypt(entry.PathConfig)
			if err != nil {
				return fmt.Errorf("解密入口 %d 代理路径失败", entry.ID)
			}
			var path networkEntryPath
			if err := json.Unmarshal([]byte(raw), &path); err != nil {
				return fmt.Errorf("入口 %d 代理路径无效", entry.ID)
			}
			if err := path.appendTo(config, entry); err != nil {
				return fmt.Errorf("入口 %d: %w", entry.ID, err)
			}
		}
	}
	config["inbounds"] = inbounds
	return nil
}

// Called after B's access filtering. Retaining B's identity also preserves group order and billing.
func (h *handlers) projectNetworkEntries(nodes []subscriptionManifestNode, now time.Time) ([]subscriptionManifestNode, error) {
	if len(nodes) == 0 {
		return nodes, nil
	}
	ids := make([]uint, 0, len(nodes))
	for _, node := range nodes {
		ids = append(ids, node.ID)
	}
	var entries []model.NetworkEntry
	if err := h.db.Where("endpoint_id IN ? AND enabled = ?", ids, true).Order("id").Find(&entries).Error; err != nil {
		return nil, err
	}
	result := make([]subscriptionManifestNode, 0, len(nodes)+len(entries))
	for _, base := range nodes {
		fronts := []subscriptionManifestNode{}
		for _, entry := range entries {
			if entry.EndpointID != base.ID {
				continue
			}
			var node model.Node
			if err := h.db.First(&node, entry.NodeID).Error; err != nil {
				return nil, err
			}
			if !node.IsEnabled || node.LifecycleStatus == resourceStatusDeleting || node.LastSeenAt == nil || node.LastSeenAt.Before(now.Add(-nodeOnlineWindow)) {
				continue
			}
			var pending int64
			if err := h.db.Model(&model.NodeConfigPublish{}).Where("node_id = ?", entry.NodeID).Count(&pending).Error; err != nil {
				return nil, err
			}
			if pending > 0 {
				continue
			}
			var config map[string]interface{}
			if err := json.Unmarshal(base.Config, &config); err != nil {
				return nil, err
			}
			// Keep implicit TLS peer identity pinned to B before replacing the transport destination.
			port := base.PublicPort
			if port <= 0 {
				port = base.Port
			}
			preserveNetworkEntryPeerIdentity(config, base.Address, base.Protocol, port)
			config["server"] = entry.Address
			config["port"] = entry.PublicPort
			raw, err := json.Marshal(config)
			if err != nil {
				return nil, err
			}
			front := base
			front.NetworkEntryID = entry.ID
			front.NetworkEntryNetwork = entry.Network
			front.Name = entry.Name + " · " + base.Name
			front.Address = entry.Address
			front.Port = entry.Port
			front.PublicPort = entry.PublicPort
			front.Config = raw
			fronts = append(fronts, front)
		}
		result = append(result, base)
		result = append(result, fronts...)
	}
	return result, nil
}

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
