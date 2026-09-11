package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Only outbound connectivity is imported; DNS, listeners and routing remain node-owned.
func proxyPoolPathFromSingBox(content []byte) (networkEntryPath, int, error) {
	var doc struct {
		Outbounds []map[string]interface{} `json:"outbounds"`
	}
	if json.Unmarshal(content, &doc) != nil || len(doc.Outbounds) == 0 {
		return networkEntryPath{}, 0, errors.New("sing-box 订阅缺少 outbounds")
	}
	path := networkEntryPath{}
	members := []interface{}{}
	known := map[string]bool{}
	for i, src := range doc.Outbounds {
		tag := clashString(src, "tag")
		if tag == "" {
			tag = fmt.Sprintf("node-%d", i+1)
		}
		if known[tag] {
			return networkEntryPath{}, 0, errors.New("订阅节点或分组标识重复")
		}
		known[tag] = true
		kind := clashString(src, "type")
		if kind == "selector" || kind == "urltest" {
			group := map[string]interface{}{"tag": tag, "type": "selector", "outbounds": src["outbounds"]}
			if kind == "urltest" {
				group["type"] = "url_test"
				copyConfigValue(group, src, "url", "url")
				copyConfigValue(group, src, "tolerance_ms", "tolerance")
				if interval := clashString(src, "interval"); interval != "" {
					duration, err := time.ParseDuration(interval)
					if err != nil || duration < time.Second || duration%time.Second != 0 {
						return networkEntryPath{}, 0, errors.New("sing-box 测速间隔必须是正整数秒")
					}
					group["interval_seconds"] = int64(duration / time.Second)
				}
			} else {
				copyConfigValue(group, src, "default", "default")
			}
			path.Groups = append(path.Groups, group)
			continue
		}
		if kind == "direct" || kind == "block" {
			path.Outbounds = append(path.Outbounds, map[string]interface{}{"tag": tag, "protocol": map[string]interface{}{"type": kind}})
			continue
		}
		protocol, err := singBoxPoolProtocol(src)
		if err != nil {
			return networkEntryPath{}, 0, fmt.Errorf("第 %d 个 sing-box 节点：%w", i+1, err)
		}
		outbound := map[string]interface{}{"tag": tag, "protocol": protocol}
		if clashString(src, "network") == "tcp" {
			outbound["udp"] = map[string]interface{}{"enabled": false}
		} else if network := clashString(src, "network"); network != "" {
			return networkEntryPath{}, 0, errors.New("暂不支持仅 UDP 的订阅节点")
		}
		path.Outbounds = append(path.Outbounds, outbound)
		members = append(members, tag)
	}
	if len(members) == 0 {
		return networkEntryPath{}, 0, errors.New("订阅中没有可用代理节点")
	}
	if len(path.Groups) > 0 {
		path.Target, _ = path.Groups[0]["tag"].(string)
	} else {
		tag := "subscription"
		for known[tag] {
			tag += "_"
		}
		path.Groups = []map[string]interface{}{{"tag": tag, "type": "url_test", "outbounds": members, "interval_seconds": 300, "tolerance_ms": 50}}
		path.Target = tag
	}
	return path, len(members), nil
}

