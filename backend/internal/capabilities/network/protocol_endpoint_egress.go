package network

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

var protocolEndpointEgressTypes = map[string]bool{
	"socks5": true, "vless": true, "vmess": true, "trojan": true,
	"shadowsocks": true, "hysteria2": true, "mieru": true,
}

// NormalizeProtocolEndpointEgressConfig validates and canonicalizes one Zero
// outbound protocol. An empty value (or direct) disables endpoint-local egress.
func NormalizeProtocolEndpointEgressConfig(raw string) (string, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "{}" {
		return "", "", nil
	}
	var protocol map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &protocol); err != nil || protocol == nil {
		return "", "", protocolEndpointEgressValidation("出口配置必须是 JSON 对象。")
	}
	kind, _ := protocol["type"].(string)
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "direct" {
		return "", "", nil
	}
	if !protocolEndpointEgressTypes[kind] {
		return "", "", protocolEndpointEgressValidation("请选择 Zero 内核支持的出口协议。")
	}
	server, _ := protocol["server"].(string)
	server = strings.TrimSpace(server)
	if server == "" || len(server) > 255 {
		return "", "", protocolEndpointEgressValidation("出口地址不能为空且不能超过 255 字节。")
	}
	port, ok := protocolEndpointEgressPort(protocol["port"])
	if !ok {
		return "", "", protocolEndpointEgressValidation("出口端口必须是 1–65535 之间的整数。")
	}
	require := func(field, message string) error {
		value, _ := protocol[field].(string)
		if strings.TrimSpace(value) == "" {
			return protocolEndpointEgressValidation(message)
		}
		return nil
	}
	switch kind {
	case "vless", "vmess":
		if err := require("id", "该出口协议缺少用户 ID。"); err != nil {
			return "", "", err
		}
	case "trojan", "hysteria2", "mieru":
		if err := require("password", "该出口协议缺少密码。"); err != nil {
			return "", "", err
		}
	case "shadowsocks":
		if err := require("password", "Shadowsocks 出口缺少密码。"); err != nil {
			return "", "", err
		}
		if err := require("cipher", "Shadowsocks 出口缺少加密方式。"); err != nil {
			return "", "", err
		}
	}
	protocol["type"], protocol["server"], protocol["port"] = kind, server, port
	canonical, err := json.Marshal(protocol)
	if err != nil {
		return "", "", fmt.Errorf("encode endpoint egress: %w", err)
	}
	return kind, string(canonical), nil
}

func protocolEndpointEgressPort(value interface{}) (int, bool) {
	var number float64
	switch typed := value.(type) {
	case float64:
		number = typed
	case json.Number:
		parsed, err := typed.Float64()
		if err != nil {
			return 0, false
		}
		number = parsed
	case int:
		number = float64(typed)
	default:
		return 0, false
	}
	if number < 1 || number > 65535 || math.Trunc(number) != number {
		return 0, false
	}
	return int(number), true
}

func protocolEndpointEgressValidation(message string) error {
	return &ProtocolEndpointMutationValidation{
		Message: "协议出口校验失败。",
		Fields:  map[string]string{"egress_config": message},
	}
}