func singBoxPoolProtocol(src map[string]interface{}) (map[string]interface{}, error) {
	kind := clashString(src, "type")
	// Reject options that would change the wire protocol or outbound chain if discarded.
	for _, key := range []string{"detour", "plugin", "plugin_opts", "obfs", "server_ports", "up_mbps", "down_mbps", "udp_over_tcp", "multiplex", "packet_encoding"} {
		if value, ok := src[key]; ok && value != nil && value != "" {
			return nil, fmt.Errorf("暂不支持字段 %s", key)
		}
	}
	server, port := clashString(src, "server"), clashInt(src, "server_port")
	if number, ok := src["server_port"].(float64); ok && number != float64(port) {
		return nil, errors.New("server_port 必须是整数")
	}
	if server == "" || port < 1 || port > 65535 {
		return nil, errors.New("缺少 server 或有效 server_port")
	}
	p := map[string]interface{}{"type": kind, "server": server, "port": port}
	switch kind {
	case "shadowsocks":
		copyConfigValue(p, src, "cipher", "method")
		copyConfigValue(p, src, "password", "password")
	case "vless", "vmess":
		copyConfigValue(p, src, "id", "uuid")
		if kind == "vless" {
			copyConfigValue(p, src, "flow", "flow")
		} else {
			if clashInt(src, "alter_id") != 0 {
				return nil, errors.New("不支持 VMess alter_id")
			}
			copyConfigValue(p, src, "cipher", "security")
		}
	case "trojan", "hysteria2":
		copyConfigValue(p, src, "password", "password")
	case "socks":
		p["type"] = "socks5"
		if version := clashString(src, "version"); version != "" && version != "5" {
			return nil, errors.New("仅支持 SOCKS5")
		}
		copyConfigValue(p, src, "username", "username")
		copyConfigValue(p, src, "password", "password")
	default:
		return nil, fmt.Errorf("不支持代理类型 %s", kind)
	}
	if err := singBoxPoolTLS(p, src); err != nil {
		return nil, err
	}
	if tr, ok := src["transport"].(map[string]interface{}); ok && len(tr) > 0 {
		if kind != "vless" && kind != "vmess" {
			return nil, errors.New("该协议暂不支持附加传输")
		}
		switch clashString(tr, "type") {
		case "ws":
			for _, k := range []string{"max_early_data", "early_data_header_name"} {
				if v, ok := tr[k]; ok && v != nil && v != "" && v != float64(0) {
					return nil, errors.New("暂不支持 WebSocket early data")
				}
			}
			ws := map[string]interface{}{"path": "/"}
			copyConfigValue(ws, tr, "path", "path")
			if headers, ok := tr["headers"].(map[string]interface{}); ok {
				for name := range headers {
					if strings.EqualFold(name, "Host") {
						return nil, errors.New("Zero 暂不支持覆盖 WebSocket Host；请使用无需覆盖 Host 的订阅节点")
					}
				}
			}
			copyConfigValue(ws, tr, "headers", "headers")
			p["ws"] = ws
		case "grpc":
			p["grpc"] = map[string]interface{}{"service_names": []string{clashString(tr, "service_name")}}
		default:
			return nil, errors.New("目前仅支持 TCP、WebSocket 和 gRPC 传输")
		}
	}
	return p, nil
}

func singBoxPoolTLS(p, src map[string]interface{}) error {
	tls, ok := src["tls"].(map[string]interface{})
	if !ok {
		return nil
	}
	enabled, _ := tls["enabled"].(bool)
	if !enabled {
		if p["type"] == "trojan" || p["type"] == "hysteria2" {
			return errors.New("该协议需要启用 TLS")
		}
		return nil
	}
	for _, key := range []string{"certificate", "certificate_path", "certificate_public_key_sha256", "ech", "fragment", "record_fragment", "min_version", "max_version", "cipher_suites"} {
		if _, ok := tls[key]; ok {
			return fmt.Errorf("暂不支持 TLS 字段 %s", key)
		}
	}
	kind := p["type"]
	if kind != "vless" && kind != "vmess" && kind != "trojan" && kind != "hysteria2" {
		return errors.New("该协议暂不支持 TLS")
	}
	target := map[string]interface{}{}
	for _, key := range []string{"server_name", "insecure", "alpn", "disable_sni"} {
		copyConfigValue(target, tls, key, key)
	}
	if utls, ok := tls["utls"].(map[string]interface{}); ok && utls["enabled"] == true {
		copyConfigValue(target, utls, "client_fingerprint", "fingerprint")
	}
	if reality, ok := tls["reality"].(map[string]interface{}); ok && reality["enabled"] == true {
		if kind != "vless" {
			return errors.New("仅 VLESS 支持 Reality")
		}
		delete(target, "insecure")
		delete(target, "alpn")
		delete(target, "disable_sni")
		copyConfigValue(target, reality, "public_key", "public_key")
		copyConfigValue(target, reality, "short_id", "short_id")
		p["reality"] = target
		return nil
	}
	if kind == "trojan" || kind == "hysteria2" {
		if _, ok := target["alpn"]; ok {
			return errors.New("该协议暂不支持自定义 ALPN")
		}
		if target["disable_sni"] == true {
			return errors.New("该协议暂不支持禁用 SNI")
		}
		key := "sni"
		if kind == "hysteria2" {
			key = "server_name"
		}
		copyConfigValue(p, target, key, "server_name")
		copyConfigValue(p, target, "insecure", "insecure")
		copyConfigValue(p, target, "client_fingerprint", "client_fingerprint")
	} else {
		p["tls"] = target
	}
	return nil
}
